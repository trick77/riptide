from datetime import UTC, datetime
from typing import Any

from fastapi import APIRouter, Depends, status
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import async_sessionmaker

from riptide_collector.catalog import CatalogStore
from riptide_collector.logging_config import get_logger
from riptide_collector.models import JenkinsEvent
from riptide_collector.schemas.jenkins import JenkinsWebhook

logger = get_logger(__name__)


def make_router(
    catalog: CatalogStore,
    session_factory: async_sessionmaker[Any],
    auth_dep: Any,
) -> APIRouter:
    router = APIRouter(prefix="/webhooks", tags=["webhooks"])

    @router.post(
        "/jenkins",
        status_code=status.HTTP_202_ACCEPTED,
        dependencies=[Depends(auth_dep)],
        summary="Jenkins webhook sink",
    )
    async def jenkins_webhook(  # pyright: ignore[reportUnusedFunction]
        event: JenkinsWebhook,
    ) -> dict[str, str]:
        raw = event.model_dump(mode="json")
        catalog.maybe_reload()

        resolution = (
            catalog.resolve_jenkins(event.service_id)
            if event.service_id
            else catalog.resolve_jenkins(event.job_name)
        )
        if resolution is None:
            logger.warning("jenkins_unknown_job", job=event.job_name)

        delivery_id = f"{event.job_name}#{event.build_number}#{event.phase}"

        async with session_factory() as session:
            stmt = (
                pg_insert(JenkinsEvent)
                .values(
                    delivery_id=delivery_id,
                    job_name=event.job_name,
                    build_number=event.build_number,
                    phase=event.phase,
                    status=event.status,
                    commit_sha=event.commit_sha,
                    started_at=event.started_at,
                    finished_at=event.finished_at,
                    occurred_at=event.finished_at or event.started_at or datetime.now(UTC),
                    service=resolution.service_id if resolution else None,
                    team=resolution.team_name if resolution else None,
                    payload=raw,
                )
                .on_conflict_do_nothing(index_elements=["delivery_id"])
            )
            await session.execute(stmt)
            await session.commit()

        logger.info(
            "jenkins_event_received",
            delivery_id=delivery_id,
            job=event.job_name,
            build=event.build_number,
            phase=event.phase,
            status=event.status,
            service=resolution.service_id if resolution else None,
        )
        return {"status": "accepted"}

    return router
