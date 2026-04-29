"""Auth-focused tests: per-team bearer wiring on the live FastAPI app."""

from __future__ import annotations

from httpx import AsyncClient
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from conftest import CHECKOUT_TOKEN, PLATFORM_TOKEN
from riptide_collector.models import PipelineEvent

# Minimal pipeline payload that satisfies the schema.
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
    async def test_checkout_token_records_team_checkout(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory
        body = dict(_PAYLOAD, run_id="checkout-run")
        response = await client.post(
            "/webhooks/pipeline",
            json=body,
            headers=_bearer(CHECKOUT_TOKEN),
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
            headers=_bearer(PLATFORM_TOKEN),
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

    async def test_basic_auth_scheme_rejected(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/pipeline",
            json=_PAYLOAD,
            headers={"Authorization": f"Basic {CHECKOUT_TOKEN}"},
        )
        assert r.status_code == 401


class TestPerEndpointAuth:
    """Make sure every protected endpoint applies the same auth dependency."""

    async def test_argocd_unauth(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/argocd",
            json={"app_name": "x", "revision": "abc1234"},
        )
        assert r.status_code == 401

    async def test_bitbucket_unauth(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/bitbucket",
            json={},
        )
        assert r.status_code == 401

    async def test_health_does_not_require_auth(self, client: AsyncClient) -> None:
        r = await client.get("/health")
        assert r.status_code == 200

    async def test_ready_does_not_require_auth(self, client: AsyncClient) -> None:
        r = await client.get("/ready")
        assert r.status_code == 200
