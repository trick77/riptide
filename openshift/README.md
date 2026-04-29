# OpenShift manifests

Suite-level deployment for the **riptide** observability platform.
Per-component folders sit alongside one another; v1 only ships `collector/`.

## Layout

```
openshift/
├── kustomization.yaml          # suite-level entry; compose components
├── shared/
│   ├── secret.env.example      # template for riptide-collector-secrets
│   └── team-keys.json.example  # template for riptide-collector-team-keys
└── collector/
    ├── kustomization.yaml
    ├── deployment.yaml
    ├── service.yaml
    ├── route.yaml
    ├── configmap-app.yaml             # non-secret env (log level, catalog path, team-keys path)
    ├── service-catalog.json           # canonical catalog (teams + automation)
    └── job-migrate.yaml               # alembic upgrade head, runs PreSync
```

## Database

Postgres is **not** managed by riptide. Provision it separately, then put the
connection URL into `RIPTIDE_DB_URL` in the Secret.

## One-time setup (per environment)

Two Secrets are required: one for the DB connection, one for the per-team
webhook bearer keys.

### 1. `riptide-collector-secrets` (DB URL)

```bash
cp openshift/shared/secret.env.example /tmp/secret.env
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

Build the file:

```bash
cp openshift/shared/team-keys.json.example /tmp/team-keys.json
# edit /tmp/team-keys.json — replace each value with the sha256 hex
oc create secret generic riptide-collector-team-keys \
    --namespace=$NS \
    --from-file=team-keys.json=/tmp/team-keys.json \
    --dry-run=client -o yaml | oc apply -f -
shred -u /tmp/team-keys.json
```

Every team listed in `service-catalog.json` must have a corresponding entry
in `team-keys.json` — the pod fails to start otherwise.

### 3. Image

Push the image to your registry, then set the `images:` newName/newTag in
your overlay (or in `openshift/collector/kustomization.yaml` if you're not
using overlays).

## Deploy

```bash
oc apply -k openshift/<your-overlay>/
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

`config/service-catalog.json` (symlink → `openshift/collector/service-catalog.json`)
is the source of truth. Editing the ConfigMap in the cluster directly is
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
