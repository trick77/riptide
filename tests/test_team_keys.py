import json
import os
import time
from pathlib import Path

import pytest

from riptide_collector.team_keys import TeamKeysError, TeamKeysStore, load_team_keys_from_path


def _write(path: Path, data: dict[str, dict[str, str]]) -> Path:
    path.write_text(json.dumps(data), encoding="utf-8")
    return path


class TestLoadTeamKeys:
    def test_loads_valid(self, tmp_path: Path) -> None:
        path = _write(
            tmp_path / "k.json",
            {"checkout": {"bitbucket": "b", "argocd": "a"}},
        )
        out = load_team_keys_from_path(path)
        assert out == {"checkout": {"bitbucket": "b", "argocd": "a"}}

    def test_rejects_non_object_root(self, tmp_path: Path) -> None:
        path = tmp_path / "k.json"
        path.write_text("[]", encoding="utf-8")
        with pytest.raises(TeamKeysError):
            load_team_keys_from_path(path)

    def test_rejects_flat_string_value(self, tmp_path: Path) -> None:
        path = tmp_path / "k.json"
        path.write_text(json.dumps({"checkout": "raw"}), encoding="utf-8")
        with pytest.raises(TeamKeysError, match="object of source"):
            load_team_keys_from_path(path)

    def test_rejects_empty_inner_dict(self, tmp_path: Path) -> None:
        path = tmp_path / "k.json"
        path.write_text(json.dumps({"checkout": {}}), encoding="utf-8")
        with pytest.raises(TeamKeysError, match="non-empty object"):
            load_team_keys_from_path(path)

    def test_rejects_unknown_source(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": {"github": "raw"}})
        with pytest.raises(TeamKeysError, match="unknown source"):
            load_team_keys_from_path(path)

    def test_rejects_empty_value(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": {"bitbucket": ""}})
        with pytest.raises(TeamKeysError, match="non-empty"):
            load_team_keys_from_path(path)

    def test_rejects_empty_team(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"": {"bitbucket": "raw"}})
        with pytest.raises(TeamKeysError, match="non-empty"):
            load_team_keys_from_path(path)

    def test_rejects_non_string_value(self, tmp_path: Path) -> None:
        path = tmp_path / "k.json"
        path.write_text(json.dumps({"checkout": {"bitbucket": 1234}}), encoding="utf-8")
        with pytest.raises(TeamKeysError, match="non-empty"):
            load_team_keys_from_path(path)


class TestTeamKeysStoreLookup:
    def test_lookup_returns_team_for_matching_source(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": {"argocd": "a-raw"}})
        store = TeamKeysStore(path)
        assert store.lookup("a-raw", "argocd") == "checkout"

    def test_lookup_wrong_source_returns_none(self, tmp_path: Path) -> None:
        # Strict source binding: even though the byte value matches the
        # team's argocd token, looking it up under bitbucket must fail.
        path = _write(tmp_path / "k.json", {"checkout": {"argocd": "shared-raw"}})
        store = TeamKeysStore(path)
        assert store.lookup("shared-raw", "argocd") == "checkout"
        assert store.lookup("shared-raw", "bitbucket") is None

    def test_lookup_unknown_source_returns_none(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": {"argocd": "a"}})
        store = TeamKeysStore(path)
        assert store.lookup("a", "github") is None

    def test_lookup_outbound_only_source_rejected(self, tmp_path: Path) -> None:
        # bitbucket_api is an outbound BBS DC personal access token used
        # by the collector to fetch PR diff stats. It must never
        # authenticate an inbound request, even via direct lookup with
        # the matching source name.
        path = _write(
            tmp_path / "k.json",
            {"checkout": {"bitbucket": "hmac", "bitbucket_api": "pat"}},
        )
        store = TeamKeysStore(path)
        # Direct retrieval (used by the outbound enrichment task) still works.
        assert store.get_secret("checkout", "bitbucket_api") == "pat"
        # But it cannot be exchanged for a team via inbound auth lookups.
        assert store.lookup("pat", "bitbucket_api") is None
        assert store.lookup_any_source("pat") is None

    def test_lookup_unknown_returns_none(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": {"argocd": "raw"}})
        store = TeamKeysStore(path)
        assert store.lookup("wrong", "argocd") is None

    def test_lookup_empty_returns_none(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": {"argocd": "raw"}})
        store = TeamKeysStore(path)
        assert store.lookup("", "argocd") is None
        assert store.lookup(None, "argocd") is None

    def test_lookup_any_source(self, tmp_path: Path) -> None:
        path = _write(
            tmp_path / "k.json",
            {"checkout": {"argocd": "a", "jenkins": "j"}},
        )
        store = TeamKeysStore(path)
        assert store.lookup_any_source("a") == "checkout"
        assert store.lookup_any_source("j") == "checkout"
        assert store.lookup_any_source("nope") is None

    def test_has_source_and_get_secret(self, tmp_path: Path) -> None:
        path = _write(
            tmp_path / "k.json",
            {"checkout": {"bitbucket": "b"}},
        )
        store = TeamKeysStore(path)
        assert store.has_source("checkout", "bitbucket") is True
        assert store.has_source("checkout", "argocd") is False
        assert store.has_source("ghost", "bitbucket") is False
        assert store.get_secret("checkout", "bitbucket") == "b"
        assert store.get_secret("checkout", "argocd") is None

    def test_team_names(self, tmp_path: Path) -> None:
        path = _write(
            tmp_path / "k.json",
            {"checkout": {"argocd": "a"}, "platform": {"argocd": "b"}},
        )
        store = TeamKeysStore(path)
        assert store.team_names() == {"checkout", "platform"}


class TestHotReload:
    def test_picks_up_added_team(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": {"argocd": "a"}})
        store = TeamKeysStore(path)
        assert store.lookup("b", "argocd") is None

        time.sleep(0.01)
        _write(
            tmp_path / "k.json",
            {"checkout": {"argocd": "a"}, "platform": {"argocd": "b"}},
        )
        os.utime(path, None)

        assert store.maybe_reload() is True
        assert store.lookup("b", "argocd") == "platform"

    def test_keeps_old_on_invalid_reload(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": {"argocd": "a"}})
        store = TeamKeysStore(path)

        time.sleep(0.01)
        path.write_text("not valid json", encoding="utf-8")
        os.utime(path, None)

        assert store.maybe_reload() is False
        assert store.reload_failures == 1
        assert store.lookup("a", "argocd") == "checkout"

    def test_noop_when_unchanged(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": {"argocd": "a"}})
        store = TeamKeysStore(path)
        assert store.maybe_reload() is False
