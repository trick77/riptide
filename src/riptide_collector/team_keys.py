"""Team bearer-key store with mtime-based hot reload.

Maps team name → sha256 hex of the raw bearer token. Raw keys never touch
disk; only their hashes do. Lookup hashes the incoming token and uses
constant-time comparison.

File shape:
    {
      "<team-name>": "<64-char sha256 hex>",
      ...
    }
"""

from __future__ import annotations

import hashlib
import hmac
import json
import re
import threading
from pathlib import Path
from typing import Any

from riptide_collector.logging_config import get_logger

logger = get_logger(__name__)

_HASH_RE = re.compile(r"^[0-9a-f]{64}$")


class TeamKeysError(ValueError):
    pass


def _validate(data: Any) -> dict[str, str]:
    if not isinstance(data, dict):
        raise TeamKeysError("team-keys file must be a JSON object at the top level")
    out: dict[str, str] = {}
    for team, value in data.items():
        if not isinstance(team, str) or not team:
            raise TeamKeysError("team key must be a non-empty string")
        if not isinstance(value, str) or not _HASH_RE.match(value):
            raise TeamKeysError(
                f"team {team!r} has invalid hash: expected 64-char lowercase sha256 hex"
            )
        out[team] = value
    return out


def load_team_keys_from_path(path: Path) -> dict[str, str]:
    with path.open("r", encoding="utf-8") as fh:
        data = json.load(fh)
    return _validate(data)


def _hash(raw: str) -> str:
    return hashlib.sha256(raw.encode("utf-8")).hexdigest()


class TeamKeysStore:
    """Thread-safe team-keys holder with mtime-based hot reload.

    Mirrors `CatalogStore`. Single instance per process.
    """

    def __init__(self, path: Path):
        self._path = path
        self._lock = threading.RLock()
        self._keys = load_team_keys_from_path(path)
        # Reverse index: hash -> team. Built once per load; lookups are O(1).
        self._by_hash = {h: team for team, h in self._keys.items()}
        self._mtime = path.stat().st_mtime
        self._reload_failures = 0

    @property
    def reload_failures(self) -> int:
        return self._reload_failures

    @property
    def path(self) -> Path:
        return self._path

    def team_names(self) -> set[str]:
        with self._lock:
            return set(self._keys.keys())

    def lookup(self, raw_token: str | None) -> str | None:
        """Return the team name for a raw bearer token, or None if no match.

        Constant-time comparison via `hmac.compare_digest` against the stored
        hash for each team. The reverse-index lookup is fast-path; the
        sequential compare is the actual constant-time check (timing leaks
        only the team count, which is public information anyway).
        """
        if not raw_token:
            return None
        candidate = _hash(raw_token)
        with self._lock:
            # Fast O(1) lookup AND a constant-time compare. Without the
            # compare, a timing side-channel on dict membership could leak
            # which team's key was guessed correctly.
            for stored_hash, team in self._by_hash.items():
                if hmac.compare_digest(candidate, stored_hash):
                    return team
            return None

    def maybe_reload(self) -> bool:
        """Re-read the file if mtime changed. Returns True iff reloaded."""
        with self._lock:
            try:
                mtime = self._path.stat().st_mtime
            except OSError as exc:
                logger.error(
                    "team_keys_stat_failed",
                    path=str(self._path),
                    error=str(exc),
                )
                self._reload_failures += 1
                return False

            if mtime == self._mtime:
                return False
            try:
                new_keys = load_team_keys_from_path(self._path)
            except (OSError, json.JSONDecodeError, TeamKeysError) as exc:
                logger.error(
                    "team_keys_reload_failed",
                    path=str(self._path),
                    error=str(exc),
                )
                self._reload_failures += 1
                return False
            self._keys = new_keys
            self._by_hash = {h: team for team, h in new_keys.items()}
            self._mtime = mtime
            logger.info("team_keys_reloaded", teams=len(new_keys))
            return True
