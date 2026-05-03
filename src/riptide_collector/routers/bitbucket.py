import hashlib
import hmac
import json
from datetime import UTC, datetime
from typing import Any

from fastapi import APIRouter, Header, HTTPException, Path, Request, status
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

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
    return f"{event_key or 'unknown'}#{repo}#{pr_id}#{when}"


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
) -> APIRouter:
    router = APIRouter(prefix="/webhooks", tags=["webhooks"])

    @router.post(
        "/bitbucket/{team}",
        status_code=status.HTTP_202_ACCEPTED,
        summary="Bitbucket DC webhook sink (HMAC-authenticated)",
    )
    async def bitbucket_webhook(  # pyright: ignore[reportUnusedFunction]
        request: Request,
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

        # Push: top-level `changes[]` with {ref:{displayId}, toHash, type}.
        # DELETE-type changes have no new commit and are skipped.
        if not (branch_name and commit_sha):
            for change in _as_list(body.get("changes")):
                change_dict = _as_dict(change)
                if str(change_dict.get("type", "")).upper() == "DELETE":
                    continue
                ref_display = _as_dict(change_dict.get("ref")).get("displayId")
                to_hash = change_dict.get("toHash")
                if not branch_name and isinstance(ref_display, str):
                    branch_name = ref_display
                if not commit_sha and isinstance(to_hash, str):
                    commit_sha = to_hash
                if branch_name and commit_sha:
                    break

        if not author:
            author = _user_handle(_as_dict(body.get("actor")))

        # BBS DC PR payloads don't carry diff stats — leave NULL.
        lines_added: int | None = None
        lines_removed: int | None = None
        files_changed: int | None = None

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
                    lines_added=lines_added,
                    lines_removed=lines_removed,
                    files_changed=files_changed,
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
        return {"status": "accepted"}

    return router
