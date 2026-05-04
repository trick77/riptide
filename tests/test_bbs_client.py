"""Unit tests for the BBS DC outbound HTTP client."""

from __future__ import annotations

import asyncio
from typing import Any

import httpx
import pytest

from riptide_collector import bbs_client as bbs_client_module
from riptide_collector.bbs_client import (
    BitbucketClient,
    DiffStats,
    PushCommitStats,
    parse_diff_stats,
    parse_push_commit_stats,
)


def _segment(seg_type: str, line_count: int) -> dict[str, Any]:
    return {"type": seg_type, "lines": [{"line": str(i)} for i in range(line_count)]}


class TestParseDiffStats:
    def test_single_file_added_and_removed(self) -> None:
        payload = {
            "diffs": [
                {
                    "source": {"toString": "src/foo.py"},
                    "destination": {"toString": "src/foo.py"},
                    "hunks": [
                        {
                            "segments": [
                                _segment("ADDED", 10),
                                _segment("CONTEXT", 5),
                                _segment("REMOVED", 3),
                            ]
                        }
                    ],
                }
            ],
            "truncated": False,
        }
        assert parse_diff_stats(payload) == DiffStats(
            lines_added=10, lines_removed=3, files_changed=1
        )

    def test_multi_file_multi_hunk(self) -> None:
        payload = {
            "diffs": [
                {
                    "hunks": [
                        {"segments": [_segment("ADDED", 4)]},
                        {"segments": [_segment("ADDED", 6), _segment("REMOVED", 2)]},
                    ]
                },
                {"hunks": [{"segments": [_segment("REMOVED", 9)]}]},
                {"hunks": []},  # rename / mode-only — counts as a file, no lines
            ]
        }
        assert parse_diff_stats(payload) == DiffStats(
            lines_added=10, lines_removed=11, files_changed=3
        )

    def test_truncated_returns_none(self) -> None:
        payload = {
            "diffs": [{"hunks": [{"segments": [_segment("ADDED", 1)]}]}],
            "truncated": True,
        }
        assert parse_diff_stats(payload) is None

    def test_empty_diff(self) -> None:
        assert parse_diff_stats({"diffs": [], "truncated": False}) == DiffStats(0, 0, 0)

    def test_missing_fields_treated_as_empty(self) -> None:
        # BBS DC sometimes omits fields entirely; the parser must not crash.
        assert parse_diff_stats({}) == DiffStats(0, 0, 0)
        assert parse_diff_stats(None) == DiffStats(0, 0, 0)
        assert parse_diff_stats({"diffs": "not-a-list"}) == DiffStats(0, 0, 0)

    def test_unknown_segment_type_ignored(self) -> None:
        # Future BBS versions might add segment types — they should be
        # counted as neither added nor removed.
        payload = {
            "diffs": [
                {
                    "hunks": [
                        {
                            "segments": [
                                _segment("ADDED", 2),
                                _segment("FUTURE_THING", 100),
                            ]
                        }
                    ]
                }
            ]
        }
        assert parse_diff_stats(payload) == DiffStats(
            lines_added=2, lines_removed=0, files_changed=1
        )


class TestBitbucketClientHTTP:
    @staticmethod
    def _client(handler: httpx.MockTransport) -> BitbucketClient:
        return BitbucketClient(
            base_url="https://bbs.example.com",
            client=httpx.AsyncClient(transport=handler, timeout=5.0),
        )

    async def test_success_returns_stats(self) -> None:
        captured: dict[str, Any] = {}

        def handler(request: httpx.Request) -> httpx.Response:
            captured["url"] = str(request.url)
            captured["auth"] = request.headers.get("Authorization")
            return httpx.Response(
                200,
                json={
                    "diffs": [{"hunks": [{"segments": [_segment("ADDED", 7)]}]}],
                    "truncated": False,
                },
            )

        client = self._client(httpx.MockTransport(handler))
        try:
            stats = await client.fetch_pr_diff_stats("ACME", "payments-api", 42, "tok")
        finally:
            await client.aclose()

        assert stats == DiffStats(lines_added=7, lines_removed=0, files_changed=1)
        assert "ACME/repos/payments-api/pull-requests/42/diff" in captured["url"]
        assert "contextLines=0" in captured["url"]
        assert captured["auth"] == "Bearer tok"

    async def test_non_200_returns_none(self) -> None:
        client = self._client(httpx.MockTransport(lambda _r: httpx.Response(404)))
        try:
            assert await client.fetch_pr_diff_stats("ACME", "missing", 1, "tok") is None
        finally:
            await client.aclose()

    async def test_network_error_returns_none(self) -> None:
        def handler(_r: httpx.Request) -> httpx.Response:
            raise httpx.ConnectError("boom")

        client = self._client(httpx.MockTransport(handler))
        try:
            assert await client.fetch_pr_diff_stats("ACME", "payments-api", 1, "tok") is None
        finally:
            await client.aclose()

    async def test_truncated_returns_none(self) -> None:
        client = self._client(
            httpx.MockTransport(
                lambda _r: httpx.Response(200, json={"diffs": [], "truncated": True})
            )
        )
        try:
            assert await client.fetch_pr_diff_stats("ACME", "payments-api", 1, "tok") is None
        finally:
            await client.aclose()

    async def test_non_json_response_returns_none(self) -> None:
        client = self._client(
            httpx.MockTransport(lambda _r: httpx.Response(200, content=b"not json"))
        )
        try:
            assert await client.fetch_pr_diff_stats("ACME", "payments-api", 1, "tok") is None
        finally:
            await client.aclose()


