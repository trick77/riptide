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
    timeout: 5s
    retryWaitMin: 1s
    retryWaitMax: 5s
    retryMax: 3
    headers:
      - name: Authorization
        value: "Bearer $riptide-token-checkout"
      - name: Content-Type
        value: application/json

  service.webhook.riptide-platform: |
    url: https://riptide-collector.example.com/webhooks/argocd
    timeout: 5s
    retryWaitMin: 1s
    retryWaitMax: 5s
    retryMax: 3
    headers:
      - name: Authorization
        value: "Bearer $riptide-token-platform"
      - name: Content-Type
        value: application/json
```

> **Why the explicit timeout / retry caps.** The notifications controller
> runs in its own pod (separate from `argocd-application-controller`), so a
> down or slow riptide-collector cannot block syncs or reconciliation —
> only notification dispatch is affected. The caps above bound the worst
> case per event to roughly 5s + (1s + 5s) + (5s + 5s) + 5s ≈ 25s instead
> of inheriting the library's generous defaults. Tune to taste.

Then add the tokens to `argocd-notifications-secret`. **Use the raw bearer
value here — never the sha256 hash from `team-keys.json`.** The wire flow
is `argocd-notifications` substitutes `$riptide-token-<team>` into
`Authorization: Bearer <raw>`, then riptide hashes the raw value to look
up the team. If you paste the hash here, riptide will hash *the hash*,
get a different value, and reject every event with 401.

```bash
oc -n argocd patch secret argocd-notifications-secret -p '{
  "stringData": {
    "riptide-token-checkout": "<RAW_CHECKOUT_TOKEN>",
    "riptide-token-platform": "<RAW_PLATFORM_TOKEN>"
  }
}'
```

The raw tokens are the same values you handed to the team during
onboarding — see [`onboarding-a-team.md`](onboarding-a-team.md) for how
they're generated and how the matching sha256 lands in `team-keys.json`.

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
  annotations:
    # default subscription — every Application under this project inherits it
    # (see https://argocd-notifications.readthedocs.io/en/stable/subscriptions/)
    notifications.argoproj.io/subscribe.on-deployed.riptide-checkout: ""
    notifications.argoproj.io/subscribe.on-sync-failed.riptide-checkout: ""
spec:
  # ... destinations, sourceRepos, etc.
  description: Checkout team apps
```

Apps under the `checkout` AppProject will fire the riptide webhook with the
**checkout** team's bearer.

If a team needs a per-app override (rare), set the same annotation directly
on the `Application` and it overrides the project default.

> **Why only `on-deployed` + `on-sync-failed`.** `on-deployed` already
> covers the success path (sync `Succeeded` *and* health `Healthy`), so
> adding `on-sync-succeeded` would just fire a second webhook for the same
> event. Riptide deduplicates by `delivery_id`, so the second insert is
> dropped, but you'd still see noise in logs and notifications-controller
> traffic. If your team has Applications whose `health.status` never
> reaches `Healthy` (CRDs without a health hook, Jobs, etc.), swap
> `on-deployed` for `on-sync-succeeded` instead — never subscribe to both.

### Alternative: global subscriptions in the ConfigMap

If you cannot rely on `AppProject` inheritance (e.g. Applications live
outside per-team projects), declare subscriptions globally in
`argocd-notifications-cm` with a selector. One block per team, since each
team has its own bearer / `NotificationService`:

```yaml
data:
  subscriptions: |
    - recipients:
        - riptide-checkout
      triggers:
        - on-deployed
        - on-sync-failed
      selector: app.kubernetes.io/part-of=checkout
    - recipients:
        - riptide-platform
      triggers:
        - on-deployed
        - on-sync-failed
      selector: app.kubernetes.io/part-of=platform
```

The `selector` matches labels on the `Application` resource — pick a label
your Applications consistently carry (e.g. `argocd.argoproj.io/instance`,
`app.kubernetes.io/part-of`, or a custom team label). AppProject defaults
are simpler when team ↔ AppProject is 1:1; the global form is the escape
hatch for everything else.

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
`openshift/collector/riptide.json` (`environments.production_stage`,
default `prod`). To keep the database small, list non-prod stage suffixes
in `environments.ignored_stages` (e.g. `["dev", "entw", "syst", "stage"]`)
— matching events return `202 {"status":"ignored"}` and are dropped before
insert. Aggregations group by `app_name`; cross-source joins (Pipeline,
Bitbucket, Noergler) use `revision = commit_sha`.
