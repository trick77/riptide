import json
from pathlib import Path
from typing import Any

import pytest

from riptide_collector.config import (
    DEFAULT_PRODUCTION_STAGE,
    RiptideConfigError,
    RiptideConfigStore,
    load_config_from_path,
)

VALID: dict[str, Any] = {
    "teams": [{"name": "team-x", "group_email": "x@example.com"}],
    "automation": {"renovate": {"authors": ["renovate-bot"], "branch_prefixes": ["renovate/"]}},
}


def _write(path: Path, data: dict[str, Any]) -> Path:
    path.write_text(json.dumps(data), encoding="utf-8")
    return path


class TestLoadConfig:
    def test_loads_valid_config(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        config = load_config_from_path(path)
        assert "team-x" in config.teams_by_name
        assert config.teams_by_name["team-x"].group_email == "x@example.com"

    def test_rejects_non_object_root(self, tmp_path: Path) -> None:
        path = tmp_path / "c.json"
        path.write_text("[]", encoding="utf-8")
        with pytest.raises(RiptideConfigError):
            load_config_from_path(path)

    def test_rejects_invalid_email(self, tmp_path: Path) -> None:
        bad = json.loads(json.dumps(VALID))
        bad["teams"][0]["group_email"] = "not-an-email"
        path = _write(tmp_path / "c.json", bad)
        with pytest.raises(RiptideConfigError, match="group_email"):
            load_config_from_path(path)

    def test_rejects_duplicate_team_name(self, tmp_path: Path) -> None:
        bad = json.loads(json.dumps(VALID))
        bad["teams"].append({"name": "team-x", "group_email": "y@example.com"})
        path = _write(tmp_path / "c.json", bad)
        with pytest.raises(RiptideConfigError, match="duplicate team"):
            load_config_from_path(path)

    def test_rejects_missing_team_name(self, tmp_path: Path) -> None:
        bad = json.loads(json.dumps(VALID))
        bad["teams"].append({"group_email": "y@example.com"})
        path = _write(tmp_path / "c.json", bad)
        with pytest.raises(RiptideConfigError, match="missing `name`"):
            load_config_from_path(path)


class TestRiptideConfigStoreReload:
    def test_team_lookup(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)
        team = store.team("team-x")
        assert team is not None
        assert team.group_email == "x@example.com"
        assert store.team("ghost") is None
        assert store.team(None) is None

    def test_hot_reload_picks_up_change(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)
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
        store = RiptideConfigStore(path)

        import os
        import time

        time.sleep(0.01)
        path.write_text("not valid json", encoding="utf-8")
        os.utime(path, None)

        assert store.maybe_reload() is False
        assert store.reload_failures == 1
        # Old config still works
        assert store.team("team-x") is not None

    def test_reload_noop_when_unchanged(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)
        assert store.maybe_reload() is False


class TestAutomationDetection:
    def test_author_match(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)
        assert store.detect_automation_source("renovate-bot", None) == "renovate"

    def test_branch_prefix_match(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)
        assert store.detect_automation_source("alice", "renovate/something") == "renovate"

    def test_other_bot_fallback(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)
        assert store.detect_automation_source("some-bot", "feature/x") == "other-bot"

    def test_human_returns_none(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)
        assert store.detect_automation_source("alice", "feature/x") is None

    def test_review_bot_author_match(self, tmp_path: Path) -> None:
        # Review-time bots (e.g. noergler) need a config entry to be
        # recognised because their handle ("noergler") doesn't match the
        # `*-bot` / `*[bot]` heuristic. With the entry in place, comment
        # rows authored by noergler land with is_automated=true and the
        # DX Core 4 pickup-time query filters them out.
        data = json.loads(json.dumps(VALID))
        data["automation"]["noergler"] = {"authors": ["noergler"], "branch_prefixes": []}
        path = _write(tmp_path / "c.json", data)
        store = RiptideConfigStore(path)
        assert store.detect_automation_source("noergler", None) == "noergler"


class TestEnvironments:
    def test_defaults_when_block_absent(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        config = load_config_from_path(path)
        assert config.environments.production_stage == DEFAULT_PRODUCTION_STAGE

    def test_reads_configured_production_stage(self, tmp_path: Path) -> None:
        data = json.loads(json.dumps(VALID))
        data["environments"] = {"production_stage": "PROD"}
        path = _write(tmp_path / "c.json", data)
        config = load_config_from_path(path)
        assert config.environments.production_stage == "prod"

    def test_rejects_non_object(self, tmp_path: Path) -> None:
        data = json.loads(json.dumps(VALID))
        data["environments"] = "prod"
        path = _write(tmp_path / "c.json", data)
        with pytest.raises(RiptideConfigError, match="environments"):
            load_config_from_path(path)

    def test_rejects_empty_production_stage(self, tmp_path: Path) -> None:
        data = json.loads(json.dumps(VALID))
        data["environments"] = {"production_stage": "  "}
        path = _write(tmp_path / "c.json", data)
        with pytest.raises(RiptideConfigError, match="production_stage"):
            load_config_from_path(path)

    def test_ignored_stages_default_empty(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        config = load_config_from_path(path)
        assert config.environments.ignored_stages == frozenset()

    def test_reads_ignored_stages_lowercased(self, tmp_path: Path) -> None:
        data = json.loads(json.dumps(VALID))
        data["environments"] = {"ignored_stages": ["Dev", "ENTW", " syst "]}
        path = _write(tmp_path / "c.json", data)
        config = load_config_from_path(path)
        assert config.environments.ignored_stages == frozenset({"dev", "entw", "syst"})

    def test_rejects_non_list_ignored_stages(self, tmp_path: Path) -> None:
        data = json.loads(json.dumps(VALID))
        data["environments"] = {"ignored_stages": "dev"}
        path = _write(tmp_path / "c.json", data)
        with pytest.raises(RiptideConfigError, match="ignored_stages"):
            load_config_from_path(path)

    def test_rejects_empty_string_ignored_stage(self, tmp_path: Path) -> None:
        data = json.loads(json.dumps(VALID))
        data["environments"] = {"ignored_stages": ["dev", "  "]}
        path = _write(tmp_path / "c.json", data)
        with pytest.raises(RiptideConfigError, match="ignored_stages"):
            load_config_from_path(path)

    def test_rejects_production_stage_in_ignored_list(self, tmp_path: Path) -> None:
        data = json.loads(json.dumps(VALID))
        data["environments"] = {"production_stage": "prod", "ignored_stages": ["dev", "PROD"]}
        path = _write(tmp_path / "c.json", data)
        with pytest.raises(RiptideConfigError, match="production_stage"):
            load_config_from_path(path)


class TestServiceAccountDetection:
    def test_host_declared_service_account_is_automation(self, tmp_path: Path) -> None:
        # Bitbucket marks its own system user as a service account, so no
        # installation has to list it by name.
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)

        assert (
            store.detect_automation_source("bitbucket.system-user", None, "Bitbucket", True)
            == "service-account"
        )

    def test_configured_source_keeps_its_own_name(self, tmp_path: Path) -> None:
        # The host's verdict is a fallback: a configured bot stays attributed
        # to the tool it is, so per-source views keep working.
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)

        assert store.detect_automation_source("renovate-bot", None, None, True) == "renovate"

    def test_human_unaffected(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)

        assert store.detect_automation_source("alice", "feature/x", "Alice", False) is None

    def test_bot_shaped_name_does_not_mask_a_service_account(self, tmp_path: Path) -> None:
        # A stated fact beats a guess from the handle: labelling a technical
        # account `other-bot` would drop it into bot-velocity views, which
        # exist to show work that bots actually author.
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)

        assert store.detect_automation_source("ci-bot", None, None, True) == "service-account"


