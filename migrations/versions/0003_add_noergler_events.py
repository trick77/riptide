"""noergler_events: PR-review finops + reviewer-precision feedback

Revision ID: 0003
Revises: 0002
Create Date: 2026-04-29
"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects.postgresql import JSONB

revision: str = "0003"
down_revision: str | None = "0002"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.create_table(
        "noergler_events",
        sa.Column("id", sa.BigInteger, primary_key=True, autoincrement=True),
        sa.Column("delivery_id", sa.String, nullable=False),
        sa.Column(
            "event_type",
            sa.String,
            nullable=False,
            comment="completed | feedback",
        ),
        sa.Column("pr_key", sa.String, nullable=True),
        sa.Column("repo", sa.String, nullable=True),
        sa.Column("commit_sha", sa.String, nullable=True),
        # completed-only:
        sa.Column("run_id", sa.String, nullable=True),
        sa.Column("model", sa.String, nullable=True),
        sa.Column("prompt_tokens", sa.Integer, nullable=True),
        sa.Column("completion_tokens", sa.Integer, nullable=True),
        sa.Column("elapsed_ms", sa.Integer, nullable=True),
        sa.Column("findings_count", sa.Integer, nullable=True),
        sa.Column("cost_usd", sa.Numeric(12, 6), nullable=True),
        # feedback-only:
        sa.Column("finding_id", sa.String, nullable=True),
        sa.Column("verdict", sa.String, nullable=True),
        sa.Column("actor", sa.String, nullable=True),
        sa.Column("occurred_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column(
            "created_at",
            sa.DateTime(timezone=True),
            nullable=False,
            server_default=sa.func.now(),
        ),
        sa.Column(
            "modified_at",
            sa.DateTime(timezone=True),
            nullable=False,
            server_default=sa.func.now(),
        ),
        sa.Column("service", sa.String, nullable=True),
        sa.Column("team", sa.String, nullable=True),
        sa.Column("payload", JSONB, nullable=False),
        sa.UniqueConstraint("delivery_id", name="uq_noergler_events_delivery_id"),
    )
    op.create_index("ix_noergler_events_event_type", "noergler_events", ["event_type"])
    op.create_index("ix_noergler_events_pr_key", "noergler_events", ["pr_key"])
    op.create_index("ix_noergler_events_commit_sha", "noergler_events", ["commit_sha"])
    op.create_index("ix_noergler_events_model", "noergler_events", ["model"])
    op.create_index(
        "ix_noergler_events_service_created_at",
        "noergler_events",
        ["service", "created_at"],
    )

    op.execute(
        """
        CREATE TRIGGER trg_noergler_events_modified_at
        BEFORE UPDATE ON noergler_events
        FOR EACH ROW EXECUTE FUNCTION riptide_set_modified_at();
        """
    )


def downgrade() -> None:
    op.execute("DROP TRIGGER IF EXISTS trg_noergler_events_modified_at ON noergler_events;")
    op.drop_table("noergler_events")
