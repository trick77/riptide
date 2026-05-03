import hashlib
import hmac
import json
from pathlib import Path
from typing import Any

import pytest
from httpx import AsyncClient
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from _keys import (
    CHECKOUT_ARGOCD,
    CHECKOUT_BITBUCKET,
    CHECKOUT_JENKINS,
)
from riptide_collector.models import ArgoCDEvent, BitbucketEvent, PipelineEvent

FIXTURES = Path(__file__).parent / "fixtures"
ARGOCD_AUTH = {"Authorization": f"Bearer {CHECKOUT_ARGOCD}"}
PIPELINE_AUTH = {"Authorization": f"Bearer {CHECKOUT_JENKINS}"}


def _load(name: str) -> dict[str, Any]:
    return json.loads((FIXTURES / name).read_text())


def _sign(secret: str, body: bytes) -> str:
    return "sha256=" + hmac.new(secret.encode("utf-8"), body, hashlib.sha256).hexdigest()


def post_bitbucket(
    client: AsyncClient,
    payload: Any,
    *,
    team: str = "checkout",
    secret: str = CHECKOUT_BITBUCKET,
    extra_headers: dict[str, str] | None = None,
):
    """Helper: serialise payload and POST to /webhooks/bitbucket/{team}
    with a valid X-Hub-Signature so tests don't have to repeat the
    signing dance.
    """
    raw = json.dumps(payload).encode("utf-8")
    headers = {
        "Content-Type": "application/json",
        "X-Hub-Signature": _sign(secret, raw),
    }
    if extra_headers:
        headers.update(extra_headers)
    return client.post(f"/webhooks/bitbucket/{team}", content=raw, headers=headers)


class TestBitbucketAuth:
    async def test_missing_signature_returns_401(self, client: AsyncClient) -> None:
        response = await client.post("/webhooks/bitbucket/checkout", json={})
        assert response.status_code == 401

    async def test_bad_signature_returns_401(self, client: AsyncClient) -> None:
        raw = b"{}"
        response = await client.post(
            "/webhooks/bitbucket/checkout",
            content=raw,
            headers={
                "Content-Type": "application/json",
                "X-Hub-Signature": "sha256=deadbeef",
            },
        )
        assert response.status_code == 401

    async def test_unknown_team_returns_401(self, client: AsyncClient) -> None:
        raw = b"{}"
        response = await client.post(
            "/webhooks/bitbucket/ghost",
            content=raw,
            headers={
                "Content-Type": "application/json",
                "X-Hub-Signature": _sign(CHECKOUT_BITBUCKET, raw),
            },
        )
        assert response.status_code == 401


