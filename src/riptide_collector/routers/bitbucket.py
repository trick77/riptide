import hashlib
import hmac
import json
from datetime import UTC, datetime
from typing import Any

from fastapi import APIRouter, BackgroundTasks, Header, HTTPException, Path, Request, status
from sqlalchemy import text
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from riptide_collector.bbs_client import BitbucketClient
from riptide_collector.config import RiptideConfigStore
from riptide_collector.logging_config import get_logger
from riptide_collector.models import BitbucketEvent
from riptide_collector.parsers import (
    extract_jira_keys,
    is_revert_commit,
    lower,
    parse_change_type,
)
from riptide_collector.team_keys import TeamKeysStore

PR_MERGED_EVENT_KEY = "pr:merged"
REFS_CHANGED_EVENT_KEY = "repo:refs_changed"

# TODO: re-enable BBS DC outbound callbacks once we're ready to run
# them in production. Currently disabled everywhere — both the
# `pr:merged` diff-stat enrichment and the `repo:refs_changed`
# push-commit enrichment skip their background tasks. The webhook
# still ingests and persists the raw event; the enriched columns
# (`lines_added`, `lines_removed`, `files_changed`, `push_commit_count`,
# `push_author_count`) stay NULL until we flip this back on. Flip to
# False to re-enable; the rest of the wiring (BitbucketClient, token
# bucket, retry, team_keys lookup) is untouched.
_BBS_CALLBACKS_DISABLED = True

logger = get_logger(__name__)


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


