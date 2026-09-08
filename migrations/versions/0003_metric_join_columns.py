"""columns that make bot detection and deploy→commit correlation work

Three additive nullable columns, all driven by what production data showed:

- `bitbucket_events.author_display_name`: a review bot posting under an
  ordinary-looking login is only recognisable by its display name; without
  it the bot counts as a human reviewer and collapses pickup time.
- `pipeline_events.image_ref`: the full image reference the run published.
  Argo CD stores exactly these strings in `payload->'images'`, so this is
  the exact join from a deploy back to the build and its commit — image
  tags are versions, not commit SHAs.
- `noergler_events.reviewer_handle` / `reviewer_is_bot`: the account the
  reviewer acts as and whether it is automation, both self-reported, so
  automation identity comes from the stream rather than per-installation
  configuration. The handle is the join key back to the Bitbucket rows the
  reviewer's comments produced.

Revision ID: 0003
Revises: 0002
Create Date: 2026-09-08

"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "0003"
down_revision: str | None = "0002"
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.add_column(
        "bitbucket_events",
        sa.Column(
            "author_display_name",
            sa.String,
            nullable=True,
            comment="actor.displayName; bot accounts often identify themselves only here",
        ),
    )
    op.add_column(
        "pipeline_events",
        sa.Column(
            "image_ref",
            sa.String,
            nullable=True,
            comment="full image reference published by the run; joins to argocd payload->'images'",
        ),
    )
    op.add_column(
        "noergler_events",
        sa.Column(
            "reviewer_handle",
            sa.String,
            nullable=True,
            comment="git-host account the reviewer posts under; self-reported automation identity",
        ),
    )
    op.add_column(
        "noergler_events",
        sa.Column(
            "reviewer_is_bot",
            sa.Boolean,
            nullable=True,
            comment="whether reviewer_handle is automation, as declared by the sender",
        ),
    )
    op.create_index("ix_pipeline_events_image_ref", "pipeline_events", ["image_ref"])


def downgrade() -> None:
    op.drop_index("ix_pipeline_events_image_ref", table_name="pipeline_events")
    op.drop_column("noergler_events", "reviewer_is_bot")
    op.drop_column("noergler_events", "reviewer_handle")
    op.drop_column("pipeline_events", "image_ref")
    op.drop_column("bitbucket_events", "author_display_name")
