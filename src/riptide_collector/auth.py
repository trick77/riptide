import hashlib
import hmac
from collections.abc import Awaitable, Callable

from fastapi import Header, HTTPException, Path, Request, status

from riptide_collector.logging_config import get_logger
from riptide_collector.team_keys import KNOWN_SOURCES, TeamKeysStore

logger = get_logger(__name__)


def _verify_hmac_sha256(secret: str, body: bytes, header: str | None) -> bool:
    """Validate `X-Hub-Signature: sha256=<hex>` against `body`.

    Constant-time compare via `hmac.compare_digest`. Rejects missing or
    malformed headers up front so the digest comparison only runs on a
    structurally-valid header — never on user-controlled junk of
    arbitrary length.
    """
    if not header:
        return False
    prefix, _, hex_sig = header.partition("=")
    if prefix.lower() != "sha256" or not hex_sig:
        return False
    expected = hmac.new(secret.encode("utf-8"), body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(hex_sig.lower(), expected.lower())


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


def make_hmac_dependency(
    team_keys: TeamKeysStore,
    source: str,
) -> Callable[..., Awaitable[bytes]]:
    """Build a FastAPI dependency that authenticates an HMAC-signed webhook.

    Mirrors `make_team_bearer_dependency` for sources that sign requests
    with `X-Hub-Signature: sha256=<hex>` (currently Bitbucket DC) instead
    of carrying a Bearer token. The dependency:

    1. Reads the raw request bytes once (HMAC must verify the exact bytes
       the sender signed — re-serialising JSON would break the digest).
    2. Looks up the per-team secret for `source`.
    3. Validates the signature in constant time, including a dummy-key
       fallback for unknown teams so the rejection path takes the same
       wall-time regardless of whether the team exists. Without this an
       attacker could enumerate team names by timing.
    4. Returns the raw bytes on success so the handler can `json.loads`
       them once. FastAPI caches `await request.body()` so re-reading in
       the handler would also work, but plumbing the bytes through the
       dependency keeps the handler from worrying about it.
    """
    if source not in KNOWN_SOURCES:
        raise ValueError(f"unknown source {source!r}; allowed: {sorted(KNOWN_SOURCES)}")

    async def verify_hmac_signature(
        request: Request,
        team: str = Path(..., min_length=1),
        x_hub_signature: str | None = Header(default=None),
    ) -> bytes:
        raw = await request.body()
        team_keys.maybe_reload()
        secret = team_keys.get_secret(team, source)
        verify_secret = secret if secret is not None else "\x00" * 32
        signature_ok = _verify_hmac_sha256(verify_secret, raw, x_hub_signature)
        if secret is None or not signature_ok:
            logger.warning(
                f"{source}_hmac_rejected",
                team=team,
                has_secret=secret is not None,
                has_signature=bool(x_hub_signature),
            )
            raise HTTPException(
                status_code=status.HTTP_401_UNAUTHORIZED,
                detail="Invalid signature.",
            )
        return raw

    return verify_hmac_signature
