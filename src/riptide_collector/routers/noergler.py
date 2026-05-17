from typing import Any

from fastapi import APIRouter, Depends, status
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from riptide_collector.logging_config import get_logger
from riptide_collector.models import NoerglerEvent
from riptide_collector.parsers import lower
from riptide_collector.schemas.noergler import (
    NoerglerFeedback,
    NoerglerPrCompleted,
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
        summary="Noergler PR-review webhook sink (per-PR rollup + reviewer-precision)",
    )
    async def noergler_webhook(  # pyright: ignore[reportUnusedFunction]
        event: NoerglerWebhook,
        caller_team: str = Depends(auth_dep),
    ) -> dict[str, str]:
        raw = event.model_dump(mode="json")

        if isinstance(event, NoerglerPrCompleted):
            values = _values_pr_completed(event, caller_team, raw)
        else:
            values = _values_feedback(event, caller_team, raw)

        try:
            async with session_factory() as session:
                stmt = (
                    pg_insert(NoerglerEvent)
                    .values(**values)
                    .on_conflict_do_nothing(index_elements=["delivery_id"])
                    .returning(NoerglerEvent.delivery_id)
                )
                inserted = (await session.execute(stmt)).scalar_one_or_none()
                await session.commit()
        except Exception:
            logger.exception(
                "webhook_persist_failed",
                webhook_source="noergler",
                delivery_id=values["delivery_id"],
                team=caller_team,
            )
            raise

        logger.info(
            "webhook_processed",
            webhook_source="noergler",
            outcome="accepted" if inserted is not None else "deduped",
            delivery_id=values["delivery_id"],
            event_type=values["event_type"],
            team=caller_team,
        )
        return {"status": "accepted"}

    return router


def _values_pr_completed(
    event: NoerglerPrCompleted, caller_team: str, raw: dict[str, Any]
) -> dict[str, Any]:
    # A PR has at most one terminal outcome, but Bitbucket can redeliver the
    # lifecycle webhook (e.g. retries). Idempotency keyed on (pr_key, outcome)
    # — including outcome in the key lets a deleted-after-decline edge case
    # land two distinct rows if the operator wires both events. pr_key is
    # lowercased to match the stored column so a casing-flipped redelivery
    # (PROJ/... vs proj/...) still dedupes.
    delivery_id = f"pr_completed#{lower(event.pr_key)}#{event.outcome}"
    return {
        "delivery_id": delivery_id,
        "event_type": "pr_completed",
        "outcome": event.outcome,
        "pr_key": lower(event.pr_key),
        "repo": lower(event.repo),
        "commit_sha": lower(event.source_commit_sha),
        "merge_commit_sha": lower(event.merge_commit_sha) if event.merge_commit_sha else None,
        "lines_added": event.lines_added,
        "lines_removed": event.lines_removed,
        "files_changed": event.files_changed,
        "total_runs": event.total_runs,
        "models_used": event.models_used,
        "prompt_tokens": event.total_prompt_tokens,
        "completion_tokens": event.total_completion_tokens,
        "elapsed_ms": event.total_elapsed_ms,
        "findings_count": event.total_findings_count,
        "cost_usd": event.total_cost_usd,
        "first_review_at": event.first_review_at,
        "occurred_at": event.closed_at,
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
    return {
        "delivery_id": delivery_id,
        "event_type": "feedback",
        "pr_key": lower(event.pr_key),
        "repo": lower(event.repo),
        "commit_sha": lower(event.commit_sha),
        "finding_id": event.finding_id,
        "verdict": event.verdict,
        # Lowercase the actor so future joins to a directory / Slack handle
        # mapping are case-stable. Same rule the project applies to repos
        # and refs.
        "actor": lower(event.actor),
        "occurred_at": event.occurred_at,
        "team": caller_team,
        "payload": raw,
    }
