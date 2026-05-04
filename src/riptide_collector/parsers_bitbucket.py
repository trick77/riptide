"""Pure extraction of Bitbucket DC webhook payloads into a typed draft.

Routers do HTTP + auth + persistence; this module owns the shape-coercion
and field-extraction logic. All functions here are pure and trivially
unit-testable without HTTP, DB, or config dependencies.
"""

from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any

from riptide_collector.parsers import (
    extract_jira_keys,
    is_revert_commit,
    lower,
    parse_change_type,
)


@dataclass(frozen=True)
class BitbucketEventDraft:
    """Result of parsing a Bitbucket DC webhook body — ready for insert.

    Identifier fields (`repo_full_name`, `commit_sha`, `branch_name`) are
    already lowercased for join-stability. The original casing survives
    on `payload`, which is the raw request body.

    `automation_source` is intentionally absent — it depends on
    `RiptideConfigStore`, which lives in the router layer.
    """

    delivery_id: str
    event_type: str
    repo_full_name: str | None
    pr_id: int | None
    commit_sha: str | None
    author: str | None
    branch_name: str | None
    change_type: str | None
    jira_keys: list[str]
    is_revert: bool
    occurred_at: datetime
    # Aliases the request body — the dataclass is frozen, but the dict it
    # references is not. Don't mutate it after extraction; the JSONB column
    # writes whatever the dict contains at flush time.
    payload: dict[str, Any]


@dataclass(frozen=True)
class BitbucketSkip:
    """A delivery we accepted but won't persist — e.g. tag-only push."""

    reason: str
    delivery_id: str
    event_type: str
    repo_full_name: str | None


def _as_dict(value: Any) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _as_list(value: Any) -> list[Any]:
    return value if isinstance(value, list) else []


def _parse_dt(value: Any) -> datetime | None:
    """Parse an ISO-8601 string and normalise to UTC.

    Naive datetimes are assumed to be UTC; aware datetimes are converted.
    Returns None for unparseable input.
    """
    if not isinstance(value, str):
        return None
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    return parsed.astimezone(UTC) if parsed.tzinfo else parsed.replace(tzinfo=UTC)


def _extract_repo_full_name(body: dict[str, Any]) -> str | None:
    """Build `<projectKey>/<slug>` from a BBS DC payload.

    Push events carry `repository` at the top level. PR events nest the
    repository inside `pullRequest.toRef.repository`; fall back to that
    if the top-level field is missing.
    """
    repo = _as_dict(body.get("repository"))
    if not repo:
        pr = _as_dict(body.get("pullRequest"))
        repo = _as_dict(_as_dict(pr.get("toRef")).get("repository"))
    project_key = _as_dict(repo.get("project")).get("key")
    slug = repo.get("slug")
    if isinstance(project_key, str) and isinstance(slug, str):
        return f"{project_key}/{slug}"
    return None


def _user_handle(user: dict[str, Any]) -> str | None:
    """Pick the most stable identifier from a BBS DC user dict.

    `name` is the login (immutable); `slug` is the URL-safe form;
    `displayName` is human-readable. Prefer login → slug → display.
    """
    for key in ("name", "slug", "displayName"):
        value = user.get(key)
        if isinstance(value, str) and value:
            return value
    return None


def _synth_delivery_id(event_key: str | None, body: dict[str, Any]) -> str:
    pr = _as_dict(body.get("pullRequest"))
    pr_id = pr.get("id")
    repo = _extract_repo_full_name(body) or "unknown"
    when = body.get("date") or "?"
    # Mix in the first change's toHash so two same-second pushes on the
    # same repo (no PR id, no X-Request-UUID) don't collapse to the same
    # synthetic id and get silently deduped by ON CONFLICT DO NOTHING.
    first_change = _as_dict(next(iter(_as_list(body.get("changes"))), None))
    to_hash = first_change.get("toHash") if isinstance(first_change.get("toHash"), str) else None
    return f"{event_key or 'unknown'}#{repo}#{pr_id}#{to_hash or '-'}#{when}"


