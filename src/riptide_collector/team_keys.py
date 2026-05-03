"""Team bearer-key store with mtime-based hot reload.

Maps team name → per-source raw secret. Each team can have one secret
per source (`bitbucket`, `argocd`, `jenkins`, `noergler`). A leaked
secret is therefore scoped to a single source.

File shape:
    {
      "<team-name>": {
        "bitbucket": "<raw-token>",
        "argocd":    "<raw-token>",
        "jenkins":   "<raw-token>",
        "noergler":  "<raw-token>"
      },
      ...
    }

Bearer endpoints look up by `(token, source)` so an `argocd` key cannot
authenticate `/webhooks/pipeline` or `/webhooks/bitbucket`. The Bitbucket
endpoint uses HMAC instead of Bearer; its `bitbucket` secret is the HMAC
key BBS programs into `configuration.secret`.

Lookups walk every entry with `hmac.compare_digest` so the wall time of
an authentication doesn't leak which team's token was the closest match.
"""

from __future__ import annotations

import hmac
import json
import threading
from pathlib import Path
from typing import Any

from riptide_collector.logging_config import get_logger

logger = get_logger(__name__)

KNOWN_SOURCES: frozenset[str] = frozenset({"bitbucket", "argocd", "jenkins", "noergler"})


class TeamKeysError(ValueError):
    pass


def _validate(data: Any) -> dict[str, dict[str, str]]:
    if not isinstance(data, dict):
        raise TeamKeysError("team-keys file must be a JSON object at the top level")
    out: dict[str, dict[str, str]] = {}
    for team, value in data.items():
        if not isinstance(team, str) or not team:
            raise TeamKeysError("team key must be a non-empty string")
        if not isinstance(value, dict) or not value:
            raise TeamKeysError(
                f"team {team!r} value must be a non-empty object of source → raw token"
            )
        sources: dict[str, str] = {}
        for source, token in value.items():
            if not isinstance(source, str) or source not in KNOWN_SOURCES:
                raise TeamKeysError(
                    f"team {team!r} has unknown source {source!r}; allowed: {sorted(KNOWN_SOURCES)}"
                )
            if not isinstance(token, str) or not token:
                raise TeamKeysError(f"team {team!r} source {source!r} must be a non-empty string")
            sources[source] = token
        out[team] = sources
    return out


def load_team_keys_from_path(path: Path) -> dict[str, dict[str, str]]:
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

    def lookup(self, raw_token: str | None, source: str) -> str | None:
        """Return the team name whose `source` secret matches `raw_token`.

        Strict source binding: a token registered under `argocd` cannot
        match a `bitbucket` lookup, even if the same byte value happened
        to be used across sources. Always runs `hmac.compare_digest`
        against every team's secret for the given source — defers the
        return until the loop completes — so wall-time doesn't leak which
        team matched.
        """
        if not raw_token or source not in KNOWN_SOURCES:
            return None
        match: str | None = None
        with self._lock:
            for team, sources in self._keys.items():
                stored = sources.get(source)
                if stored is None:
                    continue
                if hmac.compare_digest(raw_token, stored):
                    match = team
        return match

    def lookup_any_source(self, raw_token: str | None) -> str | None:
        """Return the team for whom `raw_token` matches *any* of their
        per-source secrets. Used by the source-agnostic ping path
        (`/auth/ping`) where the caller is just proving they hold one of
        their team's secrets — which one doesn't matter."""
        if not raw_token:
            return None
        match: str | None = None
        with self._lock:
            for team, sources in self._keys.items():
                for stored in sources.values():
                    if hmac.compare_digest(raw_token, stored):
                        match = team
        return match

    def has_source(self, team: str, source: str) -> bool:
        with self._lock:
            return source in self._keys.get(team, {})

    def get_secret(self, team: str, source: str) -> str | None:
        with self._lock:
            return self._keys.get(team, {}).get(source)

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
