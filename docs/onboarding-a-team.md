# Onboarding a team to riptide

End-to-end checklist for adding a team so their delivery events show up in
the metrics. The catalog declares teams; per-source aggregations key on the
upstream identifiers (`repo_full_name`, `pipeline_name`, `app_name`, `repo`)
already present on each event, and cross-source joins use `commit_sha`.

## 1. Add the team to the catalog

Open a PR editing [`openshift/collector/riptide-catalog.json`](../openshift/collector/riptide-catalog.json):

```json
{
  "name": "checkout",
  "group_email": "team-checkout@example.com"
}
```

CI validates the file (uniqueness, email shape) — fix any errors before
merging. After merge the running collector pod re-reads the file within
~30 seconds (or restart for instant pickup):

```bash
oc -n $NS rollout restart deployment/riptide-collector
```

## 2. Generate the team's bearer key

The bearer authenticates every webhook from the team's CI / notification
systems. Riptide stores the **sha256 hash** of the key; the raw value goes
to the team and is never written to disk on the server side.

```bash
RAW=$(openssl rand -base64 32)
echo "Give to team checkout (one-way handoff): $RAW"
printf %s "$RAW" | sha256sum | awk '{print $1}'
# → 64-char hex hash
```

Append the hash to the cluster's `team-keys.json` and rotate the Secret:

```bash
# fetch current, edit, push back
oc -n $NS get secret riptide-collector-team-keys \
   -o jsonpath='{.data.team-keys\.json}' | base64 -d > /tmp/team-keys.json
# add: "checkout": "<the-hash>"
oc -n $NS create secret generic riptide-collector-team-keys \
   --from-file=team-keys.json=/tmp/team-keys.json \
   --dry-run=client -o yaml | oc apply -f -
shred -u /tmp/team-keys.json
oc -n $NS rollout restart deployment/riptide-collector
```

Every team in the catalog must have an entry in `team-keys.json`, or the
pod fails to start. The hot-reloader picks up edits automatically; the
restart above is just to surface validation errors immediately.

## 3. Wire the team's webhooks

The team configures their own webhooks against
`https://<route>/webhooks/{bitbucket,pipeline,argocd}`, with the raw bearer
in the `Authorization: Bearer <RAW>` header. See:

- **Bitbucket**: [setup-bitbucket-webhook.md](setup-bitbucket-webhook.md)
- **ArgoCD**: [setup-argocd-notification.md](setup-argocd-notification.md)
  (use a per-team `NotificationService` with the team's bearer)
- **Tekton**: [setup-tekton-pipeline.md](setup-tekton-pipeline.md)
  (per-team `EventListener` with the team's bearer)
- **Jenkins**: [setup-jenkins-notification.md](setup-jenkins-notification.md)
  (per-folder notification config with the team's bearer)

## 4. Smoke test

Open a throwaway PR, merge it, let CI run, deploy to prod, then run:

```bash
uv run python scripts/check_onboarding.py <repo-name-or-app-name-or-pipeline-name>
```

The script reports whether each of the three sources has produced events for
that identifier in the last hour. If any is missing, jump to the
troubleshooting sections of the relevant setup doc.
