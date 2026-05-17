from datetime import UTC, datetime
from typing import Any

from fastapi import APIRouter, Depends, status
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from riptide_collector.logging_config import get_logger
from riptide_collector.models import PipelineEvent
from riptide_collector.parsers import lower
from riptide_collector.schemas.pipeline import PipelineWebhook

logger = get_logger(__name__)


def make_router(
    session_factory: async_sessionmaker[AsyncSession],
    auth_dep: Any,
) -> APIRouter:
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

        commit_sha = lower(event.commit_sha)

        try:
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
                        commit_sha=commit_sha,
                        started_at=event.started_at,
                        finished_at=event.finished_at,
                        occurred_at=event.finished_at or event.started_at or datetime.now(UTC),
                        team=caller_team,
                        payload=raw,
                    )
                    .on_conflict_do_nothing(index_elements=["delivery_id"])
                    .returning(PipelineEvent.delivery_id)
                )
                inserted = (await session.execute(stmt)).scalar_one_or_none()
                await session.commit()
        except Exception:
            logger.exception(
                "webhook_persist_failed",
                webhook_source="pipeline",
                delivery_id=delivery_id,
                team=caller_team,
            )
            raise

        # `source` is the CI vendor (jenkins / tekton …). Splunk reserves the
        # field name `source`, so it travels under `ci_system` on the wire.
        logger.info(
            "webhook_processed",
            webhook_source="pipeline",
            outcome="accepted" if inserted is not None else "deduped",
            delivery_id=delivery_id,
            ci_system=event.source,
            pipeline=event.pipeline_name,
            run_id=event.run_id,
            phase=event.phase,
            run_status=event.status,
            team=caller_team,
        )
        return {"status": "accepted"}

    return router
