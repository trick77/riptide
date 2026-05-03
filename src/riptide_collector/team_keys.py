"""Team bearer-key store with mtime-based hot reload.

Maps team name → raw bearer token. Lookup walks every entry with
`hmac.compare_digest` so the wall time of an authentication doesn't leak
which team's token was the closest match.

File shape:
    {
      "<team-name>": "<raw-token>",
      ...
    }

The file lives inside a Kubernetes Secret in production. There is no
on-disk hashing layer: storing raw tokens here matches what already lives
in argocd-notifications-secret and means there's only one representation
of the credential to keep straight (raw on the wire, raw on disk, raw in
this file).
"""

from __future__ import annotations

import hmac
import json
import threading
from pathlib import Path
from typing import Any

from riptide_collector.logging_config import get_logger

logger = get_logger(__name__)


class TeamKeysError(ValueError):
    pass


def _validate(data: Any) -> dict[str, str]:
    if not isinstance(data, dict):
        raise TeamKeysError("team-keys file must be a JSON object at the top level")
    out: dict[str, str] = {}
    for team, value in data.items():
        if not isinstance(team, str) or not team:
            raise TeamKeysError("team key must be a non-empty string")
        if not isinstance(value, str) or not value:
            raise TeamKeysError(f"team {team!r} has invalid value: expected a non-empty string")
        out[team] = value
    return out


def load_team_keys_from_path(path: Path) -> dict[str, str]:
    with path.open("r", encoding="utf-8") as fh:
        data = json.load(fh)
    return _validate(data)


class TeamKeysStore:
    """Thread-safe team-keys holder with mtime-based hot reload.

    Mirrors `RiptideConfigStore`. Single instance per process.
    """

    def __init__(self, path: Path):
        self._path = path
        self._lock = threading.RLock()
        self._keys = load_team_keys_from_path(path)
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

        Always runs `hmac.compare_digest` against every stored value and
        defers the return until the loop completes — this prevents a timing
        side-channel from leaking *which* team matched (or how far down the
        dict iteration order it sits). Total work is constant in the team
        count (which is public anyway).
        """
        if not raw_token:
            return None
        match: str | None = None
        with self._lock:
            for team, stored in self._keys.items():
                # Bitwise OR via short-circuit-free assignment: every iteration
                # runs compare_digest regardless of prior matches.
                if hmac.compare_digest(raw_token, stored):
                    match = team
        return match

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
            self._mtime = mtime
            logger.info("team_keys_reloaded", teams=len(new_keys))
            return True
