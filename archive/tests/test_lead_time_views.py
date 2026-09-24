"""Tests for the DORA lead-time views (`riptide_collector.views`).

The views are the metric definition, so they are exercised as SQL against a
real Postgres with hand-built events: one app repo whose commits land on
master, a GitOps release commit naming the range it shipped, and Argo CD
deploys of that revision.
"""

from __future__ import annotations

import json
from datetime import UTC, datetime, timedelta
from typing import Any

import pytest_asyncio
from sqlalchemy import text
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from riptide_collector.views import CREATE_VIEWS, DROP_VIEWS

APP_REPO = "acme/payments-api"
GITOPS_REPO = "acme/payments-infra"
BASE = datetime(2026, 4, 1, 8, 0, tzinfo=UTC)

# The range endpoints: the App-repo commit the previous release shipped, and
# the one this release ships.
SHA_PREV = "a" * 40
SHA_HEAD = "d" * 40


def _commit(
    sha: str,
    *,
    at: datetime,
    author: str = "alice",
    author_type: str = "NORMAL",
    parents: int = 1,
    message: str = "change",
) -> dict[str, Any]:
    epoch_ms = int(at.timestamp() * 1000)
    return {
        "id": sha,
        "message": message,
        "authorTimestamp": epoch_ms,
        "committerTimestamp": epoch_ms,
        "author": {"name": author, "displayName": author, "type": author_type},
        "parents": [{"id": "0" * 40}] * parents,
    }


async def _push(
    session: AsyncSession,
    *,
    delivery_id: str,
    repo: str,
    at: datetime,
    commits: list[dict[str, Any]],
    branch: str = "master",
) -> None:
    payload = {"eventKey": "repo:refs_changed", "commits": commits}
    await session.execute(
        text(
            "INSERT INTO bitbucket_events "
            "(delivery_id, event_type, repo_full_name, branch_name, commit_sha, "
            " occurred_at, team, jira_keys, payload) "
            "VALUES (:d, 'repo:refs_changed', :repo, :branch, :sha, :at, 'checkout', "
            " '{}', CAST(:payload AS jsonb))"
        ),
        {
            "d": delivery_id,
            "repo": repo,
            "branch": branch,
            "sha": commits[-1]["id"],
            "at": at,
            "payload": json.dumps(payload),
        },
    )


async def _deploy(
    session: AsyncSession,
    *,
    delivery_id: str,
    app: str,
    environment: str,
    revision: str,
    at: datetime,
) -> None:
    await session.execute(
        text(
            "INSERT INTO argocd_events "
            "(delivery_id, app_name, revision, operation_phase, environment, "
            " occurred_at, team, payload) "
            "VALUES (:d, :app, :rev, 'Succeeded', :env, :at, 'checkout', '{}')"
        ),
        {"d": delivery_id, "app": app, "rev": revision, "env": environment, "at": at},
    )


def _release_note(old: str, new: str) -> str:
    # The shape a release-note generator writes into the GitOps commit message.
    return (
        "Payments PROD 2.0.41\n\n"
        "[__payments-api 2.0.37 → 2.0.41__]"
        f"(https://git.example.com/projects/acme/repos/payments-api/compare/diff"
        f"?targetBranch={old}&sourceBranch={new})"
    )


@pytest_asyncio.fixture
async def seeded(
    session_factory: async_sessionmaker[AsyncSession],
) -> async_sessionmaker[AsyncSession]:
    """One release shipping four commits, deployed to intg then prod."""
    async with session_factory() as session:
        # Previous release's head — the range starts after this one.
        await _push(
            session,
            delivery_id="p0",
            repo=APP_REPO,
            at=BASE,
            commits=[_commit(SHA_PREV, at=BASE, message="previous release head")],
        )
        # Three changes land on master, each committed two hours apart. The
        # first push carries two commits, as a PR merge of two commits does.
        await _push(
            session,
            delivery_id="p1",
            repo=APP_REPO,
            at=BASE + timedelta(hours=2),
            commits=[
                _commit("b" * 40, at=BASE + timedelta(hours=1)),
                _commit("9" * 40, at=BASE + timedelta(hours=2)),
            ],
        )
        await _push(
            session,
            delivery_id="p2",
            repo=APP_REPO,
            at=BASE + timedelta(hours=4),
            commits=[
                _commit(
                    "c" * 40,
                    at=BASE + timedelta(hours=4),
                    parents=2,
                    message="Pull request #7: merge",
                ),
            ],
        )
        await _push(
            session,
            delivery_id="p3",
            repo=APP_REPO,
            at=BASE + timedelta(hours=6),
            commits=[
                _commit(
                    SHA_HEAD,
                    at=BASE + timedelta(hours=6),
                    author="ci-service",
                    author_type="SERVICE",
                    message="[maven-release-plugin] prepare release payments-api-2.0.41",
                ),
            ],
        )
        # A feature-branch push must not enter the metric.
        await _push(
            session,
            delivery_id="p4",
            repo=APP_REPO,
            at=BASE + timedelta(hours=5),
            branch="feature/x",
            commits=[_commit("e" * 40, at=BASE + timedelta(hours=5))],
        )
        # The GitOps release commit naming the range, then its deploys.
        await _push(
            session,
            delivery_id="g1",
            repo=GITOPS_REPO,
            at=BASE + timedelta(hours=7),
            commits=[
                _commit(
                    "f" * 40,
                    at=BASE + timedelta(hours=7),
                    message=_release_note(SHA_PREV, SHA_HEAD),
                ),
            ],
        )
        await _deploy(
            session,
            delivery_id="d-intg",
            app="payments-intg",
            environment="intg",
            revision="f" * 40,
            at=BASE + timedelta(hours=8),
        )
        # Same revision, two prod Apps: one change must not count twice.
        await _deploy(
            session,
            delivery_id="d-prod-1",
            app="payments-intranet-prod",
            environment="prod",
            revision="f" * 40,
            at=BASE + timedelta(hours=10),
        )
        await _deploy(
            session,
            delivery_id="d-prod-2",
            app="payments-extranet-prod",
            environment="prod",
            revision="f" * 40,
            at=BASE + timedelta(hours=12),
        )
        await session.commit()

    async with session_factory() as session:
        for statement in DROP_VIEWS:
            await session.execute(text(statement))
        for statement in CREATE_VIEWS:
            await session.execute(text(statement))
        await session.commit()

    return session_factory