@pytest.mark.parametrize(
    "base_url,expected_prefix",
    [
        ("https://bbs.example.com", "https://bbs.example.com/rest/api/latest"),
        ("https://bbs.example.com/", "https://bbs.example.com/rest/api/latest"),
    ],
)
async def test_base_url_trailing_slash_stripped(base_url: str, expected_prefix: str) -> None:
    captured: dict[str, str] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["url"] = str(request.url)
        return httpx.Response(200, json={"diffs": [], "truncated": False})

    client = BitbucketClient(
        base_url=base_url,
        client=httpx.AsyncClient(transport=httpx.MockTransport(handler)),
    )
    try:
        await client.fetch_pr_diff_stats("ACME", "payments-api", 1, "tok")
    finally:
        await client.aclose()
    assert captured["url"].startswith(expected_prefix)


def _build_client(
    handler: httpx.MockTransport,
    *,
    max_concurrency: int = 1,
    rate_per_second: float = 1000.0,
    burst: float = 1000.0,
    max_attempts: int = 3,
    backoff_base_seconds: float = 0.5,
    backoff_cap_seconds: float = 8.0,
) -> BitbucketClient:
    return BitbucketClient(
        base_url="https://bbs.example.com",
        client=httpx.AsyncClient(transport=handler, timeout=5.0),
        max_concurrency=max_concurrency,
        rate_per_second=rate_per_second,
        burst=burst,
        max_attempts=max_attempts,
        backoff_base_seconds=backoff_base_seconds,
        backoff_cap_seconds=backoff_cap_seconds,
    )


