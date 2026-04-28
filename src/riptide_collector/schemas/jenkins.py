"""Strict Jenkins payload contract — Jenkins jobs MUST send these fields.

If a field is missing, we 422 — that's a wiring bug to surface, not data to
silently drop.
"""

from datetime import datetime

from pydantic import BaseModel, ConfigDict, Field


class JenkinsWebhook(BaseModel):
    model_config = ConfigDict(extra="allow")

    job_name: str = Field(..., min_length=1)
    build_number: int = Field(..., ge=0)
    phase: str = Field(..., min_length=1, description="STARTED / COMPLETED / FINALIZED")
    status: str | None = Field(default=None, description="SUCCESS / FAILURE / etc.")
    commit_sha: str = Field(..., min_length=7, description="git commit SHA being built")
    started_at: datetime
    finished_at: datetime | None = None
    service_id: str | None = Field(
        default=None,
        description="Optional explicit service id; if absent, resolve via job_name.",
    )