class TestBitbucketWebhook:
    async def test_pr_merged_inserted_with_team_from_path(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory
        payload = _load("bitbucket_pr_merged.json")
        response = await post_bitbucket(
            client,
            payload,
            extra_headers={
                "X-Request-UUID": "uuid-1",
                "X-Event-Key": "pullrequest:fulfilled",
            },
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
        response = await post_bitbucket(client, payload, extra_headers={"X-Request-UUID": "uuid-r"})
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
        r1 = await post_bitbucket(client, payload, extra_headers={"X-Request-UUID": "dup-1"})
        r2 = await post_bitbucket(client, payload, extra_headers={"X-Request-UUID": "dup-1"})
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
        response = await post_bitbucket(
            client, payload, extra_headers={"X-Request-UUID": "uuid-upper"}
        )
        assert response.status_code == 202

        async with self._fresh_session_factory(client)() as session:
            row = (await session.execute(select(BitbucketEvent))).scalar_one()
            assert row.repo_full_name == "acme/payments-api"

    async def test_unknown_repo_still_recorded_with_team_from_path(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory
        payload = _load("bitbucket_pr_merged.json")
        payload["repository"]["full_name"] = "ghost/repo"
        response = await post_bitbucket(
            client, payload, extra_headers={"X-Request-UUID": "ghost-1"}
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
            headers=PIPELINE_AUTH,
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
            headers=PIPELINE_AUTH,
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
        response = await client.post("/webhooks/pipeline", json=payload, headers=PIPELINE_AUTH)
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
        r1 = await client.post("/webhooks/pipeline", json=jenkins, headers=PIPELINE_AUTH)
        r2 = await client.post("/webhooks/pipeline", json=tekton, headers=PIPELINE_AUTH)
        assert r1.status_code == r2.status_code == 202

        factory = TestBitbucketWebhook._fresh_session_factory(client)
        async with factory() as session:
            rows = (await session.execute(select(PipelineEvent))).all()
            assert len(rows) == 2

    async def test_missing_required_field_returns_422(self, client: AsyncClient) -> None:
        payload = _load("pipeline_jenkins_completed.json")
        del payload["commit_sha"]
        response = await client.post("/webhooks/pipeline", json=payload, headers=PIPELINE_AUTH)
        assert response.status_code == 422


class TestArgoCDWebhook:
    async def test_valid_payload_inserted(self, client: AsyncClient) -> None:
        payload = _load("argocd_synced.json")
        response = await client.post(
            "/webhooks/argocd",
            json=payload,
            headers=ARGOCD_AUTH,
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
        response = await client.post("/webhooks/argocd", json=payload, headers=ARGOCD_AUTH)
        assert response.status_code == 202

        factory = TestBitbucketWebhook._fresh_session_factory(client)
        async with factory() as session:
            row = (await session.execute(select(ArgoCDEvent))).scalar_one()
            assert row.destination_namespace is None
            assert row.environment is None

    async def test_environment_extracted_from_namespace_suffix(self, client: AsyncClient) -> None:
        payload = _load("argocd_synced.json")
        payload["destination_namespace"] = "checkout-intg"
        response = await client.post("/webhooks/argocd", json=payload, headers=ARGOCD_AUTH)
        assert response.status_code == 202

        factory = TestBitbucketWebhook._fresh_session_factory(client)
        async with factory() as session:
            row = (await session.execute(select(ArgoCDEvent))).scalar_one()
            assert row.environment == "intg"

    async def test_uppercase_revision_normalised_to_lowercase(self, client: AsyncClient) -> None:
        payload = _load("argocd_synced.json")
        # commit SHAs sometimes arrive uppercase from non-git tools
        payload["revision"] = payload["revision"].upper()
        response = await client.post("/webhooks/argocd", json=payload, headers=ARGOCD_AUTH)
        assert response.status_code == 202

        factory = TestBitbucketWebhook._fresh_session_factory(client)
        async with factory() as session:
            row = (await session.execute(select(ArgoCDEvent))).scalar_one()
            assert row.revision == row.revision.lower()

    async def test_missing_revision_returns_422(self, client: AsyncClient) -> None:
        payload = _load("argocd_synced.json")
        del payload["revision"]
        response = await client.post("/webhooks/argocd", json=payload, headers=ARGOCD_AUTH)
        assert response.status_code == 422

    async def test_ignored_stage_is_dropped(self, client_with_ignored_stages: AsyncClient) -> None:
        payload = _load("argocd_synced.json")
        payload["destination_namespace"] = "checkout-dev"
        response = await client_with_ignored_stages.post(
            "/webhooks/argocd", json=payload, headers=ARGOCD_AUTH
        )
        assert response.status_code == 202
        assert response.json() == {"status": "ignored"}

        factory = TestBitbucketWebhook._fresh_session_factory(client_with_ignored_stages)
        async with factory() as session:
            rows = (await session.execute(select(ArgoCDEvent))).all()
            assert rows == []

    async def test_non_ignored_stage_still_inserts(
        self, client_with_ignored_stages: AsyncClient
    ) -> None:
        payload = _load("argocd_synced.json")
        payload["destination_namespace"] = "checkout-prod"
        response = await client_with_ignored_stages.post(
            "/webhooks/argocd", json=payload, headers=ARGOCD_AUTH
        )
        assert response.status_code == 202
        assert response.json() == {"status": "accepted"}

        factory = TestBitbucketWebhook._fresh_session_factory(client_with_ignored_stages)
        async with factory() as session:
            row = (await session.execute(select(ArgoCDEvent))).scalar_one()
            assert row.environment == "prod"


@pytest.mark.parametrize(
    "endpoint,payload,auth",
    [
        ("/webhooks/pipeline", {"pipeline_name": "x"}, PIPELINE_AUTH),
        ("/webhooks/argocd", {"app_name": "x"}, ARGOCD_AUTH),
    ],
)
async def test_invalid_schemas_return_422(
    client: AsyncClient,
    endpoint: str,
    payload: dict[str, Any],
    auth: dict[str, str],
) -> None:
    response = await client.post(endpoint, json=payload, headers=auth)
    assert response.status_code == 422