class TestAutomationByDisplayName:
    def test_display_name_match_when_login_is_ordinary(self, tmp_path: Path) -> None:
        # A review bot provisioned as a normal user account: the login is a
        # short handle like any human's, and only `displayName` says what it
        # is. Matching the login alone lets it count as a human reviewer,
        # which drives the DX Core 4 pickup-time metric toward zero.
        data = json.loads(json.dumps(VALID))
        data["automation"]["noergler"] = {"authors": ["noergler"], "branch_prefixes": []}
        path = _write(tmp_path / "c.json", data)
        store = RiptideConfigStore(path)

        assert store.detect_automation_source("rop", None, "noergler") == "noergler"

    def test_display_name_match_is_case_insensitive(self, tmp_path: Path) -> None:
        # Display names are human-formatted; a case-only mismatch against the
        # configured handle would silently reproduce the zero-pickup-time bug.
        data = json.loads(json.dumps(VALID))
        data["automation"]["noergler"] = {"authors": ["noergler"], "branch_prefixes": []}
        path = _write(tmp_path / "c.json", data)
        store = RiptideConfigStore(path)

        assert store.detect_automation_source("rop", None, "Noergler") == "noergler"

    def test_bot_shaped_display_name_falls_back_to_other_bot(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)

        assert store.detect_automation_source("svc01", None, "release-bot") == "other-bot"

    def test_human_display_name_stays_human(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)

        assert store.detect_automation_source("alice", "feature/x", "Alice Example") is None

    def test_login_match_still_wins_without_display_name(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "c.json", VALID)
        store = RiptideConfigStore(path)

        assert store.detect_automation_source("renovate-bot", None) == "renovate"
