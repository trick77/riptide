<p align="left">
  <img src="logo-text.png" alt="riptide">
</p>

[![CI](https://github.com/trick77/riptide/actions/workflows/ci.yaml/badge.svg)](https://github.com/trick77/riptide/actions/workflows/ci.yaml) ![Go 1.27](https://img.shields.io/badge/go-1.27-blue)

Engineering metrics for teams in typical private cloud corporate environments.
riptide collects delivery events from Bitbucket Data Center, Jenkins or Tekton,
Argo CD and the [noergler](https://github.com/trick77/noergler) code reviewer,
and shows each team where its changes wait, how evenly it ships, what its CI
costs and what its reviews cost.

Built for self-hosted toolchains behind a corporate firewall: one Go binary
with the UI embedded, Postgres, no SaaS, no data leaving the network. Config
is a JSON file changed by pull request, not an admin UI.

![riptide team page](docs/riptide-ui.png)

## Features

**The team page**

- One page per team, a mirror for the team, not a ranking of teams or people
- Leads with "What now?": the findings riptide's rules raise, each naming its
  rule and number and linking to the facts behind it, plus a conversation
  starter
- Sections by question: Flow (where a change waits, from first commit to
  prod), Deploys (how evenly it ships), Work mix, Build (CI success and duration
  per kind of run, repos that got slower or flakier than before), Costs (noergler review spend), Data
  (the PRs, deploys and runs behind every number, searchable) and Frameworks
  (which number belongs to DORA, DX Core 4, SPACE, DevEx or Flow)
- Whole calendar weeks (4, 12 or 26), each number against the same span right
  before it, never against other teams or survey benchmarks
- Every number says which way is better and has a "Was heisst das?" with its
  definition, its basis and the query it follows
- Filter by product (Bitbucket project), bots in or out; the view lives in the
  query string, so a link or a printout reproduces it
- German or English, Swiss number and date formats in both; print / PDF

**The data**

- Raw webhook payloads stored append-only in Postgres, the extracted columns
  next to them
- Metrics computed on read in Go, against real Postgres in the tests; the UI
  renders and computes nothing
- Deploys tied to commits through the full image reference, lead time per
  commit as DORA defines it, GitOps stage promotions kept out of PR metrics
- Bots and service accounts recognised at ingest and declared by the senders,
  shown apart from human work
- Jira keys extracted from PR titles, branches and commit messages

**Operations**

- One instance, many teams: each team has its own key per source, a leaked
  key is scoped to one team and one source
- `riptide.json` (teams, stages, automation, excluded projects) is
  hot-reloaded
- Logs as one JSON object per line, ready for Splunk
- Distroless image, exec probes, external Postgres

How each metric is defined, with the SQL it follows and the traps in the
event tables, is in [docs/metrics.md](docs/metrics.md).

## How it works

1. **Webhooks**: Bitbucket, the CI, Argo CD and noergler post to riptide's
   collector listener. The sender's key decides the team.
2. **Store**: the payload is parsed, the raw body kept, and one row written per
   event: `bitbucket_events`, `pipeline_events`, `argocd_events`,
   `noergler_events`.
3. **Compute**: on every page view the backend reads the events of the
   window and computes the numbers in `backend/internal/metrics`. No rollup
   tables, no jobs.
4. **Show**: the UI listener serves the page and the read API behind your
   auth proxy.

| Source | Endpoint | Auth | `team-keys.json` key |
| --- | --- | --- | --- |
| Bitbucket DC | `POST /webhooks/bitbucket/{team}` | `X-Hub-Signature: sha256=<hex>`, HMAC over the raw body | `bitbucket` |
| Jenkins, Tekton, any CI | `POST /webhooks/pipeline` | `Authorization: Bearer <key>` | `jenkins` |
| Argo CD | `POST /webhooks/argocd` | `Authorization: Bearer <key>` | `argocd` |
| noergler *(optional)* | `POST /webhooks/noergler` | `Authorization: Bearer <key>` | `noergler` |

Every CI posts to the one pipeline endpoint, told apart by its `source` field.
Senders check their key at startup with `GET /auth/ping`, which answers the
team.

## Quick start

```bash
docker compose up   # Postgres, `riptide migrate`, then `riptide serve`
```

- UI: http://127.0.0.1:8080
- Webhooks: http://localhost:8000, Swagger UI on `/docs`, the contract on
  `/openapi.yaml` ([`backend/api/openapi.yaml`](backend/api/openapi.yaml))
- Dev keys for the sample teams `checkout` and `platform`:
  [`config/team-keys.json`](config/team-keys.json)

## Configuration

Environment (a `.env` in the working directory fills in what is unset):

| Variable | Default | |
| --- | --- | --- |
| `BACKEND_DB_URL` | `postgres://riptide:riptide@localhost:5432/riptide` | Postgres URL with credentials |
| `BACKEND_CONFIG_PATH` | `config/riptide.json` | teams, stages, automation, excluded projects |
| `BACKEND_TEAM_KEYS_PATH` | `config/team-keys.json` | one key per team and source, mount it from a Secret |
| `BACKEND_CONFIG_RELOAD_SECONDS` | `30` | how often both files are re-read |
| `BACKEND_LISTEN_ADDR` | `:8000` | collector: webhooks, `/auth/ping`, probes |
| `BACKEND_UI_ADDR` | `127.0.0.1:8080` | UI and `/api` |
| `BACKEND_UI_ALLOW_REMOTE` | `false` | allow a non-loopback `BACKEND_UI_ADDR` |
| `BACKEND_LOG_LEVEL` | `INFO` | |
| `BACKEND_ENV` | `dev` | stamped on every log line as `env` |
| `BACKEND_ANALYSIS_MODEL` | empty (off) | llmwire profile id that words the overview's assessment; locally `mimo-v2.6-pro`, deployed `gpt-6.1-sol` through LiteLLM (infra repo) |
| `BACKEND_ANALYSIS_REASONING` | empty (balanced) | the model's effort level |
| `BACKEND_ANALYSIS_TZ` | `Europe/Zurich` | calendar of the assessed closed weeks and the Monday schedule |

The model's key is llmwire's own variable, `LLMWIRE_<PROVIDER>_API_KEY`
(locally `LLMWIRE_MIMO_API_KEY`), or `LLMWIRE_LITELLM_BASE_URL` /
`_API_KEY` / `_MODELS` behind a gateway. A named model without a key, or
without strict JSON schema support, refuses the start.
`riptide analyse -team <team> [-weeks 4] [-dry-run]` words one view from
the shell: `-dry-run` prints the model's input (repos aliased) and calls
nothing.

[`config/riptide.json`](config/riptide.json) holds the teams,
`environments.production_stage` (the namespace suffix that counts as
production, default `prod`), `excluded_projects` (Bitbucket projects the
metrics leave out), `jira.base_url` (Jira keys in the PR table link to
`<base_url>/browse/<KEY>`; unset, they are shown unlinked) and `automation`
(bot accounts nobody declares). Adding a
team: [docs/onboarding-a-team.md](docs/onboarding-a-team.md).

`team-keys.json`:

```json
{
  "<team>": {
    "bitbucket": "<hmac-secret>",
    "argocd":    "<bearer-token>",
    "jenkins":   "<bearer-token>",
    "noergler":  "<bearer-token>"
  }
}
```

A key works only on its own source's endpoint: an `argocd` key sent to
`/webhooks/pipeline` gets a 401, even from the same team. Bitbucket is
HMAC-only: Bitbucket DC's REST API silently drops `credentials.password`, so
`riptide onboard-bitbucket` provisions the HMAC secret and the team comes from
the URL path.

A key read from a Kubernetes Secret with `oc get secret … -o jsonpath` is
still base64: pipe it through `base64 -d`. Pasted wrapped, it answers
`401 {"detail":"Invalid credentials."}`.

## Running it

| Command | Does |
| --- | --- |
| `riptide serve` (default) | collector on `:8000` and UI listener on `127.0.0.1:8080` |
| `riptide migrate` | applies pending migrations and exits; run it before `serve`, which refuses a schema that is behind |
| `riptide onboard-bitbucket` | creates, updates or removes the riptide webhook on Bitbucket repos |
| `riptide check-onboarding` | lists which repos, pipelines and apps reported recently |
| `riptide version` | prints the version |
| `riptide -healthcheck` | liveness probe: exit 0 when live |
| `riptide -readycheck` | readiness probe: exit 0 when the database answers |

The read API has no authentication of its own. In the pod the UI listener
stays on loopback, where only the auth proxy sidecar reaches it; the route
that carries webhooks cannot reach `/api`. Every authenticated user can read
every team, deliberately. riptide refuses a non-loopback `BACKEND_UI_ADDR`
unless `BACKEND_UI_ALLOW_REMOTE=true` (compose sets it and publishes the port
on the host's loopback).

`/healthz` and `/readyz` answer loopback callers only, so wire them as exec
probes (`/riptide -healthcheck`, `/riptide -readycheck`). The image is
distroless: no shell, no wget.

Postgres is provisioned externally; riptide only migrates its schema. The
local `compose.yaml` database is for development.

## Read API

On the UI listener, shapes in [`backend/api/openapi.yaml`](backend/api/openapi.yaml):

- `GET /api/teams`: the configured teams
- `GET /api/metrics?team=&weeks=4|12|26&tz=`: the page's numbers, the
  previous period and the findings; `product=` and `bots=1` narrow or widen it
- `GET /api/facts`: the same parameters plus `q`, the PRs, deploys and runs
  behind the numbers

## Development

```bash
make check                                  # the gate (ship): lint, vuln scan, tests, coverage
docker compose up -d db
SHIP_TEST_DSN='postgres://riptide:riptide@localhost:5432/riptide?sslmode=disable' go -C backend test ./...
make fe-build                               # build the UI into the binary
npm --prefix ui run dev                     # UI with hot reload against a local riptide serve
```

The gate is [ship](https://github.com/trick77/ship). The schema is in
[docs/schema.md](docs/schema.md), generated from the migrations. Releases are
cut by hand with the Release workflow.

## Documentation

- [Metrics and the event tables](docs/metrics.md)
- [Setup: Bitbucket webhook](docs/setup-bitbucket-webhook.md)
- [Setup: Jenkins notification](docs/setup-jenkins-notification.md)
- [Setup: Tekton / OpenShift Pipelines](docs/setup-tekton-pipeline.md)
- [Setup: Argo CD notification](docs/setup-argocd-notification.md)
- [Setup: noergler notification](docs/setup-noergler-notification.md)
- [Onboarding a team](docs/onboarding-a-team.md)
- [Change notes](docs/change-notes.md): what to do on the operator side after a deploy
- [Change lead time (DORA)](docs/dora-lead-time.md)
- [Correlating deploys back to commits](docs/correlating-deploys-to-commits.md)
- [Database schema](docs/schema.md)

## Licence

[MIT](LICENSE)
