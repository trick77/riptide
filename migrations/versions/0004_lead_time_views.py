"""views for per-commit DORA lead time

Lead time was a proxy: one App-repo commit per release (the newest), which
answers "how stale was the freshest change" rather than DORA's "how long from
commit to running in production". On real data the difference is 7x — prod p50
26.7 h as a proxy against 193.6 h per commit — and the proxy is the flattering
one, so it is replaced rather than kept alongside.

The view SQL lives in `riptide_collector.views` so this migration and the tests
execute the same definition. The index exists because the range lookups are per
repo and per time window: without it the metric query does not finish on a
table of any size.

Revision ID: 0004
Revises: 0003
Create Date: 2026-09-08

"""

from collections.abc import Sequence

from alembic import op

from riptide_collector.views import CREATE_VIEWS, DROP_VIEWS

revision: str = "0004"
down_revision: str | None = "0003"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.create_index(
        "ix_bitbucket_events_repo_branch_occurred",
        "bitbucket_events",
        ["repo_full_name", "branch_name", "occurred_at"],
    )
    for statement in CREATE_VIEWS:
        op.execute(statement)


def downgrade() -> None:
    for statement in DROP_VIEWS:
        op.execute(statement)
    op.drop_index("ix_bitbucket_events_repo_branch_occurred", table_name="bitbucket_events")
