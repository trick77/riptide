"""Pure functions for extracting metadata from PR/commit fields.

These are intentionally separate from any I/O or DB layer so they're easy to
unit-test and reuse from migrations or scripts.
"""

import re

CHANGE_TYPE_MAP: dict[str, str] = {
    "feature": "feature",
    "feat": "feature",
    "bugfix": "bugfix",
    "fix": "bugfix",
    "hotfix": "hotfix",
    "chore": "chore",
    "refactor": "refactor",
    "docs": "docs",
}

JIRA_KEY_RE = re.compile(r"\b[A-Z][A-Z0-9]+-\d+\b")
REVERT_RE = re.compile(r'^Revert "', re.IGNORECASE)
BOT_SHAPED_RE = re.compile(r"(.*-bot$|.*\[bot\]$|^bot-.*)", re.IGNORECASE)


def parse_change_type(branch_name: str | None) -> str | None:
    """Map a branch name to its change_type.

    None if branch_name is missing; "other" if the prefix is unknown.
    """
    if not branch_name:
        return None
    prefix = branch_name.split("/", 1)[0].lower() if "/" in branch_name else branch_name.lower()
    return CHANGE_TYPE_MAP.get(prefix, "other")


def extract_jira_keys(*sources: str | None) -> list[str]:
    """Extract distinct Jira keys (in first-seen order) from any text sources."""
    seen: dict[str, None] = {}
    for src in sources:
        if not src:
            continue
        for match in JIRA_KEY_RE.findall(src):
            seen.setdefault(match, None)
    return list(seen.keys())


def is_revert_commit(commit_message: str | None) -> bool:
    if not commit_message:
        return False
    return bool(REVERT_RE.match(commit_message))


def looks_bot_shaped(author: str | None) -> bool:
    if not author:
        return False
    return bool(BOT_SHAPED_RE.match(author))


def lower(value: str | None) -> str | None:
    """Lowercase a string or pass through None / empty.

    Used at ingest time to normalise identifiers stored in join columns
    (service, commit_sha, revision, repo_full_name, branch_name).
    """
    return value.lower() if isinstance(value, str) and value else value


def parse_environment(namespace: str | None) -> str | None:
    """Extract the lowercased stage suffix from an Argo CD destination namespace.

    Convention: `<thing>-<stage>` (e.g. `payments-prod`, `checkout-intg`).
    Returns the suffix after the last `-`, or None if the namespace is
    empty / has no `-` / has an empty suffix. Unknown suffixes are
    preserved verbatim — the config's `production_stage` decides which
    one means production at query time.
    """
    if not isinstance(namespace, str):
        return None
    namespace = namespace.strip()
    if "-" not in namespace:
        return None
    prefix, suffix = namespace.rsplit("-", 1)
    if not prefix or not suffix:
        return None
    return suffix.lower()
