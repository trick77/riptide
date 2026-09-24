"""Test bearer / HMAC secrets used across the test suite.

team-keys.json stores per-source raw tokens (no on-disk hashing layer).
The fixture maps team name → {source: raw secret}. Source names are
constrained to `riptide_collector.team_keys.KNOWN_SOURCES`.

Bearer endpoints (`/webhooks/argocd`, `/webhooks/pipeline`,
`/webhooks/noergler`) require the matching source token; cross-source
tokens are rejected by design (strict source binding). Bitbucket uses
HMAC, so its secret is the team's `bitbucket` key here.
"""

from __future__ import annotations

CHECKOUT_BITBUCKET = "test-checkout-bitbucket-hmac-secret"
PLATFORM_BITBUCKET = "test-platform-bitbucket-hmac-secret"

CHECKOUT_ARGOCD = "test-checkout-argocd-bearer"
PLATFORM_ARGOCD = "test-platform-argocd-bearer"

CHECKOUT_JENKINS = "test-checkout-jenkins-bearer"
PLATFORM_JENKINS = "test-platform-jenkins-bearer"

CHECKOUT_NOERGLER = "test-checkout-noergler-bearer"
PLATFORM_NOERGLER = "test-platform-noergler-bearer"


TEAM_KEYS: dict[str, dict[str, str]] = {
    "checkout": {
        "bitbucket": CHECKOUT_BITBUCKET,
        "argocd": CHECKOUT_ARGOCD,
        "jenkins": CHECKOUT_JENKINS,
        "noergler": CHECKOUT_NOERGLER,
    },
    "platform": {
        "bitbucket": PLATFORM_BITBUCKET,
        "argocd": PLATFORM_ARGOCD,
        "jenkins": PLATFORM_JENKINS,
        "noergler": PLATFORM_NOERGLER,
    },
}
