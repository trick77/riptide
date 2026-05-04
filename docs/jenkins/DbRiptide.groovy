// DbRiptide.groovy
//
// Jenkins shared-library helper for posting build events to riptide-collector.
// Loaded via `def riptide = load 'DbRiptide.groovy'` and called from a
// Jenkinsfile. Mirrors the style of the existing `Argument.getRequiredValue`
// / `Argument.getOptionalValue` helpers used elsewhere in the library.
//
// Riptide pipeline contract (`POST /webhooks/pipeline`):
//   source         required, e.g. "jenkins"
//   pipeline_name  required, Jenkins job name
//   run_id         required, Jenkins build number (string)
//   phase          required, "STARTED" / "COMPLETED" (this helper emits
//                  these two; "FINALIZED" is reserved for future use)
//   status         optional, "SUCCESS" / "FAILURE" / "UNSTABLE" / ...
//   commit_sha     required, >=7 chars
//   started_at     required, ISO 8601 UTC
//   finished_at    optional, ISO 8601 UTC
//
// The team is derived from the bearer token, never from the payload.
// Notifications are best-effort: a slow or unavailable collector must
// NEVER fail the build.
//
// Requires the Jenkins HTTP Request plugin.

// ---------------------------------------------------------------------------
// Notify STARTED — call right when the build begins.
// Best-effort: never fails the build.
// ---------------------------------------------------------------------------
void notifyStarted(final Map args) {

    def collectorUrl = Argument.getRequiredValue(args, "collectorUrl")
    // `?:` keeps resolveCommitSha() lazy — Groovy evaluates default
    // arguments eagerly, so passing it as the third arg of
    // getOptionalValue would `sh 'git rev-parse HEAD'` on every call
    // even when commitSha was supplied explicitly.
    def commitSha = Argument.getOptionalValue(args, "commitSha", null) ?: resolveCommitSha()
    def credentialsId = Argument.getOptionalValue(args, "credentialsId", "RIPTIDE_TOKEN")
    def pipelineName = Argument.getOptionalValue(args, "pipelineName", env.JOB_NAME)
    def runId = Argument.getOptionalValue(args, "runId", env.BUILD_NUMBER)
    def startedAt = Argument.getOptionalValue(args, "startedAt", currentBuildStartedAtIso())
    int timeoutSeconds = Argument.getOptionalValue(args, "timeoutSeconds", 15) as int
    int httpTimeoutSeconds = Argument.getOptionalValue(args, "httpTimeoutSeconds", 10) as int

    if (!commitSha) {
        warnRiptide("missing commit", "no commitSha and resolveCommitSha() returned null; skipping STARTED")
        return
    }

    // Status is intentionally omitted on STARTED — the schema treats it as
    // optional and emitting "IN_PROGRESS" would invent a value the rest of
    // the system never produces.
    def body = [
            source       : 'jenkins',
            pipeline_name: pipelineName,
            run_id       : runId,
            phase        : 'STARTED',
            commit_sha   : commitSha,
            started_at   : startedAt,
    ]

    postEventBestEffort(collectorUrl, credentialsId, body, timeoutSeconds, httpTimeoutSeconds)
}

// ---------------------------------------------------------------------------
// Notify COMPLETED — call from `post.always { ... }`.
// Best-effort: never fails the build.
//
// Belt-and-suspenders: snapshot currentBuild.result before and restore it
// after, so a hiccup in the notify path can never demote a SUCCESS build.
// ---------------------------------------------------------------------------
void notifyCompleted(final Map args) {

    def collectorUrl = Argument.getRequiredValue(args, "collectorUrl")
    def commitSha = Argument.getOptionalValue(args, "commitSha", null) ?: resolveCommitSha()
    def credentialsId = Argument.getOptionalValue(args, "credentialsId", "RIPTIDE_TOKEN")
    def pipelineName = Argument.getOptionalValue(args, "pipelineName", env.JOB_NAME)
    def runId = Argument.getOptionalValue(args, "runId", env.BUILD_NUMBER)
    def status = Argument.getOptionalValue(args, "status", currentBuild.currentResult ?: 'SUCCESS')
    def startedAt = Argument.getOptionalValue(args, "startedAt", currentBuildStartedAtIso())
    def finishedAt = Argument.getOptionalValue(args, "finishedAt", nowIso())
    int timeoutSeconds = Argument.getOptionalValue(args, "timeoutSeconds", 15) as int
    int httpTimeoutSeconds = Argument.getOptionalValue(args, "httpTimeoutSeconds", 10) as int

    if (!commitSha) {
        warnRiptide("missing commit", "no commitSha and resolveCommitSha() returned null; skipping COMPLETED")
        return
    }

    def body = [
            source       : 'jenkins',
            pipeline_name: pipelineName,
            run_id       : runId,
            phase        : 'COMPLETED',
            status       : status,
            commit_sha   : commitSha,
            started_at   : startedAt,
            finished_at  : finishedAt,
    ]

    def preResult = currentBuild.result
    postEventBestEffort(collectorUrl, credentialsId, body, timeoutSeconds, httpTimeoutSeconds)
    // Restore — notify must never demote the build. Guard `preResult != null`
    // because Jenkins refuses `currentBuild.result = null`: you cannot
    // un-fail a build mid-flight, and a no-op assignment risks a warning on
    // some core versions.
    if (currentBuild.result != preResult && preResult != null) {
        currentBuild.result = preResult
    }
}

