# Setup: Tekton (OpenShift Pipelines) notification

Have a Tekton `Pipeline` POST a small JSON payload to riptide-collector at the
end of each `PipelineRun`. The endpoint is shared with Jenkins and any other
CI (`POST /webhooks/pipeline`) — Tekton callers identify themselves via
`"source": "tekton"`.

## Approach

Add a **`finally:` task** to your Pipeline that always runs (regardless of
prior task success/failure) and POSTs the riptide payload. This is the
idiomatic Tekton way to do post-run notifications without coupling to Tekton
Triggers or CloudEvents.

## 1) The notify Task

`tekton/tasks/riptide-notify.yaml`:

```yaml
apiVersion: tekton.dev/v1
kind: Task
metadata:
  name: riptide-notify
spec:
  params:
    - name: pipeline-name
      description: Tekton Pipeline metadata.name
    - name: run-id
      description: PipelineRun metadata.name
    - name: aggregate-status
      description: $(tasks.status) from the calling Pipeline (Succeeded/Failed/Completed)
    - name: commit-sha
      description: git SHA built / deployed
    - name: started-at
      description: PipelineRun status.startTime (ISO 8601 UTC)
    - name: finished-at
      description: PipelineRun status.completionTime (ISO 8601 UTC)
    - name: riptide-url
      default: https://riptide-collector.example.com/webhooks/pipeline
  steps:
    - name: post
      image: registry.access.redhat.com/ubi9/ubi-minimal:latest
      env:
        - name: RIPTIDE_TOKEN
          valueFrom:
            secretKeyRef:
              name: riptide-token
              key: token
      script: |
        #!/bin/sh
        set -eu
        microdnf install -y --nodocs jq curl >/dev/null
        # Tekton's $(tasks.status) is one of: Succeeded, Failed, Completed, None
        STATUS="$(params.aggregate-status)"
        BODY=$(jq -n \
          --arg src "tekton" \
          --arg pn "$(params.pipeline-name)" \
          --arg rid "$(params.run-id)" \
          --arg phase "COMPLETED" \
          --arg st "$STATUS" \
          --arg sha "$(params.commit-sha)" \
          --arg sa "$(params.started-at)" \
          --arg fa "$(params.finished-at)" \
          '{source:$src, pipeline_name:$pn, run_id:$rid, phase:$phase,
            status:$st, commit_sha:$sha, started_at:$sa, finished_at:$fa}')
        curl -fsS -X POST "$(params.riptide-url)" \
          -H "Authorization: Bearer ${RIPTIDE_TOKEN}" \
          -H "Content-Type: application/json" \
          --data "$BODY"
```

## 2) Wire it into your Pipeline

In any Pipeline you want measured, add a `finally:` block:

```yaml
apiVersion: tekton.dev/v1
kind: Pipeline
metadata:
  name: payments-api-deploy
spec:
  params:
    - name: commit-sha
  tasks:
    - name: build
      taskRef: { name: build }
      params:
        - name: commit-sha
          value: $(params.commit-sha)
    - name: deploy
      runAfter: [build]
      taskRef: { name: deploy }
  finally:
    - name: notify-riptide
      taskRef: { name: riptide-notify }
      params:
        - name: pipeline-name
          value: $(context.pipeline.name)
        - name: run-id
          value: $(context.pipelineRun.name)
        - name: aggregate-status
          value: $(tasks.status)
        - name: commit-sha
          value: $(params.commit-sha)
        - name: started-at
          value: $(context.pipelineRun.startTime)
        - name: finished-at
          value: $(context.pipelineRun.completionTime)
```

## 3) Provide the bearer token Secret

```bash
oc -n <pipeline-namespace> create secret generic riptide-token \
  --from-literal=token='<TOKEN>'
```

## Catalog mapping

In `config/service-catalog.json`, list your Tekton pipeline names in the
`pipelines` array — same field Jenkins uses:

```json
{
  "id": "payments-api",
  "team": "checkout",
  "pipelines": ["payments-api-deploy"]
}
```

A service can mix Jenkins jobs and Tekton pipelines in the same array — riptide
resolves by name regardless of source.

## Verify

```sql
SELECT delivery_id, source, pipeline_name, run_id, status, duration_seconds,
       service, team
FROM pipeline_events
WHERE source = 'tekton' AND pipeline_name = '<your pipeline>'
ORDER BY created_at DESC
LIMIT 5;
```

## Why a `finally` task and not Tekton Triggers / CloudEvents?

`finally:` runs once per `PipelineRun` regardless of upstream failures, with
the data we want already in scope (`$(tasks.status)`, `$(context.pipelineRun.*)`).
Tekton CloudEvents would force riptide to learn the CloudEvents envelope; the
`finally` approach keeps the wire format identical to Jenkins so the same
endpoint serves both.
