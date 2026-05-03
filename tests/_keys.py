"""Test bearer constants used across the test suite.

Imported by `conftest.py` (config/team-keys fixtures) and by individual
test modules. Lives in its own module so test files don't have to rely on
`from conftest import …` (which only works because pytest puts the tests
directory on sys.path).

team-keys.json stores raw tokens (no on-disk hashing layer), so the
fixture maps team name → raw token directly.
"""

from __future__ import annotations

CHECKOUT_TOKEN = "test-checkout-token-please-do-not-use-in-prod"
PLATFORM_TOKEN = "test-platform-token-please-do-not-use-in-prod"


TEAM_KEYS: dict[str, str] = {
    "checkout": CHECKOUT_TOKEN,
    "platform": PLATFORM_TOKEN,
}
