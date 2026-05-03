"""
Onboard Bitbucket Data Center repositories to riptide webhook delivery.

Usage:
    python scripts/bitbucket_onboarding.py config.json [--name riptide] [--dry-run] [--env-file PATH]
    python scripts/bitbucket_onboarding.py config.json --remove [--dry-run] [--env-file PATH]

`--remove` deletes the riptide webhook from every repo in the config instead
of creating/updating it. Same config file drives both directions.

The JSON config describes target repos, URLs, and the team the inbound
webhooks are recorded as. Secrets (BITBUCKET_TOKEN, RIPTIDE_TEAM_KEY) are
resolved from, in order (first match wins):
    1. --env-file <path>
    2. .env in CWD
    3. Process environment

Explicit `--env-file` always wins so a stale exported value in the shell
doesn't silently override what the operator put in the file.

See scripts/bitbucket-onboarding.env.example for a template.

Auth on the inbound side: BBS HMAC.

BBS DC's webhook can either Basic-auth via its top-level `credentials`
block or sign deliveries via `configuration.secret` (HMAC-SHA256, sent
as `X-Hub-Signature: sha256=<hex>`). Empirically the Basic path is broken
over REST: BBS drops `credentials.password` on POST/PUT (the UI form
submission works, REST doesn't — verified against BBS DC). HMAC via
`configuration.secret` round-trips fine.

So this script provisions HMAC: `configuration.secret = RIPTIDE_TEAM_KEY`,
where RIPTIDE_TEAM_KEY is the team's `bitbucket` entry in
`team-keys.json`. The webhook URL becomes `<webhook_url>/<team>` so
riptide's `POST /webhooks/bitbucket/{team}` can identify the caller from
the path before HMAC validation.

This script is intentionally stdlib-only so it can run on any host with Python
3.10+ without a venv or `pip install`.
"""

from __future__ import annotations

import argparse
import contextlib
import json
import logging
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

logger = logging.getLogger("bitbucket_onboarding")

DEFAULT_WEBHOOK_NAME = "riptide"

# Events riptide's bitbucket router (src/riptide_collector/routers/bitbucket.py)
# can extract data from: PR lifecycle + push (for revert detection in
# `push.changes[]`).
REQUIRED_WEBHOOK_EVENTS: tuple[str, ...] = (
    "pr:opened",
    "pr:from_ref_updated",
    "pr:comment:added",
    "pr:merged",
    "pr:deleted",
    "repo:refs_changed",
)


# --------------------------------------------------------------------------- #
# Config loading
# --------------------------------------------------------------------------- #


@dataclass(frozen=True)
class RepoSpec:
    project: str
    repo: str

    @property
    def key(self) -> str:
        return f"{self.project}/{self.repo}"


@dataclass(frozen=True)
class OnboardingInput:
    bitbucket_url: str
    webhook_url: str
    team: str
    repos: list[RepoSpec]


def _load_env_file(path: Path) -> dict[str, str]:
    """Minimal .env parser: KEY=VALUE lines, ignores comments and blanks."""
    if not path.exists():
        return {}
    env: dict[str, str] = {}
    for raw in path.read_text().splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if "=" not in line:
            continue
        key, _, value = line.partition("=")
        key = key.strip()
        value = value.strip().strip('"').strip("'")
        if key:
            env[key] = value
    return env


def _resolve_env(env_file: Path | None, required: tuple[str, ...]) -> dict[str, str]:
    """Load `required` env vars from --env-file > .env in CWD > process env.

    Explicit `--env-file` wins over process env so a stale exported value
    in the operator's shell doesn't silently override what they put in
    the file. (The previous order was the opposite and caused real
    confusion in practice.)
    """
    merged: dict[str, str] = {}
    for k in required:
        value = os.environ.get(k)
        if value:
            merged[k] = value
    merged.update(_load_env_file(Path.cwd() / ".env"))
    if env_file is not None:
        merged.update(_load_env_file(env_file))

    missing = [k for k in required if not merged.get(k)]
    if missing:
        sys.stderr.write(
            "ERROR: missing required environment variable(s): "
            + ", ".join(missing)
            + "\nSet them in the process env, a .env in CWD, or pass --env-file.\n"
        )
        raise SystemExit(2)
    return merged