class TestCommitSightings:
    async def test_only_master_commits_are_sighted(self, seeded: Any) -> None:
        async with seeded() as session:
            rows = (
                (
                    await session.execute(
                        text("SELECT commit_sha FROM commit_sightings WHERE repo_full_name = :r"),
                        {"r": APP_REPO},
                    )
                )
                .scalars()
                .all()
            )

        # The feature-branch commit is absent: a change enters the release
        # stream when it lands on master, and counting both double-counts it.
        assert "e" * 40 not in rows
        assert {SHA_PREV, "b" * 40, "9" * 40, "c" * 40, SHA_HEAD} == set(rows)

    async def test_classification_columns(self, seeded: Any) -> None:
        async with seeded() as session:
            rows = dict(
                (
                    await session.execute(
                        text(
                            "SELECT commit_sha, (is_merge, author_is_service_account)::text "
                            "FROM commit_sightings WHERE repo_full_name = :r"
                        ),
                        {"r": APP_REPO},
                    )
                ).all()
            )

        assert rows["c" * 40] == "(t,f)"  # merge commit
        assert rows[SHA_HEAD] == "(f,t)"  # release tooling, SERVICE account
        assert rows["b" * 40] == "(f,f)"  # an ordinary change


class TestLeadTimeChanges:
    async def test_lead_time_measured_from_commit_to_first_deploy(self, seeded: Any) -> None:
        async with seeded() as session:
            row = (
                await session.execute(
                    text(
                        "SELECT extract(epoch FROM lead_time)/3600 FROM lead_time_changes "
                        "WHERE environment = 'prod' AND commit_sha = :sha"
                    ),
                    {"sha": "b" * 40},
                )
            ).scalar_one()

        # Committed at BASE+1h, first prod deploy at BASE+10h.
        assert row == 9

    async def test_second_deploy_of_the_same_release_does_not_recount(self, seeded: Any) -> None:
        async with seeded() as session:
            count = (
                await session.execute(
                    text(
                        "SELECT count(*) FROM lead_time_changes "
                        "WHERE environment = 'prod' AND commit_sha = :sha"
                    ),
                    {"sha": "b" * 40},
                )
            ).scalar_one()

        # Two prod Apps shipped it; it reached production once.
        assert count == 1

    async def test_range_excludes_the_previous_release_head(self, seeded: Any) -> None:
        async with seeded() as session:
            shas = (
                (
                    await session.execute(
                        text("SELECT commit_sha FROM lead_time_changes WHERE environment = 'prod'")
                    )
                )
                .scalars()
                .all()
            )

        # The range is (previous head, this head]: the old head shipped last time.
        assert SHA_PREV not in shas
        assert set(shas) == {"b" * 40, "9" * 40, "c" * 40, SHA_HEAD}

    async def test_every_commit_of_a_push_is_counted(self, seeded: Any) -> None:
        # Range membership is decided per push, so both commits of a
        # two-commit push are attributed to the same release — and each keeps
        # its own commit timestamp, an hour apart here.
        async with seeded() as session:
            rows = dict(
                (
                    await session.execute(
                        text(
                            "SELECT commit_sha, extract(epoch FROM lead_time)/3600 "
                            "FROM lead_time_changes WHERE environment = 'prod' "
                            "  AND commit_sha IN (:a, :b)"
                        ),
                        {"a": "b" * 40, "b": "9" * 40},
                    )
                ).all()
            )

        assert rows == {"b" * 40: 9, "9" * 40: 8}

    async def test_environments_are_measured_separately(self, seeded: Any) -> None:
        async with seeded() as session:
            rows = dict(
                (
                    await session.execute(
                        text(
                            "SELECT environment, extract(epoch FROM lead_time)/3600 "
                            "FROM lead_time_changes WHERE commit_sha = :sha"
                        ),
                        {"sha": "b" * 40},
                    )
                ).all()
            )

        assert rows == {"intg": 7, "prod": 9}

    async def test_default_metric_filter_keeps_only_real_changes(self, seeded: Any) -> None:
        async with seeded() as session:
            shas = (
                (
                    await session.execute(
                        text(
                            "SELECT commit_sha FROM lead_time_changes "
                            "WHERE environment = 'prod' "
                            "  AND NOT is_merge AND NOT author_is_service_account"
                        )
                    )
                )
                .scalars()
                .all()
            )

        # The merge commit and the release-plugin commit are artifacts of
        # shipping, not changes that were shipped. Both commits of the
        # two-commit push survive.
        assert sorted(shas) == sorted(["b" * 40, "9" * 40])
