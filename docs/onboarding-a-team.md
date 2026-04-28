# Onboarding a team / service to riptide

End-to-end checklist for adding a team and one or more services so their
delivery events show up in the metrics.

## 1. Edit the catalog

Open a PR editing [`config/service-catalog.json`](../config/service-catalog.json):

- Add the team under `teams[]`:
  ```json
  {
    "name": "checkout",
    "group_email": "team-checkout@example.com",
    "slack": "#team-checkout"
  }
  ```
- Add each service under `services[]`:
  ```json
  {
    "id": "payments-api",
    "display_name": "Payments API",
    "team": "checkout",
    "bitbucket_repos": ["acme/payments-api"],
    "argocd_apps": ["payments-api-prod"],
    "jenkins_jobs": ["payments-api-deploy"]
  }
  ```

CI validates the file (uniqueness, dangling team refs, email shape) — fix any
errors before merging.

## 2. Merge → catalog hot-reloads

After merge, the running collector pod re-reads the file within ~30 seconds
(or restart the pod for instant pickup):

```bash
oc -n riptide rollout restart deployment/riptide-collector
```

## 3. Wire the webhooks

For each repo / job / Application:

- **Bitbucket**: follow [setup-bitbucket-webhook.md](setup-bitbucket-webhook.md)
- **Jenkins**: follow [setup-jenkins-notification.md](setup-jenkins-notification.md)
- **ArgoCD**: follow [setup-argocd-notification.md](setup-argocd-notification.md)

## 4. Smoke test

Open a throwaway PR, merge it, let CI run, deploy to prod, then run:

```bash
uv run python scripts/check_onboarding.py payments-api
```

The script reports whether each of the three sources has produced events for
that service in the last hour. If any is missing, jump to the troubleshooting
sections of the relevant setup doc.
