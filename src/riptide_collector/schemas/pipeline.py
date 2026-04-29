"""Source-agnostic pipeline event schema.

Used by Jenkins, Tekton, and any other CI emitting build/deploy events. The
`source` field tags which CI produced the event so downstream queries can
slice by tooling.
"""

from datetime import UTC, datetime

from pydantic import BaseModel, ConfigDict, Field, field_validator


def _to_utc(value: datetime | None) -> datetime | None:
    if value is None:
        return None
    return value.astimezone(UTC) if value.tzinfo else value.replace(tzinfo=UTC)


class PipelineWebhook(BaseModel):
    model_config = ConfigDict(extra="allow")

    source: str = Field(
        ...,
        min_length=1,
        description="ci system tag, e.g. 'jenkins', 'tekton'",
        examples=["jenkins", "tekton"],
    )
    pipeline_name: str = Field(
        ..., min_length=1, description="Jenkins job name / Tekton pipeline name"
    )
    run_id: str = Field(
        ...,
        min_length=1,
        description="ci-system run id (Jenkins build number, Tekton PipelineRun name)",
    )
    phase: str = Field(..., min_length=1, description="STARTED / COMPLETED / FINALIZED")
    status: str | None = Field(default=None, description="SUCCESS / FAILURE / etc.")
    commit_sha: str = Field(..., min_length=7, description="git commit SHA being built")
    started_at: datetime
    finished_at: datetime | None = None
    service_id: str | None = Field(
        default=None,
        min_length=1,
        description="Optional opaque service identifier (e.g. CMDB id 'srv0417'). "
        "If absent, falls back to `pipeline_name`.",
    )

    @field_validator("started_at", "finished_at")
    @classmethod
    def _normalise_tz(cls, v: datetime | None) -> datetime | None:
        return _to_utc(v)
