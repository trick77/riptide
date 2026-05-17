import time
import uuid
from contextlib import asynccontextmanager
from typing import Any

import structlog
from fastapi import FastAPI, Request, Response

from riptide_collector import __version__
from riptide_collector.auth import make_hmac_dependency, make_team_bearer_dependency
from riptide_collector.config import RiptideConfigStore
from riptide_collector.db import make_engine, make_session_factory
from riptide_collector.logging_config import configure_logging, get_logger
from riptide_collector.routers import argocd, bitbucket, health, noergler, pipeline
from riptide_collector.settings import Settings, load_settings
from riptide_collector.team_keys import TeamKeysStore

logger = get_logger(__name__)

_SILENT_PATHS = frozenset({"/health", "/ready"})


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
    configure_logging(settings.log_level, env=settings.env)

    config = RiptideConfigStore(settings.config_path)
    team_keys = TeamKeysStore(settings.team_keys_path)
    _cross_validate(config, team_keys)

    engine = make_engine(settings.db_url)
    session_factory = make_session_factory(engine)
    argocd_auth = make_team_bearer_dependency(team_keys, "argocd")
    pipeline_auth = make_team_bearer_dependency(team_keys, "jenkins")
    noergler_auth = make_team_bearer_dependency(team_keys, "noergler")
    any_auth = make_team_bearer_dependency(team_keys, "any")
    bitbucket_hmac = make_hmac_dependency(team_keys, "bitbucket")

    @asynccontextmanager
    async def lifespan(_app: FastAPI):  # pyright: ignore[reportUnusedFunction]
        logger.info(
            "riptide_collector_starting",
            version=__version__,
            teams=len(config.get().teams_by_name),
            keys=len(team_keys.team_names()),
            production_stage=config.get().environments.production_stage,
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

    @app.middleware("http")
    async def access_log(  # pyright: ignore[reportUnusedFunction]
        request: Request, call_next: Any
    ) -> Response:
        # Liveness/readiness checks fire every few seconds; logging them
        # buries real traffic in Splunk. Pass through unobserved.
        # rstrip handles `/health/` (trailing slash) too.
        if request.url.path.rstrip("/") in _SILENT_PATHS:
            return await call_next(request)
        request_id = request.headers.get("x-request-id") or uuid.uuid4().hex
        bound = ("request_id", "method", "path")
        structlog.contextvars.bind_contextvars(
            request_id=request_id,
            method=request.method,
            path=request.url.path,
        )
        started = time.perf_counter()
        # Sentinel for the unhandled-exception path: if call_next raises
        # before we overwrite status_code, the finally block still emits
        # a numeric status. FastAPI's default exception handler turns the
        # exception into a real 500 response after the middleware unwinds.
        status_code = 500
        try:
            response: Response = await call_next(request)
            status_code = response.status_code
            return response
        finally:
            logger.info(
                "http_request",
                status_code=status_code,
                duration_ms=round((time.perf_counter() - started) * 1000, 1),
            )
            # Unbind only what we bound — don't nuke contextvars set by
            # callers up the stack (lifespan, future auth layers).
            structlog.contextvars.unbind_contextvars(*bound)

    app.include_router(health.make_router(config, session_factory, team_keys, any_auth))
    app.include_router(bitbucket.make_router(config, session_factory, bitbucket_hmac))
    app.include_router(pipeline.make_router(session_factory, pipeline_auth))
    app.include_router(argocd.make_router(config, session_factory, argocd_auth))
    app.include_router(noergler.make_router(session_factory, noergler_auth))

    app.state.config = config
    app.state.team_keys = team_keys
    app.state.engine = engine
    app.state.session_factory = session_factory
    app.state.settings = settings

    return app


app = create_app()
