# riptide docs

Operator-facing guides for wiring riptide into your delivery pipeline.

| Doc | Audience | Purpose |
|---|---|---|
| [metrics.md](metrics.md) | Anyone reading the numbers or querying the tables | How each metric is defined, as SQL, and how to read the event tables |
| [setup-bitbucket-webhook.md](setup-bitbucket-webhook.md) | Repo admin | Add a Bitbucket webhook for a repo |
| [setup-jenkins-notification.md](setup-jenkins-notification.md) | Pipeline owner | Send build events from Jenkins (uses shared `/webhooks/pipeline`) |
| [setup-tekton-pipeline.md](setup-tekton-pipeline.md) | Pipeline owner | Send build events from Tekton / OpenShift Pipelines |
| [setup-argocd-notification.md](setup-argocd-notification.md) | Platform / GitOps team | Send sync events from ArgoCD |
| [setup-noergler-notification.md](setup-noergler-notification.md) | noergler operator | Forward review-cost rollups from noergler |
| [onboarding-a-team.md](onboarding-a-team.md) | Anyone onboarding a service | End-to-end checklist |
| [argocd-notification-template.yaml](argocd-notification-template.yaml) | GitOps admin | Drop-in Argo template referenced by setup-argocd-notification |
