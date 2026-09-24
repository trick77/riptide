> **Archived.** This is the Python implementation (FastAPI, SQLAlchemy,
> Alembic, structlog) that preceded the Go one in `backend/`, kept for
> reference and superseded on 2026-09-24. Nothing builds or tests it: no
> workflow runs against `archive/`, Dependabot does not watch it, and
> `release.yaml` ignores changes here so an edit cannot cut a release.

# riptide-collector (Python, archived)

What is here:

| Path | Was |
|---|---|
| `src/riptide_collector/` | the collector: routers, parsers, config and team-key hot reload, logging |
| `tests/` | pytest suite (testcontainers Postgres); `tests/fixtures/` is copied to `backend/internal/parse/testdata/` |
| `migrations/`, `alembic.ini` | Alembic revisions 0001–0004; the Go schema in `backend/internal/store/migrations/0001_initial.sql` is their squashed end state |
| `scripts/bitbucket_onboarding.py`, `scripts/check_onboarding.py` | operator scripts, now `riptide onboard-bitbucket` and `riptide check-onboarding` |
| `pyproject.toml`, `uv.lock`, `Containerfile`, `.pre-commit-config.yaml` | build and tooling |

To run it again, from this directory: `uv sync`, then `uv run pytest` (needs
Docker for testcontainers, or `RIPTIDE_TEST_DB_URL`). The Go collector does
not share a database with it: the Go schema starts fresh, with no Alembic
history.
