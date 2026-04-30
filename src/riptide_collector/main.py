from contextlib import asynccontextmanager
from typing import Any

from fastapi import FastAPI

from riptide_collector import __version__
from riptide_collector.auth import make_team_bearer_dependency
from riptide_collector.catalog import CatalogStore
from riptide_collector.db import make_engine, make_session_factory
from riptide_collector.logging_config import configure_logging, get_logger
from riptide_collector.routers import argocd, bitbucket, health, noergler, pipeline
from riptide_collector.settings import Settings, load_settings
from riptide_collector.team_keys import TeamKeysStore

logger = get_logger(__name__)


class StartupValidationError(RuntimeError):
    pass


def _cross_validate(catalog: CatalogStore, team_keys: TeamKeysStore) -> None:
    """Every team in the catalog must have a key entry. Fail fast if not."""
    catalog_teams = set(catalog.get().teams_by_name.keys())
    key_teams = team_keys.team_names()
    missing = catalog_teams - key_teams
    extra = key_teams - catalog_teams
    if missing:
        raise StartupValidationError(
            f"team-keys file is missing entries for teams in the catalog: {sorted(missing)}"
        )
    if extra:
        # Extra keys aren't fatal — a team can have a key before its catalog
        # entry lands. But surface it so it doesn't hide drift indefinitely.
        logger.warning("team_keys_extra_entries", extra=sorted(extra))


def create_app(settings: Settings | None = None) -> FastAPI:
    settings = settings or load_settings()
    configure_logging(settings.log_level)

    catalog = CatalogStore(settings.catalog_path)
    team_keys = TeamKeysStore(settings.team_keys_path)
    _cross_validate(catalog, team_keys)

    engine = make_engine(settings.db_url)
    session_factory = make_session_factory(engine)
    auth_dep = make_team_bearer_dependency(team_keys)

    @asynccontextmanager
    async def lifespan(_app: FastAPI):  # pyright: ignore[reportUnusedFunction]
        logger.info(
            "riptide_collector_starting",
            version=__version__,
            teams=len(catalog.get().teams_by_name),
            keys=len(team_keys.team_names()),
            production_stage=catalog.get().environments.production_stage,
        )
        try:
            yield
        finally:
            await engine.dispose()
            logger.info("riptide_collector_stopped")

    app: Any = FastAPI(
        title="riptide-collector",
        version=__version__,
        description=(
            "DevOps delivery-metrics ingestion. "
            "Collects raw events from Bitbucket / Jenkins / ArgoCD into Postgres."
        ),
        lifespan=lifespan,
    )

    app.include_router(health.make_router(catalog, session_factory, team_keys, auth_dep))
    app.include_router(bitbucket.make_router(catalog, session_factory, auth_dep))
    app.include_router(pipeline.make_router(session_factory, auth_dep))
    app.include_router(argocd.make_router(session_factory, auth_dep))
    app.include_router(noergler.make_router(session_factory, auth_dep))

    app.state.catalog = catalog
    app.state.team_keys = team_keys
    app.state.engine = engine
    app.state.session_factory = session_factory
    app.state.settings = settings

    return app


app = create_app()