def resolve_secrets(env_file: Path | None) -> tuple[str, str]:
    """Return (BITBUCKET_TOKEN, RIPTIDE_TEAM_KEY) for the onboard/remove paths."""
    merged = _resolve_env(env_file, ("BITBUCKET_TOKEN", "RIPTIDE_TEAM_KEY"))
    return merged["BITBUCKET_TOKEN"], merged["RIPTIDE_TEAM_KEY"]


def resolve_discover_secrets(env_file: Path | None) -> str:
    """Return BITBUCKET_TOKEN for the --discover path (no RIPTIDE_TEAM_KEY needed)."""
    return _resolve_env(env_file, ("BITBUCKET_TOKEN",))["BITBUCKET_TOKEN"]


def _mask(secret: str) -> str:
    if len(secret) <= 4:
        return "****"
    return f"{secret[:4]}-****"


def load_onboarding_input(path: Path) -> OnboardingInput:
    try:
        data = json.loads(path.read_text())
    except (OSError, json.JSONDecodeError) as exc:
        raise SystemExit(f"ERROR: cannot read {path}: {exc}") from exc

    if not isinstance(data, dict):
        raise SystemExit("ERROR: config root must be a JSON object")

    bitbucket_url = _require_https(data.get("bitbucket_url"), "bitbucket_url")
    webhook_url = _require_url(data.get("webhook_url"), "webhook_url")
    team = data.get("team")
    if not isinstance(team, str) or not team.strip():
        raise SystemExit("ERROR: 'team' must be a non-empty string")
    projects_raw = data.get("projects")

    if not isinstance(projects_raw, list) or not projects_raw:
        raise SystemExit("ERROR: 'projects' must be a non-empty list")

    seen: set[tuple[str, str]] = set()
    repos: list[RepoSpec] = []
    for p_idx, entry in enumerate(projects_raw):
        if not isinstance(entry, dict):
            raise SystemExit(f"ERROR: projects[{p_idx}] must be an object")
        project = entry.get("project")
        repos_list = entry.get("repos")
        if not isinstance(project, str) or not project.strip():
            raise SystemExit(f"ERROR: projects[{p_idx}].project must be a non-empty string")
        if not isinstance(repos_list, list) or not repos_list:
            raise SystemExit(
                f"ERROR: projects[{p_idx}].repos must be a non-empty list of repo slugs"
            )
        project_key = project.strip()
        for r_idx, repo in enumerate(repos_list):
            if not isinstance(repo, str) or not repo.strip():
                raise SystemExit(
                    f"ERROR: projects[{p_idx}].repos[{r_idx}] must be a non-empty string"
                )
            key = (project_key, repo.strip())
            if key in seen:
                raise SystemExit(f"ERROR: duplicate repo entry: {key[0]}/{key[1]}")
            seen.add(key)
            repos.append(RepoSpec(project=key[0], repo=key[1]))

    return OnboardingInput(
        bitbucket_url=bitbucket_url.rstrip("/"),
        webhook_url=webhook_url,
        team=team.strip(),
        repos=repos,
    )


