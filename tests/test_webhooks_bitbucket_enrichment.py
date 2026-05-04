"""Bitbucket PR diff-stat enrichment: background-task path on `pr:merged`.

The webhook returns 202 immediately; a FastAPI BackgroundTask then
calls BBS DC for the PR's diff stats and UPDATEs the row. With
httpx.ASGITransport the test client waits for background tasks to
complete before returning, so we can assert the row state directly
after the POST.
"""

from __future__ import annotations

from typing import Any

import pytest
from httpx import AsyncClient
from sqlalchemy import select

from _keys import CHECKOUT_BITBUCKET_API
from riptide_collector.bbs_client import BitbucketClient, DiffStats, PushCommitStats
from riptide_collector.models import BitbucketEvent
from riptide_collector.routers import bitbucket as bitbucket_router
from test_webhooks import _load, post_bitbucket


@pytest.fixture(autouse=True)
def _enable_bbs_callbacks(monkeypatch: pytest.MonkeyPatch) -> None:  # pyright: ignore[reportUnusedFunction]
    """The router disables BBS DC outbound callbacks via a module-level
    flag. These tests verify the enrichment wiring is correct, so they
    flip it back on for their scope. When the operator re-enables the
    callbacks in production, this file's expectations match reality.
    """
    monkeypatch.setattr(bitbucket_router, "_BBS_CALLBACKS_DISABLED", False)


def _state_of(client: AsyncClient) -> Any:
    return client._transport.app.state  # type: ignore[attr-defined]


def _patch_fetch(
    monkeypatch: pytest.MonkeyPatch,
    result: DiffStats | None,
    captured: dict[str, Any] | None = None,
) -> None:
    """Replace BitbucketClient.fetch_pr_diff_stats with a stub.

    Capturing the call lets the test assert the right project/slug/pr_id
    were threaded through without poking at internal state.
    """

    async def _stub(
        self: BitbucketClient,
        project_key: str,
        slug: str,
        pr_id: int,
        token: str,
    ) -> DiffStats | None:
        del self
        if captured is not None:
            captured["project_key"] = project_key
            captured["slug"] = slug
            captured["pr_id"] = pr_id
            captured["token"] = token
        return result

    monkeypatch.setattr(BitbucketClient, "fetch_pr_diff_stats", _stub)


