"""Auth-focused tests: per-source bearer + strict source binding."""

from __future__ import annotations

import base64

from httpx import AsyncClient
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from _keys import (
    CHECKOUT_ARGOCD,
    CHECKOUT_BITBUCKET,
    CHECKOUT_JENKINS,
    CHECKOUT_NOERGLER,
    PLATFORM_JENKINS,
)
from riptide_collector.models import PipelineEvent

_PAYLOAD = {
    "source": "tekton",
    "pipeline_name": "demo-deploy",
    "run_id": "r1",
    "phase": "COMPLETED",
    "status": "SUCCESS",
    "commit_sha": "abc1234567890abc1234567890abc1234567890a",
    "started_at": "2026-04-29T18:00:00Z",
    "finished_at": "2026-04-29T18:01:00Z",
}


def _bearer(raw: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {raw}"}


class TestAuthHappyPath:
    async def test_jenkins_token_records_team_checkout(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory
        body = dict(_PAYLOAD, run_id="checkout-run")
        response = await client.post(
            "/webhooks/pipeline",
            json=body,
            headers=_bearer(CHECKOUT_JENKINS),
        )
        assert response.status_code == 202

        factory = client._transport.app.state.session_factory  # type: ignore[attr-defined]
        async with factory() as session:
            row = (
                await session.execute(
                    select(PipelineEvent).where(PipelineEvent.run_id == "checkout-run")
                )
            ).scalar_one()
            assert row.team == "checkout"

    async def test_platform_token_records_team_platform(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory
        body = dict(_PAYLOAD, run_id="platform-run")
        response = await client.post(
            "/webhooks/pipeline",
            json=body,
            headers=_bearer(PLATFORM_JENKINS),
        )
        assert response.status_code == 202

        factory = client._transport.app.state.session_factory  # type: ignore[attr-defined]
        async with factory() as session:
            row = (
                await session.execute(
                    select(PipelineEvent).where(PipelineEvent.run_id == "platform-run")
                )
            ).scalar_one()
            assert row.team == "platform"


class TestAuthRejections:
    async def test_no_header(self, client: AsyncClient) -> None:
        r = await client.post("/webhooks/pipeline", json=_PAYLOAD)
        assert r.status_code == 401

    async def test_unknown_token(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/pipeline",
            json=_PAYLOAD,
            headers=_bearer("not-a-real-key"),
        )
        assert r.status_code == 401

    async def test_basic_auth_rejected(self, client: AsyncClient) -> None:
        # Basic auth is no longer accepted on Bearer endpoints.
        encoded = base64.b64encode(f"checkout:{CHECKOUT_JENKINS}".encode()).decode("ascii")
        r = await client.post(
            "/webhooks/pipeline",
            json=_PAYLOAD,
            headers={"Authorization": f"Basic {encoded}"},
        )
        assert r.status_code == 401


class TestStrictSourceBinding:
    """A token registered under one source must NOT authenticate another."""

    async def test_argocd_token_rejected_on_pipeline(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/pipeline",
            json=_PAYLOAD,
            headers=_bearer(CHECKOUT_ARGOCD),
        )
        assert r.status_code == 401

    async def test_jenkins_token_rejected_on_argocd(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/argocd",
            json={
                "app_name": "x",
                "revision": "abc1234567890abc1234567890abc1234567890a",
            },
            headers=_bearer(CHECKOUT_JENKINS),
        )
        assert r.status_code == 401

    async def test_argocd_token_accepted_on_argocd(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/argocd",
            json={
                "app_name": "x",
                "revision": "abc1234567890abc1234567890abc1234567890a",
            },
            headers=_bearer(CHECKOUT_ARGOCD),
        )
        assert r.status_code == 202

    async def test_noergler_token_rejected_on_pipeline(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/pipeline",
            json=_PAYLOAD,
            headers=_bearer(CHECKOUT_NOERGLER),
        )
        assert r.status_code == 401

    async def test_bitbucket_secret_rejected_on_bearer_endpoint(self, client: AsyncClient) -> None:
        # The bitbucket HMAC secret is not a bearer token for any other endpoint.
        r = await client.post(
            "/webhooks/pipeline",
            json=_PAYLOAD,
            headers=_bearer(CHECKOUT_BITBUCKET),
        )
        assert r.status_code == 401


class TestPerEndpointAuth:
    async def test_argocd_unauth(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/argocd",
            json={"app_name": "x", "revision": "abc1234"},
        )
        assert r.status_code == 401

    async def test_health_does_not_require_auth(self, client: AsyncClient) -> None:
        r = await client.get("/health")
        assert r.status_code == 200

    async def test_ready_does_not_require_auth(self, client: AsyncClient) -> None:
        r = await client.get("/ready")
        assert r.status_code == 200
