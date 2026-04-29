import hashlib
import json
import os
import time
from pathlib import Path

import pytest

from riptide_collector.team_keys import TeamKeysError, TeamKeysStore, load_team_keys_from_path


def _h(raw: str) -> str:
    return hashlib.sha256(raw.encode("utf-8")).hexdigest()


def _write(path: Path, data: dict[str, str]) -> Path:
    path.write_text(json.dumps(data), encoding="utf-8")
    return path


class TestLoadTeamKeys:
    def test_loads_valid(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": _h("raw")})
        out = load_team_keys_from_path(path)
        assert out == {"checkout": _h("raw")}

    def test_rejects_non_object_root(self, tmp_path: Path) -> None:
        path = tmp_path / "k.json"
        path.write_text("[]", encoding="utf-8")
        with pytest.raises(TeamKeysError):
            load_team_keys_from_path(path)

    def test_rejects_short_hash(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": "abc"})
        with pytest.raises(TeamKeysError, match="64-char"):
            load_team_keys_from_path(path)

    def test_rejects_uppercase_hash(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": _h("raw").upper()})
        with pytest.raises(TeamKeysError, match="64-char"):
            load_team_keys_from_path(path)

    def test_rejects_empty_team(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"": _h("raw")})
        with pytest.raises(TeamKeysError, match="non-empty"):
            load_team_keys_from_path(path)


class TestTeamKeysStoreLookup:
    def test_lookup_returns_team(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": _h("checkout-raw")})
        store = TeamKeysStore(path)
        assert store.lookup("checkout-raw") == "checkout"

    def test_lookup_unknown_returns_none(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": _h("raw")})
        store = TeamKeysStore(path)
        assert store.lookup("wrong") is None

    def test_lookup_empty_returns_none(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": _h("raw")})
        store = TeamKeysStore(path)
        assert store.lookup("") is None
        assert store.lookup(None) is None

    def test_team_names(self, tmp_path: Path) -> None:
        path = _write(
            tmp_path / "k.json",
            {"checkout": _h("a"), "platform": _h("b")},
        )
        store = TeamKeysStore(path)
        assert store.team_names() == {"checkout", "platform"}


class TestHotReload:
    def test_picks_up_added_team(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": _h("a")})
        store = TeamKeysStore(path)
        assert store.lookup("b") is None

        time.sleep(0.01)
        _write(tmp_path / "k.json", {"checkout": _h("a"), "platform": _h("b")})
        os.utime(path, None)

        assert store.maybe_reload() is True
        assert store.lookup("b") == "platform"

    def test_keeps_old_on_invalid_reload(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": _h("a")})
        store = TeamKeysStore(path)

        time.sleep(0.01)
        path.write_text("not valid json", encoding="utf-8")
        os.utime(path, None)

        assert store.maybe_reload() is False
        assert store.reload_failures == 1
        assert store.lookup("a") == "checkout"

    def test_noop_when_unchanged(self, tmp_path: Path) -> None:
        path = _write(tmp_path / "k.json", {"checkout": _h("a")})
        store = TeamKeysStore(path)
        assert store.maybe_reload() is False
