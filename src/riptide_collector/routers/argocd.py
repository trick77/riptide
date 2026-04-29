from datetime import UTC, datetime
from typing import Any

from fastapi import APIRouter, Depends, status
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from riptide_collector.logging_config import get_logger
from riptide_collector.models import ArgoCDEvent
from riptide_collector.parsers import lower
from riptide_collector.schemas.argocd import ArgoCDWebhook

logger = get_logger(__name__)


def make_router(
    session_factory: async_sessionmaker[AsyncSession],
    auth_dep: Any,
) -> APIRouter:
    router = APIRouter(prefix="/webhooks", tags=["webhooks"])

    @router.post(
        "/argocd",
        status_code=status.HTTP_202_ACCEPTED,
        summary="ArgoCD webhook sink",
    )
    async def argocd_webhook(  # pyright: ignore[reportUnusedFunction]
        event: ArgoCDWebhook,
        caller_team: str = Depends(auth_dep),
    ) -> dict[str, str]:
        raw = event.model_dump(mode="json")

        revision = lower(event.revision)

        # Stable dedup key: started_at is fixed for a sync attempt; phase varies
        # across the lifecycle (Running → Succeeded/Failed) and SHOULD produce
        # distinct rows. finished_at is excluded — it can drift between retries
        # of the same phase and would cause duplicates.
        started_repr = event.started_at.isoformat() if event.started_at else "unknown"
        delivery_id = (
            f"{event.app_name}#{revision}#{started_repr}#{event.operation_phase or 'unknown'}"
        )

        async with session_factory() as session:
            stmt = (
                pg_insert(ArgoCDEvent)
                .values(
                    delivery_id=delivery_id,
                    app_name=event.app_name,
                    revision=revision,
                    sync_status=event.sync_status,
                    operation_phase=event.operation_phase,
                    started_at=event.started_at,
                    finished_at=event.finished_at,
                    occurred_at=event.finished_at or event.started_at or datetime.now(UTC),
                    team=caller_team,
                    payload=raw,
                )
                .on_conflict_do_nothing(index_elements=["delivery_id"])
            )
            await session.execute(stmt)
            await session.commit()

        logger.info(
            "argocd_event_received",
            delivery_id=delivery_id,
            app=event.app_name,
            revision=revision,
            phase=event.operation_phase,
            team=caller_team,
        )
        return {"status": "accepted"}

    return router