class TestRetry:
    async def test_retries_on_503_then_succeeds(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        sleeps: list[float] = []

        async def fake_sleep(seconds: float) -> None:
            sleeps.append(seconds)

        monkeypatch.setattr(bbs_client_module.asyncio, "sleep", fake_sleep)

        calls = {"n": 0}

        def handler(_r: httpx.Request) -> httpx.Response:
            calls["n"] += 1
            if calls["n"] == 1:
                return httpx.Response(503)
            return httpx.Response(
                200,
                json={
                    "diffs": [{"hunks": [{"segments": [_segment("ADDED", 3)]}]}],
                    "truncated": False,
                },
            )

        client = _build_client(httpx.MockTransport(handler))
        try:
            stats = await client.fetch_pr_diff_stats("ACME", "r", 1, "tok")
        finally:
            await client.aclose()

        assert stats == DiffStats(lines_added=3, lines_removed=0, files_changed=1)
        assert calls["n"] == 2
        assert len(sleeps) == 1

    async def test_gives_up_after_max_attempts(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        async def fake_sleep(_seconds: float) -> None:
            return None

        monkeypatch.setattr(bbs_client_module.asyncio, "sleep", fake_sleep)

        calls = {"n": 0}

        def handler(_r: httpx.Request) -> httpx.Response:
            calls["n"] += 1
            return httpx.Response(503)

        client = _build_client(httpx.MockTransport(handler), max_attempts=3)
        try:
            assert await client.fetch_pr_diff_stats("ACME", "r", 1, "tok") is None
        finally:
            await client.aclose()
        assert calls["n"] == 3

    async def test_does_not_retry_on_404(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        async def fake_sleep(_seconds: float) -> None:
            return None

        monkeypatch.setattr(bbs_client_module.asyncio, "sleep", fake_sleep)

        calls = {"n": 0}

        def handler(_r: httpx.Request) -> httpx.Response:
            calls["n"] += 1
            return httpx.Response(404)

        client = _build_client(httpx.MockTransport(handler))
        try:
            assert await client.fetch_pr_diff_stats("ACME", "r", 1, "tok") is None
        finally:
            await client.aclose()
        assert calls["n"] == 1

    async def test_retries_on_network_error_then_succeeds(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        async def fake_sleep(_seconds: float) -> None:
            return None

        monkeypatch.setattr(bbs_client_module.asyncio, "sleep", fake_sleep)

        calls = {"n": 0}

        def handler(_r: httpx.Request) -> httpx.Response:
            calls["n"] += 1
            if calls["n"] == 1:
                raise httpx.ConnectError("boom")
            return httpx.Response(200, json={"diffs": [], "truncated": False})

        client = _build_client(httpx.MockTransport(handler))
        try:
            stats = await client.fetch_pr_diff_stats("ACME", "r", 1, "tok")
        finally:
            await client.aclose()
        assert stats == DiffStats(0, 0, 0)
        assert calls["n"] == 2

    async def test_honors_retry_after_seconds(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        sleeps: list[float] = []

        async def fake_sleep(seconds: float) -> None:
            sleeps.append(seconds)

        monkeypatch.setattr(bbs_client_module.asyncio, "sleep", fake_sleep)

        calls = {"n": 0}

        def handler(_r: httpx.Request) -> httpx.Response:
            calls["n"] += 1
            if calls["n"] == 1:
                return httpx.Response(429, headers={"Retry-After": "1"})
            return httpx.Response(200, json={"diffs": [], "truncated": False})

        client = _build_client(httpx.MockTransport(handler))
        try:
            await client.fetch_pr_diff_stats("ACME", "r", 1, "tok")
        finally:
            await client.aclose()

        assert sleeps == [1.0]


class TestSemaphore:
    async def test_caps_in_flight_requests(self) -> None:
        gate = asyncio.Event()
        in_flight = 0
        peak = 0
        lock = asyncio.Lock()

        async def slow_handler(_r: httpx.Request) -> httpx.Response:
            nonlocal in_flight, peak
            async with lock:
                in_flight += 1
                peak = max(peak, in_flight)
            await gate.wait()
            async with lock:
                in_flight -= 1
            return httpx.Response(200, json={"diffs": [], "truncated": False})

        # MockTransport supports an async handler.
        client = _build_client(
            httpx.MockTransport(slow_handler),
            max_concurrency=2,
        )
        try:
            tasks = [
                asyncio.create_task(
                    client.fetch_pr_diff_stats("ACME", "r", i, "tok")
                )
                for i in range(5)
            ]
            # Let tasks accumulate at the semaphore.
            for _ in range(20):
                await asyncio.sleep(0)
            assert peak <= 2
            gate.set()
            await asyncio.gather(*tasks)
        finally:
            await client.aclose()

        assert peak == 2


class TestTokenBucket:
    async def test_sustained_rate_sleeps(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        # Fake monotonic clock that only advances when asyncio.sleep is called.
        now = {"t": 0.0}
        sleeps: list[float] = []

        def fake_now() -> float:
            return now["t"]

        async def fake_sleep(seconds: float) -> None:
            sleeps.append(seconds)
            now["t"] += seconds

        monkeypatch.setattr(
            bbs_client_module._TokenBucket, "_now", staticmethod(fake_now)
        )
        monkeypatch.setattr(bbs_client_module.asyncio, "sleep", fake_sleep)

        def handler(_r: httpx.Request) -> httpx.Response:
            return httpx.Response(200, json={"diffs": [], "truncated": False})

        # rate=2/s, burst=2 → first 2 calls are free, next 2 wait 0.5s each.
        client = _build_client(
            httpx.MockTransport(handler),
            rate_per_second=2.0,
            burst=2.0,
        )
        try:
            for i in range(4):
                await client.fetch_pr_diff_stats("ACME", "r", i, "tok")
        finally:
            await client.aclose()

        assert sum(sleeps) >= 1.0 - 1e-9
        # Two free, two delayed by 1/rate each.
        assert len([s for s in sleeps if s > 0]) == 2

    async def test_refills_after_idle(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        now = {"t": 0.0}
        sleeps: list[float] = []

        def fake_now() -> float:
            return now["t"]

        async def fake_sleep(seconds: float) -> None:
            sleeps.append(seconds)
            now["t"] += seconds

        monkeypatch.setattr(
            bbs_client_module._TokenBucket, "_now", staticmethod(fake_now)
        )
        monkeypatch.setattr(bbs_client_module.asyncio, "sleep", fake_sleep)

        def handler(_r: httpx.Request) -> httpx.Response:
            return httpx.Response(200, json={"diffs": [], "truncated": False})

        client = _build_client(
            httpx.MockTransport(handler),
            rate_per_second=2.0,
            burst=1.0,
        )
        try:
            # Drain initial token.
            await client.fetch_pr_diff_stats("ACME", "r", 1, "tok")
            sleeps_before = list(sleeps)
            # Advance the fake clock past one refill interval.
            now["t"] += 10.0
            await client.fetch_pr_diff_stats("ACME", "r", 2, "tok")
        finally:
            await client.aclose()

        # No new sleep was added by the second call.
        assert sleeps == sleeps_before


def _commit(name: str) -> dict[str, Any]:
    return {"id": f"sha-{name}", "author": {"name": name}}


class TestParsePushCommitStats:
    def test_counts_commits_and_distinct_authors(self) -> None:
        payload = {
            "values": [_commit("alice"), _commit("bob"), _commit("alice")],
        }
        assert parse_push_commit_stats(payload) == PushCommitStats(
            commit_count=3, author_count=2
        )

    def test_authors_normalised_lowercase(self) -> None:
        payload = {"values": [_commit("Alice"), _commit("ALICE")]}
        assert parse_push_commit_stats(payload) == PushCommitStats(
            commit_count=2, author_count=1
        )

    def test_empty_values(self) -> None:
        assert parse_push_commit_stats({"values": []}) == PushCommitStats(0, 0)

    def test_missing_values_key_returns_none(self) -> None:
        assert parse_push_commit_stats({}) is None
        assert parse_push_commit_stats(None) is None

    def test_missing_author_handle_skipped(self) -> None:
        payload = {
            "values": [
                {"id": "1", "author": {}},  # no name
                {"id": "2"},  # no author
                _commit("alice"),
            ]
        }
        assert parse_push_commit_stats(payload) == PushCommitStats(
            commit_count=3, author_count=1
        )


class TestFetchPushCommitStats:
    @staticmethod
    def _client(handler: httpx.MockTransport) -> BitbucketClient:
        return BitbucketClient(
            base_url="https://bbs.example.com",
            client=httpx.AsyncClient(transport=handler, timeout=5.0),
        )

    async def test_success(self) -> None:
        captured: dict[str, Any] = {}

        def handler(request: httpx.Request) -> httpx.Response:
            captured["url"] = str(request.url)
            captured["auth"] = request.headers.get("Authorization")
            return httpx.Response(
                200,
                json={
                    "values": [_commit("alice"), _commit("bob")],
                },
            )

        client = self._client(httpx.MockTransport(handler))
        try:
            stats = await client.fetch_push_commit_stats(
                "ACME", "payments-api", "deadbeef", "cafef00d", "tok"
            )
        finally:
            await client.aclose()

        assert stats == PushCommitStats(commit_count=2, author_count=2)
        assert "ACME/repos/payments-api/commits" in captured["url"]
        assert "since=deadbeef" in captured["url"]
        assert "until=cafef00d" in captured["url"]
        assert "limit=1000" in captured["url"]
        assert captured["auth"] == "Bearer tok"

    async def test_empty_values_returns_zero(self) -> None:
        client = self._client(
            httpx.MockTransport(lambda _r: httpx.Response(200, json={"values": []}))
        )
        try:
            stats = await client.fetch_push_commit_stats(
                "ACME", "r", "a", "b", "tok"
            )
        finally:
            await client.aclose()
        assert stats == PushCommitStats(0, 0)

    async def test_404_returns_none(self) -> None:
        client = self._client(httpx.MockTransport(lambda _r: httpx.Response(404)))
        try:
            stats = await client.fetch_push_commit_stats(
                "ACME", "missing", "a", "b", "tok"
            )
        finally:
            await client.aclose()
        assert stats is None

    async def test_network_error_returns_none(
        self, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        async def fake_sleep(_seconds: float) -> None:
            return None

        monkeypatch.setattr(bbs_client_module.asyncio, "sleep", fake_sleep)

        def handler(_r: httpx.Request) -> httpx.Response:
            raise httpx.ConnectError("boom")

        client = self._client(httpx.MockTransport(handler))
        try:
            stats = await client.fetch_push_commit_stats(
                "ACME", "r", "a", "b", "tok"
            )
        finally:
            await client.aclose()
        assert stats is None

    async def test_non_json_returns_none(self) -> None:
        client = self._client(
            httpx.MockTransport(lambda _r: httpx.Response(200, content=b"not json"))
        )
        try:
            stats = await client.fetch_push_commit_stats(
                "ACME", "r", "a", "b", "tok"
            )
        finally:
            await client.aclose()
        assert stats is None

    async def test_malformed_payload_returns_none(self) -> None:
        # 200 OK but no `values` key — same shape as a non-commits
        # endpoint mistakenly hit.
        client = self._client(
            httpx.MockTransport(
                lambda _r: httpx.Response(200, json={"errors": ["nope"]})
            )
        )
        try:
            stats = await client.fetch_push_commit_stats(
                "ACME", "r", "a", "b", "tok"
            )
        finally:
            await client.aclose()
        assert stats is None
