# Onboarding a team to riptide

End-to-end checklist for adding a team so their delivery events show up in
the metrics. The config declares teams; per-source aggregations key on the
upstream identifiers (`repo_full_name`, `pipeline_name`, `app_name`, `repo`)
already present on each event, and cross-source joins use `commit_sha`.

## 1. Add the team to the config

Open a PR editing [`config/riptide.json`](../config/riptide.json):

```json
{
  "name": "checkout",
  "group_email": "team-checkout@example.com"
}
```

CI validates the file (uniqueness, email shape) — fix any errors before
merging. After merge the running collector re-reads the file within
~30 seconds (or restart it for instant pickup).

## 2. Generate the team's per-source secrets

Each source the team uses gets its **own raw secret** (Bitbucket = HMAC
key, ArgoCD / Jenkins / Tekton = Bearer token, Noergler = Bearer token
if used). A leaked secret is therefore scoped to one source.

```bash
BB=$(openssl rand -base64 32)   # Bitbucket HMAC
AC=$(openssl rand -base64 32)   # ArgoCD bearer
JK=$(openssl rand -base64 32)   # Jenkins/Tekton bearer
echo "Hand off (one-way) to team checkout:"
echo "  bitbucket=$BB"
echo "  argocd=$AC"
echo "  jenkins=$JK"
```

`team-keys.json` is an object keyed by team then by source:

```json
{
  "checkout": {
    "bitbucket": "<BB>",
    "argocd":    "<AC>",
    "jenkins":   "<JK>"
  }
}
```

Add the entry to the production `team-keys.json` (the file at
`RIPTIDE_TEAM_KEYS_PATH`, never committed).

Every team in the config must have an entry in `team-keys.json` (with at
least one source) or the collector fails to start. Source names outside the
allowed set (`bitbucket`, `argocd`, `jenkins`, `noergler`) are rejected
at load time. The hot-reloader picks up edits automatically; a rejected
reload is logged and the previous keys stay in force.

## 3. Wire the team's webhooks

Each source uses the team's source-specific secret:

- **Bitbucket** → `POST /webhooks/bitbucket/{team}`, HMAC via
  `X-Hub-Signature` (BBS handles signing, secret is the team's
  `bitbucket` key). The canonical path is `riptide onboard-bitbucket`:
  [setup-bitbucket-webhook.md](setup-bitbucket-webhook.md).
- **ArgoCD** → `POST /webhooks/argocd`, `Authorization: Bearer <argocd>`.
  See [setup-argocd-notification.md](setup-argocd-notification.md).
- **Tekton** → `POST /webhooks/pipeline`, `Authorization: Bearer <jenkins>`
  (the `jenkins` key covers both Jenkins and Tekton).
  See [setup-tekton-pipeline.md](setup-tekton-pipeline.md).
- **Jenkins** → `POST /webhooks/pipeline`, `Authorization: Bearer <jenkins>`.
  See [setup-jenkins-notification.md](setup-jenkins-notification.md).

## 4. Smoke test

Open a throwaway PR, merge it, let CI run, deploy to prod, then run:

```bash
RIPTIDE_DB_URL=... riptide check-onboarding --team <team> [--since 1h]
```

It lists every repo, pipeline and app of the team that reported in the window,
with event counts, and exits non-zero when Bitbucket, the pipelines or Argo CD
sent nothing at all. If any is missing, jump to the
troubleshooting sections of the relevant setup doc.
