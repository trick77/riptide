import hashlib
import hmac
import json

from fastapi import APIRouter, Header, HTTPException, Path, Request, status
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from riptide_collector.config import RiptideConfigStore
from riptide_collector.logging_config import get_logger
from riptide_collector.models import BitbucketEvent
from riptide_collector.parsers_bitbucket import (
    BitbucketEventDraft,
    BitbucketSkip,
    extract_event,
)
from riptide_collector.team_keys import TeamKeysStore

logger = get_logger(__name__)


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

        parsed = extract_event(
            body,
            x_event_key=x_event_key,
            x_request_uuid=x_request_uuid,
            x_hook_uuid=x_hook_uuid,
        )

        if isinstance(parsed, BitbucketSkip):
            logger.info(
                "bitbucket_event_skipped_no_branch_change",
                delivery_id=parsed.delivery_id,
                event_type=parsed.event_type,
                repo=parsed.repo_full_name,
                team=team,
            )
            return {"status": "ignored", "reason": parsed.reason}

        # Explicit narrowing — if a third variant of the union is ever added,
        # this assert turns into a typing error rather than a silent bug.
        assert isinstance(parsed, BitbucketEventDraft)
        draft = parsed
        automation_source = config.detect_automation_source(draft.author, draft.branch_name)

        async with session_factory() as session:
            stmt = (
                pg_insert(BitbucketEvent)
                .values(
                    delivery_id=draft.delivery_id,
                    event_type=draft.event_type,
                    repo_full_name=draft.repo_full_name,
                    pr_id=draft.pr_id,
                    commit_sha=draft.commit_sha,
                    author=draft.author,
                    branch_name=draft.branch_name,
                    change_type=draft.change_type,
                    jira_keys=draft.jira_keys,
                    automation_source=automation_source,
                    lines_added=None,
                    lines_removed=None,
                    files_changed=None,
                    is_revert=draft.is_revert,
                    occurred_at=draft.occurred_at,
                    team=team,
                    payload=draft.payload,
                )
                .on_conflict_do_nothing(index_elements=["delivery_id"])
            )
            await session.execute(stmt)
            await session.commit()

        logger.info(
            "bitbucket_event_received",
            delivery_id=draft.delivery_id,
            event_type=draft.event_type,
            repo=draft.repo_full_name,
            team=team,
        )
        return {"status": "accepted"}

    return router
