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
// Notification is best-effort: a slow or unavailable riptide-collector must
// NEVER fail the build. We bound the call with `timeout`, swallow exceptions,
// and accept any HTTP status (we just log it).
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
    try {
        // Hard wall-clock ceiling so a hung connection can't drag the build.
        timeout(time: 15, unit: 'SECONDS') {
            withCredentials([string(credentialsId: 'RIPTIDE_TOKEN', variable: 'TOKEN')]) {
                def url = 'https://riptide-collector.example.com/webhooks/pipeline'
                def resp = httpRequest(
                    httpMode: 'POST',
                    url: url,
                    customHeaders: [[name: 'Authorization', value: "Bearer ${TOKEN}"]],
                    contentType: 'APPLICATION_JSON',
                    requestBody: groovy.json.JsonOutput.toJson(body),
                    timeout: 10,                   // HTTP Request plugin: per-request seconds
                    quiet: true,                   // keep build log clean
                    validResponseCodes: '100:599', // accept any status; we just log it
                    consoleLogResponseBody: false,
                )
                if (resp.status >= 200 && resp.status < 300) {
                    echo "riptide-notify: OK (http=${resp.status})"
                } else {
                    // Reached the server but it returned an error status.
                    echo '!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!'
                    echo '!! WARNING: RIPTIDE-COLLECTOR REJECTED THE EVENT            !!'
                    echo "!! http=${resp.status}  url=${url}"
                    echo '!! build result is UNAFFECTED — this is best-effort         !!'
                    echo '!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!'
                }
            }
        }
    } catch (org.jenkinsci.plugins.workflow.steps.FlowInterruptedException e) {
        // timeout() aborted us — riptide-collector did not respond in time.
        // Do NOT rethrow: that would propagate the abort and fail the build.
        echo '!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!'
        echo '!! WARNING: RIPTIDE-COLLECTOR UNREACHABLE                   !!'
        echo '!! reason=wall-clock timeout (no response in 15s)           !!'
        echo '!! build result is UNAFFECTED — this is best-effort         !!'
        echo '!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!'
    } catch (Throwable t) {
        // Connection refused, DNS failure, TLS error, plugin error, etc.
        echo '!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!'
        echo '!! WARNING: RIPTIDE-COLLECTOR UNREACHABLE                   !!'
        echo "!! reason=${t.class.simpleName}: ${t.message}"
        echo '!! build result is UNAFFECTED — this is best-effort         !!'
        echo '!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!'
    }
}

pipeline {
    agent any
    stages {
        stage('Build') { steps { echo 'build' } }
    }
    post {
        // Snapshot the build result before notify and restore it afterwards as
        // belt-and-suspenders: notify must never demote a SUCCESS build.
        always {
            script {
                def preResult = currentBuild.result
                riptideNotify('COMPLETED')
                if (currentBuild.result != preResult) { currentBuild.result = preResult }
            }
        }
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
