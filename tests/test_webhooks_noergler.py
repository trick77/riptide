"""Tests for /webhooks/noergler and /auth/ping."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from httpx import AsyncClient
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from _keys import (
    CHECKOUT_ARGOCD,
    CHECKOUT_NOERGLER,
    PLATFORM_NOERGLER,
)
from riptide_collector.models import NoerglerEvent

FIXTURES = Path(__file__).parent / "fixtures"
AUTH = {"Authorization": f"Bearer {CHECKOUT_NOERGLER}"}


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
        r = await client.get("/auth/ping", headers={"Authorization": f"Bearer {PLATFORM_NOERGLER}"})
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
        r = await client.get("/auth/ping", headers={"Authorization": f"Basic {CHECKOUT_NOERGLER}"})
        assert r.status_code == 401

    async def test_any_source_token_accepted_on_ping(self, client: AsyncClient) -> None:
        # /auth/ping uses lookup_any_source — any per-source token works.
        r = await client.get("/auth/ping", headers={"Authorization": f"Bearer {CHECKOUT_ARGOCD}"})
        assert r.status_code == 200
        assert r.json()["team"] == "checkout"


class TestAuthRejection:
    async def test_missing_bearer(self, client: AsyncClient) -> None:
        r = await client.post("/webhooks/noergler", json=_load("noergler_pr_completed_merged.json"))
        assert r.status_code == 401

    async def test_unknown_bearer(self, client: AsyncClient) -> None:
        r = await client.post(
            "/webhooks/noergler",
            json=_load("noergler_pr_completed_merged.json"),
            headers={"Authorization": "Bearer wrong"},
        )
        assert r.status_code == 401


class TestPrCompletedMerged:
    async def test_records_rollup_with_caller_team(
        self,
        client: AsyncClient,
        session_factory: async_sessionmaker[AsyncSession],
    ) -> None:
        del session_factory
        payload = _load("noergler_pr_completed_merged.json")
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 202

        async with _fresh_session_factory(client)() as session:
            row = (await session.execute(select(NoerglerEvent))).scalar_one()
            assert row.event_type == "pr_completed"
            assert row.outcome == "merged"
            assert row.team == "checkout"
            assert row.repo == "acme/payments-api"
            # pr_key lowercased for stable cross-source joins.
            assert row.pr_key == "proj/payments-api#42"
            assert row.commit_sha == "abc1234567890abc1234567890abc1234567890a"
            assert row.merge_commit_sha == "def4567890abc1234567890abc1234567890abcd"
            assert row.lines_added == 320
            assert row.lines_removed == 75
            assert row.files_changed == 12
            assert row.total_runs == 3
            assert row.models_used == ["gpt-4o-2024-08-06"]
            assert row.prompt_tokens == 38420
            assert row.completion_tokens == 2110
            assert row.elapsed_ms == 24800
            assert row.findings_count == 7
            assert row.cost_usd is not None
            assert float(row.cost_usd) == 0.3821
            assert row.first_review_at is not None
            assert row.occurred_at is not None

    async def test_idempotent_preserves_original_row(self, client: AsyncClient) -> None:
        # Catches a regression to ON CONFLICT DO UPDATE: the second insert
        # must be a no-op, not replace the original row's id / created_at.
        payload = _load("noergler_pr_completed_merged.json")
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

    async def test_naive_closed_at_is_normalised_to_utc(self, client: AsyncClient) -> None:
        # If the sender omits the trailing Z / offset, we must still record
        # the timestamp as UTC rather than reject the payload or treat it
        # as local time.
        payload = _load("noergler_pr_completed_merged.json")
        payload["closed_at"] = "2026-04-29T18:42:00"  # no offset, no Z
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 202

        async with _fresh_session_factory(client)() as session:
            row = (await session.execute(select(NoerglerEvent))).scalar_one()
            assert row.occurred_at.tzinfo is not None
            assert row.occurred_at.utcoffset().total_seconds() == 0  # type: ignore[union-attr]

    async def test_negative_tokens_rejected(self, client: AsyncClient) -> None:
        payload = _load("noergler_pr_completed_merged.json")
        payload["total_prompt_tokens"] = -1
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 422

    async def test_total_runs_must_be_positive(self, client: AsyncClient) -> None:
        # total_runs = 0 means nothing was reviewed; the sender should not
        # have emitted a rollup at all. 422 catches this contract violation.
        payload = _load("noergler_pr_completed_merged.json")
        payload["total_runs"] = 0
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 422

    async def test_empty_models_used_rejected(self, client: AsyncClient) -> None:
        payload = _load("noergler_pr_completed_merged.json")
        payload["models_used"] = []
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 422

    async def test_merged_requires_merge_commit_sha(self, client: AsyncClient) -> None:
        # outcome='merged' without merge_commit_sha must fail loudly — every
        # real merge produces a commit, so a sender omitting it is a bug.
        payload = _load("noergler_pr_completed_merged.json")
        del payload["merge_commit_sha"]
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 422

    async def test_declined_with_merge_commit_sha_rejected(self, client: AsyncClient) -> None:
        # A declined / deleted PR cannot have a merge commit; surfacing the
        # inconsistency loudly catches sender bugs at the boundary.
        payload = _load("noergler_pr_completed_declined.json")
        payload["merge_commit_sha"] = "def4567890abc1234567890abc1234567890abcd"
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 422

    async def test_delivery_id_dedupes_across_pr_key_casing(self, client: AsyncClient) -> None:
        # pr_key is lowercased before hashing into delivery_id, so the same
        # PR redelivered with different casing must collapse to one row.
        payload = _load("noergler_pr_completed_merged.json")
        r1 = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        flipped = {**payload, "pr_key": payload["pr_key"].lower()}
        r2 = await client.post("/webhooks/noergler", json=flipped, headers=AUTH)
        assert r1.status_code == r2.status_code == 202

        async with _fresh_session_factory(client)() as session:
            rows = (await session.execute(select(NoerglerEvent))).scalars().all()
            assert len(rows) == 1


class TestPrCompletedNonMerged:
    async def test_declined_recorded_with_outcome(self, client: AsyncClient) -> None:
        payload = _load("noergler_pr_completed_declined.json")
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 202

        async with _fresh_session_factory(client)() as session:
            row = (await session.execute(select(NoerglerEvent))).scalar_one()
            assert row.outcome == "declined"
            # Declined PRs never produce a merge commit.
            assert row.merge_commit_sha is None
            # Cost was still incurred — FinOps must see it.
            assert float(row.cost_usd) == 0.042  # type: ignore[arg-type]

    async def test_deleted_recorded_with_outcome(self, client: AsyncClient) -> None:
        payload = _load("noergler_pr_completed_deleted.json")
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 202

        async with _fresh_session_factory(client)() as session:
            row = (await session.execute(select(NoerglerEvent))).scalar_one()
            assert row.outcome == "deleted"
            assert row.merge_commit_sha is None

    async def test_invalid_outcome_rejected(self, client: AsyncClient) -> None:
        payload = _load("noergler_pr_completed_merged.json")
        payload["outcome"] = "abandoned"
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 422

    async def test_idempotency_keyed_on_pr_and_outcome(self, client: AsyncClient) -> None:
        # Same pr_key but different outcomes must coexist as distinct rows
        # (declined-then-deleted is a real Bitbucket flow).
        declined = _load("noergler_pr_completed_declined.json")
        deleted = {
            **declined,
            "outcome": "deleted",
            "closed_at": "2026-04-30T10:00:00Z",
        }
        r1 = await client.post("/webhooks/noergler", json=declined, headers=AUTH)
        r2 = await client.post("/webhooks/noergler", json=deleted, headers=AUTH)
        assert r1.status_code == r2.status_code == 202

        async with _fresh_session_factory(client)() as session:
            rows = (await session.execute(select(NoerglerEvent))).scalars().all()
            assert len(rows) == 2
            assert {row.outcome for row in rows} == {"declined", "deleted"}


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
            assert row.cost_usd is None
            assert row.outcome is None

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
        payload = {
            **_load("noergler_pr_completed_merged.json"),
            "event_type": "exploded",
        }
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 422

    async def test_legacy_completed_event_type_rejected(self, client: AsyncClient) -> None:
        # The pre-rollup 'completed' event was removed without a back-compat
        # shim. A noergler still on the old contract must fail loudly.
        payload = {
            **_load("noergler_pr_completed_merged.json"),
            "event_type": "completed",
        }
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 422

    async def test_unknown_field_rejected(self, client: AsyncClient) -> None:
        # extra="forbid": a sender typo (e.g. cost_used instead of cost_usd)
        # must 422 instead of silently landing in `payload` JSONB.
        payload = {
            **_load("noergler_pr_completed_merged.json"),
            "definitely_not_a_field": "x",
        }
        r = await client.post("/webhooks/noergler", json=payload, headers=AUTH)
        assert r.status_code == 422
