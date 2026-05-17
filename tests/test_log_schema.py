"""Schema regression tests for the structured log output.

Why these exist: PR #48 unified all webhook handlers onto one
`msg=webhook_processed` event with a fixed kwarg vocabulary and forbids
Splunk-reserved field names on the wire. Without these tests the schema
silently drifts on the next add-a-source / add-a-field PR — which is
exactly what the unification was supposed to end.

Two layers are covered:

1. **Call-site invariants** (per router): capture stdout via `capfd` and
   parse the JSON lines. This is what Splunk actually sees, so it's the
   right thing to assert against. (`structlog.testing.capture_logs` was
   tried first but its global-config swap doesn't reach loggers cached
   under `cache_logger_on_first_use=True`, which silently breaks across
   tests.)
2. **Processor invariants** (one test class): run the configured processor
   chain on a synthetic event_dict and assert `event→msg`, `level→log_level`,
   reserved-name stripping, and `service/version/env` stamping.
"""

from __future__ import annotations

import io
import json
import logging
from collections.abc import Iterator
from pathlib import Path
from typing import Any, cast

import pytest
import structlog
from httpx import AsyncClient

from _keys import CHECKOUT_NOERGLER
from riptide_collector import __version__
from riptide_collector.logging_config import configure_logging

FIXTURES = Path(__file__).parent / "fixtures"


def _load(name: str) -> dict[str, Any]:
    return json.loads((FIXTURES / name).read_text())


def _parse_json_lines(stream_text: str) -> list[dict[str, Any]]:
    """Return JSON-object lines from a captured log stream, ignoring
    non-JSON noise (httpx debug lines, etc.)."""
    out: list[dict[str, Any]] = []
    for raw in stream_text.splitlines():
        line = raw.strip()
        if not line.startswith("{"):
            continue
        try:
            out.append(json.loads(line))
        except json.JSONDecodeError:
            continue
    return out


def _processed(events: list[dict[str, Any]]) -> list[dict[str, Any]]:
    return [e for e in events if e.get("msg") == "webhook_processed"]


@pytest.fixture
def log_buffer(client: AsyncClient) -> Iterator[io.StringIO]:
    """Swap every root StreamHandler's stream to an in-memory buffer for
    the test. Depends on `client` so it runs AFTER `configure_logging`
    has installed the JSON handler.

    Why not capfd/capsys: the structlog handler captures `sys.stdout`
    at handler-creation time. Once create_app runs in the client fixture,
    that reference is frozen — pytest's later fd/sys redirection doesn't
    reach it. Swapping `handler.stream` directly is the only reliable
    way to test what Splunk would actually see.
    """
    del client  # ordering dependency only
    buf = io.StringIO()
    root = logging.getLogger()
    saved: list[tuple[logging.StreamHandler[Any], Any]] = []
    for handler in root.handlers:
        if isinstance(handler, logging.StreamHandler):
            saved.append((handler, handler.stream))
            handler.stream = buf
    try:
        yield buf
    finally:
        for handler, original in saved:
            handler.stream = original


@pytest.fixture
def log_buffer_ignored(
    client_with_ignored_stages: AsyncClient,
) -> Iterator[io.StringIO]:
    del client_with_ignored_stages
    buf = io.StringIO()
    root = logging.getLogger()
    saved: list[tuple[logging.StreamHandler[Any], Any]] = []
    for handler in root.handlers:
        if isinstance(handler, logging.StreamHandler):
            saved.append((handler, handler.stream))
            handler.stream = buf
    try:
        yield buf
    finally:
        for handler, original in saved:
            handler.stream = original


# ---------- call-site invariants ----------


