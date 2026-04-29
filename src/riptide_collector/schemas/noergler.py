"""Noergler PR-review payloads.

Two event types, both keyed off review activity (not PR lifecycle — PR
lifecycle already comes in via Bitbucket):

- `completed`: emitted after an LLM review run finishes. Carries the finops
  signal — model, token counts, elapsed time, cost.
- `feedback`: emitted when a reviewer disagrees with or acknowledges a
  finding. Carries the reviewer-precision signal.
"""

from __future__ import annotations

from datetime import UTC, datetime
from decimal import Decimal
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator


def _to_utc(value: datetime) -> datetime:
    return value.astimezone(UTC) if value.tzinfo else value.replace(tzinfo=UTC)


class _Common(BaseModel):
    # Strict — we own this contract (same posture as pipeline / argocd).
    # Unknown fields surface as 422 so a sender typo doesn't silently land
    # in `payload` JSONB and skew metrics.
    model_config = ConfigDict(extra="forbid")

    service_id: str | None = Field(
        default=None,
        min_length=1,
        description="Optional opaque service identifier (e.g. CMDB id 'srv0417'). "
        "If absent, falls back to `repo`.",
    )


class NoerglerCompleted(_Common):
    event_type: Literal["completed"]
    pr_key: str = Field(..., min_length=1, description="Bitbucket PR key, e.g. 'PROJ/repo#42'")
    repo: str = Field(..., min_length=1)
    commit_sha: str = Field(..., min_length=7)
    run_id: str = Field(..., min_length=1, description="noergler review-run id (idempotency key)")
    model: str = Field(..., min_length=1, description="LLM identifier, e.g. 'gpt-4o-2024-08-06'")
    prompt_tokens: int = Field(..., ge=0)
    completion_tokens: int = Field(..., ge=0)
    elapsed_ms: int = Field(..., ge=0)
    findings_count: int = Field(..., ge=0)
    cost_usd: Decimal = Field(..., ge=0)
    finished_at: datetime

    @field_validator("finished_at")
    @classmethod
    def _normalise_tz(cls, v: datetime) -> datetime:
        return _to_utc(v)


class NoerglerFeedback(_Common):
    event_type: Literal["feedback"]
    pr_key: str = Field(..., min_length=1)
    finding_id: str = Field(
        ...,
        min_length=1,
        description=(
            "Stable opaque identifier for the finding the verdict applies to. "
            "Noergler must emit the same finding_id verbatim across retries and "
            "verdict flips — riptide does not normalise (`Finding-X` and "
            "`finding-x` are treated as distinct findings)."
        ),
    )
    verdict: Literal["disagreed", "acknowledged"]
    actor: str = Field(..., min_length=1, description="user who reacted to the finding")
    repo: str | None = Field(default=None, min_length=1)
    commit_sha: str | None = Field(
        default=None,
        min_length=7,
        description=(
            "Optional commit SHA the finding was raised on. Lets readers join "
            "feedback rows to deployments / pipelines for 'did the user disagree "
            "with a finding on code that later regressed' analysis."
        ),
    )
    occurred_at: datetime

    @field_validator("occurred_at")
    @classmethod
    def _normalise_tz(cls, v: datetime) -> datetime:
        return _to_utc(v)


NoerglerWebhook = Annotated[
    NoerglerCompleted | NoerglerFeedback,
    Field(discriminator="event_type"),
]
