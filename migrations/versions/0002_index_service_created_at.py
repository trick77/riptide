"""composite (service, created_at) indexes for diagnostics

Revision ID: 0002
Revises: 0001
Create Date: 2026-04-28
"""

from collections.abc import Sequence

from alembic import op

revision: str = "0002"
down_revision: str | None = "0001"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None

EVENT_TABLES = ("bitbucket_events", "pipeline_events", "argocd_events")


def upgrade() -> None:
    for table in EVENT_TABLES:
        op.create_index(
            f"ix_{table}_service_created_at",
            table,
            ["service", "created_at"],
        )


def downgrade() -> None:
    for table in EVENT_TABLES:
        op.drop_index(f"ix_{table}_service_created_at", table_name=table)
