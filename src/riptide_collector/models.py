from datetime import datetime
from decimal import Decimal
from typing import Any

from sqlalchemy import (
    BigInteger,
    Boolean,
    Computed,
    DateTime,
    Index,
    Integer,
    Numeric,
    String,
    UniqueConstraint,
    func,
)
from sqlalchemy.dialects.postgresql import ARRAY, JSONB
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column


class Base(DeclarativeBase):
    pass


class BitbucketEvent(Base):
    __tablename__ = "bitbucket_events"
    __table_args__ = (
        UniqueConstraint("delivery_id", name="uq_bitbucket_events_delivery_id"),
        Index("ix_bitbucket_events_repo_full_name", "repo_full_name"),
        Index("ix_bitbucket_events_pr_id", "pr_id"),
        Index("ix_bitbucket_events_commit_sha", "commit_sha"),
        Index(
            "ix_bitbucket_events_jira_keys_gin",
            "jira_keys",
            postgresql_using="gin",
        ),
    )

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True, autoincrement=True)
    delivery_id: Mapped[str] = mapped_column(String, nullable=False)
    event_type: Mapped[str] = mapped_column(String, nullable=False)
    repo_full_name: Mapped[str | None] = mapped_column(String, nullable=True)
    pr_id: Mapped[int | None] = mapped_column(Integer, nullable=True)
    commit_sha: Mapped[str | None] = mapped_column(String, nullable=True)
    author: Mapped[str | None] = mapped_column(String, nullable=True)
    branch_name: Mapped[str | None] = mapped_column(String, nullable=True)
    change_type: Mapped[str | None] = mapped_column(String, nullable=True)
    jira_keys: Mapped[list[str]] = mapped_column(ARRAY(String), nullable=False, server_default="{}")
    automation_source: Mapped[str | None] = mapped_column(String, nullable=True)
    is_automated: Mapped[bool] = mapped_column(
        Boolean,
        Computed("(automation_source IS NOT NULL)", persisted=True),
        nullable=False,
    )
    lines_added: Mapped[int | None] = mapped_column(Integer, nullable=True)
    lines_removed: Mapped[int | None] = mapped_column(Integer, nullable=True)
    files_changed: Mapped[int | None] = mapped_column(Integer, nullable=True)
    is_revert: Mapped[bool] = mapped_column(Boolean, nullable=False, server_default="false")
    occurred_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, server_default=func.now()
    )
    modified_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        nullable=False,
        server_default=func.now(),
        onupdate=func.now(),
    )
    team: Mapped[str | None] = mapped_column(String, nullable=True)
    payload: Mapped[dict[str, Any]] = mapped_column(JSONB, nullable=False)


class PipelineEvent(Base):
    __tablename__ = "pipeline_events"
    __table_args__ = (
        UniqueConstraint("delivery_id", name="uq_pipeline_events_delivery_id"),
        Index("ix_pipeline_events_source", "source"),
        Index("ix_pipeline_events_pipeline_name", "pipeline_name"),
        Index("ix_pipeline_events_commit_sha", "commit_sha"),
    )

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True, autoincrement=True)
    delivery_id: Mapped[str] = mapped_column(String, nullable=False)
    source: Mapped[str] = mapped_column(String, nullable=False)
    pipeline_name: Mapped[str] = mapped_column(String, nullable=False)
    run_id: Mapped[str] = mapped_column(String, nullable=False)
    phase: Mapped[str] = mapped_column(String, nullable=False)
    status: Mapped[str | None] = mapped_column(String, nullable=True)
    commit_sha: Mapped[str | None] = mapped_column(String, nullable=True)
    started_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    duration_seconds: Mapped[int | None] = mapped_column(
        Integer,
        Computed("EXTRACT(EPOCH FROM finished_at - started_at)::int", persisted=True),
        nullable=True,
    )
    occurred_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, server_default=func.now()
    )
    modified_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        nullable=False,
        server_default=func.now(),
        onupdate=func.now(),
    )
    team: Mapped[str | None] = mapped_column(String, nullable=True)
    payload: Mapped[dict[str, Any]] = mapped_column(JSONB, nullable=False)


