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
