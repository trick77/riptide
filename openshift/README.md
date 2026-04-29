# OpenShift manifests

Suite-level deployment for the **riptide** observability platform.
Per-component folders sit alongside one another; v1 only ships `collector/`.

## Layout

```
openshift/
├── kustomization.yaml          # suite-level entry; composes components
├── secret.env.example          # template for riptide-collector-secrets
└── collector/
    ├── kustomization.yaml
    ├── deployment.yaml
    ├── service.yaml
    ├── route.yaml
    ├── configmap-app.yaml      # non-secret env (log level, catalog path, team-keys path)
    ├── service-catalog.json    # in-repo sample catalog (teams + automation)
    ├── team-keys.json          # in-repo dev sample (sha256 hashes); prod replaces via Secret
    └── job-migrate.yaml        # alembic upgrade head, runs PreSync
```

## Database

Postgres is **not** managed by riptide. Provision it separately, then put the
connection URL into `RIPTIDE_DB_URL` in the Secret.

## One-time setup (per environment)

Two Secrets are required: one for the DB connection, one for the per-team
webhook bearer keys.

### 1. `riptide-collector-secrets` (DB URL)

```bash
cp openshift/secret.env.example /tmp/secret.env
# edit /tmp/secret.env — set the real RIPTIDE_DB_URL
oc create secret generic riptide-collector-secrets \
    --namespace=$NS \
    --from-env-file=/tmp/secret.env \
    --dry-run=client -o yaml | oc apply -f -
shred -u /tmp/secret.env
```

### 2. `riptide-collector-team-keys` (per-team bearer hashes)

Each team has its own bearer key. Riptide stores **sha256 hashes** only; the
raw keys go to the teams' webhook configurations (Bitbucket, ArgoCD, Tekton,
Jenkins).

Generate one entry per team:

```bash
RAW=$(openssl rand -base64 32)
echo "Give to team checkout: $RAW"
printf %s "$RAW" | sha256sum | awk '{print $1}'   # → put under "checkout"
```

Build a real team-keys file (start from the in-repo sample if helpful, but
**replace every value** with a fresh hash — the committed sample contains
deterministic dev tokens that must never reach production):

```bash
$EDITOR /tmp/team-keys.json   # JSON: { "<team>": "<64-char sha256 hex>", ... }
oc create secret generic riptide-collector-team-keys \
    --namespace=$NS \
    --from-file=team-keys.json=/tmp/team-keys.json \
    --dry-run=client -o yaml | oc apply -f -
shred -u /tmp/team-keys.json
```

Every team listed in `service-catalog.json` must have a corresponding entry
in `team-keys.json` — the pod fails to start otherwise.

### 3. Namespace

Riptide does not create the namespace itself (it's an env-specific
decision). Either provision it via your normal flow:

```bash
oc create namespace $NS
```

…or have your overlay's `kustomization.yaml` set `namespace: $NS` so every
resource lands in it.

### 4. Image

Push the image to your registry, then set the `images:` `newName`/`newTag`
in your overlay (the base no longer pins a registry — see PR #8).

## Deploy

```bash
oc apply -k openshift/overlays/<your-overlay>/
```

This applies (in order, via Kustomize):
1. The collector ConfigMaps + the migration Job (runs alembic upgrade head)
2. The collector Deployment + Service + Route

Apply only the collector base: `oc apply -k openshift/collector/`.

## Resource quotas

Every container declares both `requests` and `limits` for CPU and memory.
Defaults are conservative; tune from real metrics. The migration Job is
short-lived and gets a smaller budget than the long-running app.

| Container | requests cpu / mem | limits cpu / mem |
|---|---|---|
| `app` (Deployment) | 100m / 128Mi | 500m / 512Mi |
| `alembic` (Job) | 50m / 128Mi | 300m / 256Mi |

## Editing the catalog

`openshift/collector/service-catalog.json` is the in-repo sample and the
single source of truth. Editing the ConfigMap in the cluster directly is
wrong — it gets overwritten on the next `oc apply -k`. To onboard a team,
see `docs/onboarding-a-team.md`.

## Adding a new suite component

When `riptide-api` or `riptide-dashboard` ships, add a sibling folder
(`openshift/api/`, `openshift/dashboard/`) with its own kustomization and
manifests, and add a line to `openshift/kustomization.yaml`:

```yaml
resources:
  - collector
  - api          # new
  - dashboard    # new
```
