# Setup: ArgoCD notification

Have ArgoCD push sync events to riptide-collector via the
**argocd-notifications** controller. Each team uses **its own bearer
token**: configure one `NotificationService` per team and route teams to
the right one via `AppProject` defaults so you don't annotate every app.

## Prerequisites

- The argocd-notifications controller is installed (ships with most recent
  ArgoCD distributions; if not, see
  [the upstream docs](https://argocd-notifications.readthedocs.io/)).
- Access to edit `argocd-notifications-cm` and `argocd-notifications-secret`.
- Each team's raw bearer token (the platform team hands these out — see
  `docs/onboarding-a-team.md`).

## 1) Per-team webhook services

Edit `argocd-notifications-cm` (in the `argocd` namespace). One
`service.webhook.<team>` block per team:

```yaml
data:
  service.webhook.riptide-checkout: |
    url: https://riptide-collector.example.com/webhooks/argocd
    headers:
      - name: Authorization
        value: "Bearer $riptide-token-checkout"
      - name: Content-Type
        value: application/json

  service.webhook.riptide-platform: |
    url: https://riptide-collector.example.com/webhooks/argocd
    headers:
      - name: Authorization
        value: "Bearer $riptide-token-platform"
      - name: Content-Type
        value: application/json
```

Then add the tokens to `argocd-notifications-secret`:

```bash
oc -n argocd patch secret argocd-notifications-secret -p '{
  "stringData": {
    "riptide-token-checkout": "<RAW_CHECKOUT_TOKEN>",
    "riptide-token-platform": "<RAW_PLATFORM_TOKEN>"
  }
}'
```

## 2) Install the template + triggers

The bundled template is at
[`docs/argocd-notification-template.yaml`](argocd-notification-template.yaml).

```bash
oc -n argocd apply -f docs/argocd-notification-template.yaml
```

This adds:
- `template.app-deployed-riptide`
- `trigger.on-deployed`, `trigger.on-sync-succeeded`, `trigger.on-sync-failed`
  (riptide-flavored)

If you are upgrading from an earlier riptide release, **re-apply this
ConfigMap** so the template body includes the new `destination_namespace`
field — riptide derives the `environment` column (and the prod-vs-non-prod
metric filters) from the namespace suffix.

## 3) Route teams to their service via AppProject defaults

Each team should have its own `AppProject`. Set a default subscription on
the project so every Application under it inherits the right team's
service — no per-app annotations:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: checkout
  namespace: argocd
spec:
  # ... destinations, sourceRepos, etc.
  description: Checkout team apps
  # default subscription:
  # (see https://argocd-notifications.readthedocs.io/en/stable/subscriptions/)
metadata:
  annotations:
    notifications.argoproj.io/subscribe.on-deployed.riptide-checkout: ""
    notifications.argoproj.io/subscribe.on-sync-succeeded.riptide-checkout: ""
    notifications.argoproj.io/subscribe.on-sync-failed.riptide-checkout: ""
```

Apps under the `checkout` AppProject will fire the riptide webhook with the
**checkout** team's bearer.

If a team needs a per-app override (rare), set the same annotation directly
on the `Application` and it overrides the project default.

## Verify

Trigger a sync, then:

```sql
SELECT delivery_id, app_name, revision, operation_phase, team,
       destination_namespace, environment
FROM argocd_events
ORDER BY created_at DESC
LIMIT 5;
```

`team` should equal the team whose bearer was used. `environment` is the
lowercased suffix of `destination_namespace` (after the last `-`); which
suffix counts as "production" is configured in
`openshift/collector/service-catalog.json` (`environments.production_stage`,
default `prod`). Aggregations group by `app_name`; cross-source joins
(Pipeline, Bitbucket, Noergler) use `revision = commit_sha`.