async def test_pr_merged_enriches_row_with_diff_stats(
    client_with_bbs_enrichment: AsyncClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    captured: dict[str, Any] = {}
    _patch_fetch(
        monkeypatch,
        DiffStats(lines_added=120, lines_removed=15, files_changed=3),
        captured,
    )

    payload = _load("bitbucket_pr_merged.json")
    response = await post_bitbucket(
        client_with_bbs_enrichment,
        payload,
        extra_headers={"X-Request-UUID": "merge-1", "X-Event-Key": "pr:merged"},
    )
    assert response.status_code == 202

    factory = _state_of(client_with_bbs_enrichment).session_factory
    async with factory() as session:
        row = (await session.execute(select(BitbucketEvent))).scalar_one()
        assert row.lines_added == 120
        assert row.lines_removed == 15
        assert row.files_changed == 3

    # Project key + slug come from the payload's raw casing (the BBS API
    # path is case-sensitive); the join column is lowercased separately.
    assert captured["project_key"] == "ACME"
    assert captured["slug"] == "payments-api"
    assert captured["pr_id"] == 42
    assert captured["token"] == CHECKOUT_BITBUCKET_API


async def test_pr_opened_does_not_trigger_enrichment(
    client_with_bbs_enrichment: AsyncClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls = 0

    async def _stub(
        self: BitbucketClient,
        project_key: str,
        slug: str,
        pr_id: int,
        token: str,
    ) -> DiffStats | None:
        nonlocal calls
        del self, project_key, slug, pr_id, token
        calls += 1
        return DiffStats(0, 0, 0)

    monkeypatch.setattr(BitbucketClient, "fetch_pr_diff_stats", _stub)

    payload = _load("bitbucket_pr_merged.json")
    response = await post_bitbucket(
        client_with_bbs_enrichment,
        payload,
        extra_headers={"X-Request-UUID": "open-1", "X-Event-Key": "pr:opened"},
    )
    assert response.status_code == 202
    assert calls == 0

    factory = _state_of(client_with_bbs_enrichment).session_factory
    async with factory() as session:
        row = (await session.execute(select(BitbucketEvent))).scalar_one()
        assert row.lines_added is None


async def test_push_event_does_not_trigger_enrichment(
    client_with_bbs_enrichment: AsyncClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls = 0

    async def _stub(*_args: Any, **_kwargs: Any) -> DiffStats | None:
        nonlocal calls
        calls += 1
        return None

    monkeypatch.setattr(BitbucketClient, "fetch_pr_diff_stats", _stub)

    payload = _load("bitbucket_refs_changed.json")
    response = await post_bitbucket(
        client_with_bbs_enrichment,
        payload,
        extra_headers={"X-Request-UUID": "push-1", "X-Event-Key": "repo:refs_changed"},
    )
    assert response.status_code == 202
    assert calls == 0


async def test_truncated_or_failed_fetch_leaves_row_null(
    client_with_bbs_enrichment: AsyncClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _patch_fetch(monkeypatch, None)

    payload = _load("bitbucket_pr_merged.json")
    response = await post_bitbucket(
        client_with_bbs_enrichment,
        payload,
        extra_headers={"X-Request-UUID": "merge-fail", "X-Event-Key": "pr:merged"},
    )
    assert response.status_code == 202

    factory = _state_of(client_with_bbs_enrichment).session_factory
    async with factory() as session:
        row = (await session.execute(select(BitbucketEvent))).scalar_one()
        assert row.lines_added is None
        assert row.lines_removed is None
        assert row.files_changed is None


async def test_pr_merged_for_team_without_api_token_logs_and_skips(
    client_with_bbs_enrichment: AsyncClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    # platform team has no `bitbucket_api` slot in TEAM_KEYS — fetch
    # should not be called and the row should keep NULL stats.
    from _keys import PLATFORM_BITBUCKET

    calls = 0

    async def _stub(*_args: Any, **_kwargs: Any) -> DiffStats | None:
        nonlocal calls
        calls += 1
        return DiffStats(1, 1, 1)

    monkeypatch.setattr(BitbucketClient, "fetch_pr_diff_stats", _stub)

    payload = _load("bitbucket_pr_merged.json")
    response = await post_bitbucket(
        client_with_bbs_enrichment,
        payload,
        team="platform",
        secret=PLATFORM_BITBUCKET,
        extra_headers={"X-Request-UUID": "no-token", "X-Event-Key": "pr:merged"},
    )
    assert response.status_code == 202
    assert calls == 0

    factory = _state_of(client_with_bbs_enrichment).session_factory
    async with factory() as session:
        row = (await session.execute(select(BitbucketEvent))).scalar_one()
        assert row.lines_added is None


async def test_enrichment_fetch_exception_does_not_break_request(
    client_with_bbs_enrichment: AsyncClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    async def _stub(*_args: Any, **_kwargs: Any) -> DiffStats | None:
        raise RuntimeError("simulated bug")

    monkeypatch.setattr(BitbucketClient, "fetch_pr_diff_stats", _stub)

    payload = _load("bitbucket_pr_merged.json")
    response = await post_bitbucket(
        client_with_bbs_enrichment,
        payload,
        extra_headers={"X-Request-UUID": "merge-boom", "X-Event-Key": "pr:merged"},
    )
    # Webhook still 202; row is still inserted; stats are still NULL.
    assert response.status_code == 202

    factory = _state_of(client_with_bbs_enrichment).session_factory
    async with factory() as session:
        row = (await session.execute(select(BitbucketEvent))).scalar_one()
        assert row.lines_added is None


async def test_pr_merged_with_path_traversal_in_slug_skips_fetch(
    client_with_bbs_enrichment: AsyncClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    # Defensive: a payload claiming a slug like "../admin" must not
    # produce an outbound URL. The router refuses to schedule the task
    # rather than trust httpx URL-encoding to neutralise the traversal.
    calls = 0

    async def _stub(*_args: Any, **_kwargs: Any) -> DiffStats | None:
        nonlocal calls
        calls += 1
        return DiffStats(0, 0, 0)

    monkeypatch.setattr(BitbucketClient, "fetch_pr_diff_stats", _stub)

    payload = _load("bitbucket_pr_merged.json")
    payload["pullRequest"]["toRef"]["repository"]["slug"] = "../admin"
    response = await post_bitbucket(
        client_with_bbs_enrichment,
        payload,
        extra_headers={"X-Request-UUID": "traversal", "X-Event-Key": "pr:merged"},
    )
    assert response.status_code == 202
    assert calls == 0


def _patch_push_fetch(
    monkeypatch: pytest.MonkeyPatch,
    result: PushCommitStats | None,
    captured: dict[str, Any] | None = None,
) -> None:
    async def _stub(
        self: BitbucketClient,
        project_key: str,
        slug: str,
        from_hash: str,
        to_hash: str,
        token: str,
    ) -> PushCommitStats | None:
        del self
        if captured is not None:
            captured["project_key"] = project_key
            captured["slug"] = slug
            captured["from_hash"] = from_hash
            captured["to_hash"] = to_hash
            captured["token"] = token
        return result

    monkeypatch.setattr(BitbucketClient, "fetch_push_commit_stats", _stub)


async def test_push_enriches_row_with_commit_counts(
    client_with_bbs_enrichment: AsyncClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    captured: dict[str, Any] = {}
    _patch_push_fetch(
        monkeypatch,
        PushCommitStats(commit_count=7, author_count=3),
        captured,
    )

    payload = _load("bitbucket_refs_changed.json")
    response = await post_bitbucket(
        client_with_bbs_enrichment,
        payload,
        extra_headers={"X-Request-UUID": "push-enrich", "X-Event-Key": "repo:refs_changed"},
    )
    assert response.status_code == 202

    factory = _state_of(client_with_bbs_enrichment).session_factory
    async with factory() as session:
        row = (await session.execute(select(BitbucketEvent))).scalar_one()
        assert row.push_commit_count == 7
        assert row.push_author_count == 3

    assert captured["project_key"] == "ACME"
    assert captured["slug"] == "payments-api"
    assert captured["from_hash"] == "1111111111111111111111111111111111111111"
    assert captured["to_hash"] == "feedfacefeedfacefeedfacefeedfacefeedface"
    assert captured["token"] == CHECKOUT_BITBUCKET_API


async def test_push_failed_fetch_leaves_columns_null(
    client_with_bbs_enrichment: AsyncClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _patch_push_fetch(monkeypatch, None)

    payload = _load("bitbucket_refs_changed.json")
    response = await post_bitbucket(
        client_with_bbs_enrichment,
        payload,
        extra_headers={"X-Request-UUID": "push-fail", "X-Event-Key": "repo:refs_changed"},
    )
    assert response.status_code == 202

    factory = _state_of(client_with_bbs_enrichment).session_factory
    async with factory() as session:
        row = (await session.execute(select(BitbucketEvent))).scalar_one()
        assert row.push_commit_count is None
        assert row.push_author_count is None


async def test_pr_merged_does_not_trigger_push_enrichment(
    client_with_bbs_enrichment: AsyncClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    push_calls = 0

    async def _push_stub(*_args: Any, **_kwargs: Any) -> PushCommitStats | None:
        nonlocal push_calls
        push_calls += 1
        return None

    monkeypatch.setattr(BitbucketClient, "fetch_push_commit_stats", _push_stub)
    _patch_fetch(monkeypatch, DiffStats(0, 0, 0))

    payload = _load("bitbucket_pr_merged.json")
    response = await post_bitbucket(
        client_with_bbs_enrichment,
        payload,
        extra_headers={"X-Request-UUID": "pr-only", "X-Event-Key": "pr:merged"},
    )
    assert response.status_code == 202
    assert push_calls == 0


async def test_pr_merged_without_bbs_client_keeps_stats_null(
    client: AsyncClient,
) -> None:
    # Default `client` fixture has no `bitbucket_base_url` configured,
    # so `app.state.bbs_client is None`. The router should silently
    # skip enrichment — the merge event still inserts cleanly.
    payload = _load("bitbucket_pr_merged.json")
    response = await post_bitbucket(
        client,
        payload,
        extra_headers={"X-Request-UUID": "no-bbs", "X-Event-Key": "pr:merged"},
    )
    assert response.status_code == 202

    factory = client._transport.app.state.session_factory  # type: ignore[attr-defined]
    async with factory() as session:
        row = (await session.execute(select(BitbucketEvent))).scalar_one()
        assert row.pr_id == 42
        assert row.lines_added is None
