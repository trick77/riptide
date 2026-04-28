import json
from pathlib import Path
from typing import Any

import pytest

from riptide_collector.catalog import CatalogError, CatalogStore, load_catalog_from_path

VALID: dict[str, Any] = {
    "services": [
        {
            "id": "svc-a",
            "team": "team-x",
            "bitbucket_repos": ["org/a"],
            "argocd_apps": ["a-prod"],
            "jenkins_jobs": ["a-deploy"],
        }
    ],
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
        assert len(catalog.services) == 1
        assert catalog.services[0].id == "svc-a"
        assert "team-x" in catalog.teams_by_name

    def test_rejects_non_object_root(self, tmp_path: Path) -> None:
        path = tmp_path / "c.json"
        path.write_text("[]", encoding="utf-8")
        with pytest.raises(CatalogError):
            load_catalog_from_path(path)

    def test_rejects_dangling_team_reference(self, tmp_path: Path) -> None:
        bad = json.loads(json.dumps(VALID))
        bad["services"][0]["team"] = "ghost"
        path = _write(tmp_path / "c.json", bad)
        with pytest.raises(CatalogError, match="unknown team"):
            load_catalog_from_path(path)

    def test_rejects_invalid_email(self, tmp_path: Path) -> None:
        bad = json.loads(json.dumps(VALID))
        bad["teams"][0]["group_email"] = "not-an-email"
        path = _write(tmp_path / "c.json", bad)
        with pytest.raises(CatalogError, match="group_email"):
            load_catalog_from_path(path)

    def test_rejects_duplicate_service_id(self, tmp_path: Path) -> None:
        bad = json.loads(json.dumps(VALID))
        bad["services"].append(json.loads(json.dumps(bad["services"][0])))
        path = _write(tmp_path / "c.json", bad)
        with pytest.raises(CatalogError, match="duplicate service id"):
            load_catalog_from_path(path)

    def test_rejects_duplicate_team_name(self, tmp_path: Path) -> None:
        bad = json.loads(json.dumps(VALID))
        bad["teams"].append({"name": "team-x", "group_email": "y@example.com"})
        path = _write(tmp_path / "c.json", bad)
        with pytest.raises(CatalogError, match="duplicate team"):
            load_catalog_from_path(path)

    def test_rejects_shared_repo(self, tmp_path: Path) -> None:
        bad = json.loads(json.dumps(VALID))
        bad["services"].append(
            {
                "id": "svc-b",
                "team": "team-x",
                "bitbucket_repos": ["org/a"],
                "argocd_apps": [],
                "jenkins_jobs": [],
            }
        )
        path = _write(tmp_path / "c.json", bad)
        with pytest.raises(CatalogError, match="bitbucket_repo"):
            load_catalog_from_path(path)


class TestCatalogStoreReload:
    def test_resolvers_return_resolution(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = CatalogStore(path)
        assert store.resolve_bitbucket("org/a") is not None
        assert store.resolve_bitbucket("ghost/repo") is None
        assert store.resolve_jenkins("a-deploy") is not None
        assert store.resolve_argocd("a-prod") is not None

    def test_team_lookup(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = CatalogStore(path)
        team = store.team("team-x")
        assert team is not None
        assert team.group_email == "x@example.com"

    def test_hot_reload_picks_up_change(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = CatalogStore(path)
        assert store.resolve_bitbucket("org/new") is None

        updated = json.loads(json.dumps(VALID))
        updated["services"][0]["bitbucket_repos"].append("org/new")
        # Bump mtime by writing again with a later timestamp
        import os
        import time

        time.sleep(0.01)
        path.write_text(json.dumps(updated), encoding="utf-8")
        os.utime(path, None)

        assert store.maybe_reload() is True
        assert store.resolve_bitbucket("org/new") is not None

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
        assert store.resolve_bitbucket("org/a") is not None

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
