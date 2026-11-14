// DbRiptide.groovy
//
// Jenkins shared-library helper for posting build events to riptide.
// Loaded via `def riptide = load 'DbRiptide.groovy'` and called from a
// Jenkinsfile. Mirrors the style of the existing `Argument.getRequiredValue`
// / `Argument.getOptionalValue` helpers used elsewhere in the library.
//
// THIS FILE IS A TEMPLATE. Before deploying it into a Jenkins shared
// library, edit COLLECTOR_URLS below to point at your real test/prod
// riptide hostnames. The placeholder values use
// `example.invalid` (RFC 6761 reserved, DNS-guaranteed not to resolve)
// so a forgotten swap fails loudly via the best-effort `unreachable`
// warning instead of silently 200-ing some unrelated host.
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
//   image_refs     optional, every image the run published, full references
//                  (registry/path/name:tag) exactly as Argo CD reports them.
//                  One event per run however many images: an event per image
//                  would count as extra builds. (`image_ref`, one string, is
//                  accepted too and merged.)
//   actor_handle   optional, git account Jenkins commits and pushes as
//   actor_account_kind optional, "service" (default when actor_handle is set)
//   build_type     optional, COMPLETED only: release / hotfix / build / ...
//
// The team is derived from the bearer token, never from the payload.
// Notifications are best-effort: a slow or unavailable collector must
// NEVER fail the build.
//
// Picking the collector:
//   * Default — no `stage` and no `collectorUrl` → DEFAULT_STAGE ('prod').
//   * `stage: 'test'` — route to the test collector.
//   * `collectorUrl: '...'` — explicit override (for one-off / sandboxes);
//     wins over `stage`.
//   * Unknown `stage` value → warn loudly, fall back to DEFAULT_STAGE.
//
// Requires the Jenkins HTTP Request plugin.

import groovy.transform.Field

// REPLACE these URLs with the actual riptide endpoints in your
// environment. `example.invalid` is RFC 6761 reserved and is guaranteed
// not to resolve, so a forgotten swap fails loudly via the best-effort
// `unreachable` warning instead of silently 200-ing some unrelated host.
@Field
final Map<String, String> COLLECTOR_URLS = [
        test: 'https://riptide-test.example.invalid',
        prod: 'https://riptide.example.invalid',
]

@Field
final String DEFAULT_STAGE = 'prod'

// ---------------------------------------------------------------------------
// Notify STARTED — call right when the build begins.
// Best-effort: never fails the build.
// ---------------------------------------------------------------------------
void notifyStarted(final Map args) {

    def collectorUrl = resolveCollectorUrl(args)
    if (!collectorUrl) {
        warnRiptide("no collector url", "neither collectorUrl nor stage resolved to a URL; skipping STARTED")
        return
    }
    // Direct Map access (Argument.getOptionalValue rejects null defaults).
    // `?:` also keeps resolveCommitSha() lazy — `sh 'git rev-parse HEAD'`
    // only runs when commitSha wasn't supplied.
    def commitSha = args.commitSha ?: resolveCommitSha()
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
    addActor(body, args)

    postEventBestEffort(collectorUrl, credentialsId, body, timeoutSeconds, httpTimeoutSeconds)
}

