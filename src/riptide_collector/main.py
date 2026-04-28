from contextlib import asynccontextmanager
from typing import Any

from fastapi import FastAPI

from riptide_collector import __version__
from riptide_collector.auth import make_bearer_dependency
from riptide_collector.catalog import CatalogStore
from riptide_collector.db import make_engine, make_session_factory
from riptide_collector.logging_config import configure_logging, get_logger
from riptide_collector.routers import argocd, bitbucket, health, jenkins
from riptide_collector.settings import Settings, load_settings

logger = get_logger(__name__)


def create_app(settings: Settings | None = None) -> FastAPI:
    settings = settings or load_settings()
    configure_logging(settings.log_level)

    catalog = CatalogStore(settings.catalog_path)
    engine = make_engine(settings.db_url)
    session_factory = make_session_factory(engine)
    auth_dep = make_bearer_dependency(settings.webhook_token)

    @asynccontextmanager
    async def lifespan(_app: FastAPI):  # pyright: ignore[reportUnusedFunction]
        logger.info(
            "riptide_collector_starting",
            version=__version__,
            services=len(catalog.get().services),
            teams=len(catalog.get().teams_by_name),
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

    app.include_router(health.make_router(catalog, session_factory))
    app.include_router(bitbucket.make_router(catalog, session_factory, auth_dep))
    app.include_router(jenkins.make_router(catalog, session_factory, auth_dep))
    app.include_router(argocd.make_router(catalog, session_factory, auth_dep))

    app.state.catalog = catalog
    app.state.engine = engine
    app.state.session_factory = session_factory
    app.state.settings = settings

    return app


app = create_app()
