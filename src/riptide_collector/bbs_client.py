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

import asyncio
import random
from dataclasses import dataclass
from typing import Any

import httpx

from riptide_collector.logging_config import get_logger

logger = get_logger(__name__)

DEFAULT_TIMEOUT_SECONDS = 10.0

_RETRYABLE_STATUSES = frozenset({429, 502, 503, 504})


@dataclass(frozen=True, slots=True)
class DiffStats:
    lines_added: int
    lines_removed: int
    files_changed: int


@dataclass(frozen=True, slots=True)
class PushCommitStats:
    commit_count: int
    author_count: int


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


def parse_push_commit_stats(payload: Any) -> PushCommitStats | None:
    """Parse a BBS DC `/projects/{key}/repos/{slug}/commits` response.

    Counts entries in `values[]` and the distinct lowercased
    `author.name` across them. Returns None on a structurally invalid
    payload (no `values` key at all). An empty list is a legitimate
    answer (zero commits in the range) and returns `(0, 0)`.
    """
    body = _as_dict(payload)
    if "values" not in body:
        return None
    values = _as_list(body.get("values"))
    authors: set[str] = set()
    for commit in values:
        author = _as_dict(_as_dict(commit).get("author")).get("name")
        if isinstance(author, str) and author:
            authors.add(author.lower())
    return PushCommitStats(commit_count=len(values), author_count=len(authors))


class _TokenBucket:
    """Simple async token bucket using a monotonic clock.

    Tokens refill continuously at `refill_per_second` up to `capacity`.
    `acquire()` blocks until a token is available. The lock is held
    only while computing the deficit; the actual sleep happens
    unlocked so concurrent waiters don't serialize artificially.
    """

    def __init__(self, capacity: float, refill_per_second: float) -> None:
        self._capacity = float(capacity)
        self._refill = float(refill_per_second)
        self._tokens = float(capacity)
        self._updated_at = self._now()
        self._lock = asyncio.Lock()

    @staticmethod
    def _now() -> float:
        return asyncio.get_event_loop().time()

    async def acquire(self, tokens: float = 1.0) -> None:
        async with self._lock:
            now = self._now()
            elapsed = now - self._updated_at
            if elapsed > 0:
                self._tokens = min(self._capacity, self._tokens + elapsed * self._refill)
                self._updated_at = now
            if self._tokens >= tokens:
                self._tokens -= tokens
                return
            deficit = tokens - self._tokens
            wait = deficit / self._refill if self._refill > 0 else 0.0
            # Reserve the tokens now so other waiters don't double-count.
            self._tokens -= tokens
            self._updated_at = now + wait
        if wait > 0:
            await asyncio.sleep(wait)


