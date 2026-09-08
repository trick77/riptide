"""Source-agnostic pipeline event schema.

Used by Jenkins, Tekton, and any other CI emitting build/deploy events. The
`source` field tags which CI produced the event so downstream queries can
slice by tooling.
"""

from datetime import UTC, datetime
from typing import Literal

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
    image_ref: str | None = Field(
        default=None,
        description=(
            "full image reference the run published, e.g. 'registry/path/app:2.0.41'. "
            "Argo CD reports the same strings in its rendered image list, so sending it "
            "makes deploy → build → commit an exact join even when the tag is a version "
            "rather than a commit SHA. Omit it (or send an empty string) for runs "
            "that publish no image."
        ),
    )
    actor_handle: str | None = Field(
        default=None,
        description=(
            "the git-host account this CI system acts through, e.g. the user whose "
            "name appears on merges and pushes it makes. Declaring it (with "
            "actor_account_kind) keeps those events out of human-activity metrics — "
            "a CI service account can easily be a third of all repository events."
        ),
    )
    actor_account_kind: Literal["bot", "service", "human"] = Field(
        default="service",
        description=(
            "what actor_handle is: 'service' for a technical account a system acts "
            "through, 'bot' for automation acting on its own, 'human' for a person. "
            "riptide stores this declaration instead of guessing from the name."
        ),
    )
    started_at: datetime
    finished_at: datetime | None = None

    @field_validator("actor_handle")
    @classmethod
    def _empty_actor_handle_is_none(cls, v: str | None) -> str | None:
        return v.strip() or None if v else None

    @field_validator("started_at", "finished_at")
    @classmethod
    def _normalise_tz(cls, v: datetime | None) -> datetime | None:
        return _to_utc(v)

    @field_validator("image_ref")
    @classmethod
    def _empty_image_ref_is_none(cls, v: str | None) -> str | None:
        # Templating a param that wasn't set yields "" far more often than it
        # yields an absent key. Rejecting that would drop the whole run —
        # build duration and success/failure with it — over an optional field.
        return v.strip() or None if v else None
