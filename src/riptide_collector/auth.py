from collections.abc import Awaitable, Callable

from fastapi import Header, HTTPException, status

from riptide_collector.team_keys import TeamKeysStore


def make_team_bearer_dependency(
    team_keys: TeamKeysStore,
) -> Callable[[str | None], Awaitable[str]]:
    """Build a FastAPI dependency that authenticates a request and returns
    the calling team's name.

    The dependency:
      * 401s on missing / malformed Authorization header
      * 401s on unknown bearer
      * returns the team name (str) on success
    """

    async def verify_team_bearer(
        authorization: str | None = Header(default=None),
    ) -> str:
        if not authorization or not authorization.lower().startswith("bearer "):
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Missing or malformed Authorization header.",
            )
        token = authorization.split(None, 1)[1].strip()
        team_keys.maybe_reload()
        team = team_keys.lookup(token)
        if team is None:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Invalid bearer token.",
            )
        return team

    return verify_team_bearer
