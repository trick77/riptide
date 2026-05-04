import json
from collections.abc import Awaitable, Callable

from fastapi import APIRouter, Depends, Header, Path, status
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

logger = get_logger(__name__)


def make_router(
    config: RiptideConfigStore,
    session_factory: async_sessionmaker[AsyncSession],
    hmac_dep: Callable[..., Awaitable[bytes]],
) -> APIRouter:
    router = APIRouter(prefix="/webhooks", tags=["webhooks"])

    @router.post(
        "/bitbucket/{team}",
        status_code=status.HTTP_202_ACCEPTED,
        summary="Bitbucket DC webhook sink (HMAC-authenticated)",
    )
    async def bitbucket_webhook(  # pyright: ignore[reportUnusedFunction]
        raw: bytes = Depends(hmac_dep),
        team: str = Path(..., min_length=1),
        x_event_key: str | None = Header(default=None),
        x_request_uuid: str | None = Header(default=None),
        x_hook_uuid: str | None = Header(default=None),
    ) -> dict[str, str]:
        # HMAC + raw-body read happen in the dependency; we get verified
        # bytes here. Parse once, dispatch to the pure extractor.
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
