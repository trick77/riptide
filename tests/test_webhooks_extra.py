"""Extra coverage for less-trodden bitbucket webhook paths."""

from typing import Any

from httpx import AsyncClient
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from riptide_collector.models import BitbucketEvent
from test_webhooks import (
    ARGOCD_AUTH,
    PIPELINE_AUTH,
    TestBitbucketWebhook,
    _load,
    post_bitbucket,
)


async def test_push_event_records_branch_and_commit(
    client: AsyncClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    # is_revert detection on push is deferred — BBS DC payloads don't
    # carry commit messages, so we'd need a REST round-trip to look them
    # up. For now push events land with is_revert=False; this test just
    # verifies the basic branch/commit/author extraction off the BBS DC
    # `changes[]` shape.
    del session_factory
    payload = _load("bitbucket_refs_changed.json")
    response = await post_bitbucket(client, payload, extra_headers={"X-Request-UUID": "push-1"})
    assert response.status_code == 202

    factory = TestBitbucketWebhook._fresh_session_factory(client)
    async with factory() as session:
        row = (await session.execute(select(BitbucketEvent))).scalar_one()
        assert row.repo_full_name == "acme/payments-api"
        assert row.author == "alice"
        assert row.branch_name == "master"
        assert row.commit_sha == "feedface" * 5
        assert row.is_revert is False


async def test_non_object_payload_ignored(client: AsyncClient) -> None:
    response = await post_bitbucket(
        client, ["not", "an", "object"], extra_headers={"X-Request-UUID": "list-1"}
    )
    assert response.status_code == 202
    assert response.json()["status"] == "ignored"


async def test_argocd_phase_transitions_create_distinct_rows(
    client: AsyncClient,
) -> None:
    base = {
        "app_name": "payments-api-prod",
        "revision": "abc1234567890abc1234567890abc1234567890a",
        "sync_status": "Synced",
        "started_at": "2026-04-28T10:09:00Z",
    }
    running = {**base, "operation_phase": "Running"}
    succeeded = {**base, "operation_phase": "Succeeded", "finished_at": "2026-04-28T10:09:45Z"}

    r1 = await client.post("/webhooks/argocd", json=running, headers=ARGOCD_AUTH)
    r2 = await client.post("/webhooks/argocd", json=succeeded, headers=ARGOCD_AUTH)
    r3 = await client.post("/webhooks/argocd", json=succeeded, headers=ARGOCD_AUTH)
    assert r1.status_code == r2.status_code == r3.status_code == 202

    from riptide_collector.models import ArgoCDEvent

    factory = TestBitbucketWebhook._fresh_session_factory(client)
    async with factory() as session:
        rows = (await session.execute(select(ArgoCDEvent))).all()
        # Two phases (Running + Succeeded), retry of Succeeded is deduped.
        assert len(rows) == 2


async def test_pipeline_naive_timestamps_normalised_to_utc(
    client: AsyncClient,
) -> None:
    payload = {
        "source": "jenkins",
        "pipeline_name": "payments-api-deploy",
        "run_id": "999",
        "phase": "COMPLETED",
        "status": "SUCCESS",
        "commit_sha": "abc1234567890abc1234567890abc1234567890a",
        # No 'Z', no offset — naive — must be coerced to UTC, not rejected.
        "started_at": "2026-04-28T10:00:00",
        "finished_at": "2026-04-28T10:01:00",
    }
    response = await client.post("/webhooks/pipeline", json=payload, headers=PIPELINE_AUTH)
    assert response.status_code == 202

    from riptide_collector.models import PipelineEvent

    factory = TestBitbucketWebhook._fresh_session_factory(client)
    async with factory() as session:
        row = (await session.execute(select(PipelineEvent))).scalar_one()
        assert row.started_at is not None
        assert row.started_at.tzinfo is not None
        assert row.duration_seconds == 60


async def test_synth_delivery_id_when_no_uuid(
    client: AsyncClient,
) -> None:
    payload: dict[str, Any] = {
        "eventKey": "pr:opened",
        "date": "2026-04-28T13:00:00+0000",
        "actor": {"name": "alice"},
        "pullRequest": {
            "id": 7,
            "title": "no headers",
            "fromRef": {
                "displayId": "feature/x",
                "latestCommit": "abc1234567890abc1234567890abc1234567890a",
                "repository": {
                    "slug": "payments-api",
                    "project": {"key": "ACME"},
                },
            },
            "toRef": {
                "displayId": "master",
                "repository": {
                    "slug": "payments-api",
                    "project": {"key": "ACME"},
                },
            },
            "author": {"user": {"name": "alice"}},
        },
    }
    r1 = await post_bitbucket(client, payload)
    r2 = await post_bitbucket(client, payload)
    assert r1.status_code == 202
    assert r2.status_code == 202

    factory = TestBitbucketWebhook._fresh_session_factory(client)
    async with factory() as session:
        rows = (await session.execute(select(BitbucketEvent))).all()
        assert len(rows) == 1  # synthetic delivery id dedupes
