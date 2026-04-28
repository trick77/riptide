# Setup: Jenkins notification

Have a Jenkins pipeline POST a small JSON payload to riptide-collector at the
end of each build. The endpoint is shared with Tekton and any other CI
(`POST /webhooks/pipeline`) — distinguish your CI via the `source` field.

## Pipeline contract

riptide-collector accepts the following JSON. **All required fields are
mandatory** — without them the metrics break.

```json
{
  "source": "jenkins",
  "pipeline_name": "<env.JOB_NAME>",
  "run_id": "<env.BUILD_NUMBER>",
  "phase": "COMPLETED",
  "status": "<SUCCESS|FAILURE|UNSTABLE|...>",
  "commit_sha": "<env.GIT_COMMIT>",
  "started_at": "<ISO 8601 UTC>",
  "finished_at": "<ISO 8601 UTC>",
  "service_id": "<optional explicit service id from catalog>"
}
```

If `service_id` is omitted, riptide tries to resolve via `pipeline_name` against the service catalog's `pipelines` array.

## Jenkinsfile snippet

Requires the **HTTP Request** plugin and a `Secret text` credential named `RIPTIDE_TOKEN`.

```groovy
def riptideNotify(String phase) {
    def started = currentBuild.startTimeInMillis
    def finished = System.currentTimeMillis()
    def body = [
        source: 'jenkins',
        pipeline_name: env.JOB_NAME,
        run_id: env.BUILD_NUMBER,
        phase: phase,
        status: currentBuild.currentResult ?: 'IN_PROGRESS',
        commit_sha: env.GIT_COMMIT,
        started_at: new Date(started).format("yyyy-MM-dd'T'HH:mm:ss'Z'", TimeZone.getTimeZone('UTC')),
        finished_at: phase == 'COMPLETED'
            ? new Date(finished).format("yyyy-MM-dd'T'HH:mm:ss'Z'", TimeZone.getTimeZone('UTC'))
            : null,
    ]
    withCredentials([string(credentialsId: 'RIPTIDE_TOKEN', variable: 'TOKEN')]) {
        httpRequest(
            httpMode: 'POST',
            url: 'https://riptide-collector.example.com/webhooks/pipeline',
            customHeaders: [[name: 'Authorization', value: "Bearer ${TOKEN}"]],
            contentType: 'APPLICATION_JSON',
            requestBody: groovy.json.JsonOutput.toJson(body),
            validResponseCodes: '200:299',
        )
    }
}

pipeline {
    agent any
    stages {
        stage('Build') { steps { echo 'build' } }
    }
    post {
        always { script { riptideNotify('COMPLETED') } }
    }
}
```

## Verify

```sql
SELECT delivery_id, source, pipeline_name, run_id, phase, status,
       duration_seconds, service, team
FROM pipeline_events
WHERE source = 'jenkins' AND pipeline_name = '<job>'
ORDER BY created_at DESC
LIMIT 5;
```
