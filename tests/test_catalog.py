import json
from pathlib import Path
from typing import Any

import pytest

from riptide_collector.catalog import (
    DEFAULT_PRODUCTION_STAGE,
    CatalogError,
    CatalogStore,
    load_catalog_from_path,
)

VALID: dict[str, Any] = {
    "teams": [{"name": "team-x", "group_email": "x@example.com"}],
    "automation": {"renovate": {"authors": ["renovate-bot"], "branch_prefixes": ["renovate/"]}},
}


def _write(path: Path, data: dict[str, Any]) -> Path:
    path.write_text(json.dumps(data), encoding="utf-8")
    return path


class TestLoadCatalog:
    def test_loads_valid_catalog(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        catalog = load_catalog_from_path(path)
        assert "team-x" in catalog.teams_by_name
        assert catalog.teams_by_name["team-x"].group_email == "x@example.com"

    def test_rejects_non_object_root(self, tmp_path: Path) -> None:
        path = tmp_path / "c.json"
        path.write_text("[]", encoding="utf-8")
        with pytest.raises(CatalogError):
            load_catalog_from_path(path)

    def test_rejects_invalid_email(self, tmp_path: Path) -> None:
        bad = json.loads(json.dumps(VALID))
        bad["teams"][0]["group_email"] = "not-an-email"
        path = _write(tmp_path / "c.json", bad)
        with pytest.raises(CatalogError, match="group_email"):
            load_catalog_from_path(path)

    def test_rejects_duplicate_team_name(self, tmp_path: Path) -> None:
        bad = json.loads(json.dumps(VALID))
        bad["teams"].append({"name": "team-x", "group_email": "y@example.com"})
        path = _write(tmp_path / "c.json", bad)
        with pytest.raises(CatalogError, match="duplicate team"):
            load_catalog_from_path(path)

    def test_rejects_missing_team_name(self, tmp_path: Path) -> None:
        bad = json.loads(json.dumps(VALID))
        bad["teams"].append({"group_email": "y@example.com"})
        path = _write(tmp_path / "c.json", bad)
        with pytest.raises(CatalogError, match="missing `name`"):
            load_catalog_from_path(path)


class TestCatalogStoreReload:
    def test_team_lookup(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = CatalogStore(path)
        team = store.team("team-x")
        assert team is not None
        assert team.group_email == "x@example.com"
        assert store.team("ghost") is None
        assert store.team(None) is None

    def test_hot_reload_picks_up_change(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = CatalogStore(path)
        assert store.team("team-y") is None

        updated = json.loads(json.dumps(VALID))
        updated["teams"].append({"name": "team-y", "group_email": "y@example.com"})
        import os
        import time

        time.sleep(0.01)
        path.write_text(json.dumps(updated), encoding="utf-8")
        os.utime(path, None)

        assert store.maybe_reload() is True
        assert store.team("team-y") is not None

    def test_hot_reload_keeps_old_on_failure(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = CatalogStore(path)

        import os
        import time

        time.sleep(0.01)
        path.write_text("not valid json", encoding="utf-8")
        os.utime(path, None)

        assert store.maybe_reload() is False
        assert store.reload_failures == 1
        # Old catalog still works
        assert store.team("team-x") is not None

    def test_reload_noop_when_unchanged(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = CatalogStore(path)
        assert store.maybe_reload() is False


class TestAutomationDetection:
    def test_author_match(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = CatalogStore(path)
        assert store.detect_automation_source("renovate-bot", None) == "renovate"

    def test_branch_prefix_match(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = CatalogStore(path)
        assert store.detect_automation_source("alice", "renovate/something") == "renovate"

    def test_other_bot_fallback(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = CatalogStore(path)
        assert store.detect_automation_source("some-bot", "feature/x") == "other-bot"

    def test_human_returns_none(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = CatalogStore(path)
        assert store.detect_automation_source("alice", "feature/x") is None


class TestEnvironments:
    def test_defaults_when_block_absent(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        catalog = load_catalog_from_path(path)
        assert catalog.environments.production_stage == DEFAULT_PRODUCTION_STAGE

    def test_reads_configured_production_stage(self, tmp_path: Path) -> None:
        data = json.loads(json.dumps(VALID))
        data["environments"] = {"production_stage": "PROD"}
        path = _write(tmp_path / "c.json", data)
        catalog = load_catalog_from_path(path)
        assert catalog.environments.production_stage == "prod"

    def test_rejects_non_object(self, tmp_path: Path) -> None:
        data = json.loads(json.dumps(VALID))
        data["environments"] = "prod"
        path = _write(tmp_path / "c.json", data)
        with pytest.raises(CatalogError, match="environments"):
            load_catalog_from_path(path)

    def test_rejects_empty_production_stage(self, tmp_path: Path) -> None:
        data = json.loads(json.dumps(VALID))
        data["environments"] = {"production_stage": "  "}
        path = _write(tmp_path / "c.json", data)
        with pytest.raises(CatalogError, match="production_stage"):
            load_catalog_from_path(path)