def _require_https(value: Any, field_name: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise SystemExit(f"ERROR: '{field_name}' must be a non-empty string")
    parsed = urlparse(value)
    if parsed.scheme != "https" or not parsed.netloc:
        raise SystemExit(f"ERROR: '{field_name}' must be an https URL")
    return value


def _require_url(value: Any, field_name: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise SystemExit(f"ERROR: '{field_name}' must be a non-empty string")
    parsed = urlparse(value)
    if parsed.scheme not in ("http", "https") or not parsed.netloc:
        raise SystemExit(f"ERROR: '{field_name}' must be an http(s) URL")
    return value


# --------------------------------------------------------------------------- #
# Minimal stdlib HTTP client for Bitbucket Data Center
# --------------------------------------------------------------------------- #


class HTTPStatusError(Exception):
    def __init__(self, status_code: int, text: str, url: str):
        super().__init__(f"HTTP {status_code} for {url}: {text[:200]}")
        self.status_code = status_code
        self.text = text
        self.url = url


@dataclass
class _Response:
    status_code: int
    text: str

    def json(self) -> Any:
        return json.loads(self.text) if self.text else {}


class BitbucketHTTP:
    """Stdlib-only synchronous Bitbucket Data Center REST client."""

    def __init__(self, base_url: str, token: str, timeout: float = 30.0):
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.timeout = timeout

    def _request(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        body: Any | None = None,
    ) -> _Response:
        url = self.base_url + path
        if params:
            url = url + "?" + urllib.parse.urlencode(params)

        data: bytes | None = None
        headers = {
            "Authorization": f"Bearer {self.token}",
            "Accept": "application/json",
        }
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"

        req = urllib.request.Request(url, data=data, method=method, headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                text = resp.read().decode("utf-8", errors="replace")
                return _Response(status_code=resp.status, text=text)
        except urllib.error.HTTPError as exc:
            text = ""
            with contextlib.suppress(Exception):
                text = exc.read().decode("utf-8", errors="replace")
            raise HTTPStatusError(exc.code, text, url) from exc

    def get_repo(self, project: str, repo: str) -> dict[str, Any]:
        resp = self._request("GET", f"/rest/api/1.0/projects/{project}/repos/{repo}")
        return resp.json()

    def list_pull_requests(self, project: str, repo: str, limit: int = 1) -> dict[str, Any]:
        resp = self._request(
            "GET",
            f"/rest/api/1.0/projects/{project}/repos/{repo}/pull-requests",
            params={"limit": limit},
        )
        return resp.json()

    def list_webhooks(self, project: str, repo: str) -> list[dict[str, Any]]:
        """Return all webhooks (handles pagination)."""
        values: list[dict[str, Any]] = []
        start = 0
        while True:
            resp = self._request(
                "GET",
                f"/rest/api/1.0/projects/{project}/repos/{repo}/webhooks",
                params={"start": start, "limit": 100},
            )
            page = resp.json()
            values.extend(page.get("values") or [])
            if page.get("isLastPage", True):
                break
            next_start = page.get("nextPageStart")
            if not isinstance(next_start, int) or next_start <= start:
                break
            start = next_start
        return values

    def create_webhook(self, project: str, repo: str, body: dict[str, Any]) -> dict[str, Any]:
        resp = self._request(
            "POST",
            f"/rest/api/1.0/projects/{project}/repos/{repo}/webhooks",
            body=body,
        )
        return resp.json()

    def update_webhook(
        self, project: str, repo: str, webhook_id: int, body: dict[str, Any]
    ) -> dict[str, Any]:
        resp = self._request(
            "PUT",
            f"/rest/api/1.0/projects/{project}/repos/{repo}/webhooks/{webhook_id}",
            body=body,
        )
        return resp.json()

    def delete_webhook(self, project: str, repo: str, webhook_id: int) -> None:
        self._request(
            "DELETE",
            f"/rest/api/1.0/projects/{project}/repos/{repo}/webhooks/{webhook_id}",
        )

    def list_admin_repos(self) -> list[tuple[str, str]]:
        """Return [(project_key, repo_slug), ...] for every repo the
        authenticated user has REPO_ADMIN permission on. Paginated.
        """
        out: list[tuple[str, str]] = []
        start = 0
        while True:
            resp = self._request(
                "GET",
                "/rest/api/1.0/repos",
                params={"permission": "REPO_ADMIN", "start": start, "limit": 100},
            )
            page = resp.json()
            for entry in page.get("values") or []:
                project = (entry.get("project") or {}).get("key")
                slug = entry.get("slug")
                if isinstance(project, str) and isinstance(slug, str):
                    out.append((project, slug))
            if page.get("isLastPage", True):
                break
            next_start = page.get("nextPageStart")
            if not isinstance(next_start, int) or next_start <= start:
                break
            start = next_start
        return out


# --------------------------------------------------------------------------- #
# Onboarder
# --------------------------------------------------------------------------- #


@dataclass
class RepoResult:
    repo: RepoSpec
    status: str  # "ok", "failed", "skipped"
    detail: str = ""
    diff: list[str] = field(default_factory=list)


class RepoOnboarder:
    def __init__(
        self,
        client: BitbucketHTTP,
        webhook_url: str,
        team: str,
        team_key: str,
        webhook_name: str = DEFAULT_WEBHOOK_NAME,
        dry_run: bool = False,
    ):
        self.client = client
        self.webhook_url = webhook_url
        self.team = team
        self.team_key = team_key
        self.webhook_name = webhook_name
        self.dry_run = dry_run

    # -- Step 1 -- #
    def verify_permissions(self, spec: RepoSpec) -> None:
        """Confirm read access to repo and its PRs. Raises HTTPStatusError on failure.

        The PR-list call looks redundant next to get_repo, but it's the only
        way to prove the token has PR-read scope (some BBS roles can read
        repo metadata without seeing PRs). Webhook write scope is checked
        implicitly: a missing scope surfaces as 403 on create/update.
        """
        self.client.get_repo(spec.project, spec.repo)
        self.client.list_pull_requests(spec.project, spec.repo, limit=1)
        logger.info("[%s] read permissions OK", spec.key)

    # -- Step 2 -- #
    @property
    def _team_webhook_url(self) -> str:
        # Riptide's bitbucket endpoint is `POST /webhooks/bitbucket/{team}`;
        # team identity comes from the URL path (BBS HMAC carries no team
        # claim of its own). Strip any trailing slash on the configured
        # base before appending the team segment to avoid `//team`.
        return f"{self.webhook_url.rstrip('/')}/{self.team}"

    def _build_webhook_body(self) -> dict[str, Any]:
        # HMAC mode: `configuration.secret` is the HMAC key. BBS signs each
        # delivery and sends `X-Hub-Signature: sha256=<hex>` over the raw
        # request body. Riptide validates and 401s on mismatch.
        #
        # The Basic-auth alternative (`credentials.{username,password}`) is
        # broken over REST on BBS DC — POST/PUT silently drops the password.
        # See module docstring.
        return {
            "name": self.webhook_name,
            "url": self._team_webhook_url,
            "active": True,
            "events": list(REQUIRED_WEBHOOK_EVENTS),
            "configuration": {"secret": self.team_key},
            "sslVerificationRequired": True,
        }

    def _diff_webhook(self, existing: dict[str, Any]) -> list[str]:
        diffs: list[str] = []
        target_url = self._team_webhook_url
        if existing.get("url") != target_url:
            diffs.append(f"url: {existing.get('url')!r} -> {target_url!r}")
        existing_events = set(existing.get("events") or [])
        required = set(REQUIRED_WEBHOOK_EVENTS)
        if existing_events != required:
            missing = sorted(required - existing_events)
            extra = sorted(existing_events - required)
            diffs.append(f"events: missing={missing} extra={extra}")
        if not existing.get("active", True):
            diffs.append("active: False -> True")
        # BBS redacts `configuration.secret` on read-back (presence is
        # visible, value isn't), so we cannot tell whether the stored HMAC
        # secret matches what we want to send. Always rewrite — every
        # onboard run is the canonical source. Cheap PUT; avoids the
        # silent-failure mode where a stale secret sits in BBS forever.
        diffs.append("configuration.secret: (rewriting — BBS redacts on read-back)")
        return diffs

    def upsert_webhook(self, spec: RepoSpec) -> tuple[int, list[str]]:
        """Create or update the webhook. Returns (webhook_id, diff)."""
        hooks = self.client.list_webhooks(spec.project, spec.repo)
        existing = next((h for h in hooks if h.get("name") == self.webhook_name), None)
        body = self._build_webhook_body()

        if existing is None:
            logger.info("[%s] creating webhook %r", spec.key, self.webhook_name)
            if self.dry_run:
                logger.info("[%s] DRY-RUN body=%s", spec.key, _redact(body))
                return -1, ["create"]
            created = self.client.create_webhook(spec.project, spec.repo, body)
            return int(created["id"]), ["create"]

        diff = self._diff_webhook(existing)
        if not diff:
            logger.info("[%s] webhook already up to date", spec.key)
            return int(existing["id"]), []

        logger.info("[%s] updating webhook id=%s changes=%s", spec.key, existing.get("id"), diff)
        if self.dry_run:
            logger.info("[%s] DRY-RUN body=%s", spec.key, _redact(body))
            return int(existing["id"]), diff
        self.client.update_webhook(spec.project, spec.repo, int(existing["id"]), body)
        return int(existing["id"]), diff

    # -- Orchestrator -- #
    def onboard(self, spec: RepoSpec) -> RepoResult:
        try:
            self.verify_permissions(spec)
        except HTTPStatusError as exc:
            return RepoResult(
                spec,
                "failed",
                detail=f"permission check HTTP {exc.status_code}: {exc.text[:200]}",
            )
        except urllib.error.URLError as exc:
            return RepoResult(spec, "failed", detail=f"permission check: {exc}")

        try:
            _, diff = self.upsert_webhook(spec)
        except HTTPStatusError as exc:
            return RepoResult(
                spec,
                "failed",
                detail=f"upsert webhook HTTP {exc.status_code}: {exc.text[:200]}",
            )
        except urllib.error.URLError as exc:
            return RepoResult(spec, "failed", detail=f"upsert webhook: {exc}")

        if self.dry_run:
            return RepoResult(spec, "ok", detail="dry-run", diff=diff)

        return RepoResult(spec, "ok", detail="webhook configured", diff=diff)

    # -- Deboarding -- #
    def remove_webhook(self, spec: RepoSpec) -> RepoResult:
        """Delete the riptide webhook from the repo. No-op if absent."""
        try:
            hooks = self.client.list_webhooks(spec.project, spec.repo)
        except HTTPStatusError as exc:
            return RepoResult(
                spec,
                "failed",
                detail=f"list webhooks HTTP {exc.status_code}: {exc.text[:200]}",
            )
        except urllib.error.URLError as exc:
            return RepoResult(spec, "failed", detail=f"list webhooks: {exc}")

        existing = next((h for h in hooks if h.get("name") == self.webhook_name), None)
        if existing is None:
            logger.info("[%s] no %r webhook found, nothing to remove", spec.key, self.webhook_name)
            return RepoResult(spec, "skipped", detail=f"no {self.webhook_name!r} webhook found")

        webhook_id = int(existing["id"])
        if self.dry_run:
            logger.info("[%s] DRY-RUN would delete webhook id=%d", spec.key, webhook_id)
            return RepoResult(spec, "ok", detail=f"dry-run: would remove webhook id={webhook_id}")

        try:
            self.client.delete_webhook(spec.project, spec.repo, webhook_id)
        except HTTPStatusError as exc:
            return RepoResult(
                spec,
                "failed",
                detail=f"delete webhook HTTP {exc.status_code}: {exc.text[:200]}",
            )
        except urllib.error.URLError as exc:
            return RepoResult(spec, "failed", detail=f"delete webhook: {exc}")

        logger.info("[%s] removed webhook id=%d", spec.key, webhook_id)
        return RepoResult(spec, "ok", detail=f"webhook removed: id={webhook_id}")


def _redact(body: dict[str, Any]) -> dict[str, Any]:
    copy = dict(body)
    cfg = copy.get("configuration")
    if isinstance(cfg, dict) and "secret" in cfg:
        redacted = dict(cfg)
        redacted["secret"] = "***"
        copy["configuration"] = redacted
    creds = copy.get("credentials")
    if isinstance(creds, dict) and "password" in creds:
        redacted_creds = dict(creds)
        redacted_creds["password"] = "***"
        copy["credentials"] = redacted_creds
    return copy


# --------------------------------------------------------------------------- #
# CLI
# --------------------------------------------------------------------------- #


def _parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="python scripts/bitbucket_onboarding.py",
        description="Onboard Bitbucket Data Center repos to riptide by creating/updating their webhooks (idempotent).",
        epilog=(
            "Write permission check is implicit: if the token lacks repo-write, the webhook "
            "create/update will surface a 403. RIPTIDE_TEAM_KEY must be a valid team API key "
            "registered with the running riptide service. To verify end-to-end delivery, open "
            "a real PR after onboarding and run scripts/check_onboarding.py."
        ),
    )
    parser.add_argument(
        "config",
        type=Path,
        nargs="?",
        help="Path to onboarding JSON config (omit when --discover is set)",
    )
    parser.add_argument(
        "--name",
        default=DEFAULT_WEBHOOK_NAME,
        help=f"Webhook name (default: {DEFAULT_WEBHOOK_NAME})",
    )
    parser.add_argument(
        "--dry-run", action="store_true", help="Print planned changes without mutating Bitbucket"
    )
    parser.add_argument(
        "--remove",
        action="store_true",
        help="Deboard: delete the webhook from every repo in the config instead of creating/updating it",
    )
    parser.add_argument(
        "--env-file", type=Path, default=None, help="Additional .env file to read secrets from"
    )
    parser.add_argument(
        "--discover",
        action="store_true",
        help="List every repo the BITBUCKET_TOKEN user can REPO_ADMIN on, "
        "grouped by project, as JSON on stdout. Doesn't onboard anything. "
        "Logs go to stderr so the output is pipe-clean. Requires "
        "--bitbucket-url and BITBUCKET_TOKEN; ignores config / RIPTIDE_TEAM_KEY.",
    )
    parser.add_argument(
        "--bitbucket-url",
        default=None,
        help="Base URL for --discover (e.g. https://bitbucket.example.com). Ignored otherwise.",
    )
    parser.add_argument("--verbose", "-v", action="store_true", help="Enable DEBUG logging")
    return parser.parse_args(argv)


def _print_summary(results: list[RepoResult]) -> None:
    width_repo = max((len(r.repo.key) for r in results), default=10)
    header = f"{'repo'.ljust(width_repo)}  status   detail"
    print()
    print(header)
    print("-" * len(header))
    for r in results:
        print(f"{r.repo.key.ljust(width_repo)}  {r.status.ljust(7)}  {r.detail}")


def _run(args: argparse.Namespace) -> int:
    token, team_key = resolve_secrets(args.env_file)
    inp = load_onboarding_input(args.config)

    logger.info("Bitbucket URL: %s", inp.bitbucket_url)
    logger.info("Webhook URL:   %s", inp.webhook_url)
    logger.info("Team:          %s", inp.team)
    if urlparse(inp.webhook_url).scheme == "http":
        logger.warning("webhook_url is http:// — riptide will receive the team key in cleartext")
    logger.info("Bitbucket token loaded: %s", _mask(token))
    logger.info("Riptide team key loaded: %s", _mask(team_key))
    logger.info("Target repos (%d): %s", len(inp.repos), ", ".join(r.key for r in inp.repos))
    if args.remove:
        logger.info("REMOVE mode: will delete the %r webhook from each repo", args.name)
    if args.dry_run:
        logger.info("DRY-RUN: no writes will be issued")

    client = BitbucketHTTP(base_url=inp.bitbucket_url, token=token)
    onboarder = RepoOnboarder(
        client,
        webhook_url=inp.webhook_url,
        team=inp.team,
        team_key=team_key,
        webhook_name=args.name,
        dry_run=args.dry_run,
    )

    action = onboarder.remove_webhook if args.remove else onboarder.onboard
    results: list[RepoResult] = []
    for spec in inp.repos:
        logger.info("--- %s ---", spec.key)
        try:
            result = action(spec)
        except Exception as exc:
            logger.exception("[%s] unexpected error", spec.key)
            result = RepoResult(spec, "failed", detail=f"unexpected: {exc}")
        results.append(result)
        if result.status == "failed":
            logger.error("[%s] aborting: %s", spec.key, result.detail)
            remaining = [s.key for s in inp.repos[len(results) :]]
            if remaining:
                logger.error("not processed: %s", ", ".join(remaining))
            break

    _print_summary(results)
    failed = [r for r in results if r.status == "failed"]
    return 1 if failed else 0


def _run_discover(args: argparse.Namespace) -> int:
    """List every repo the BITBUCKET_TOKEN user can REPO_ADMIN on, grouped by
    project, as JSON on stdout. Logs to stderr so the output is pipe-clean.
    """
    if not args.bitbucket_url:
        sys.stderr.write("ERROR: --discover requires --bitbucket-url\n")
        return 2
    parsed = urlparse(args.bitbucket_url)
    if parsed.scheme != "https" or not parsed.netloc:
        sys.stderr.write("ERROR: --bitbucket-url must be an https URL\n")
        return 2

    token = resolve_discover_secrets(args.env_file)
    logger.info("Bitbucket URL: %s", args.bitbucket_url)
    logger.info("Bitbucket token loaded: %s", _mask(token))

    client = BitbucketHTTP(base_url=args.bitbucket_url.rstrip("/"), token=token)
    try:
        admin_repos = client.list_admin_repos()
    except HTTPStatusError as exc:
        logger.error("discover: HTTP %d: %s", exc.status_code, exc.text[:200])
        return 1
    except urllib.error.URLError as exc:
        logger.error("discover: %s", exc)
        return 1

    by_project: dict[str, list[str]] = {}
    for project, slug in admin_repos:
        by_project.setdefault(project, []).append(slug)

    projects = [
        {"project": project, "repos": sorted(repos)}
        for project, repos in sorted(by_project.items())
    ]
    logger.info(
        "discover: %d repos across %d projects",
        sum(len(p["repos"]) for p in projects),
        len(projects),
    )
    json.dump(projects, sys.stdout, indent=2)
    sys.stdout.write("\n")
    return 0


def main(argv: list[str] | None = None) -> int:
    args = _parse_args(argv)
    # Discover dumps JSON to stdout; route everything else (logs) to stderr so
    # the output is pipe-clean.
    log_stream = sys.stderr if args.discover else None
    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)s %(name)s: %(message)s",
        stream=log_stream,
    )
    if args.discover:
        return _run_discover(args)
    if args.config is None:
        sys.stderr.write("ERROR: config path is required (or pass --discover)\n")
        return 2
    return _run(args)


if __name__ == "__main__":
    sys.exit(main())
