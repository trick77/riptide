"""Startup-time cross-validation between catalog and team-keys.

Every team in the catalog must have an entry in team-keys.json. Catalog
teams without keys → fatal at startup. Extra keys (no matching catalog
team) → warning, but app still starts.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest

from _keys import TEAM_KEYS, hash_token
from riptide_collector.main import StartupValidationError, create_app
from riptide_collector.settings import Settings


def _settings(
    catalog: dict[str, Any], keys: dict[str, str], tmp_path: Path, db_url: str
) -> Settings:
    catalog_file = tmp_path / "catalog.json"
    catalog_file.write_text(json.dumps(catalog), encoding="utf-8")
    keys_file = tmp_path / "team-keys.json"
    keys_file.write_text(json.dumps(keys), encoding="utf-8")
    return Settings(
        db_url=db_url,
        catalog_path=catalog_file,
        team_keys_path=keys_file,
    )


def test_missing_team_key_fails_startup(tmp_path: Path, db_url: str) -> None:
    catalog: dict[str, Any] = {
        "teams": [
            {"name": "checkout", "group_email": "team-checkout@example.com"},
            {"name": "ghost", "group_email": "team-ghost@example.com"},
        ],
        "automation": {},
    }
    keys = {"checkout": hash_token("ck")}  # no entry for "ghost"
    settings = _settings(catalog, keys, tmp_path, db_url)

    with pytest.raises(StartupValidationError, match="ghost"):
        create_app(settings)


def test_extra_key_only_warns_does_not_fail(tmp_path: Path, db_url: str) -> None:
    catalog: dict[str, Any] = {
        "teams": [{"name": "checkout", "group_email": "team-checkout@example.com"}],
        "automation": {},
    }
    keys = {**TEAM_KEYS}  # has both checkout and platform; only checkout in catalog
    settings = _settings(catalog, keys, tmp_path, db_url)

    # Should not raise
    app = create_app(settings)
    assert app is not None
