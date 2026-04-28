"""Extra coverage for less-trodden bitbucket webhook paths."""

from typing import Any

from httpx import AsyncClient
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from riptide_collector.models import BitbucketEvent
from test_webhooks import AUTH, TestBitbucketWebhook


async def test_push_event_with_revert_commit(
    client: AsyncClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    del session_factory
    payload: dict[str, Any] = {
        "actor": {"nickname": "alice"},
        "repository": {"full_name": "acme/payments-api"},
        "push": {
            "changes": [
                {
                    "new": {
                        "name": "main",
                        "target": {
                            "hash": "feedface" * 5,
                            "message": 'Revert "feature: bad change"\n\nABC-99',
                        },
                    }
                }
            ]
        },
        "date": "2026-04-28T12:00:00Z",
    }
    response = await client.post(
        "/webhooks/bitbucket",
        json=payload,
        headers={**AUTH, "X-Request-UUID": "push-revert"},
    )
    assert response.status_code == 202

    factory = TestBitbucketWebhook._fresh_session_factory(client)
    async with factory() as session:
        row = (await session.execute(select(BitbucketEvent))).scalar_one()
        assert row.is_revert is True
        assert "ABC-99" in row.jira_keys
        assert row.author == "alice"
        assert row.branch_name == "main"


async def test_non_object_payload_ignored(client: AsyncClient) -> None:
    response = await client.post(
        "/webhooks/bitbucket",
        json=["not", "an", "object"],
        headers={**AUTH, "X-Request-UUID": "list-1"},
    )
    assert response.status_code == 202
    assert response.json()["status"] == "ignored"


async def test_synth_delivery_id_when_no_uuid(
    client: AsyncClient,
) -> None:
    payload: dict[str, Any] = {
        "repository": {"full_name": "acme/payments-api"},
        "pullrequest": {
            "id": 7,
            "title": "no headers",
            "source": {"branch": {"name": "feature/x"}},
        },
        "date": "2026-04-28T13:00:00Z",
    }
    r1 = await client.post("/webhooks/bitbucket", json=payload, headers=AUTH)
    r2 = await client.post("/webhooks/bitbucket", json=payload, headers=AUTH)
    assert r1.status_code == 202
    assert r2.status_code == 202

    factory = TestBitbucketWebhook._fresh_session_factory(client)
    async with factory() as session:
        rows = (await session.execute(select(BitbucketEvent))).all()
        assert len(rows) == 1  # synthetic delivery id dedupes
