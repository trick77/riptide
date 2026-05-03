"""Unit tests for the BBS DC outbound HTTP client."""

from __future__ import annotations

from typing import Any

import httpx
import pytest

from riptide_collector.bbs_client import BitbucketClient, DiffStats, parse_diff_stats


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