def _verify_hmac(secret: str, body: bytes, header: str | None) -> bool:
    """Validate `X-Hub-Signature: sha256=<hex>` against `body`.

    Constant-time compare via `hmac.compare_digest`. Rejects missing /
    malformed headers up front; the digest comparison itself only runs
    on a structurally-valid header so we never call compare_digest on
    user-controlled junk of arbitrary length.
    """
    if not header:
        return False
    prefix, _, hex_sig = header.partition("=")
    if prefix.lower() != "sha256" or not hex_sig:
        return False
    expected = hmac.new(secret.encode("utf-8"), body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(hex_sig.lower(), expected.lower())


def make_router(
    config: RiptideConfigStore,
    session_factory: async_sessionmaker[AsyncSession],
    team_keys: TeamKeysStore,
    bbs_client: BitbucketClient | None = None,
) -> APIRouter:
    router = APIRouter(prefix="/webhooks", tags=["webhooks"])

    @router.post(
        "/bitbucket/{team}",
        status_code=status.HTTP_202_ACCEPTED,
        summary="Bitbucket DC webhook sink (HMAC-authenticated)",
    )
    async def bitbucket_webhook(  # pyright: ignore[reportUnusedFunction]
        request: Request,
        background_tasks: BackgroundTasks,
        team: str = Path(..., min_length=1),
        x_event_key: str | None = Header(default=None),
        x_request_uuid: str | None = Header(default=None),
        x_hook_uuid: str | None = Header(default=None),
        x_hub_signature: str | None = Header(default=None),
    ) -> dict[str, str]:
        # Read raw bytes first — HMAC must be computed over the exact
        # bytes BBS signed, not over a re-serialised JSON.
        raw = await request.body()

        team_keys.maybe_reload()
        secret = team_keys.get_secret(team, "bitbucket")
        # Run HMAC even for unknown teams against a dummy key so the
        # rejection path takes the same wall-time as a wrong-signature
        # path. Without this, "unknown team" returns ~instantly while
        # "known team, bad signature" pays the SHA-256 over the body —
        # an attacker can use the gap to enumerate team names. Cheap.
        verify_secret = secret if secret is not None else "\x00" * 32
        signature_ok = _verify_hmac(verify_secret, raw, x_hub_signature)
        if secret is None or not signature_ok:
            logger.warning(
                "bitbucket_hmac_rejected",
                team=team,
                has_secret=secret is not None,
                has_signature=bool(x_hub_signature),
            )
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Invalid signature.",
            )

        try:
            body = json.loads(raw) if raw else {}
        except json.JSONDecodeError:
            return {"status": "ignored", "reason": "non-json payload"}
        if not isinstance(body, dict):
            return {"status": "ignored", "reason": "non-object payload"}

        config.maybe_reload()

        delivery_id = x_request_uuid or x_hook_uuid or _synth_delivery_id(x_event_key, body)
        event_type = x_event_key or "unknown"

        repo_full_name = _extract_repo_full_name(body)

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
            # PR-side revert detection: the title is the only signal we
            # have without a REST round-trip. Push-side detection would
            # need the commit messages between fromHash..toHash.
            if is_revert_commit(title):
                is_revert = True

        # Push: top-level `changes[]` with {ref:{displayId,type}, toHash, type}.
        # DELETE-type changes have no new commit and are skipped. Tag
        # pushes (ref.type == "TAG") are skipped too — `branch_name` and
        # `parse_change_type` would mis-bucket the tag's displayId
        # otherwise.
        raw_changes = _as_list(body.get("changes"))
        had_branch_change = False
        # Captured for the push-commits enrichment background task; we
        # need both endpoints of the range to query
        # `/commits?since={fromHash}&until={toHash}`.
        push_from_hash: str | None = None
        push_to_hash: str | None = None
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
            from_hash = change_dict.get("fromHash")
            if not branch_name and isinstance(ref_display, str):
                branch_name = ref_display
            if not commit_sha and isinstance(to_hash, str):
                commit_sha = to_hash
            if push_to_hash is None and isinstance(to_hash, str):
                push_to_hash = to_hash
            if push_from_hash is None and isinstance(from_hash, str):
                push_from_hash = from_hash
            if branch_name and commit_sha and push_from_hash and push_to_hash:
                break

        # A push event with `changes[]` but no usable branch change
        # (tag-only or DELETE-only) carries no data we'd join on. Drop
        # it rather than write an orphan row; still log so the operator
        # can see the delivery arrived.
        if not pr and raw_changes and not had_branch_change:
            logger.info(
                "bitbucket_event_skipped_no_branch_change",
                delivery_id=delivery_id,
                event_type=event_type,
                repo=lower(repo_full_name),
                team=team,
            )
            return {"status": "ignored", "reason": "no branch change in push"}

        if not author:
            author = _user_handle(_as_dict(body.get("actor")))

        # BBS DC PR payloads don't carry diff stats; the row inserts with
        # NULLs and a background task fills them in for `pr:merged` events.
        # Push-side `is_revert` is the same plumbing, deferred separately.

        # Lowercase identifiers used for joins / aggregations. Raw values are
        # preserved on the original `payload` JSONB.
        repo_full_name = lower(repo_full_name)
        branch_name = lower(branch_name)
        commit_sha = lower(commit_sha)

        change_type = parse_change_type(branch_name)
        jira_keys = extract_jira_keys(title, description, branch_name)
        automation_source = config.detect_automation_source(author, branch_name)

        occurred_at = _parse_dt(body.get("date")) or datetime.now(UTC)

        async with session_factory() as session:
            stmt = (
                pg_insert(BitbucketEvent)
                .values(
                    delivery_id=delivery_id,
                    event_type=event_type,
                    repo_full_name=repo_full_name,
                    pr_id=pr_id,
                    commit_sha=commit_sha,
                    author=author,
                    branch_name=branch_name,
                    change_type=change_type,
                    jira_keys=jira_keys,
                    automation_source=automation_source,
                    lines_added=None,
                    lines_removed=None,
                    files_changed=None,
                    is_revert=is_revert,
                    occurred_at=occurred_at,
                    team=team,
                    payload=body,
                )
                .on_conflict_do_nothing(index_elements=["delivery_id"])
            )
            await session.execute(stmt)
            await session.commit()

        logger.info(
            "bitbucket_event_received",
            delivery_id=delivery_id,
            event_type=event_type,
            repo=repo_full_name,
            team=team,
        )

        if (
            not _BBS_CALLBACKS_DISABLED
            and x_event_key == PR_MERGED_EVENT_KEY
            and pr_id is not None
            and bbs_client is not None
        ):
            project_key, slug = _split_repo(body)
            if project_key and slug:
                background_tasks.add_task(
                    _enrich_pr_size,
                    session_factory=session_factory,
                    team_keys=team_keys,
                    bbs_client=bbs_client,
                    delivery_id=delivery_id,
                    team=team,
                    project_key=project_key,
                    slug=slug,
                    pr_id=pr_id,
                )

        if (
            not _BBS_CALLBACKS_DISABLED
            and x_event_key == REFS_CHANGED_EVENT_KEY
            and bbs_client is not None
            and push_from_hash
            and push_to_hash
        ):
            project_key, slug = _split_repo(body)
            if project_key and slug:
                background_tasks.add_task(
                    _enrich_push_commits,
                    session_factory=session_factory,
                    team_keys=team_keys,
                    bbs_client=bbs_client,
                    delivery_id=delivery_id,
                    team=team,
                    project_key=project_key,
                    slug=slug,
                    from_hash=push_from_hash,
                    to_hash=push_to_hash,
                )

        return {"status": "accepted"}

    return router


