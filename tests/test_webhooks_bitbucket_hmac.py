"""Bitbucket DC webhook auth: HMAC over X-Hub-Signature.

The endpoint is `POST /webhooks/bitbucket/{team}` — team identity comes
from the URL, signature comes from `X-Hub-Signature: sha256=<hex>`,
secret is the team's `bitbucket` entry in team-keys.json.
"""

from __future__ import annotations

import hashlib
import hmac
import json
from typing import Any

from httpx import AsyncClient
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from _keys import (
    CHECKOUT_BITBUCKET,
    CHECKOUT_JENKINS,
    PLATFORM_BITBUCKET,
)
from riptide_collector.models import BitbucketEvent

_BODY: dict[str, Any] = {
    "repository": {"full_name": "acme/payments-api"},
    "pullrequest": {
        "id": 1,
        "title": "feat: ABC-1 add x",
        "source": {
            "branch": {"name": "feature/ABC-1-add-x"},
            "commit": {"hash": "deadbeef" * 5},
        },
    },
    "date": "2026-04-29T10:00:00Z",
}


def _sign(secret: str, body: bytes) -> str:
    return "sha256=" + hmac.new(secret.encode("utf-8"), body, hashlib.sha256).hexdigest()


def _raw(payload: Any) -> bytes:
    return json.dumps(payload).encode("utf-8")


class TestHMACHappyPath:
    async def test_valid_signature_inserts_with_team_from_path(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory
        body = _raw(_BODY)
        r = await client.post(
            "/webhooks/bitbucket/checkout",
            content=body,
            headers={
                "Content-Type": "application/json",
                "X-Hub-Signature": _sign(CHECKOUT_BITBUCKET, body),
                "X-Request-UUID": "hmac-ok",
            },
        )
        assert r.status_code == 202

        factory = client._transport.app.state.session_factory  # type: ignore[attr-defined]
        async with factory() as session:
            row = (await session.execute(select(BitbucketEvent))).scalar_one()
            assert row.team == "checkout"
            assert row.repo_full_name == "acme/payments-api"

    async def test_signature_isolated_per_team(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        # Sign with team A's secret, post to team B's URL → 401.
        del session_factory
        body = _raw(_BODY)
        r = await client.post(
            "/webhooks/bitbucket/platform",
            content=body,
            headers={
                "Content-Type": "application/json",
                "X-Hub-Signature": _sign(CHECKOUT_BITBUCKET, body),
            },
        )
        assert r.status_code == 401

        # Sign with team B's secret, post to team B's URL → 202.
        r = await client.post(
            "/webhooks/bitbucket/platform",
            content=body,
            headers={
                "Content-Type": "application/json",
                "X-Hub-Signature": _sign(PLATFORM_BITBUCKET, body),
                "X-Request-UUID": "platform-ok",
            },
        )
        assert r.status_code == 202


class TestHMACRejections:
    async def test_missing_signature_header(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/bitbucket/checkout",
            content=_raw(_BODY),
            headers={"Content-Type": "application/json"},
        )
        assert r.status_code == 401

    async def test_malformed_signature_header(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/bitbucket/checkout",
            content=_raw(_BODY),
            headers={
                "Content-Type": "application/json",
                "X-Hub-Signature": "deadbeef",  # missing sha256= prefix
            },
        )
        assert r.status_code == 401

    async def test_wrong_algorithm_prefix(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/bitbucket/checkout",
            content=_raw(_BODY),
            headers={
                "Content-Type": "application/json",
                "X-Hub-Signature": "sha1=" + "0" * 40,
            },
        )
        assert r.status_code == 401

    async def test_signature_over_tampered_body(self, client: AsyncClient) -> None:
        body = _raw(_BODY)
        sig = _sign(CHECKOUT_BITBUCKET, body)
        # Body BBS would deliver vs body we signed differ by one byte.
        tampered = body + b" "
        r = await client.post(
            "/webhooks/bitbucket/checkout",
            content=tampered,
            headers={
                "Content-Type": "application/json",
                "X-Hub-Signature": sig,
            },
        )
        assert r.status_code == 401

    async def test_unknown_team_in_path(self, client: AsyncClient) -> None:
        body = _raw(_BODY)
        r = await client.post(
            "/webhooks/bitbucket/ghost",
            content=body,
            headers={
                "Content-Type": "application/json",
                "X-Hub-Signature": _sign(CHECKOUT_BITBUCKET, body),
            },
        )
        assert r.status_code == 401

    async def test_bearer_jenkins_token_does_not_pass_hmac(self, client: AsyncClient) -> None:
        # The team's jenkins bearer is not the bitbucket secret; signing
        # with it must fail HMAC even on the team's own URL.
        body = _raw(_BODY)
        r = await client.post(
            "/webhooks/bitbucket/checkout",
            content=body,
            headers={
                "Content-Type": "application/json",
                "X-Hub-Signature": _sign(CHECKOUT_JENKINS, body),
            },
        )
        assert r.status_code == 401
