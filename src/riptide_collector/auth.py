from collections.abc import Awaitable, Callable

from fastapi import Header, HTTPException, status

from riptide_collector.logging_config import get_logger
from riptide_collector.team_keys import KNOWN_SOURCES, TeamKeysStore

logger = get_logger(__name__)


def make_team_bearer_dependency(
    team_keys: TeamKeysStore,
    source: str,
) -> Callable[[str | None], Awaitable[str]]:
    """Build a FastAPI dependency that authenticates a Bearer-token request
    against the team's *source-specific* secret and returns the team name.

    Strict source binding: a token registered under one source cannot
    authenticate an endpoint that requires a different source. This
    contains the blast radius of a leaked secret to a single source.

    The special pseudo-source `"any"` accepts any of the team's per-source
    secrets and is used by the source-agnostic `/auth/ping` reachability
    check.

    Bitbucket is *not* covered by this dependency: BBS is HMAC-only
    (`/webhooks/bitbucket/{team}` validates `X-Hub-Signature` directly).
    """
    if source not in KNOWN_SOURCES and source != "any":
        raise ValueError(f"unknown source {source!r}; allowed: {sorted(KNOWN_SOURCES)} or 'any'")

    async def verify_team_bearer(
        authorization: str | None = Header(default=None),
    ) -> str:
        if not authorization:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Missing or malformed Authorization header.",
            )

        scheme, _, value = authorization.partition(" ")
        if scheme.lower() != "bearer":
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Missing or malformed Authorization header.",
            )

        token = value.strip()
        if not token:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Missing or malformed Authorization header.",
            )

        team_keys.maybe_reload()
        team = (
            team_keys.lookup_any_source(token)
            if source == "any"
            else team_keys.lookup(token, source)
        )
        if team is None:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Invalid credentials.",
            )

        return team

    return verify_team_bearer
