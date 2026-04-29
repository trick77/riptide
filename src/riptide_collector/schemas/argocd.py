"""ArgoCD notification payload — populated from the bundled NotificationTemplate."""

from datetime import UTC, datetime

from pydantic import BaseModel, ConfigDict, Field, field_validator


class ArgoCDWebhook(BaseModel):
    model_config = ConfigDict(extra="allow")

    app_name: str = Field(..., min_length=1)
    service_id: str | None = Field(
        default=None,
        min_length=1,
        description="Optional opaque service identifier (e.g. CMDB id 'srv0417'). "
        "If absent, falls back to `app_name`.",
    )
    revision: str = Field(..., min_length=7, description="commit SHA being deployed")
    sync_status: str | None = None
    operation_phase: str | None = Field(default=None, description="Succeeded / Failed / Running")
    started_at: datetime | None = None
    finished_at: datetime | None = None

    @field_validator("started_at", "finished_at")
    @classmethod
    def _normalise_tz(cls, v: datetime | None) -> datetime | None:
        if v is None:
            return None
        return v.astimezone(UTC) if v.tzinfo else v.replace(tzinfo=UTC)
