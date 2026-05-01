"""Test bearer constants + their hashes.

Imported by `conftest.py` (config/team-keys fixtures) and by individual
test modules. Lives in its own module so test files don't have to rely on
`from conftest import …` (which only works because pytest puts the tests
directory on sys.path).
"""

from __future__ import annotations

import hashlib

CHECKOUT_TOKEN = "test-checkout-token-please-do-not-use-in-prod"
PLATFORM_TOKEN = "test-platform-token-please-do-not-use-in-prod"


def hash_token(raw: str) -> str:
    return hashlib.sha256(raw.encode("utf-8")).hexdigest()


TEAM_KEYS: dict[str, str] = {
    "checkout": hash_token(CHECKOUT_TOKEN),
    "platform": hash_token(PLATFORM_TOKEN),
}
