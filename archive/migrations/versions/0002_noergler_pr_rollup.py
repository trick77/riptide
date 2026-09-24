"""noergler events: switch from per-run 'completed' to per-PR 'pr_completed' rollup

Adds outcome / lines_added / lines_removed / files_changed / total_runs /
merge_commit_sha / models_used / first_review_at columns to noergler_events.
The pre-existing prompt_tokens / completion_tokens / elapsed_ms / findings_count /
cost_usd columns are reused as aggregated totals — no rename, just a
semantic shift.

Revision ID: 0002
Revises: 0001
Create Date: 2026-05-17

"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op
from sqlalchemy.dialects.postgresql import ARRAY

revision: str = "0002"
down_revision: str | None = "0001"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.add_column("noergler_events", sa.Column("outcome", sa.String, nullable=True))
    op.add_column("noergler_events", sa.Column("merge_commit_sha", sa.String, nullable=True))
    op.add_column("noergler_events", sa.Column("lines_added", sa.Integer, nullable=True))
    op.add_column("noergler_events", sa.Column("lines_removed", sa.Integer, nullable=True))
    op.add_column("noergler_events", sa.Column("files_changed", sa.Integer, nullable=True))
    op.add_column("noergler_events", sa.Column("total_runs", sa.Integer, nullable=True))
    op.add_column(
        "noergler_events",
        sa.Column("models_used", ARRAY(sa.String), nullable=True),
    )
    op.add_column(
        "noergler_events",
        sa.Column("first_review_at", sa.DateTime(timezone=True), nullable=True),
    )

    op.create_index("ix_noergler_events_outcome", "noergler_events", ["outcome"])
    # The 'model' (singular) index made sense when each row was a single
    # review run. Per-PR rollups carry models_used (an array) instead, so
    # the singular-model index no longer matches the query patterns.
    op.drop_index("ix_noergler_events_model", table_name="noergler_events")

    # run_id was the per-run idempotency key for the old 'completed' event.
    # The rollup uses (pr_key, outcome) — run_id is dead. 'model' (singular)
    # is superseded by models_used; the original value is still recoverable
    # from the payload JSONB on historical rows if anyone ever needs it.
    op.drop_column("noergler_events", "run_id")
    op.drop_column("noergler_events", "model")


def downgrade() -> None:
    op.add_column("noergler_events", sa.Column("model", sa.String, nullable=True))
    op.add_column("noergler_events", sa.Column("run_id", sa.String, nullable=True))
    op.create_index("ix_noergler_events_model", "noergler_events", ["model"])
    op.drop_index("ix_noergler_events_outcome", table_name="noergler_events")

    op.drop_column("noergler_events", "first_review_at")
    op.drop_column("noergler_events", "models_used")
    op.drop_column("noergler_events", "total_runs")
    op.drop_column("noergler_events", "files_changed")
    op.drop_column("noergler_events", "lines_removed")
    op.drop_column("noergler_events", "lines_added")
    op.drop_column("noergler_events", "merge_commit_sha")
    op.drop_column("noergler_events", "outcome")