// ---------------------------------------------------------------------------
// Notify COMPLETED — call from `post.always { ... }` ONLY.
// Best-effort: never fails the build.
//
// Never from a scripted `finally` without passing `status:`: there the
// exception has not propagated yet, `currentBuild.currentResult` still reads
// SUCCESS, and every failed build is reported as a success. Scripted
// pipelines use `runWithEvents`, which decides the status from the exception.
//
// Images: `imageRefs:` (a list) and/or `imageRef:` (one string). Add an image
// once it is pushed; it is reported whatever the final status.
//
// Belt-and-suspenders: snapshot currentBuild.result before and restore it
// after, so a hiccup in the notify path can never demote a SUCCESS build.
// ---------------------------------------------------------------------------
void notifyCompleted(final Map args) {

    def collectorUrl = resolveCollectorUrl(args)
    if (!collectorUrl) {
        warnRiptide("no collector url", "neither collectorUrl nor stage resolved to a URL; skipping COMPLETED")
        return
    }
    // Direct Map access (Argument.getOptionalValue rejects null defaults).
    def commitSha = args.commitSha ?: resolveCommitSha()
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
    // Whatever the status: callers add an image once it is pushed, and an
    // UNSTABLE run, or one failing in a later step, still published it.
    // flatten() takes a single String passed as imageRefs as one image
    // (String + List would concatenate, and collect would walk characters).
    def images = [args.imageRefs, args.imageRef].flatten()
            .collect { it?.toString()?.trim() }
            .findAll { it }
            .unique()
    if (images) {
        body.image_refs = images
    }
    // `buildType:` (release, hotfix, build, ...): the pipeline's own word.
    def buildType = args.buildType?.toString()?.trim()
    if (buildType) {
        body.build_type = buildType
    }
    addActor(body, args)

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
//   riptide.runWithEvents([:]) {              // defaults: stage='prod'
//       sh './build.sh'
//   }
//
//   // ...or pin to test:
//   riptide.runWithEvents(stage: 'test') { sh './build.sh' }
//
//   // Images known only once the body has run: set them on the same map,
//   // it is read again when COMPLETED is sent.
//   def riptideArgs = [imageRefs: []]
//   riptide.runWithEvents(riptideArgs) {
//       apps.each { app -> /* push */ riptideArgs.imageRefs << "${app.image}:${app.version}" }
//   }
//
// IMPORTANT: this wrapper runs in-band, so when the body throws it determines
// the COMPLETED status from the exception — NOT from
// `currentBuild.currentResult`, which Jenkins only updates AFTER the script
// block has propagated the exception. Reading currentResult there would
// falsely report SUCCESS for a build that is about to be marked FAILURE. On
// the normal path nothing is propagating, so currentResult is read and
// carries UNSTABLE set by `unstable(...)` or junit.
//
// The event describes the run, so its status is the build's result, not the
// wrapped body's: an earlier stage left FAILURE by `catchError` reports
// FAILURE even if the body passes. Wrap the whole build, not one stage.
//
// For maximum fidelity (UNSTABLE / ABORTED / etc.) prefer calling
// `notifyCompleted` from `post.always { script { ... } }` instead — by then
// Jenkins has settled the result.
// ---------------------------------------------------------------------------
void runWithEvents(final Map args, Closure body) {

    notifyStarted(args)
    def preResult = currentBuild.result
    String resolvedStatus = 'FAILURE'
    try {
        body()
        resolvedStatus = currentBuild.currentResult ?: 'SUCCESS'
    } catch (org.jenkinsci.plugins.workflow.steps.FlowInterruptedException t) {
        // User-aborted (or upstream timeout): distinguish from a code
        // FAILURE so dashboards can split "broken build" from "operator
        // killed it" without joining to Jenkins' own state.
        resolvedStatus = 'ABORTED'
        throw t
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
 * Declare the git account Jenkins commits and pushes as (`actorHandle:`), so
 * riptide keeps its pushes and merges out of human activity. Omitted when
 * unset.
 */
private void addActor(Map body, final Map args) {
    if (args.actorHandle) {
        body.actor_handle = args.actorHandle as String
        body.actor_account_kind = (args.actorAccountKind ?: 'service') as String
    }
}

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

/**
 * Resolve the collector URL from `args`:
 *   1. Explicit `collectorUrl:` wins (one-off / sandbox override).
 *   2. `stage:` is looked up in COLLECTOR_URLS.
 *   3. Default: COLLECTOR_URLS[DEFAULT_STAGE].
 *
 * An unknown `stage` value warns loudly and falls back to DEFAULT_STAGE.
 * Returns null only if DEFAULT_STAGE itself isn't in COLLECTOR_URLS,
 * which is a misconfiguration of this file.
 */
private String resolveCollectorUrl(final Map args) {
    // Direct Map access (Argument.getOptionalValue rejects null defaults).
    def explicit = args.collectorUrl
    if (explicit) {
        return explicit
    }
    def stage = Argument.getOptionalValue(args, "stage", DEFAULT_STAGE)
    def url = COLLECTOR_URLS[stage]
    if (!url) {
        warnRiptide(
                "unknown stage",
                "stage='${stage}' not in ${COLLECTOR_URLS.keySet()}; falling back to DEFAULT_STAGE='${DEFAULT_STAGE}'"
        )
        return COLLECTOR_URLS[DEFAULT_STAGE]
    }
    return url
}

private void warnRiptide(String reason, String detail) {
    // Single `echo` so the `[Pipeline] echo` step marker appears once,
    // not five times — keeps the banner visually contiguous in the log.
    // Inner lines with dynamic content drop the leading `!!` since we
    // can't pad to a trailing `!!`; fixed-content lines keep both ends.
    echo([
            '!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!',
            "  WARNING: RIPTIDE ${reason.toUpperCase()}",
            "  ${detail}",
            '  build result is UNAFFECTED — this is best-effort',
            '!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!',
    ].join('\n'))
}

static String stripTrailingSlash(String url) {
    if (url == null) {
        return url
    }
    return url.endsWith('/') ? url.substring(0, url.length() - 1) : url
}

return this
