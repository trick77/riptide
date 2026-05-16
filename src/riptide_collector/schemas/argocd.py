"""ArgoCD notification payload — populated from the bundled NotificationTemplate."""

from datetime import UTC, datetime

from pydantic import BaseModel, ConfigDict, Field, field_validator


class ArgoCDWebhook(BaseModel):
    model_config = ConfigDict(extra="allow")

    app_name: str = Field(..., min_length=1)
    revision: str = Field(..., min_length=7, description="commit SHA being deployed")
    sync_status: str | None = None
    operation_phase: str | None = Field(default=None, description="Succeeded / Failed / Running")
    started_at: datetime | None = None
    finished_at: datetime | None = None
    destination_namespace: str | None = Field(
        default=None,
        description="kubernetes namespace the app deployed into; suffix drives `environment`",
    )
    images: list[str] = Field(
        ...,
        description=(
            "rendered images from the synced manifests (Argo CD field path "
            "`.app.status.summary.images`). may be empty for apps without pods. "
            "the App-Repo commit SHA is typically embedded in the image tag and "
            "bridges argocd.revision (GitOps-repo SHA) to pipeline.commit_sha "
            "(App-repo SHA) — correlation logic is built on top of this field."
        ),
    )

    @field_validator("started_at", "finished_at")
    @classmethod
    def _normalise_tz(cls, v: datetime | None) -> datetime | None:
        if v is None:
            return None
        return v.astimezone(UTC) if v.tzinfo else v.replace(tzinfo=UTC)
