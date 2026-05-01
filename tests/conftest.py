from __future__ import annotations

import json
import os
from collections.abc import AsyncIterator, Iterator
from pathlib import Path
from typing import Any

import pytest
import pytest_asyncio
from httpx import ASGITransport, AsyncClient
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine
from testcontainers.postgres import PostgresContainer

from _keys import TEAM_KEYS
from riptide_collector.main import create_app
from riptide_collector.models import Base
from riptide_collector.settings import Settings

VALID_CATALOG: dict[str, Any] = {
    "teams": [
        {"name": "checkout", "group_email": "team-checkout@example.com"},
        {"name": "platform", "group_email": "team-platform@example.com"},
    ],
    "automation": {
        "renovate": {
            "authors": ["renovate-bot", "renovate[bot]"],
            "branch_prefixes": ["renovate/"],
        },
        "dependabot": {
            "authors": ["dependabot[bot]"],
            "branch_prefixes": ["dependabot/"],
        },
    },
}


@pytest.fixture(scope="session")
def db_url() -> Iterator[str]:
    """Provide a Postgres URL.

    If RIPTIDE_TEST_DB_URL is set (e.g. CI service container), use it directly.
    Otherwise spin up a throwaway Postgres via testcontainers (local dev).
    """
    explicit = os.environ.get("RIPTIDE_TEST_DB_URL")
    if explicit:
        yield explicit
        return
    container = PostgresContainer("postgres:17-alpine", driver="asyncpg")
    container.start()
    try:
        yield container.get_connection_url()
    finally:
        container.stop()


@pytest_asyncio.fixture(scope="session")
async def initialized_engine(db_url: str) -> AsyncIterator[None]:
    engine = create_async_engine(db_url, future=True)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.drop_all)
        await conn.run_sync(Base.metadata.create_all)
    await engine.dispose()
    yield


@pytest_asyncio.fixture
async def session_factory(
    db_url: str, initialized_engine: None
) -> AsyncIterator[async_sessionmaker[AsyncSession]]:
    del initialized_engine
    engine = create_async_engine(db_url, future=True)
    factory = async_sessionmaker(engine, expire_on_commit=False, class_=AsyncSession)
    async with engine.begin() as conn:
        for table in ("bitbucket_events", "pipeline_events", "argocd_events", "noergler_events"):
            await conn.exec_driver_sql(f"TRUNCATE TABLE {table} RESTART IDENTITY")
    try:
        yield factory
    finally:
        await engine.dispose()


@pytest.fixture
def catalog_file(tmp_path: Path) -> Path:
    path = tmp_path / "riptide-catalog.json"
    path.write_text(json.dumps(VALID_CATALOG), encoding="utf-8")
    return path


@pytest.fixture
def team_keys_file(tmp_path: Path) -> Path:
    path = tmp_path / "team-keys.json"
    path.write_text(json.dumps(TEAM_KEYS), encoding="utf-8")
    return path


@pytest.fixture
def settings(catalog_file: Path, team_keys_file: Path, db_url: str) -> Settings:
    os.environ["RIPTIDE_DB_URL"] = db_url
    os.environ["RIPTIDE_CATALOG_PATH"] = str(catalog_file)
    os.environ["RIPTIDE_TEAM_KEYS_PATH"] = str(team_keys_file)
    return Settings()


@pytest_asyncio.fixture
async def client(
    settings: Settings,
    session_factory: async_sessionmaker[AsyncSession],
) -> AsyncIterator[AsyncClient]:
    del session_factory  # ensures schema exists and tables are truncated per-test
    app = create_app(settings)
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as ac:
        yield ac
