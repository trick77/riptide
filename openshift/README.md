# OpenShift manifests

Suite-level deployment for the **riptide** observability platform. Per-component
folders sit alongside one another; v1 only ships `collector/`.

## Layout

```
openshift/
├── kustomization.yaml          # suite-level entry; compose components
├── shared/
│   ├── namespace.yaml
│   └── secret.env.example      # template for the riptide-collector-secrets Secret
└── collector/
    ├── kustomization.yaml
    ├── deployment.yaml
    ├── service.yaml
    ├── route.yaml
    ├── configmap-app.yaml             # non-secret env (log level, catalog path)
    ├── configmap-service-catalog.yaml # generated from config/service-catalog.json
    └── job-migrate.yaml               # alembic upgrade head, runs PreSync
```

## Database

Postgres is **not** managed by riptide. Provision it in the cluster (or wherever)
separately, then put the connection URL into `RIPTIDE_DB_URL` in the Secret.

## One-time setup (per environment)

1. Create the Secret from the template:

   ```bash
   cp openshift/shared/secret.env.example /tmp/secret.env
   # edit /tmp/secret.env — set real RIPTIDE_DB_URL and RIPTIDE_WEBHOOK_TOKEN
   oc create secret generic riptide-collector-secrets \
       --namespace=riptide \
       --from-env-file=/tmp/secret.env \
       --dry-run=client -o yaml | oc apply -f -
   shred -u /tmp/secret.env
   ```

2. Push the image to your registry, then update the `images:` newName/newTag in
   `openshift/collector/kustomization.yaml` (or set via overlay).

## Deploy

```bash
oc apply -k openshift/
```

This applies (in order, via Kustomize):
1. The `riptide` namespace
2. The collector ConfigMaps + the migration Job (runs alembic upgrade head)
3. The collector Deployment + Service + Route

Apply only the collector: `oc apply -k openshift/collector/`.

## Resource quotas

Every container in this folder declares both `requests` and `limits` for CPU
and memory. Defaults are conservative; tune from real metrics. The migration
Job is short-lived and gets a smaller budget than the long-running app.

| Container | requests cpu / mem | limits cpu / mem |
|---|---|---|
| `app` (Deployment) | 100m / 128Mi | 500m / 512Mi |
| `alembic` (Job) | 50m / 128Mi | 300m / 256Mi |

## Editing the catalog

`config/service-catalog.json` is the source of truth. Editing the ConfigMap
in the cluster directly is wrong — it will be overwritten on the next
`oc apply -k`. To onboard a service or team, see `docs/onboarding-a-team.md`.

## Adding a new suite component

When `riptide-api` or `riptide-dashboard` ships, add a sibling folder
(`openshift/api/`, `openshift/dashboard/`) with its own kustomization and
manifests, and add a line to `openshift/kustomization.yaml`:

```yaml
resources:
  - shared/namespace.yaml
  - collector
  - api          # new
  - dashboard    # new
```
