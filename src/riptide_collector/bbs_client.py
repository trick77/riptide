"""Outbound HTTP client for Bitbucket Data Center.

The collector calls back into BBS DC for one thing today: per-PR diff
stats (lines added / removed, files changed) on `pr:merged` events,
because those numbers don't ride on the webhook payload. The same
client is the foundation for the deferred push-side `is_revert`
detection (commit messages between fromHash..toHash).

Endpoint choice
---------------
`GET /rest/api/latest/projects/{key}/repos/{slug}/pull-requests/{id}/diff?contextLines=0`

Returns a structured tree:

    {
      "diffs": [
        {
          "source": {"toString": "src/foo.py"},
          "destination": {"toString": "src/foo.py"},
          "hunks": [
            { "segments": [
                {"type": "ADDED",   "lines": [...]},
                {"type": "REMOVED", "lines": [...]},
                {"type": "CONTEXT", "lines": [...]}
            ]}
          ]
        },
        ...
      ],
      "truncated": false
    }

Lines added / removed = total `len(segment.lines)` over segments with
`type == "ADDED"` / `"REMOVED"` across all hunks across all diffs.
Files changed = number of entries in `diffs[]`. Pure-rename / mode-only
changes lack `hunks` but still appear as a `diffs[]` entry, so they
contribute to `files_changed` with zero line count — matches Bitbucket
UI behaviour.

`truncated: true` is treated as a soft failure: the row keeps NULL.
Falling back to paginated `/changes` summation would work but adds a
second code path; we'd rather see the gap and decide later than ship a
fallback that masks BBS-side limits silently.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

import httpx

from riptide_collector.logging_config import get_logger

logger = get_logger(__name__)

DEFAULT_TIMEOUT_SECONDS = 10.0


@dataclass(frozen=True, slots=True)
class DiffStats:
    lines_added: int
    lines_removed: int
    files_changed: int


def _as_dict(value: Any) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _as_list(value: Any) -> list[Any]:
    return value if isinstance(value, list) else []


def parse_diff_stats(payload: Any) -> DiffStats | None:
    """Parse a BBS DC `/pull-requests/{id}/diff` JSON response.

    Returns None if the response signals truncation (BBS skipped some
    files) — the caller should treat that as "no data" rather than
    record partial counts.
    """
    body = _as_dict(payload)
    if body.get("truncated") is True:
        return None
    diffs = _as_list(body.get("diffs"))
    added = 0
    removed = 0
    for diff in diffs:
        for hunk in _as_list(_as_dict(diff).get("hunks")):
            for segment in _as_list(_as_dict(hunk).get("segments")):
                seg = _as_dict(segment)
                seg_type = seg.get("type")
                if seg_type not in ("ADDED", "REMOVED"):
                    continue
                line_count = len(_as_list(seg.get("lines")))
                if seg_type == "ADDED":
                    added += line_count
                else:
                    removed += line_count
    return DiffStats(lines_added=added, lines_removed=removed, files_changed=len(diffs))


class BitbucketClient:
    """Async HTTP client for BBS DC. Holds one shared httpx.AsyncClient.

    Single attempt per call, fixed timeout, no retries — webhooks have
    already been 202'd by the time we call out. A failed enrichment
    just leaves the row's stats NULL and emits a structured log.
    """

    def __init__(
        self,
        base_url: str,
        timeout_seconds: float = DEFAULT_TIMEOUT_SECONDS,
        client: httpx.AsyncClient | None = None,
    ):
        # When `client` is provided (tests, custom transports), the
        # caller owns the timeout; `timeout_seconds` is only used to
        # configure a freshly-built client.
        self._base_url = base_url.rstrip("/")
        self._client = client or httpx.AsyncClient(timeout=timeout_seconds)

    async def aclose(self) -> None:
        await self._client.aclose()

    async def fetch_pr_diff_stats(
        self,
        project_key: str,
        slug: str,
        pr_id: int,
        token: str,
    ) -> DiffStats | None:
        url = (
            f"{self._base_url}/rest/api/latest/projects/{project_key}"
            f"/repos/{slug}/pull-requests/{pr_id}/diff"
        )
        try:
            response = await self._client.get(
                url,
                params={"contextLines": "0"},
                headers={
                    "Authorization": f"Bearer {token}",
                    "Accept": "application/json",
                },
            )
        except httpx.HTTPError as exc:
            logger.warning(
                "bitbucket_diff_fetch_failed",
                project_key=project_key,
                slug=slug,
                pr_id=pr_id,
                error=str(exc),
                error_type=type(exc).__name__,
            )
            return None

        if response.status_code != 200:
            logger.warning(
                "bitbucket_diff_fetch_failed",
                project_key=project_key,
                slug=slug,
                pr_id=pr_id,
                status=response.status_code,
            )
            return None

        try:
            payload = response.json()
        except ValueError as exc:
            logger.warning(
                "bitbucket_diff_fetch_failed",
                project_key=project_key,
                slug=slug,
                pr_id=pr_id,
                error=f"non-json response: {exc}",
            )
            return None

        stats = parse_diff_stats(payload)
        if stats is None:
            logger.info(
                "bitbucket_diff_truncated",
                project_key=project_key,
                slug=slug,
                pr_id=pr_id,
            )
        return stats