class NoerglerEvent(Base):
    __tablename__ = "noergler_events"
    __table_args__ = (
        UniqueConstraint("delivery_id", name="uq_noergler_events_delivery_id"),
        Index("ix_noergler_events_event_type", "event_type"),
        Index("ix_noergler_events_pr_key", "pr_key"),
        Index("ix_noergler_events_commit_sha", "commit_sha"),
        Index("ix_noergler_events_model", "model"),
    )

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True, autoincrement=True)
    delivery_id: Mapped[str] = mapped_column(String, nullable=False)
    event_type: Mapped[str] = mapped_column(String, nullable=False)
    pr_key: Mapped[str | None] = mapped_column(String, nullable=True)
    repo: Mapped[str | None] = mapped_column(String, nullable=True)
    commit_sha: Mapped[str | None] = mapped_column(String, nullable=True)
    run_id: Mapped[str | None] = mapped_column(String, nullable=True)
    model: Mapped[str | None] = mapped_column(String, nullable=True)
    prompt_tokens: Mapped[int | None] = mapped_column(Integer, nullable=True)
    completion_tokens: Mapped[int | None] = mapped_column(Integer, nullable=True)
    elapsed_ms: Mapped[int | None] = mapped_column(Integer, nullable=True)
    findings_count: Mapped[int | None] = mapped_column(Integer, nullable=True)
    cost_usd: Mapped[Decimal | None] = mapped_column(Numeric(12, 6), nullable=True)
    finding_id: Mapped[str | None] = mapped_column(String, nullable=True)
    verdict: Mapped[str | None] = mapped_column(String, nullable=True)
    actor: Mapped[str | None] = mapped_column(String, nullable=True)
    occurred_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, server_default=func.now()
    )
    modified_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        nullable=False,
        server_default=func.now(),
        onupdate=func.now(),
    )
    team: Mapped[str | None] = mapped_column(String, nullable=True)
    payload: Mapped[dict[str, Any]] = mapped_column(JSONB, nullable=False)


class ArgoCDEvent(Base):
    __tablename__ = "argocd_events"
    __table_args__ = (
        UniqueConstraint("delivery_id", name="uq_argocd_events_delivery_id"),
        Index("ix_argocd_events_app_name", "app_name"),
        Index("ix_argocd_events_revision", "revision"),
        Index("ix_argocd_events_environment", "environment"),
    )

    id: Mapped[int] = mapped_column(BigInteger, primary_key=True, autoincrement=True)
    delivery_id: Mapped[str] = mapped_column(String, nullable=False)
    app_name: Mapped[str] = mapped_column(String, nullable=False)
    revision: Mapped[str] = mapped_column(String, nullable=False)
    sync_status: Mapped[str | None] = mapped_column(String, nullable=True)
    # destination_namespace is stored case-preserved (raw from Argo CD);
    # environment is the lowercased suffix used for prod-vs-non-prod filtering.
    destination_namespace: Mapped[str | None] = mapped_column(String, nullable=True)
    environment: Mapped[str | None] = mapped_column(String, nullable=True)
    operation_phase: Mapped[str | None] = mapped_column(String, nullable=True)
    started_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    duration_seconds: Mapped[int | None] = mapped_column(
        Integer,
        Computed("EXTRACT(EPOCH FROM finished_at - started_at)::int", persisted=True),
        nullable=True,
    )
    occurred_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, server_default=func.now()
    )
    modified_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        nullable=False,
        server_default=func.now(),
        onupdate=func.now(),
    )
    team: Mapped[str | None] = mapped_column(String, nullable=True)
    payload: Mapped[dict[str, Any]] = mapped_column(JSONB, nullable=False)
