from datetime import UTC, datetime
from typing import Any

from fastapi import APIRouter, Depends, status
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from riptide_collector.catalog import CatalogStore
from riptide_collector.logging_config import get_logger
from riptide_collector.models import PipelineEvent
from riptide_collector.schemas.pipeline import PipelineWebhook

logger = get_logger(__name__)


def make_router(
    catalog: CatalogStore,
    session_factory: async_sessionmaker[AsyncSession],
    auth_dep: Any,
) -> APIRouter:
    del catalog  # not needed; team comes from caller, not the catalog
    router = APIRouter(prefix="/webhooks", tags=["webhooks"])

    @router.post(
        "/pipeline",
        status_code=status.HTTP_202_ACCEPTED,
        summary="CI pipeline webhook sink (Jenkins, Tekton, …)",
    )
    async def pipeline_webhook(  # pyright: ignore[reportUnusedFunction]
        event: PipelineWebhook,
        caller_team: str = Depends(auth_dep),
    ) -> dict[str, str]:
        raw = event.model_dump(mode="json")

        # source is part of the dedup key so distinct CI systems with the same
        # pipeline name (e.g. a Jenkins job and a Tekton pipeline both called
        # 'deploy') don't collide.
        delivery_id = f"{event.source}#{event.pipeline_name}#{event.run_id}#{event.phase}"

        # Service identity is observed: explicit hint wins, else the pipeline name.
        service = event.service_id or event.pipeline_name

        async with session_factory() as session:
            stmt = (
                pg_insert(PipelineEvent)
                .values(
                    delivery_id=delivery_id,
                    source=event.source,
                    pipeline_name=event.pipeline_name,
                    run_id=event.run_id,
                    phase=event.phase,
                    status=event.status,
                    commit_sha=event.commit_sha,
                    started_at=event.started_at,
                    finished_at=event.finished_at,
                    occurred_at=event.finished_at or event.started_at or datetime.now(UTC),
                    service=service,
                    team=caller_team,
                    payload=raw,
                )
                .on_conflict_do_nothing(index_elements=["delivery_id"])
            )
            await session.execute(stmt)
            await session.commit()

        logger.info(
            "pipeline_event_received",
            delivery_id=delivery_id,
            source=event.source,
            pipeline=event.pipeline_name,
            run=event.run_id,
            phase=event.phase,
            status=event.status,
            service=service,
            team=caller_team,
        )
        return {"status": "accepted"}

    return router
