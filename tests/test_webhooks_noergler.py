"""Tests for /webhooks/noergler and /auth/ping."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from httpx import AsyncClient
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from _keys import CHECKOUT_TOKEN, PLATFORM_TOKEN
from riptide_collector.models import NoerglerEvent

FIXTURES = Path(__file__).parent / "fixtures"
AUTH = {"Authorization": f"Bearer {CHECKOUT_TOKEN}"}


def _load(name: str) -> dict[str, Any]:
    return json.loads((FIXTURES / name).read_text(encoding="utf-8"))


def _fresh_session_factory(client: AsyncClient) -> async_sessionmaker[AsyncSession]:
    return client._transport.app.state.session_factory  # type: ignore[attr-defined]


class TestAuthPing:
    async def test_returns_team_for_valid_bearer(self, client: AsyncClient) -> None:
        r = await client.get("/auth/ping", headers=AUTH)
        assert r.status_code == 200
        body = r.json()
        assert body["status"] == "ok"
        assert body["team"] == "checkout"

    async def test_platform_bearer_resolves_to_platform(self, client: AsyncClient) -> None:
        r = await client.get("/auth/ping", headers={"Authorization": f"Bearer {PLATFORM_TOKEN}"})
        assert r.status_code == 200
        assert r.json()["team"] == "platform"

    async def test_no_bearer_returns_401(self, client: AsyncClient) -> None:
        r = await client.get("/auth/ping")
        assert r.status_code == 401

    async def test_unknown_bearer_returns_401(self, client: AsyncClient) -> None:
        r = await client.get("/auth/ping", headers={"Authorization": "Bearer not-a-real-key"})
        assert r.status_code == 401

    async def test_non_bearer_scheme_returns_401(self, client: AsyncClient) -> None:
        # Must reject Basic / token / raw — only "Bearer <token>" is valid.
        r = await client.get("/auth/ping", headers={"Authorization": f"Basic {CHECKOUT_TOKEN}"})
        assert r.status_code == 401


class TestAuthRejection:
    async def test_missing_bearer(self, client: AsyncClient) -> None:
        r = await client.post("/webhooks/noergler", json=_load("noergler_completed.json"))
        assert r.status_code == 401

    async def test_unknown_bearer(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/noergler",
            json=_load("noergler_completed.json"),
            headers={"Authorization": "Bearer wrong"},
        )
        assert r.status_code == 401


class TestCompletedEvent:
    async def test_records_finops_with_caller_team(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory
        payload = _load("noergler_completed.json")
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 202

        async with _fresh_session_factory(client)() as session:
            row = (await session.execute(select(NoerglerEvent))).scalar_one()
            assert row.event_type == "completed"
            assert row.team == "checkout"
            assert row.repo == "acme/payments-api"
            # pr_key lowercased for stable cross-source joins.
            assert row.pr_key == "proj/payments-api#42"
            assert row.run_id == "run-2026-04-29-00001"
            assert row.model == "gpt-4o-2024-08-06"
            assert row.prompt_tokens == 12345
            assert row.completion_tokens == 678
            assert row.findings_count == 3
            assert row.cost_usd is not None
            assert float(row.cost_usd) == 0.1245
            assert row.commit_sha == "abc1234567890abc1234567890abc1234567890a"

    async def test_idempotent_preserves_original_row(self, client: AsyncClient) -> None:
        # Catches a regression to ON CONFLICT DO UPDATE: the second insert
        # must be a no-op, not replace the original row's id / created_at.
        payload = _load("noergler_completed.json")
        r1 = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r1.status_code == 202

        async with _fresh_session_factory(client)() as session:
            first = (await session.execute(select(NoerglerEvent))).scalar_one()
            first_id = first.id
            first_created = first.created_at

        r2 = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r2.status_code == 202

        async with _fresh_session_factory(client)() as session:
            rows = (await session.execute(select(NoerglerEvent))).scalars().all()
            assert len(rows) == 1
            assert rows[0].id == first_id
            assert rows[0].created_at == first_created

    async def test_naive_finished_at_is_normalised_to_utc(self, client: AsyncClient) -> None:
        # If the sender omits the trailing Z / offset, we must still record
        # the timestamp as UTC rather than reject the payload or treat it
        # as local time.
        payload = _load("noergler_completed.json")
        payload["finished_at"] = "2026-04-29T18:01:00"  # no offset, no Z
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 202

        async with _fresh_session_factory(client)() as session:
            row = (await session.execute(select(NoerglerEvent))).scalar_one()
            assert row.occurred_at.tzinfo is not None
            assert row.occurred_at.utcoffset().total_seconds() == 0  # type: ignore[union-attr]

    async def test_negative_tokens_rejected(self, client: AsyncClient) -> None:
        payload = _load("noergler_completed.json")
        payload["prompt_tokens"] = -1
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 422


class TestFeedbackEvent:
    async def test_records_disagreement(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory
        payload = _load("noergler_feedback.json")
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 202

        async with _fresh_session_factory(client)() as session:
            row = (await session.execute(select(NoerglerEvent))).scalar_one()
            assert row.event_type == "feedback"
            assert row.team == "checkout"
            assert row.verdict == "disagreed"
            # actor lowercased (the fixture sends mixed case)
            assert row.actor == "alice@example.com"
            assert row.finding_id == "finding-2026-04-29-0001"
            # commit_sha lowercased; allows joining feedback to deployments
            assert row.commit_sha == "abc1234567890abc1234567890abc1234567890a"
            # finops columns are unset for feedback
            assert row.model is None
            assert row.cost_usd is None

    async def test_commit_sha_optional_on_feedback(self, client: AsyncClient) -> None:
        # Feedback without a commit_sha is accepted — reviewer-precision can
        # still be aggregated by team/repo/week.
        payload = _load("noergler_feedback.json")
        del payload["commit_sha"]
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 202

        async with _fresh_session_factory(client)() as session:
            row = (await session.execute(select(NoerglerEvent))).scalar_one()
            assert row.commit_sha is None

    async def test_idempotent_per_verdict(self, client: AsyncClient) -> None:
        payload = _load("noergler_feedback.json")
        r1 = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        r2 = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r1.status_code == r2.status_code == 202

        async with _fresh_session_factory(client)() as session:
            rows = (await session.execute(select(NoerglerEvent))).all()
            assert len(rows) == 1

    async def test_disagreed_then_acknowledged_produces_two_rows(self, client: AsyncClient) -> None:
        payload = _load("noergler_feedback.json")
        r1 = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        flipped = {**payload, "verdict": "acknowledged"}
        r2 = await client.post("/webhooks/noergler", json=flipped, headers=AUTH)
        assert r1.status_code == r2.status_code == 202

        async with _fresh_session_factory(client)() as session:
            rows = (await session.execute(select(NoerglerEvent))).all()
            assert len(rows) == 2

    async def test_invalid_verdict_rejected(self, client: AsyncClient) -> None:
        payload = _load("noergler_feedback.json")
        payload["verdict"] = "shrug"
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 422


class TestDiscriminator:
    async def test_unknown_event_type_rejected(self, client: AsyncClient) -> None:
        payload = {**_load("noergler_completed.json"), "event_type": "exploded"}
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 422

    async def test_unknown_field_rejected(self, client: AsyncClient) -> None:
        # extra="forbid": a sender typo (e.g. cost_used instead of cost_usd)
        # must 422 instead of silently landing in `payload` JSONB.
        payload = {**_load("noergler_completed.json"), "definitely_not_a_field": "x"}
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 422