class TestWebhookProcessedSchema:
    async def test_pipeline_accepted_then_deduped(
        self, client: AsyncClient, log_buffer: io.StringIO
    ) -> None:
        payload = _load("pipeline_jenkins_completed.json")
        headers = {"Authorization": "Bearer test-checkout-jenkins-bearer"}

        r1 = await client.post("/webhooks/pipeline", json=payload, headers=headers)
        r2 = await client.post("/webhooks/pipeline", json=payload, headers=headers)

        assert r1.status_code == 202
        assert r2.status_code == 202
        events = _processed(_parse_json_lines(log_buffer.getvalue()))
        assert len(events) == 2

        first, second = events
        for ev in events:
            for key in (
                "msg",
                "log_level",
                "timestamp",
                "service",
                "version",
                "env",
                "webhook_source",
                "outcome",
                "delivery_id",
                "team",
            ):
                assert key in ev, f"missing {key} in {ev}"
            # Splunk-reserved field names must not appear on the wire.
            for forbidden in ("source", "event", "level", "host", "index", "sourcetype"):
                assert forbidden not in ev, f"reserved {forbidden} leaked in {ev}"
            assert ev["webhook_source"] == "pipeline"

        assert first["outcome"] == "accepted"
        assert second["outcome"] == "deduped"
        assert first["ci_system"] == "jenkins"
        # Generic field names, not pipeline-namespaced.
        assert "status" in first
        assert "run_id" in first

    async def test_argocd_accepted_then_deduped(
        self, client: AsyncClient, log_buffer: io.StringIO
    ) -> None:
        payload = _load("argocd_synced.json")
        headers = {"Authorization": "Bearer test-checkout-argocd-bearer"}

        r1 = await client.post("/webhooks/argocd", json=payload, headers=headers)
        r2 = await client.post("/webhooks/argocd", json=payload, headers=headers)

        assert r1.status_code == 202
        assert r2.status_code == 202
        events = _processed(_parse_json_lines(log_buffer.getvalue()))
        assert len(events) == 2
        assert events[0]["outcome"] == "accepted"
        assert events[1]["outcome"] == "deduped"
        for ev in events:
            assert ev["webhook_source"] == "argocd"
            assert ev["delivery_id"]
            assert "source" not in ev

    async def test_argocd_ignored_carries_delivery_id(
        self,
        client_with_ignored_stages: AsyncClient,
        log_buffer_ignored: io.StringIO,
    ) -> None:
        payload = _load("argocd_synced.json")
        # The canonical fixture targets prod, which the ignored-stages
        # fixture leaves alone. Force a stage the fixture covers.
        payload["destination_namespace"] = "payments-api-syst"
        headers = {"Authorization": "Bearer test-checkout-argocd-bearer"}

        r = await client_with_ignored_stages.post("/webhooks/argocd", json=payload, headers=headers)

        assert r.status_code == 202
        events = _processed(_parse_json_lines(log_buffer_ignored.getvalue()))
        assert len(events) == 1
        ev = events[0]
        assert ev["outcome"] == "ignored"
        assert ev["reason"] == "stage_in_ignored_stages"
        # Regression guard: argocd_event_ignored used to omit delivery_id,
        # leaving triage with no key to grep on.
        assert ev["delivery_id"]
        assert "environment" in ev

    async def test_noergler_uses_generic_event_type_field(
        self, client: AsyncClient, log_buffer: io.StringIO
    ) -> None:
        payload = _load("noergler_pr_completed_merged.json")
        headers = {"Authorization": f"Bearer {CHECKOUT_NOERGLER}"}

        r = await client.post("/webhooks/noergler", json=payload, headers=headers)

        assert r.status_code == 202
        events = _processed(_parse_json_lines(log_buffer.getvalue()))
        assert len(events) == 1
        ev = events[0]
        assert ev["webhook_source"] == "noergler"
        # Field is `event_type`, not `noergler_event_type` — webhook_source
        # already disambiguates in Splunk panels.
        assert ev["event_type"] == "pr_completed"
        assert "noergler_event_type" not in ev


class TestHttpRequestLog:
    async def test_request_id_header_propagates(
        self, client: AsyncClient, log_buffer: io.StringIO
    ) -> None:
        r = await client.get(
            "/auth/ping",
            headers={
                "Authorization": "Bearer test-checkout-argocd-bearer",
                "X-Request-Id": "test-rid-abc",
            },
        )
        assert r.status_code == 200

        events = _parse_json_lines(log_buffer.getvalue())
        http = [e for e in events if e.get("msg") == "http_request"]
        assert len(http) == 1
        ev = http[0]
        assert ev["request_id"] == "test-rid-abc"
        assert ev["path"] == "/auth/ping"
        assert ev["method"] == "GET"
        assert ev["status_code"] == 200
        assert isinstance(ev["duration_ms"], (int, float))

    async def test_health_path_not_logged(
        self, client: AsyncClient, log_buffer: io.StringIO
    ) -> None:
        for _ in range(3):
            await client.get("/health")
        events = _parse_json_lines(log_buffer.getvalue())
        assert [e for e in events if e.get("msg") == "http_request"] == []


