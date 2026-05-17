"""Noergler PR-review payloads.

Two event types:

- `pr_completed`: emitted once per PR lifecycle (merged / declined / deleted).
  Carries the roll-up finops signal — aggregated tokens, elapsed time, cost,
  findings and the final PR diff-size — plus an `outcome` so consumers can
  filter for merged PRs vs. abandoned ones.
- `feedback`: emitted when a reviewer disagrees with or acknowledges a
  finding. Reactive and not tied to the PR lifecycle.
"""

from __future__ import annotations

from datetime import UTC, datetime
from decimal import Decimal
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator


def _to_utc(value: datetime) -> datetime:
    return value.astimezone(UTC) if value.tzinfo else value.replace(tzinfo=UTC)


class _Common(BaseModel):
    # Strict — we own this contract (same posture as pipeline / argocd).
    # Unknown fields surface as 422 so a sender typo doesn't silently land
    # in `payload` JSONB and skew metrics.
    model_config = ConfigDict(extra="forbid")


class NoerglerPrCompleted(_Common):
    event_type: Literal["pr_completed"]
    outcome: Literal["merged", "declined", "deleted"] = Field(
        ...,
        description=(
            "PR lifecycle outcome. Only 'merged' PRs reach production; "
            "'declined' and 'deleted' incurred LLM cost without shipping code "
            "and must be filtered out of throughput / DORA metrics."
        ),
    )
    pr_key: str = Field(..., min_length=1, description="Bitbucket PR key, e.g. 'PROJ/repo#42'")
    repo: str = Field(..., min_length=1)
    source_commit_sha: str = Field(
        ...,
        min_length=7,
        description="Last reviewed source-branch commit (HEAD of fromRef when the PR closed).",
    )
    merge_commit_sha: str | None = Field(
        default=None,
        min_length=7,
        description="Merge commit SHA. Set only when outcome='merged'.",
    )
    lines_added: int = Field(..., ge=0, description="Final cumulative PR diff: lines added.")
    lines_removed: int = Field(..., ge=0, description="Final cumulative PR diff: lines removed.")
    files_changed: int = Field(..., ge=0, description="Final cumulative PR diff: files changed.")
    total_runs: int = Field(..., ge=1, description="Number of review runs aggregated.")
    total_prompt_tokens: int = Field(..., ge=0)
    total_completion_tokens: int = Field(..., ge=0)
    total_elapsed_ms: int = Field(..., ge=0)
    total_findings_count: int = Field(..., ge=0)
    total_cost_usd: Decimal = Field(..., ge=0)
    models_used: list[str] = Field(
        ...,
        min_length=1,
        description="Distinct LLM identifiers used across the PR's review runs.",
    )
    first_review_at: datetime
    closed_at: datetime = Field(
        ...,
        description="Timestamp the PR reached its terminal outcome (merged/declined/deleted).",
    )

    @field_validator("first_review_at", "closed_at")
    @classmethod
    def _normalise_tz(cls, v: datetime) -> datetime:
        return _to_utc(v)

    @field_validator("models_used")
    @classmethod
    def _models_non_empty(cls, v: list[str]) -> list[str]:
        if any(not m.strip() for m in v):
            raise ValueError("models_used entries must be non-empty strings")
        return v

    @model_validator(mode="after")
    def _check_merge_commit_consistency(self) -> NoerglerPrCompleted:
        # Catch sender bugs: only merged PRs produce a merge commit, and a
        # merged PR is exactly the case where the sender must supply one.
        # Without this, "outcome=declined + merge_commit_sha=<sha>" would
        # silently land in the DB.
        if self.outcome == "merged" and not self.merge_commit_sha:
            raise ValueError("merge_commit_sha is required when outcome='merged'")
        if self.outcome != "merged" and self.merge_commit_sha is not None:
            raise ValueError(f"merge_commit_sha must be null when outcome='{self.outcome}'")
        return self


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
    NoerglerPrCompleted | NoerglerFeedback,
    Field(discriminator="event_type"),
]