// ---------------------------------------------------------------------------
// Convenience wrapper: emit STARTED, run the body, emit COMPLETED in a
// finally block. Use this when you don't want to wire up a `post.always`
// stage manually.
//
//   riptide.runWithEvents(
//       collectorUrl: 'https://riptide.example.com',
//       commitSha:    env.GIT_COMMIT,
//   ) {
//       sh './build.sh'
//   }
//
// IMPORTANT: this wrapper runs in-band, so it determines the COMPLETED
// status from whether the body threw — NOT from `currentBuild.currentResult`,
// which Jenkins only updates AFTER the script block has propagated the
// exception. Reading currentResult here would falsely report SUCCESS for a
// build that is about to be marked FAILURE.
//
// For maximum fidelity (UNSTABLE / ABORTED / etc.) prefer calling
// `notifyCompleted` from `post.always { script { ... } }` instead — by then
// Jenkins has settled the result.
// ---------------------------------------------------------------------------
void runWithEvents(final Map args, Closure body) {

    notifyStarted(args)
    def preResult = currentBuild.result
    String resolvedStatus = 'SUCCESS'
    try {
        body()
    } catch (Throwable t) {
        resolvedStatus = 'FAILURE'
        throw t
    } finally {
        notifyCompleted(args + [status: resolvedStatus])
        if (currentBuild.result != preResult && preResult != null) {
            currentBuild.result = preResult
        }
    }
}

// ---------------------------------------------------------------------------
// Internal: POST one event. Best-effort — catches everything, logs loudly,
// returns normally. Caller is never aware of network errors.
//
// Private helper: positional params, not a `Map args` — public methods
// already validated their inputs and the helper has no other call sites.
// ---------------------------------------------------------------------------
private void postEventBestEffort(
        String collectorUrl,
        String credentialsId,
        Map body,
        int timeoutSeconds,
        int httpTimeoutSeconds) {

    def url = stripTrailingSlash(collectorUrl) + "/webhooks/pipeline"
    def jsonBody = groovy.json.JsonOutput.toJson(body)

    try {
        // Hard wall-clock ceiling so a hung connection cannot drag the build.
        timeout(time: timeoutSeconds, unit: 'SECONDS') {
            withCredentials([string(credentialsId: credentialsId, variable: 'TOKEN')]) {
                def resp = httpRequest(
                        httpMode: 'POST',
                        url: url,
                        customHeaders: [[name: 'Authorization', value: "Bearer ${TOKEN}", maskValue: true]],
                        contentType: 'APPLICATION_JSON',
                        requestBody: jsonBody,
                        timeout: httpTimeoutSeconds,
                        quiet: true,
                        validResponseCodes: '100:599',
                        consoleLogResponseBody: false,
                )
                if (resp.status >= 200 && resp.status < 300) {
                    echo "riptide-notify: OK (http=${resp.status} phase=${body.phase})"
                } else {
                    warnRiptide("rejected", "http=${resp.status} url=${url} phase=${body.phase}")
                }
            }
        }
    } catch (org.jenkinsci.plugins.workflow.steps.FlowInterruptedException e) {
        // timeout() aborted us — collector did not respond in time.
        // Do NOT rethrow; that would propagate the abort and fail the build.
        warnRiptide("timeout", "no response in ${timeoutSeconds}s url=${url}")
    } catch (Throwable t) {
        // Connection refused, DNS failure, TLS error, plugin error, etc.
        warnRiptide("unreachable", "${t.class.simpleName}: ${t.message}")
    }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Resolve the commit SHA from the workspace if not supplied explicitly.
 * Prefers `env.GIT_COMMIT` (populated by the Git plugin); falls back to
 * `git rev-parse HEAD` for non-plugin checkouts (e.g. submodule-only,
 * scripted checkouts).
 *
 * @return 40-char SHA, or null if nothing is available
 */
String resolveCommitSha() {
    if (env.GIT_COMMIT) {
        return env.GIT_COMMIT
    }
    try {
        return sh(script: 'git rev-parse HEAD', returnStdout: true).trim()
    } catch (Throwable t) {
        return null
    }
}

/**
 * ISO 8601 UTC timestamp for `now`.
 */
String nowIso() {
    return new Date().format("yyyy-MM-dd'T'HH:mm:ss'Z'", TimeZone.getTimeZone('UTC'))
}

/**
 * ISO 8601 UTC timestamp for the current build's start time.
 * Falls back to `nowIso()` if `currentBuild.startTimeInMillis` is unavailable.
 */
String currentBuildStartedAtIso() {
    try {
        def millis = currentBuild.startTimeInMillis
        return new Date(millis).format("yyyy-MM-dd'T'HH:mm:ss'Z'", TimeZone.getTimeZone('UTC'))
    } catch (Throwable t) {
        return nowIso()
    }
}

private void warnRiptide(String reason, String detail) {
    echo '!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!'
    echo "!! WARNING: RIPTIDE-COLLECTOR ${reason.toUpperCase()}"
    echo "!! ${detail}"
    echo '!! build result is UNAFFECTED — this is best-effort         !!'
    echo '!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!'
}

static String stripTrailingSlash(String url) {
    if (url == null) {
        return url
    }
    return url.endsWith('/') ? url.substring(0, url.length() - 1) : url
}

return this
