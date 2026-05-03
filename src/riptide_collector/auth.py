import base64
import binascii
from collections.abc import Awaitable, Callable

from fastapi import Header, HTTPException, status

from riptide_collector.logging_config import get_logger
from riptide_collector.team_keys import TeamKeysStore

logger = get_logger(__name__)


def make_team_bearer_dependency(
    team_keys: TeamKeysStore,
) -> Callable[[str | None], Awaitable[str]]:
    """Build a FastAPI dependency that authenticates a request and returns
    the calling team's name.

    Accepts two Authorization schemes:
      * `Bearer <RAW_TOKEN>` — canonical form used by ArgoCD, Tekton,
        Jenkins notification configs.
      * `Basic <b64(team:RAW_TOKEN)>` — used by Bitbucket Data Center, which
        only emits auth when credentials are embedded in the webhook URL
        (`https://team:token@host/...`). The username is informational; the
        password is the same raw team token Bearer accepts. A mismatch
        between the claimed username and the team resolved from the token
        is logged as a warning (not a 401) so a typo'd webhook URL surfaces
        without dropping deliveries.

    The dependency:
      * 401s on missing / malformed Authorization header
      * 401s on unknown / wrong credentials
      * returns the team name (str) on success
    """

    async def verify_team_bearer(
        authorization: str | None = Header(default=None),
    ) -> str:
        if not authorization:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Missing or malformed Authorization header.",
            )

        scheme, _, value = authorization.partition(" ")
        scheme_lower = scheme.lower()
        claimed_user: str | None = None

        if scheme_lower == "bearer":
            token = value.strip()
            if not token:
                raise HTTPException(
                    status_code=status.HTTP_401_UNAUTHORIZED,
                    detail="Missing or malformed Authorization header.",
                )
        elif scheme_lower == "basic":
            try:
                decoded = base64.b64decode(value.strip(), validate=True).decode("utf-8")
            except (binascii.Error, ValueError, UnicodeDecodeError) as exc:
                raise HTTPException(
                    status_code=status.HTTP_401_UNAUTHORIZED,
                    detail="Malformed Basic credentials.",
                ) from exc
            if ":" not in decoded:
                raise HTTPException(
                    status_code=status.HTTP_401_UNAUTHORIZED,
                    detail="Malformed Basic credentials.",
                )
            claimed_user, _, token = decoded.partition(":")
        else:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Missing or malformed Authorization header.",
            )

        team_keys.maybe_reload()
        team = team_keys.lookup(token)
        if team is None:
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Invalid credentials.",
            )

        if claimed_user and claimed_user != team:
            logger.warning(
                "basic_auth_user_team_mismatch",
                claimed=claimed_user,
                resolved=team,
            )

        return team

    return verify_team_bearer
