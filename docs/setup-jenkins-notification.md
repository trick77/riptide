# Setup: Jenkins notification

Have a Jenkins pipeline POST a small JSON payload to riptide-collector at the
end of each build.

## Pipeline contract

riptide-collector accepts the following JSON; **all required fields are mandatory** —
without them the metrics break.

```json
{
  "job_name": "<env.JOB_NAME>",
  "build_number": <env.BUILD_NUMBER>,
  "phase": "COMPLETED",
  "status": "<SUCCESS|FAILURE|UNSTABLE|...>",
  "commit_sha": "<env.GIT_COMMIT>",
  "started_at": "<ISO 8601 UTC>",
  "finished_at": "<ISO 8601 UTC>",
  "service_id": "<optional explicit service id from catalog>"
}
```

If `service_id` is omitted, riptide tries to resolve via `job_name` against the
service catalog.

## Jenkinsfile snippet

Add to your pipeline. Requires the **HTTP Request** plugin and a `Secret text`
credential named `RIPTIDE_TOKEN` containing the shared bearer token.

```groovy
def riptideNotify(String phase) {
    def started = currentBuild.startTimeInMillis
    def finished = System.currentTimeMillis()
    def body = [
        job_name: env.JOB_NAME,
        build_number: env.BUILD_NUMBER as int,
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
            url: 'https://riptide-collector.example.com/webhooks/jenkins',
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
        always {
            script { riptideNotify('COMPLETED') }
        }
    }
}
```

## Verify

```sql
-- replace <job> with your job name
SELECT delivery_id, phase, status, duration_seconds, service, team
FROM jenkins_events
WHERE job_name = '<job>'
ORDER BY created_at DESC
LIMIT 5;
```

You should see the latest build with `status = SUCCESS|FAILURE`,
`duration_seconds` populated, and `service`/`team` resolved if the job is in
the catalog.
