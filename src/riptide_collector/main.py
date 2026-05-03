from contextlib import asynccontextmanager
from typing import Any

from fastapi import FastAPI

from riptide_collector import __version__
from riptide_collector.auth import make_team_bearer_dependency
from riptide_collector.bbs_client import BitbucketClient
from riptide_collector.config import RiptideConfigStore
from riptide_collector.db import make_engine, make_session_factory
from riptide_collector.logging_config import configure_logging, get_logger
from riptide_collector.routers import argocd, bitbucket, health, noergler, pipeline
from riptide_collector.settings import Settings, load_settings
from riptide_collector.team_keys import TeamKeysStore

logger = get_logger(__name__)


class StartupValidationError(RuntimeError):
    pass


def _cross_validate(config: RiptideConfigStore, team_keys: TeamKeysStore) -> None:
    """Every team in the config must have a key entry. Fail fast if not."""
    config_teams = set(config.get().teams_by_name.keys())
    key_teams = team_keys.team_names()
    missing = config_teams - key_teams
    extra = key_teams - config_teams
    if missing:
        raise StartupValidationError(
            f"team-keys file is missing entries for teams in the config: {sorted(missing)}"
        )
    if extra:
        # Extra keys aren't fatal — a team can have a key before its config
        # entry lands. But surface it so it doesn't hide drift indefinitely.
        logger.warning("team_keys_extra_entries", extra=sorted(extra))


def create_app(settings: Settings | None = None) -> FastAPI:
    settings = settings or load_settings()
    configure_logging(settings.log_level)

    config = RiptideConfigStore(settings.config_path)
    team_keys = TeamKeysStore(settings.team_keys_path)
    _cross_validate(config, team_keys)

    engine = make_engine(settings.db_url)
    session_factory = make_session_factory(engine)
    argocd_auth = make_team_bearer_dependency(team_keys, "argocd")
    pipeline_auth = make_team_bearer_dependency(team_keys, "jenkins")
    noergler_auth = make_team_bearer_dependency(team_keys, "noergler")
    any_auth = make_team_bearer_dependency(team_keys, "any")

    bbs_base_url = config.get().environments.bitbucket_base_url
    bbs_client = BitbucketClient(bbs_base_url) if bbs_base_url else None

    @asynccontextmanager
    async def lifespan(_app: FastAPI):  # pyright: ignore[reportUnusedFunction]
        logger.info(
            "riptide_collector_starting",
            version=__version__,
            teams=len(config.get().teams_by_name),
            keys=len(team_keys.team_names()),
            production_stage=config.get().environments.production_stage,
            bbs_enrichment=bbs_client is not None,
        )
        try:
            yield
        finally:
            if bbs_client is not None:
                await bbs_client.aclose()
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

    app.include_router(health.make_router(config, session_factory, team_keys, any_auth))
    app.include_router(bitbucket.make_router(config, session_factory, team_keys, bbs_client))
    app.include_router(pipeline.make_router(session_factory, pipeline_auth))
    app.include_router(argocd.make_router(config, session_factory, argocd_auth))
    app.include_router(noergler.make_router(session_factory, noergler_auth))

    app.state.config = config
    app.state.team_keys = team_keys
    app.state.engine = engine
    app.state.session_factory = session_factory
    app.state.settings = settings
    app.state.bbs_client = bbs_client

    return app


app = create_app()