def _split_repo(body: dict[str, Any]) -> tuple[str | None, str | None]:
    """Return the un-lowercased (project_key, slug) for BBS API URL paths.

    The DB columns store the lowercased composite for joins, but the BBS
    DC REST API path is case-sensitive — use the raw values from the
    payload.
    """
    repo = _as_dict(body.get("repository"))
    if not repo:
        pr = _as_dict(body.get("pullRequest"))
        repo = _as_dict(_as_dict(pr.get("toRef")).get("repository"))
    project_key = _as_dict(repo.get("project")).get("key")
    slug = repo.get("slug")
    if not (isinstance(project_key, str) and isinstance(slug, str)):
        return None, None
    # Belt-and-braces against URL-path injection: BBS project keys and
    # repo slugs are alphanumeric + a small set of separators, never
    # contain '/'. If the payload carries something exotic, refuse to
    # build a request URL rather than letting it traverse path segments.
    if "/" in project_key or "/" in slug:
        return None, None
    return project_key, slug


async def _enrich_pr_size(
    *,
    session_factory: async_sessionmaker[AsyncSession],
    team_keys: TeamKeysStore,
    bbs_client: BitbucketClient,
    delivery_id: str,
    team: str,
    project_key: str,
    slug: str,
    pr_id: int,
) -> None:
    """Background task: fetch PR diff stats from BBS DC, UPDATE the row.

    Best-effort. Any exception is caught at the task boundary so a
    failed enrichment doesn't surface as a server error after the
    webhook has already returned 202.
    """
    try:
        token = team_keys.get_secret(team, "bitbucket_api")
        if not token:
            logger.info(
                "bitbucket_api_token_missing",
                team=team,
                delivery_id=delivery_id,
            )
            return
        stats = await bbs_client.fetch_pr_diff_stats(project_key, slug, pr_id, token)
        if stats is None:
            return
        async with session_factory() as session:
            # `delivery_id` is the upsert key on bitbucket_events (unique
            # index from migration 0001). One row per delivery; this
            # UPDATE is therefore single-row.
            await session.execute(
                text(
                    "UPDATE bitbucket_events "
                    "SET lines_added = :a, "
                    "lines_removed = :r, "
                    "files_changed = :f "
                    "WHERE delivery_id = :id"
                ),
                {
                    "a": stats.lines_added,
                    "r": stats.lines_removed,
                    "f": stats.files_changed,
                    "id": delivery_id,
                },
            )
            await session.commit()
        logger.info(
            "bitbucket_pr_size_enriched",
            delivery_id=delivery_id,
            pr_id=pr_id,
            lines_added=stats.lines_added,
            lines_removed=stats.lines_removed,
            files_changed=stats.files_changed,
        )
    except Exception as exc:
        logger.warning(
            "bitbucket_pr_size_enrich_failed",
            delivery_id=delivery_id,
            pr_id=pr_id,
            error=str(exc),
            error_type=type(exc).__name__,
        )


async def _enrich_push_commits(
    *,
    session_factory: async_sessionmaker[AsyncSession],
    team_keys: TeamKeysStore,
    bbs_client: BitbucketClient,
    delivery_id: str,
    team: str,
    project_key: str,
    slug: str,
    from_hash: str,
    to_hash: str,
) -> None:
    """Background task: fetch commit count + author count for a push.

    Best-effort, same shape as `_enrich_pr_size`. Failure leaves the
    columns NULL.
    """
    try:
        token = team_keys.get_secret(team, "bitbucket_api")
        if not token:
            logger.info(
                "bitbucket_api_token_missing",
                team=team,
                delivery_id=delivery_id,
            )
            return
        stats = await bbs_client.fetch_push_commit_stats(
            project_key, slug, from_hash, to_hash, token
        )
        if stats is None:
            return
        async with session_factory() as session:
            await session.execute(
                text(
                    "UPDATE bitbucket_events "
                    "SET push_commit_count = :c, "
                    "push_author_count = :a "
                    "WHERE delivery_id = :id"
                ),
                {
                    "c": stats.commit_count,
                    "a": stats.author_count,
                    "id": delivery_id,
                },
            )
            await session.commit()
        logger.info(
            "bitbucket_push_commits_enriched",
            delivery_id=delivery_id,
            commit_count=stats.commit_count,
            author_count=stats.author_count,
        )
    except Exception as exc:
        logger.warning(
            "bitbucket_push_commits_enrich_failed",
            delivery_id=delivery_id,
            error=str(exc),
            error_type=type(exc).__name__,
        )
