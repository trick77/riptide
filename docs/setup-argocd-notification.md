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

Then add the tokens to `argocd-notifications-secret`. Use the team's
**`argocd`** entry from `team-keys.json` — that is the only key that
authenticates `/webhooks/argocd` (strict source binding; a `jenkins` or
`bitbucket` token will be rejected). `argocd-notifications` substitutes
`$riptide-token-<team>` into `Authorization: Bearer <raw>`; riptide
compares the incoming value constant-time against `<team>.argocd`.

```bash
oc -n argocd patch secret argocd-notifications-secret -p '{
  "stringData": {
    "riptide-token-checkout": "<RAW_CHECKOUT_TOKEN>",
    "riptide-token-platform": "<RAW_PLATFORM_TOKEN>"
  }
}'
```

If you later need to retrieve a token (e.g. for the Bitbucket onboarding
script), remember Kubernetes wraps Secret values in base64 on read-back —
always pipe through `base64 -d`:

```bash
oc -n argocd get secret argocd-notifications-secret \
   -o jsonpath='{.data.riptide-token-checkout}' | base64 -d
```

The raw tokens are the same values you handed to the team during
onboarding — see [`onboarding-a-team.md`](onboarding-a-team.md).

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

## 3) Route teams to their service via subscriptions

**Without a subscription, no webhook leaves Argo CD** — the notifications
controller will reconcile the Application (`Start processing` /
`Processing completed` in its log) and emit nothing else. That silent
log pattern, plus a missing `notified.notifications.argoproj.io`
annotation on the Application, is the canonical "no subscription matches
this app" signature.

> **Why only `on-deployed` + `on-sync-failed`.** `on-deployed` already
> covers the success path (sync `Succeeded` *and* health `Healthy`), so
> adding `on-sync-succeeded` would just fire a second webhook for the same
> event. Riptide deduplicates by `delivery_id`, so the second insert is
> dropped, but you'd still see noise in logs and notifications-controller
> traffic. If your team has Applications whose `health.status` never
> reaches `Healthy` (CRDs without a health hook, Jobs, etc.), swap
> `on-deployed` for `on-sync-succeeded` instead — never subscribe to both.

> **OpenShift GitOps gotcha.** On argocd-operator-managed ArgoCD (the
> OpenShift GitOps stack), do **not** rely on
> `spec.notifications.subscriptions` on the ArgoCD CR or on a global
> `subscriptions:` key in `argocd-notifications-cm`. The operator owns
> the ConfigMap and renders subscriptions into a key called `default:`,
> which the upstream argocd-notifications controller does not read — so
> the global block is silently ignored, no webhook ever leaves the
> cluster, and the only signal is the `notified` annotation never being
> set on Applications. Use per-AppProject annotations instead (below).

### Recommended: AppProject default annotation

One annotation on each AppProject the team owns — every Application
under it inherits it, no per-app boilerplate, and it works on both
upstream Argo CD and OpenShift GitOps:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: checkout
  namespace: argocd
  annotations:
    notifications.argoproj.io/subscribe.on-deployed.riptide-checkout: ""
    notifications.argoproj.io/subscribe.on-sync-failed.riptide-checkout: ""
```

If a team owns several AppProjects (one per Bitbucket project, etc.),
add the annotation to each — it is the only form that reliably reaches
the controller on operator-managed ArgoCD.

If a team needs a per-app override (rare), set the same annotation
directly on the `Application` and it takes precedence.

### Alternative: global subscription with a team-label selector (upstream Argo CD only)

Skip this on OpenShift GitOps — see the gotcha above. On upstream
Argo CD you can avoid annotating each AppProject with a single label
selector, provided every Application carries a stable `team: <team>`
label:

```yaml
data:
  subscriptions: |
    - recipients:
        - riptide-checkout
      triggers:
        - on-deployed
        - on-sync-failed
      selector: team=checkout
    - recipients:
        - riptide-platform
      triggers:
        - on-deployed
        - on-sync-failed
      selector: team=platform
```

Apply with a strategic-merge patch on the existing `argocd-notifications-cm`
(don't replace it — the template/trigger blocks live there too):

```bash
oc -n <argocd-ns> patch cm argocd-notifications-cm --type=merge -p "$(cat <<'EOF'
{
  "data": {
    "subscriptions": "- recipients:\n    - riptide-checkout\n  triggers:\n    - on-deployed\n    - on-sync-failed\n  selector: team=checkout\n"
  }
}
EOF
)"
oc -n <argocd-ns> rollout restart deploy/argocd-notifications-controller
```

Substitute `<argocd-ns>` for whichever namespace runs the notifications
controller — in apps-in-any-namespace setups this can be a tenant
namespace (e.g. `argocd-<team>-prod`), not the default `argocd`.

The annotation form and the global form are additive, so you can run
both during a migration — duplicate fires are absorbed by riptide's
`delivery_id` dedup.

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