# ---------- processor-chain invariants ----------


class TestProcessorChain:
    def _run_chain(self, env: str, **kwargs: Any) -> dict[str, Any]:
        configure_logging("INFO", env=env)
        event_dict: dict[str, Any] = {"event": "x", "level": "info", **kwargs}
        processed: Any = event_dict
        from riptide_collector.logging_config import (
            _make_service_metadata_processor,
            _rename_level,
            _stable_field_order,
            _strip_reserved,
        )

        chain = [
            _make_service_metadata_processor(env),
            structlog.processors.EventRenamer("msg"),
            _rename_level,
            _strip_reserved,
            _stable_field_order,
        ]
        for proc in chain:
            processed = proc(cast(Any, None), "info", processed)
        assert isinstance(processed, dict)
        return processed

    def test_event_renamed_to_msg_level_to_log_level(self) -> None:
        out = self._run_chain("prod")
        assert out["msg"] == "x"
        assert out["log_level"] == "info"
        assert "event" not in out
        assert "level" not in out

    def test_service_metadata_stamped(self) -> None:
        out = self._run_chain("intg")
        assert out["service"] == "riptide-collector"
        assert out["version"] == __version__
        assert out["env"] == "intg"

    def test_splunk_reserved_kwargs_namespaced(self) -> None:
        # Anyone accidentally passing `source=...` or `host=...` as a kwarg
        # would be silently overwritten by Splunk's input metadata. The
        # `_strip_reserved` safety net moves them under `splunk_<name>`.
        out = self._run_chain(
            "prod",
            source="jenkins",
            host="x",
            index="y",
            sourcetype="z",
        )
        assert out["splunk_source"] == "jenkins"
        assert out["splunk_host"] == "x"
        assert out["splunk_index"] == "y"
        assert out["splunk_sourcetype"] == "z"
        for forbidden in ("source", "host", "index", "sourcetype"):
            assert forbidden not in out

    def test_stable_leading_key_order_with_user_kwargs(self) -> None:
        # The regression that triggered this rule: structlog inserts user
        # kwargs into event_dict *before* the timestamp/service/version/env
        # processors run, producing JSON where 'version' or 'teams' appears
        # ahead of 'timestamp'. The reorder processor must restore a stable
        # leading order regardless of what kwargs the caller passes.
        out = self._run_chain(
            "prod",
            teams=3,
            keys=2,
            production_stage="prod",
            timestamp="2026-05-17T09:04:17Z",
        )
        head = list(out.keys())[:5]
        assert head == ["timestamp", "log_level", "service", "version", "env"]
        # Caller-supplied kwargs still appear, just after the leading block.
        assert {"teams", "keys", "production_stage"}.issubset(out.keys())


class TestRenderedFieldOrder:
    """End-to-end: every JSON line emitted by the running app must start
    with `timestamp` regardless of what call site produced it.

    Catches the regression a tail/grep user would notice immediately — a
    structlog call that passes kwargs gets the kwargs as leading keys
    unless the reorder processor is wired into the chain.
    """

    async def test_every_emitted_line_starts_with_timestamp(
        self,
        client: AsyncClient,
        log_buffer: io.StringIO,
    ) -> None:
        # Hit a webhook that runs through every call-site shape: uvicorn
        # bridge (already covered at startup), the access-log middleware,
        # and a router that emits webhook_processed with kwargs.
        headers = {"Authorization": f"Bearer {CHECKOUT_NOERGLER}"}
        r = await client.post(
            "/webhooks/noergler",
            json=_load("noergler_pr_completed_merged.json"),
            headers=headers,
        )
        assert r.status_code == 202

        events = _parse_json_lines(log_buffer.getvalue())
        assert events, "no JSON log lines captured — fixture or pipeline broken"
        for line in events:
            assert next(iter(line)) == "timestamp", (
                f"line does not start with 'timestamp': {list(line)[:3]} — {line}"
            )
