"""bitbucket_events: per-push commit + author counts

Revision ID: 0002
Revises: 0001
Create Date: 2026-05-04

"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "0002"
down_revision: str | None = "0001"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.add_column(
        "bitbucket_events",
        sa.Column("push_commit_count", sa.Integer, nullable=True),
    )
    op.add_column(
        "bitbucket_events",
        sa.Column("push_author_count", sa.Integer, nullable=True),
    )


def downgrade() -> None:
    op.drop_column("bitbucket_events", "push_author_count")
    op.drop_column("bitbucket_events", "push_commit_count")
