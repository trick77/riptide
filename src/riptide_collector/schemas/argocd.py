"""ArgoCD notification payload — populated from the bundled NotificationTemplate."""

from datetime import datetime

from pydantic import BaseModel, ConfigDict, Field


class ArgoCDWebhook(BaseModel):
    model_config = ConfigDict(extra="allow")

    app_name: str = Field(..., min_length=1)
    revision: str = Field(..., min_length=7, description="commit SHA being deployed")
    sync_status: str | None = None
    operation_phase: str | None = Field(default=None, description="Succeeded / Failed / Running")
    started_at: datetime | None = None
    finished_at: datetime | None = None
