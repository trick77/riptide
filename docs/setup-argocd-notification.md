# Setup: ArgoCD notification

Have ArgoCD push sync events for prod Applications to riptide-collector via
the **argocd-notifications** controller.

## Prerequisites

- The argocd-notifications controller is installed (it ships with most
  recent ArgoCD distributions; if not, see
  [the upstream docs](https://argocd-notifications.readthedocs.io/)).
- Access to edit `argocd-notifications-cm` and `argocd-notifications-secret`.
- The shared bearer token from `riptide-collector-secrets`.

## 1) Add the webhook service

Edit `argocd-notifications-cm` (in the `argocd` namespace):

```yaml
data:
  service.webhook.riptide: |
    url: https://riptide-collector.example.com/webhooks/argocd
    headers:
      - name: Authorization
        value: "Bearer $riptide-token"
      - name: Content-Type
        value: application/json
```

Then add the token to `argocd-notifications-secret`:

```bash
oc -n argocd patch secret argocd-notifications-secret \
   -p '{"stringData": {"riptide-token": "<TOKEN>"}}'
```

## 2) Install the template + triggers

The bundled template is at [`docs/argocd-notification-template.yaml`](argocd-notification-template.yaml).

Append it to `argocd-notifications-cm`:

```bash
oc -n argocd apply -f docs/argocd-notification-template.yaml
```

This adds:
- `template.app-deployed-riptide`
- `trigger.on-deployed`, `trigger.on-sync-succeeded`, `trigger.on-sync-failed` (riptide-flavored)

## 3) Subscribe per Application

Add this annotation to each `Application` you want tracked:

```yaml
metadata:
  annotations:
    notifications.argoproj.io/subscribe.on-deployed.riptide: ""
    notifications.argoproj.io/subscribe.on-sync-succeeded.riptide: ""
    notifications.argoproj.io/subscribe.on-sync-failed.riptide: ""
```

## Verify

Trigger a sync, then:

```sql
SELECT delivery_id, app_name, revision, operation_phase, duration_seconds, service, team
FROM argocd_events
ORDER BY created_at DESC
LIMIT 5;
```

You should see one row per relevant trigger, with `revision` matching the
deployed commit SHA.
