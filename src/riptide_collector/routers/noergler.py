from typing import Any

from fastapi import APIRouter, Depends, status
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from riptide_collector.logging_config import get_logger
from riptide_collector.models import NoerglerEvent
from riptide_collector.parsers import lower
from riptide_collector.schemas.noergler import (
    NoerglerCompleted,
    NoerglerFeedback,
    NoerglerWebhook,
)

logger = get_logger(__name__)


def make_router(
    session_factory: async_sessionmaker[AsyncSession],
    auth_dep: Any,
) -> APIRouter:
    router = APIRouter(prefix="/webhooks", tags=["webhooks"])

    @router.post(
        "/noergler",
        status_code=status.HTTP_202_ACCEPTED,
        summary="Noergler PR-review webhook sink (cost + reviewer-precision)",
    )
    async def noergler_webhook(  # pyright: ignore[reportUnusedFunction]
        event: NoerglerWebhook,
        caller_team: str = Depends(auth_dep),
    ) -> dict[str, str]:
        raw = event.model_dump(mode="json")

        if isinstance(event, NoerglerCompleted):
            values = _values_completed(event, caller_team, raw)
        else:
            values = _values_feedback(event, caller_team, raw)

        async with session_factory() as session:
            stmt = (
                pg_insert(NoerglerEvent)
                .values(**values)
                .on_conflict_do_nothing(index_elements=["delivery_id"])
            )
            await session.execute(stmt)
            await session.commit()

        logger.info(
            "noergler_event_received",
            delivery_id=values["delivery_id"],
            event_type=values["event_type"],
            team=caller_team,
        )
        return {"status": "accepted"}

    return router


def _values_completed(
    event: NoerglerCompleted, caller_team: str, raw: dict[str, Any]
) -> dict[str, Any]:
    # run_id is unique per noergler review run, so it alone is a stable
    # idempotency key. Prefix with event_type so a future event re-using a
    # run_id can't collide.
    delivery_id = f"completed#{event.run_id}"
    service = lower(event.service_id) or lower(event.repo)
    return {
        "delivery_id": delivery_id,
        "event_type": "completed",
        "pr_key": lower(event.pr_key),
        "repo": lower(event.repo),
        "commit_sha": lower(event.commit_sha),
        "run_id": event.run_id,
        "model": event.model,
        "prompt_tokens": event.prompt_tokens,
        "completion_tokens": event.completion_tokens,
        "elapsed_ms": event.elapsed_ms,
        "findings_count": event.findings_count,
        "cost_usd": event.cost_usd,
        "occurred_at": event.finished_at,
        "service": service,
        "team": caller_team,
        "payload": raw,
    }


def _values_feedback(
    event: NoerglerFeedback, caller_team: str, raw: dict[str, Any]
) -> dict[str, Any]:
    # finding_id is stable per noergler finding; including verdict allows the
    # same user flipping disagree → acknowledged (or vice versa) to produce
    # distinct rows for honest auditability.
    delivery_id = f"feedback#{event.finding_id}#{event.verdict}"
    service = lower(event.service_id) or lower(event.repo)
    return {
        "delivery_id": delivery_id,
        "event_type": "feedback",
        "pr_key": lower(event.pr_key),
        "repo": lower(event.repo),
        "finding_id": event.finding_id,
        "verdict": event.verdict,
        "actor": event.actor,
        "occurred_at": event.occurred_at,
        "service": service,
        "team": caller_team,
        "payload": raw,
    }
