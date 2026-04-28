"""initial schema: bitbucket_events, pipeline_events, argocd_events

Revision ID: 0001
Revises:
Create Date: 2026-04-28

"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects.postgresql import ARRAY, JSONB

revision: str = "0001"
down_revision: str | None = None
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


TRIGGER_FN_SQL = """
CREATE OR REPLACE FUNCTION riptide_set_modified_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.modified_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
"""

DROP_TRIGGER_FN_SQL = "DROP FUNCTION IF EXISTS riptide_set_modified_at();"

EVENT_TABLES = ("bitbucket_events", "pipeline_events", "argocd_events")


def upgrade() -> None:
    op.execute(TRIGGER_FN_SQL)
    op.create_table(
        "bitbucket_events",
        sa.Column("id", sa.BigInteger, primary_key=True, autoincrement=True),
        sa.Column("delivery_id", sa.String, nullable=False),
        sa.Column("event_type", sa.String, nullable=False),
        sa.Column("repo_full_name", sa.String, nullable=True),
        sa.Column("pr_id", sa.Integer, nullable=True),
        sa.Column("commit_sha", sa.String, nullable=True),
        sa.Column("author", sa.String, nullable=True),
        sa.Column("branch_name", sa.String, nullable=True),
        sa.Column("change_type", sa.String, nullable=True),
        sa.Column("jira_keys", ARRAY(sa.String), nullable=False, server_default="{}"),
        sa.Column("automation_source", sa.String, nullable=True),
        sa.Column(
            "is_automated",
            sa.Boolean,
            sa.Computed("(automation_source IS NOT NULL)", persisted=True),
            nullable=False,
        ),
        sa.Column("lines_added", sa.Integer, nullable=True),
        sa.Column("lines_removed", sa.Integer, nullable=True),
        sa.Column("files_changed", sa.Integer, nullable=True),
        sa.Column("is_revert", sa.Boolean, nullable=False, server_default=sa.text("false")),
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
        sa.UniqueConstraint("delivery_id", name="uq_bitbucket_events_delivery_id"),
    )
    op.create_index("ix_bitbucket_events_repo_full_name", "bitbucket_events", ["repo_full_name"])
    op.create_index("ix_bitbucket_events_pr_id", "bitbucket_events", ["pr_id"])
    op.create_index("ix_bitbucket_events_commit_sha", "bitbucket_events", ["commit_sha"])
    op.create_index(
        "ix_bitbucket_events_jira_keys_gin",
        "bitbucket_events",
        ["jira_keys"],
        postgresql_using="gin",
    )

    op.create_table(
        "pipeline_events",
        sa.Column("id", sa.BigInteger, primary_key=True, autoincrement=True),
        sa.Column("delivery_id", sa.String, nullable=False),
        sa.Column(
            "source",
            sa.String,
            nullable=False,
            comment="ci system that produced the event: jenkins / tekton / etc.",
        ),
        sa.Column("pipeline_name", sa.String, nullable=False),
        sa.Column(
            "run_id",
            sa.String,
            nullable=False,
            comment="ci-system run identifier (Jenkins build number, Tekton PipelineRun name)",
        ),
        sa.Column("phase", sa.String, nullable=False),
        sa.Column("status", sa.String, nullable=True),
        sa.Column("commit_sha", sa.String, nullable=True),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column(
            "duration_seconds",
            sa.Integer,
            sa.Computed("EXTRACT(EPOCH FROM finished_at - started_at)::int", persisted=True),
            nullable=True,
        ),
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
        sa.UniqueConstraint("delivery_id", name="uq_pipeline_events_delivery_id"),
    )
    op.create_index("ix_pipeline_events_source", "pipeline_events", ["source"])
    op.create_index("ix_pipeline_events_pipeline_name", "pipeline_events", ["pipeline_name"])
    op.create_index("ix_pipeline_events_commit_sha", "pipeline_events", ["commit_sha"])

    op.create_table(
        "argocd_events",
        sa.Column("id", sa.BigInteger, primary_key=True, autoincrement=True),
        sa.Column("delivery_id", sa.String, nullable=False),
        sa.Column("app_name", sa.String, nullable=False),
        sa.Column("revision", sa.String, nullable=False),
        sa.Column("sync_status", sa.String, nullable=True),
        sa.Column("operation_phase", sa.String, nullable=True),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column(
            "duration_seconds",
            sa.Integer,
            sa.Computed("EXTRACT(EPOCH FROM finished_at - started_at)::int", persisted=True),
            nullable=True,
        ),
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
        sa.UniqueConstraint("delivery_id", name="uq_argocd_events_delivery_id"),
    )
    op.create_index("ix_argocd_events_app_name", "argocd_events", ["app_name"])
    op.create_index("ix_argocd_events_revision", "argocd_events", ["revision"])

    for table in EVENT_TABLES:
        op.execute(
            f"""
            CREATE TRIGGER trg_{table}_modified_at
            BEFORE UPDATE ON {table}
            FOR EACH ROW EXECUTE FUNCTION riptide_set_modified_at();
            """
        )


def downgrade() -> None:
    for table in EVENT_TABLES:
        op.execute(f"DROP TRIGGER IF EXISTS trg_{table}_modified_at ON {table};")
    op.drop_table("argocd_events")
    op.drop_table("pipeline_events")
    op.drop_table("bitbucket_events")
    op.execute(DROP_TRIGGER_FN_SQL)
