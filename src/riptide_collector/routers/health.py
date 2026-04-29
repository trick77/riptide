from typing import Any

from fastapi import APIRouter, Depends, status
from sqlalchemy import text
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from riptide_collector.catalog import CatalogStore
from riptide_collector.team_keys import TeamKeysStore


def make_router(
    catalog: CatalogStore,
    session_factory: async_sessionmaker[AsyncSession],
    team_keys: TeamKeysStore,
    auth_dep: Any,
) -> APIRouter:
    router = APIRouter(tags=["health"])

    @router.get("/health", status_code=status.HTTP_200_OK, summary="Liveness probe")
    async def health() -> dict[str, str]:  # pyright: ignore[reportUnusedFunction]
        return {"status": "ok"}

    @router.get("/ready", summary="Readiness probe")
    async def ready() -> dict[str, Any]:  # pyright: ignore[reportUnusedFunction]
        try:
            async with session_factory() as session:
                await session.execute(text("SELECT 1"))
        except Exception as exc:
            from fastapi import HTTPException

            raise HTTPException(
                status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
                detail=f"db unreachable: {exc}",
            ) from exc

        catalog_view = catalog.get()
        return {
            "status": "ok",
            "teams": len(catalog_view.teams_by_name),
            "team_keys": len(team_keys.team_names()),
            "catalog_reload_failures": catalog.reload_failures,
            "team_keys_reload_failures": team_keys.reload_failures,
        }

    @router.get(
        "/auth/ping",
        summary="Authenticated reachability check for senders",
    )
    async def auth_ping(  # pyright: ignore[reportUnusedFunction]
        caller_team: str = Depends(auth_dep),
    ) -> dict[str, str]:
        return {"status": "ok", "team": caller_team}

    return router
