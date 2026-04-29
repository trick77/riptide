"""Team catalog loader, validator, hot-reloader.

The catalog file declares teams (name + contact) and org-wide automation
rules (bot detection). Cross-source aggregation is done at read time on
`commit_sha` plus per-source identifiers (`repo_full_name`, `pipeline_name`,
`app_name`, `repo`); the catalog does not curate service identity.

The file is loaded at startup and re-read on mtime change.
"""

from __future__ import annotations

import json
import threading
from dataclasses import dataclass
from email.utils import parseaddr
from pathlib import Path
from typing import Any

from riptide_collector.logging_config import get_logger
from riptide_collector.parsers import looks_bot_shaped

logger = get_logger(__name__)


class CatalogError(ValueError):
    pass


@dataclass(frozen=True, slots=True)
class Team:
    name: str
    group_email: str


@dataclass(frozen=True, slots=True)
class AutomationSource:
    name: str
    authors: tuple[str, ...]
    branch_prefixes: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class Catalog:
    teams_by_name: dict[str, Team]
    automation: tuple[AutomationSource, ...]


def _validate_email(addr: str) -> bool:
    name, parsed = parseaddr(addr)
    del name
    return "@" in parsed and "." in parsed.split("@", 1)[1]


def _build_catalog(data: dict[str, Any]) -> Catalog:
    raw_teams = data.get("teams") or []
    raw_automation = data.get("automation") or {}

    if not isinstance(raw_teams, list):
        raise CatalogError("`teams` must be a list")
    if not isinstance(raw_automation, dict):
        raise CatalogError("`automation` must be an object")

    teams: dict[str, Team] = {}
    for raw in raw_teams:
        name = raw.get("name")
        email = raw.get("group_email")
        if not name or not isinstance(name, str):
            raise CatalogError("team is missing `name`")
        if name in teams:
            raise CatalogError(f"duplicate team name: {name!r}")
        if not email or not isinstance(email, str) or not _validate_email(email):
            raise CatalogError(f"team {name!r} has invalid `group_email`: {email!r}")
        teams[name] = Team(name=name, group_email=email)

    automations: list[AutomationSource] = []
    for src_name, cfg in raw_automation.items():
        if not isinstance(cfg, dict):
            raise CatalogError(f"automation.{src_name} must be an object")
        authors = tuple(cfg.get("authors") or [])
        prefixes = tuple(cfg.get("branch_prefixes") or [])
        automations.append(
            AutomationSource(name=src_name, authors=authors, branch_prefixes=prefixes)
        )

    return Catalog(
        teams_by_name=teams,
        automation=tuple(automations),
    )


def load_catalog_from_path(path: Path) -> Catalog:
    with path.open("r", encoding="utf-8") as fh:
        data = json.load(fh)
    if not isinstance(data, dict):
        raise CatalogError(f"catalog at {path} must be a JSON object at the top level")
    return _build_catalog(data)


class CatalogStore:
    """Thread-safe catalog holder with mtime-based hot reload.

    Single instance per process, owned by the FastAPI app state.
    """

    def __init__(self, path: Path):
        self._path = path
        self._lock = threading.RLock()
        self._catalog = load_catalog_from_path(path)
        self._mtime = path.stat().st_mtime
        self._reload_failures = 0

    @property
    def reload_failures(self) -> int:
        return self._reload_failures

    @property
    def path(self) -> Path:
        return self._path

    def get(self) -> Catalog:
        with self._lock:
            return self._catalog

    def maybe_reload(self) -> bool:
        """Re-read the file if mtime changed. Returns True iff reloaded."""
        with self._lock:
            try:
                mtime = self._path.stat().st_mtime
            except OSError as exc:
                logger.error(
                    "catalog_stat_failed",
                    path=str(self._path),
                    error=str(exc),
                )
                self._reload_failures += 1
                return False

            if mtime == self._mtime:
                return False
            try:
                new_catalog = load_catalog_from_path(self._path)
            except (OSError, json.JSONDecodeError, CatalogError) as exc:
                logger.error(
                    "catalog_reload_failed",
                    path=str(self._path),
                    error=str(exc),
                )
                self._reload_failures += 1
                return False
            self._catalog = new_catalog
            self._mtime = mtime
            logger.info(
                "catalog_reloaded",
                teams=len(new_catalog.teams_by_name),
            )
            return True

    def team(self, name: str | None) -> Team | None:
        if not name:
            return None
        return self._catalog.teams_by_name.get(name)

    def detect_automation_source(self, author: str | None, branch_name: str | None) -> str | None:
        catalog = self._catalog
        if author:
            for source in catalog.automation:
                if author in source.authors:
                    return source.name
        if branch_name:
            for source in catalog.automation:
                for prefix in source.branch_prefixes:
                    if branch_name.startswith(prefix):
                        return source.name
        if looks_bot_shaped(author):
            return "other-bot"
        return None