class BitbucketClient:
    """Async HTTP client for BBS DC.

    Holds one shared `httpx.AsyncClient` plus three guards:

    - an `asyncio.Semaphore(max_concurrency)` capping in-flight requests;
    - a token-bucket rate limiter smoothing sustained throughput;
    - a bounded retry with exponential backoff on transient failures
      (network errors and 429/502/503/504).
    """

    def __init__(
        self,
        base_url: str,
        timeout_seconds: float = DEFAULT_TIMEOUT_SECONDS,
        client: httpx.AsyncClient | None = None,
        max_concurrency: int = 1,
        rate_per_second: float = 0.2,
        burst: float = 1.0,
        max_attempts: int = 3,
        backoff_base_seconds: float = 0.5,
        backoff_cap_seconds: float = 8.0,
    ):
        # When `client` is provided (tests, custom transports), the
        # caller owns the timeout; `timeout_seconds` is only used to
        # configure a freshly-built client.
        self._base_url = base_url.rstrip("/")
        self._client = client or httpx.AsyncClient(timeout=timeout_seconds)
        self._semaphore = asyncio.Semaphore(max_concurrency)
        self._bucket = _TokenBucket(capacity=burst, refill_per_second=rate_per_second)
        self._max_attempts = max_attempts
        self._backoff_base = backoff_base_seconds
        self._backoff_cap = backoff_cap_seconds

    async def aclose(self) -> None:
        await self._client.aclose()

    @staticmethod
    def _parse_retry_after(response: httpx.Response) -> float | None:
        raw = response.headers.get("Retry-After")
        if raw is None:
            return None
        try:
            value = float(raw)
        except (TypeError, ValueError):
            return None
        return value if value >= 0 else None

    def _backoff_delay(self, attempt: int) -> float:
        # Full jitter: random in [0, min(cap, base * 2**attempt)].
        ceiling = min(self._backoff_cap, self._backoff_base * (2 ** attempt))
        return random.uniform(0, ceiling)

    async def _get_with_retry(
        self,
        url: str,
        params: dict[str, str],
        headers: dict[str, str],
    ) -> tuple[httpx.Response | None, Exception | None, int]:
        """Issue a GET with rate limiting, semaphore, and bounded retry.

        Returns `(response, exception, attempts)`. On terminal success or
        terminal non-retryable status, `response` is set. On terminal
        transport error, `exception` is set. Exactly one of the two is
        non-None when `attempts >= 1`.
        """
        last_response: httpx.Response | None = None
        last_exc: Exception | None = None
        for attempt in range(self._max_attempts):
            await self._bucket.acquire()
            try:
                async with self._semaphore:
                    response = await self._client.get(url, params=params, headers=headers)
            except (httpx.TransportError, httpx.TimeoutException) as exc:
                last_exc = exc
                last_response = None
                if attempt + 1 >= self._max_attempts:
                    break
                sleep_seconds = self._backoff_delay(attempt)
                logger.info(
                    "bitbucket_diff_retry",
                    attempt=attempt + 1,
                    status_or_error=type(exc).__name__,
                    sleep_seconds=sleep_seconds,
                )
                await asyncio.sleep(sleep_seconds)
                continue

            last_response = response
            last_exc = None
            if response.status_code not in _RETRYABLE_STATUSES:
                return response, None, attempt + 1
            if attempt + 1 >= self._max_attempts:
                break
            retry_after = self._parse_retry_after(response)
            sleep_seconds = (
                retry_after if retry_after is not None else self._backoff_delay(attempt)
            )
            logger.info(
                "bitbucket_diff_retry",
                attempt=attempt + 1,
                status_or_error=response.status_code,
                sleep_seconds=sleep_seconds,
            )
            await asyncio.sleep(sleep_seconds)

        return last_response, last_exc, self._max_attempts

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
        response, exc, attempts = await self._get_with_retry(
            url,
            params={"contextLines": "0"},
            headers={
                "Authorization": f"Bearer {token}",
                "Accept": "application/json",
            },
        )

        if exc is not None:
            logger.warning(
                "bitbucket_diff_fetch_failed",
                project_key=project_key,
                slug=slug,
                pr_id=pr_id,
                error=str(exc),
                error_type=type(exc).__name__,
                attempts=attempts,
            )
            return None

        assert response is not None  # mypy: exc is None implies response set

        if response.status_code != 200:
            logger.warning(
                "bitbucket_diff_fetch_failed",
                project_key=project_key,
                slug=slug,
                pr_id=pr_id,
                status=response.status_code,
                attempts=attempts,
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
                attempts=attempts,
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

    async def fetch_push_commit_stats(
        self,
        project_key: str,
        slug: str,
        from_hash: str,
        to_hash: str,
        token: str,
    ) -> PushCommitStats | None:
        """Return commit + distinct-author counts for a push range.

        One paged GET against `/commits?since={from_hash}&until={to_hash}`
        with `limit=1000`. We deliberately do not page further: the cap
        bounds BBS load (one REST call per push, per the user's "less
        traffic" preference). Pushes that exceed 1000 commits are
        extremely rare; the column stays at 1000 for those and the
        operator can read raw `payload` JSONB if they need the truth.
        """
        url = (
            f"{self._base_url}/rest/api/latest/projects/{project_key}"
            f"/repos/{slug}/commits"
        )
        response, exc, attempts = await self._get_with_retry(
            url,
            params={
                "since": from_hash,
                "until": to_hash,
                "limit": "1000",
                "avatarSize": "0",
            },
            headers={
                "Authorization": f"Bearer {token}",
                "Accept": "application/json",
            },
        )

        if exc is not None:
            logger.warning(
                "bitbucket_commits_fetch_failed",
                project_key=project_key,
                slug=slug,
                from_hash=from_hash,
                to_hash=to_hash,
                error=str(exc),
                error_type=type(exc).__name__,
                attempts=attempts,
            )
            return None

        assert response is not None  # exc is None implies response set

        if response.status_code != 200:
            logger.warning(
                "bitbucket_commits_fetch_failed",
                project_key=project_key,
                slug=slug,
                from_hash=from_hash,
                to_hash=to_hash,
                status=response.status_code,
                attempts=attempts,
            )
            return None

        try:
            payload = response.json()
        except ValueError as exc:
            logger.warning(
                "bitbucket_commits_fetch_failed",
                project_key=project_key,
                slug=slug,
                from_hash=from_hash,
                to_hash=to_hash,
                error=f"non-json response: {exc}",
                attempts=attempts,
            )
            return None

        return parse_push_commit_stats(payload)
