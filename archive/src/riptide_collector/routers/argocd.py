from datetime import UTC, datetime
from typing import Any

from fastapi import APIRouter, Depends, status
from sqlalchemy.dialects.postgresql import insert as pg_insert
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from riptide_collector.config import RiptideConfigStore
from riptide_collector.logging_config import get_logger
from riptide_collector.models import ArgoCDEvent
from riptide_collector.parsers import lower, parse_environment
from riptide_collector.schemas.argocd import ArgoCDWebhook

logger = get_logger(__name__)


def make_router(
    config: RiptideConfigStore,
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
        environment = parse_environment(event.destination_namespace)

        # Compute the dedup key unconditionally so the `ignored` log line
        # carries it too — Triage starts from `delivery_id`.
        started_repr = event.started_at.isoformat() if event.started_at else "unknown"
        delivery_id = (
            f"{event.app_name}#{revision}#{started_repr}#{event.operation_phase or 'unknown'}"
        )

        ignored_stages = config.get().environments.ignored_stages
        if environment is not None and environment in ignored_stages:
            logger.info(
                "webhook_processed",
                webhook_source="argocd",
                outcome="ignored",
                reason="stage_in_ignored_stages",
                delivery_id=delivery_id,
                app=event.app_name,
                revision=revision,
                phase=event.operation_phase,
                environment=environment,
                destination_namespace=event.destination_namespace,
                team=caller_team,
            )
            return {"status": "ignored"}

        try:
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
                        destination_namespace=event.destination_namespace,
                        environment=environment,
                        payload=raw,
                    )
                    .on_conflict_do_nothing(index_elements=["delivery_id"])
                    .returning(ArgoCDEvent.delivery_id)
                )
                inserted = (await session.execute(stmt)).scalar_one_or_none()
                await session.commit()
        except Exception:
            logger.exception(
                "webhook_persist_failed",
                webhook_source="argocd",
                delivery_id=delivery_id,
                team=caller_team,
            )
            raise

        logger.info(
            "webhook_processed",
            webhook_source="argocd",
            outcome="accepted" if inserted is not None else "deduped",
            delivery_id=delivery_id,
            app=event.app_name,
            revision=revision,
            phase=event.operation_phase,
            environment=environment,
            team=caller_team,
        )
        return {"status": "accepted"}

    return router