def extract_event(
    body: dict[str, Any],
    *,
    x_event_key: str | None,
    x_request_uuid: str | None,
    x_hook_uuid: str | None,
) -> BitbucketEventDraft | BitbucketSkip:
    """Parse a Bitbucket DC webhook body into a typed draft.

    Returns `BitbucketSkip` for deliveries we accept but won't persist
    (currently: push events whose `changes[]` carry no usable branch
    change — tag-only or DELETE-only). Returns `BitbucketEventDraft`
    otherwise.
    """
    delivery_id = x_request_uuid or x_hook_uuid or _synth_delivery_id(x_event_key, body)
    event_type = x_event_key or "unknown"
    raw_repo_full_name = _extract_repo_full_name(body)

    pr = _as_dict(body.get("pullRequest"))
    pr_id: int | None = pr.get("id") if isinstance(pr.get("id"), int) else None
    title = pr.get("title") if isinstance(pr.get("title"), str) else None
    description = pr.get("description") if isinstance(pr.get("description"), str) else None

    branch_name: str | None = None
    commit_sha: str | None = None
    author: str | None = None
    is_revert = False

    if pr:
        from_ref = _as_dict(pr.get("fromRef"))
        display_id = from_ref.get("displayId")
        latest_commit = from_ref.get("latestCommit")
        if isinstance(display_id, str):
            branch_name = display_id
        if isinstance(latest_commit, str):
            commit_sha = latest_commit
        author_user = _as_dict(_as_dict(pr.get("author")).get("user"))
        author = _user_handle(author_user)
        # PR-side revert detection: the title is the only signal we have
        # without a REST round-trip. Push-side detection would need the
        # commit messages between fromHash..toHash.
        if is_revert_commit(title):
            is_revert = True

    # Push: top-level `changes[]` with {ref:{displayId,type}, toHash, type}.
    # DELETE-type changes have no new commit and are skipped. Tag pushes
    # (ref.type == "TAG") are skipped too — `branch_name` and
    # `parse_change_type` would mis-bucket the tag's displayId otherwise.
    raw_changes = _as_list(body.get("changes"))
    had_branch_change = False
    if not (branch_name and commit_sha):
        for change in raw_changes:
            change_dict = _as_dict(change)
            if str(change_dict.get("type", "")).upper() == "DELETE":
                continue
            ref = _as_dict(change_dict.get("ref"))
            if str(ref.get("type", "BRANCH")).upper() != "BRANCH":
                continue
            had_branch_change = True
            ref_display = ref.get("displayId")
            to_hash = change_dict.get("toHash")
            if not branch_name and isinstance(ref_display, str):
                branch_name = ref_display
            if not commit_sha and isinstance(to_hash, str):
                commit_sha = to_hash
            if branch_name and commit_sha:
                break

    # A push event with `changes[]` but no usable branch change (tag-only
    # or DELETE-only) carries no data we'd join on.
    if not pr and raw_changes and not had_branch_change:
        return BitbucketSkip(
            reason="no branch change in push",
            delivery_id=delivery_id,
            event_type=event_type,
            repo_full_name=lower(raw_repo_full_name),
        )

    if not author:
        author = _user_handle(_as_dict(body.get("actor")))

    repo_full_name = lower(raw_repo_full_name)
    branch_name = lower(branch_name)
    commit_sha = lower(commit_sha)

    return BitbucketEventDraft(
        delivery_id=delivery_id,
        event_type=event_type,
        repo_full_name=repo_full_name,
        pr_id=pr_id,
        commit_sha=commit_sha,
        author=author,
        branch_name=branch_name,
        change_type=parse_change_type(branch_name),
        jira_keys=extract_jira_keys(title, description, branch_name),
        is_revert=is_revert,
        occurred_at=_parse_dt(body.get("date")) or datetime.now(UTC),
        payload=body,
    )
