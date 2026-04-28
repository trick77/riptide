import hmac

from fastapi import Header, HTTPException, status


def make_bearer_dependency(expected_token: str):
    async def verify_bearer_token(
        authorization: str | None = Header(default=None),
    ) -> None:
        if not authorization or not authorization.lower().startswith("bearer "):
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Missing or malformed Authorization header.",
            )
        token = authorization.split(None, 1)[1].strip()
        if not hmac.compare_digest(token, expected_token):
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Invalid bearer token.",
            )

    return verify_bearer_token
