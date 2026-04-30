import json
from pathlib import Path
from typing import Any

import pytest
from httpx import AsyncClient
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from _keys import CHECKOUT_TOKEN
from riptide_collector.models import ArgoCDEvent, BitbucketEvent, PipelineEvent

FIXTURES = Path(__file__).parent / "fixtures"
AUTH = {"Authorization": f"Bearer {CHECKOUT_TOKEN}"}


def _load(name: str) -> dict[str, Any]:
    return json.loads((FIXTURES / name).read_text())


class TestAuth:
    async def test_missing_authorization_returns_401(self, client: AsyncClient) -> None:
        response = await client.post("/webhooks/bitbucket", json={})
        assert response.status_code == 401

    async def test_unknown_token_returns_401(self, client: AsyncClient) -> None:
        response = await client.post(
            "/webhooks/bitbucket",
            json={},
            headers={"Authorization": "Bearer nope"},
        )
        assert response.status_code == 401

    async def test_malformed_header_returns_401(self, client: AsyncClient) -> None:
        response = await client.post(
            "/webhooks/bitbucket",
            json={},
            headers={"Authorization": CHECKOUT_TOKEN},
        )
        assert response.status_code == 401


class TestBitbucketWebhook:
    async def test_pr_merged_inserted_with_team_from_caller(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory  # the client uses its own factory; we query via a fresh one
        payload = _load("bitbucket_pr_merged.json")
        response = await client.post(
            "/webhooks/bitbucket",
            json=payload,
            headers={**AUTH, "X-Request-UUID": "uuid-1", "X-Event-Key": "pullrequest:fulfilled"},
        )
        assert response.status_code == 202

        async with self._fresh_session_factory(client)() as session:
            row = (await session.execute(select(BitbucketEvent))).scalar_one()
            assert row.team == "checkout"
            assert row.repo_full_name == "acme/payments-api"
            assert row.change_type == "feature"
            assert "ABC-123" in row.jira_keys
            assert "PROJ-9" in row.jira_keys
            assert row.lines_added == 120
            assert row.is_automated is False
            assert row.automation_source is None

    async def test_renovate_pr_tagged_as_automated(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory
        payload = _load("bitbucket_renovate_pr.json")
        response = await client.post(
            "/webhooks/bitbucket",
            json=payload,
            headers={**AUTH, "X-Request-UUID": "uuid-r"},
        )
        assert response.status_code == 202

        async with self._fresh_session_factory(client)() as session:
            row = (await session.execute(select(BitbucketEvent))).scalar_one()
            assert row.automation_source == "renovate"
            assert row.is_automated is True

    async def test_idempotency_same_uuid_inserts_once(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory
        payload = _load("bitbucket_pr_merged.json")
        headers = {**AUTH, "X-Request-UUID": "dup-1"}
        r1 = await client.post("/webhooks/bitbucket", json=payload, headers=headers)
        r2 = await client.post("/webhooks/bitbucket", json=payload, headers=headers)
        assert r1.status_code == 202
        assert r2.status_code == 202

        async with self._fresh_session_factory(client)() as session:
            count = (await session.execute(select(BitbucketEvent))).all()
            assert len(count) == 1

    async def test_uppercase_repo_normalised_to_lowercase(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory
        payload = _load("bitbucket_pr_merged.json")
        payload["repository"]["full_name"] = "ACME/Payments-API"
        response = await client.post(
            "/webhooks/bitbucket",
            json=payload,
            headers={**AUTH, "X-Request-UUID": "uuid-upper"},
        )
        assert response.status_code == 202

        async with self._fresh_session_factory(client)() as session:
            row = (await session.execute(select(BitbucketEvent))).scalar_one()
            assert row.repo_full_name == "acme/payments-api"

    async def test_unknown_repo_still_recorded_with_caller_team(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory
        payload = _load("bitbucket_pr_merged.json")
        payload["repository"]["full_name"] = "ghost/repo"
        response = await client.post(
            "/webhooks/bitbucket",
            json=payload,
            headers={**AUTH, "X-Request-UUID": "ghost-1"},
        )
        assert response.status_code == 202

        async with self._fresh_session_factory(client)() as session:
            row = (await session.execute(select(BitbucketEvent))).scalar_one()
            assert row.repo_full_name == "ghost/repo"
            assert row.team == "checkout"

    @staticmethod
    def _fresh_session_factory(client: AsyncClient) -> async_sessionmaker[AsyncSession]:
        # Reach into the running app's session factory.
        transport = client._transport  # type: ignore[attr-defined]
        return transport.app.state.session_factory  # type: ignore[no-any-return]


class TestPipelineWebhook:
    async def test_jenkins_payload_inserted(self, client: AsyncClient) -> None:
        payload = _load("pipeline_jenkins_completed.json")
        response = await client.post(
            "/webhooks/pipeline",
            json=payload,
            headers=AUTH,
        )
        assert response.status_code == 202

        factory = TestBitbucketWebhook._fresh_session_factory(client)
        async with factory() as session:
            row = (await session.execute(select(PipelineEvent))).scalar_one()
            assert row.team == "checkout"
            assert row.pipeline_name == "payments-api-deploy"
            assert row.source == "jenkins"
            assert row.status == "SUCCESS"
            assert row.duration_seconds == 210

    async def test_tekton_payload_inserted(self, client: AsyncClient) -> None:
        payload = _load("pipeline_tekton_completed.json")
        response = await client.post(
            "/webhooks/pipeline",
            json=payload,
            headers=AUTH,
        )
        assert response.status_code == 202

        factory = TestBitbucketWebhook._fresh_session_factory(client)
        async with factory() as session:
            row = (await session.execute(select(PipelineEvent))).scalar_one()
            assert row.source == "tekton"
            assert row.run_id == "payments-api-deploy-7gx2k"
            assert row.pipeline_name == "payments-api-deploy"

    async def test_uppercase_commit_sha_normalised_to_lowercase(self, client: AsyncClient) -> None:
        payload = _load("pipeline_jenkins_completed.json")
        payload["commit_sha"] = payload["commit_sha"].upper()
        response = await client.post("/webhooks/pipeline", json=payload, headers=AUTH)
        assert response.status_code == 202

        factory = TestBitbucketWebhook._fresh_session_factory(client)
        async with factory() as session:
            row = (await session.execute(select(PipelineEvent))).scalar_one()
            assert row.commit_sha is not None
            assert row.commit_sha == row.commit_sha.lower()

    async def test_jenkins_and_tekton_same_name_dedup_separately(self, client: AsyncClient) -> None:
        # Same pipeline_name + run_id but different sources must both insert.
        jenkins = _load("pipeline_jenkins_completed.json")
        tekton = _load("pipeline_tekton_completed.json")
        tekton["run_id"] = jenkins["run_id"]  # force collision attempt
        r1 = await client.post("/webhooks/pipeline", json=jenkins, headers=AUTH)
        r2 = await client.post("/webhooks/pipeline", json=tekton, headers=AUTH)
        assert r1.status_code == r2.status_code == 202

        factory = TestBitbucketWebhook._fresh_session_factory(client)
        async with factory() as session:
            rows = (await session.execute(select(PipelineEvent))).all()
            assert len(rows) == 2

    async def test_missing_required_field_returns_422(self, client: AsyncClient) -> None:
        payload = _load("pipeline_jenkins_completed.json")
        del payload["commit_sha"]
        response = await client.post("/webhooks/pipeline", json=payload, headers=AUTH)
        assert response.status_code == 422


class TestArgoCDWebhook:
    async def test_valid_payload_inserted(self, client: AsyncClient) -> None:
        payload = _load("argocd_synced.json")
        response = await client.post(
            "/webhooks/argocd",
            json=payload,
            headers=AUTH,
        )
        assert response.status_code == 202

        factory = TestBitbucketWebhook._fresh_session_factory(client)
        async with factory() as session:
            row = (await session.execute(select(ArgoCDEvent))).scalar_one()
            assert row.team == "checkout"
            assert row.app_name == "payments-api-prod"
            assert row.operation_phase == "Succeeded"
            assert row.duration_seconds == 45
            assert row.destination_namespace == "payments-prod"
            assert row.environment == "prod"

    async def test_missing_destination_namespace_yields_null_environment(
        self, client: AsyncClient
    ) -> None:
        payload = _load("argocd_synced.json")
        del payload["destination_namespace"]
        response = await client.post("/webhooks/argocd", json=payload, headers=AUTH)
        assert response.status_code == 202

        factory = TestBitbucketWebhook._fresh_session_factory(client)
        async with factory() as session:
            row = (await session.execute(select(ArgoCDEvent))).scalar_one()
            assert row.destination_namespace is None
            assert row.environment is None

    async def test_environment_extracted_from_namespace_suffix(
        self, client: AsyncClient
    ) -> None:
        payload = _load("argocd_synced.json")
        payload["destination_namespace"] = "checkout-intg"
        response = await client.post("/webhooks/argocd", json=payload, headers=AUTH)
        assert response.status_code == 202

        factory = TestBitbucketWebhook._fresh_session_factory(client)
        async with factory() as session:
            row = (await session.execute(select(ArgoCDEvent))).scalar_one()
            assert row.environment == "intg"

    async def test_uppercase_revision_normalised_to_lowercase(self, client: AsyncClient) -> None:
        payload = _load("argocd_synced.json")
        # commit SHAs sometimes arrive uppercase from non-git tools
        payload["revision"] = payload["revision"].upper()
        response = await client.post("/webhooks/argocd", json=payload, headers=AUTH)
        assert response.status_code == 202

        factory = TestBitbucketWebhook._fresh_session_factory(client)
        async with factory() as session:
            row = (await session.execute(select(ArgoCDEvent))).scalar_one()
            assert row.revision == row.revision.lower()

    async def test_missing_revision_returns_422(self, client: AsyncClient) -> None:
        payload = _load("argocd_synced.json")
        del payload["revision"]
        response = await client.post("/webhooks/argocd", json=payload, headers=AUTH)
        assert response.status_code == 422


@pytest.mark.parametrize(
    "endpoint,payload",
    [
        ("/webhooks/pipeline", {"pipeline_name": "x"}),
        ("/webhooks/argocd", {"app_name": "x"}),
    ],
)
async def test_invalid_schemas_return_422(
    client: AsyncClient, endpoint: str, payload: dict[str, Any]
) -> None:
    response = await client.post(endpoint, json=payload, headers=AUTH)
    assert response.status_code == 422
