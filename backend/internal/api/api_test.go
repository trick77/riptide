package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/trick77/riptide/internal/parse"
)

// --- probes and ping ---------------------------------------------------------

func TestHealth(t *testing.T) {
	h := newHarness(t, nil)
	w := h.serve(newRequest(http.MethodGet, "/health", nil))
	expectStatus(t, w, http.StatusOK)
	expectBody(t, w, `{"status":"ok"}`)
}

func TestReady(t *testing.T) {
	h := newHarness(t, nil)
	w := h.serve(newRequest(http.MethodGet, "/ready", nil))
	expectStatus(t, w, http.StatusOK)
	expectBody(t, w, `{"status":"ok","teams":2,"team_keys":2,"config_reload_failures":0,"team_keys_reload_failures":0}`)

	h.mem.pingErr = errBoom
	w = h.serve(newRequest(http.MethodGet, "/ready", nil))
	expectStatus(t, w, http.StatusServiceUnavailable)
	expectBody(t, w, `{"detail":"db unreachable"}`)
	if !strings.Contains(h.logs.String(), `"msg":"readiness_db_unreachable"`) || !strings.Contains(h.logs.String(), "boom") {
		t.Errorf("cause not logged: %s", h.logs.String())
	}
}

func TestReadyCountsReloadFailures(t *testing.T) {
	h := newHarness(t, nil)
	if err := os.WriteFile(h.cfgPath, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	h.runtime.Reload()
	w := h.serve(newRequest(http.MethodGet, "/ready", nil))
	if !strings.Contains(w.Body.String(), `"config_reload_failures":1`) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func ping(h *harness, auth string) (int, string) {
	r := newRequest(http.MethodGet, "/auth/ping", nil)
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	w := h.serve(r)
	return w.Code, strings.TrimSpace(w.Body.String())
}

func TestAuthPing(t *testing.T) {
	h := newHarness(t, nil)
	for _, c := range []struct {
		auth string
		code int
		body string
	}{
		{"Bearer " + checkoutNoergler, 200, `{"status":"ok","team":"checkout"}`},
		{"Bearer " + platformNoergler, 200, `{"status":"ok","team":"platform"}`},
		{"Bearer " + checkoutArgoCD, 200, `{"status":"ok","team":"checkout"}`},
		{"bearer   " + checkoutJenkins + "  ", 200, `{"status":"ok","team":"checkout"}`},
		{"Bearer " + checkoutBitbucket, 200, `{"status":"ok","team":"checkout"}`},
		{"", 401, `{"detail":"Missing or malformed Authorization header."}`},
		{"Bearer", 401, `{"detail":"Missing or malformed Authorization header."}`},
		{"Bearer    ", 401, `{"detail":"Missing or malformed Authorization header."}`},
		{"Basic " + checkoutNoergler, 401, `{"detail":"Missing or malformed Authorization header."}`},
		{"Bearer not-a-real-key", 401, `{"detail":"Invalid credentials."}`},
		{"Bearer café", 401, `{"detail":"Invalid credentials."}`},
	} {
		code, body := ping(h, c.auth)
		if code != c.code || body != c.body {
			t.Errorf("%q: %d %s, want %d %s", c.auth, code, body, c.code, c.body)
		}
	}
}

// A key authenticates only its own source.
func TestStrictSourceBinding(t *testing.T) {
	h := newHarness(t, nil)
	for _, c := range []struct {
		path, token string
		body        any
	}{
		{"/webhooks/pipeline", checkoutArgoCD, fixture(t, "pipeline_jenkins_completed.json")},
		{"/webhooks/pipeline", checkoutNoergler, fixture(t, "pipeline_jenkins_completed.json")},
		{"/webhooks/pipeline", checkoutBitbucket, fixture(t, "pipeline_jenkins_completed.json")},
		{"/webhooks/argocd", checkoutJenkins, fixture(t, "argocd_synced.json")},
		{"/webhooks/noergler", checkoutArgoCD, fixture(t, "noergler_pr_completed_merged.json")},
		{"/webhooks/noergler", "", fixture(t, "noergler_pr_completed_merged.json")},
	} {
		w := h.bearer(t, c.path, c.token, c.body)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s with %q: %d", c.path, c.token, w.Code)
		}
	}
	if len(h.mem.rows) != 0 {
		t.Errorf("rows stored: %d", len(h.mem.rows))
	}
}

// The bearer is the team identity; the platform key records platform.
func TestTeamComesFromTheKey(t *testing.T) {
	h := newHarness(t, nil)
	expectStatus(t, h.bearer(t, "/webhooks/pipeline", platformJenkins, fixture(t, "pipeline_jenkins_completed.json")), http.StatusAccepted)
	if h.mem.teams[0] != "platform" {
		t.Errorf("team = %q", h.mem.teams[0])
	}
}

// Credentials are checked before the body is read, so a bad body behind a
// bad key is a 401, never a 422 that reveals the schema.
func TestAuthBeforeValidation(t *testing.T) {
	h := newHarness(t, nil)
	w := h.bearer(t, "/webhooks/pipeline", "wrong", `{`)
	expectStatus(t, w, http.StatusUnauthorized)
}

// --- bitbucket ---------------------------------------------------------------

func TestBitbucketSignature(t *testing.T) {
	h := newHarness(t, nil)
	body := jsonBytes(t, fixture(t, "bitbucket_pr_merged.json"))
	post := func(team, sig string, raw []byte) int {
		r := newRequest(http.MethodPost, "/webhooks/bitbucket/"+team, raw)
		if sig != "" {
			r.Header.Set("X-Hub-Signature", sig)
		}
		r.Header.Set("X-Event-Key", "pr:merged")
		return h.serve(r).Code
	}
	valid := sign(checkoutBitbucket, body)
	for name, c := range map[string]struct {
		team, sig string
		raw       []byte
		want      int
	}{
		"valid":                     {"checkout", valid, body, 202},
		"uppercase hex and prefix":  {"checkout", "SHA256=" + strings.ToUpper(strings.TrimPrefix(valid, "sha256=")), body, 202},
		"missing header":            {"checkout", "", body, 401},
		"no equals sign":            {"checkout", "sha256", body, 401},
		"empty digest":              {"checkout", "sha256=", body, 401},
		"wrong algorithm":           {"checkout", "sha1=" + strings.TrimPrefix(valid, "sha256="), body, 401},
		"tampered body":             {"checkout", valid, append([]byte(" "), body...), 401},
		"other team's secret":       {"checkout", sign(platformBitbucket, body), body, 401},
		"unknown team":              {"ghost", sign(checkoutBitbucket, body), body, 401},
		"bearer secret as hmac key": {"checkout", sign(checkoutJenkins, body), body, 401},
		"non-ASCII signature":       {"checkout", "sha256=éé", body, 401},
		"platform with its secret":  {"platform", sign(platformBitbucket, body), body, 202},
	} {
		if got := post(c.team, c.sig, c.raw); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}
	var rejected []map[string]any
	for _, l := range h.lines(t) {
		if l["msg"] == "hmac_rejected" {
			rejected = append(rejected, l)
		}
	}
	if len(rejected) != 9 {
		t.Fatalf("hmac_rejected lines = %d", len(rejected))
	}
	for _, l := range rejected {
		if l["webhook_source"] != "bitbucket" || l["log_level"] != "warning" {
			t.Errorf("line = %v", l)
		}
		if l["team"] == "ghost" && l["has_secret"] != false {
			t.Errorf("unknown team has_secret = %v", l["has_secret"])
		}
	}
}

func TestBitbucketPRMerged(t *testing.T) {
	h := newHarness(t, nil)
	w := h.bitbucket(t, "checkout", checkoutBitbucket, "pr:merged", fixture(t, "bitbucket_pr_merged.json"), map[string]string{"X-Request-Id": "uuid-1"})
	expectStatus(t, w, http.StatusAccepted)
	expectBody(t, w, `{"status":"accepted"}`)
	d := h.mem.rows[0].(*parse.BitbucketDraft)
	if h.mem.teams[0] != "checkout" || d.DeliveryID != "uuid-1" || *d.RepoFullName != "acme/payments-api" || h.mem.sources[0] != "" {
		t.Errorf("row = %+v team %s source %q", d, h.mem.teams[0], h.mem.sources[0])
	}
	ev := h.processed(t)[0]
	for k, v := range map[string]any{"webhook_source": "bitbucket", "outcome": "accepted", "delivery_id": "uuid-1",
		"event_type": "pr:merged", "repo": "acme/payments-api", "team": "checkout"} {
		if ev[k] != v {
			t.Errorf("%s = %v, want %v", k, ev[k], v)
		}
	}
}

func TestBitbucketAutomation(t *testing.T) {
	h := newHarness(t, nil)
	expectStatus(t, h.bitbucket(t, "checkout", checkoutBitbucket, "pr:opened", fixture(t, "bitbucket_renovate_pr.json"), nil), http.StatusAccepted)
	body := fixture(t, "bitbucket_pr_comment_added.json")
	body["actor"] = map[string]any{"name": "bitbucket.system-user", "type": "SERVICE"}
	expectStatus(t, h.bitbucket(t, "checkout", checkoutBitbucket, "pr:comment:added", body, nil), http.StatusAccepted)
	if h.mem.sources[0] != "renovate" || h.mem.sources[1] != "service-account" {
		t.Errorf("sources = %q", h.mem.sources)
	}
}

func TestBitbucketDedupe(t *testing.T) {
	h := newHarness(t, nil)
	body := fixture(t, "bitbucket_pr_merged.json")
	hdr := map[string]string{"X-Request-UUID": "dup-1"}
	expectStatus(t, h.bitbucket(t, "checkout", checkoutBitbucket, "pr:merged", body, hdr), http.StatusAccepted)
	w := h.bitbucket(t, "checkout", checkoutBitbucket, "pr:merged", body, hdr)
	expectStatus(t, w, http.StatusAccepted)
	expectBody(t, w, `{"status":"accepted"}`)
	ev := h.processed(t)
	if len(h.mem.rows) != 1 || ev[1]["outcome"] != "deduped" {
		t.Errorf("rows %d, outcomes %v / %v", len(h.mem.rows), ev[0]["outcome"], ev[1]["outcome"])
	}
	// Without any delivery header the synthetic id still dedupes a redelivery.
	push := fixture(t, "bitbucket_refs_changed.json")
	h.bitbucket(t, "checkout", checkoutBitbucket, "repo:refs_changed", push, nil)
	h.bitbucket(t, "checkout", checkoutBitbucket, "repo:refs_changed", push, nil)
	if len(h.mem.rows) != 2 {
		t.Errorf("synthetic id did not dedupe: %d rows", len(h.mem.rows))
	}
}

func TestBitbucketSkips(t *testing.T) {
	h := newHarness(t, nil)
	push := fixture(t, "bitbucket_refs_changed.json")
	push["changes"] = []any{map[string]any{"ref": map[string]any{"displayId": "v1", "type": "TAG"}, "toHash": "1111111", "type": "ADD"}}
	for _, c := range []struct {
		event  string
		body   any
		reason string
	}{
		{"repo:refs_changed", push, "no branch change in push"},
		{"pr:merged", `["not", "an", "object"]`, "non-object payload"},
		{"pr:merged", `{nope`, "non-json payload"},
		{"pr:merged", ``, "empty payload"},
		{"diagnostics:ping", `{"test": true}`, "diagnostics ping"},
	} {
		w := h.bitbucket(t, "checkout", checkoutBitbucket, c.event, c.body, map[string]string{"X-Request-Id": "rid-" + c.reason[:3]})
		expectStatus(t, w, http.StatusAccepted)
		expectBody(t, w, `{"status":"ignored","reason":"`+c.reason+`"}`)
	}
	if len(h.mem.rows) != 0 {
		t.Errorf("rows stored: %d", len(h.mem.rows))
	}
	for _, ev := range h.processed(t) {
		if ev["outcome"] != "skipped" || ev["delivery_id"] == "" || ev["delivery_id"] == nil || ev["team"] != "checkout" {
			t.Errorf("line = %v", ev)
		}
	}
}

func TestBitbucketUnparsedDateWarns(t *testing.T) {
	h := newHarness(t, nil)
	body := fixture(t, "bitbucket_pr_merged.json")
	body["date"] = "garbage"
	expectStatus(t, h.bitbucket(t, "checkout", checkoutBitbucket, "pr:merged", body, nil), http.StatusAccepted)
	if !strings.Contains(h.logs.String(), `"msg":"bitbucket_date_unparsed"`) {
		t.Error("no warning logged")
	}
	if d := h.mem.rows[0].(*parse.BitbucketDraft); !d.OccurredAt.Equal(fixedNow) {
		t.Errorf("occurred_at = %v", d.OccurredAt)
	}
}

// --- pipeline, argocd, noergler ------------------------------------------------

func TestPipeline(t *testing.T) {
	h := newHarness(t, nil)
	body := fixture(t, "pipeline_jenkins_completed.json")
	w := h.bearer(t, "/webhooks/pipeline", checkoutJenkins, body)
	expectStatus(t, w, http.StatusAccepted)
	expectBody(t, w, `{"status":"accepted"}`)
	expectStatus(t, h.bearer(t, "/webhooks/pipeline", checkoutJenkins, body), http.StatusAccepted)
	// Same pipeline name from another CI is another run.
	tekton := fixture(t, "pipeline_jenkins_completed.json")
	tekton["source"] = "tekton"
	expectStatus(t, h.bearer(t, "/webhooks/pipeline", checkoutJenkins, tekton), http.StatusAccepted)
	if len(h.mem.rows) != 2 {
		t.Errorf("rows = %d", len(h.mem.rows))
	}
	ev := h.processed(t)
	if ev[0]["outcome"] != "accepted" || ev[1]["outcome"] != "deduped" || ev[0]["ci_system"] != "jenkins" ||
		ev[0]["run_id"] != "1234" || ev[0]["status"] != "SUCCESS" || ev[0]["pipeline"] != "payments-api-deploy" || ev[0]["phase"] != "COMPLETED" {
		t.Errorf("lines = %v", ev)
	}
}

func TestValidationIs422(t *testing.T) {
	h := newHarness(t, nil)
	for _, c := range []struct{ path, token, body string }{
		{"/webhooks/pipeline", checkoutJenkins, `{"source": "jenkins"}`},
		{"/webhooks/argocd", checkoutArgoCD, `{"app_name": "x"}`},
		{"/webhooks/noergler", checkoutNoergler, `{"event_type": "completed"}`},
		{"/webhooks/pipeline", checkoutJenkins, `not json`},
		{"/webhooks/argocd", checkoutArgoCD, `[]`},
	} {
		w := h.bearer(t, c.path, c.token, c.body)
		expectStatus(t, w, http.StatusUnprocessableEntity)
		var body struct {
			Detail []parse.FieldError `json:"detail"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Detail) == 0 || body.Detail[0].Loc[0] != "body" {
			t.Errorf("%s: body %s", c.path, w.Body.String())
		}
	}
	if len(h.processed(t)) != 0 {
		t.Error("a rejected body logged webhook_processed")
	}
}

func TestPayloadTooLarge(t *testing.T) {
	h := newHarness(t, nil)
	big := `{"pad": "` + strings.Repeat("x", maxOwnedBody) + `"}`
	expectStatus(t, h.bearer(t, "/webhooks/pipeline", checkoutJenkins, big), http.StatusRequestEntityTooLarge)
	huge := []byte(`{"pad": "` + strings.Repeat("x", maxBitbucketBody) + `"}`)
	w := h.bitbucket(t, "checkout", checkoutBitbucket, "pr:merged", huge, nil)
	expectStatus(t, w, http.StatusRequestEntityTooLarge)
	expectBody(t, w, `{"detail":"payload too large"}`)
}

func TestArgoCD(t *testing.T) {
	h := newHarness(t, nil)
	body := fixture(t, "argocd_synced.json")
	expectStatus(t, h.bearer(t, "/webhooks/argocd", checkoutArgoCD, body), http.StatusAccepted)
	expectStatus(t, h.bearer(t, "/webhooks/argocd", checkoutArgoCD, body), http.StatusAccepted)
	// Each phase transition is its own row.
	for _, phase := range []string{"Running", "Failed"} {
		b := fixture(t, "argocd_synced.json")
		b["operation_phase"] = phase
		expectStatus(t, h.bearer(t, "/webhooks/argocd", checkoutArgoCD, b), http.StatusAccepted)
	}
	if len(h.mem.rows) != 3 {
		t.Errorf("rows = %d", len(h.mem.rows))
	}
	ev := h.processed(t)
	if ev[0]["outcome"] != "accepted" || ev[1]["outcome"] != "deduped" || ev[0]["environment"] != "prod" ||
		ev[0]["app"] != "payments-api-prod" || ev[0]["phase"] != "Succeeded" || ev[0]["delivery_id"] == nil {
		t.Errorf("lines = %v", ev)
	}
}

func TestArgoCDIgnoredStage(t *testing.T) {
	h := newHarness(t, nil)
	body := fixture(t, "argocd_synced.json")
	body["destination_namespace"] = "payments-api-SYST"
	w := h.bearer(t, "/webhooks/argocd", checkoutArgoCD, body)
	expectStatus(t, w, http.StatusAccepted)
	expectBody(t, w, `{"status":"ignored"}`)
	if len(h.mem.rows) != 0 {
		t.Error("ignored stage stored")
	}
	ev := h.processed(t)[0]
	if ev["outcome"] != "ignored" || ev["reason"] != "stage_in_ignored_stages" || ev["delivery_id"] == nil ||
		ev["environment"] != "syst" || ev["destination_namespace"] != "payments-api-SYST" {
		t.Errorf("line = %v", ev)
	}
}

// Python only reloaded the config on Bitbucket requests, so an
// ignored_stages change never reached Argo CD until one arrived.
func TestArgoCDSeesAReloadedConfig(t *testing.T) {
	h := newHarness(t, nil)
	cfg := strings.Replace(configJSON, `"stage"]`, `"stage", "intg"]`, 1)
	if err := os.WriteFile(h.cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	h.runtime.Reload()
	body := fixture(t, "argocd_synced.json")
	body["destination_namespace"] = "payments-intg"
	expectBody(t, h.bearer(t, "/webhooks/argocd", checkoutArgoCD, body), `{"status":"ignored"}`)
}

func TestNoergler(t *testing.T) {
	h := newHarness(t, nil)
	expectStatus(t, h.bearer(t, "/webhooks/noergler", checkoutNoergler, fixture(t, "noergler_pr_completed_merged.json")), http.StatusAccepted)
	flipped := fixture(t, "noergler_pr_completed_merged.json")
	flipped["pr_key"] = strings.ToLower(flipped["pr_key"].(string))
	expectStatus(t, h.bearer(t, "/webhooks/noergler", checkoutNoergler, flipped), http.StatusAccepted)
	expectStatus(t, h.bearer(t, "/webhooks/noergler", checkoutNoergler, fixture(t, "noergler_pr_completed_declined.json")), http.StatusAccepted)
	fb := fixture(t, "noergler_feedback.json")
	expectStatus(t, h.bearer(t, "/webhooks/noergler", checkoutNoergler, fb), http.StatusAccepted)
	expectStatus(t, h.bearer(t, "/webhooks/noergler", checkoutNoergler, fb), http.StatusAccepted)
	fb["verdict"] = "acknowledged"
	expectStatus(t, h.bearer(t, "/webhooks/noergler", checkoutNoergler, fb), http.StatusAccepted)
	if len(h.mem.rows) != 4 {
		t.Errorf("rows = %d", len(h.mem.rows))
	}
	ev := h.processed(t)
	if ev[0]["event_type"] != "pr_completed" || ev[1]["outcome"] != "deduped" || ev[3]["event_type"] != "feedback" {
		t.Errorf("lines = %v", ev)
	}
	if _, ok := ev[0]["noergler_event_type"]; ok {
		t.Error("namespaced event type leaked")
	}
}

func TestPersistFailureIs500AndLogged(t *testing.T) {
	h := newHarness(t, nil)
	h.mem.fail = errBoom
	for _, w := range []interface{ Result() *http.Response }{
		h.bearer(t, "/webhooks/pipeline", checkoutJenkins, fixture(t, "pipeline_jenkins_completed.json")),
		h.bearer(t, "/webhooks/argocd", checkoutArgoCD, fixture(t, "argocd_synced.json")),
		h.bearer(t, "/webhooks/noergler", checkoutNoergler, fixture(t, "noergler_feedback.json")),
		h.bitbucket(t, "checkout", checkoutBitbucket, "pr:merged", fixture(t, "bitbucket_pr_merged.json"), nil),
	} {
		if code := w.Result().StatusCode; code != http.StatusInternalServerError {
			t.Errorf("status = %d", code)
		}
	}
	n := 0
	for _, l := range h.lines(t) {
		if l["msg"] == "webhook_persist_failed" {
			n++
			if l["log_level"] != "error" || l["error"] != "boom" || l["delivery_id"] == nil || l["team"] != "checkout" {
				t.Errorf("line = %v", l)
			}
		}
	}
	if n != 4 || len(h.processed(t)) != 0 {
		t.Errorf("persist_failed = %d, processed = %d", n, len(h.processed(t)))
	}
}

// JSON that JSONB refuses (a \u0000 escape) is the body's fault: a 422 on the
// owned contracts, a skip for Bitbucket, never a 500 that retries forever.
func TestUnstorablePayload(t *testing.T) {
	h := newHarness(t, nil)
	h.mem.fail = errUnstorable
	w := h.bearer(t, "/webhooks/pipeline", checkoutJenkins, fixture(t, "pipeline_jenkins_completed.json"))
	expectStatus(t, w, http.StatusUnprocessableEntity)
	if !strings.Contains(w.Body.String(), `"loc":["body"]`) || !strings.Contains(w.Body.String(), "unsupported Unicode escape sequence") {
		t.Errorf("body = %s", w.Body.String())
	}
	w = h.bitbucket(t, "checkout", checkoutBitbucket, "pr:merged", fixture(t, "bitbucket_pr_merged.json"), map[string]string{"X-Request-Id": "nul-1"})
	expectStatus(t, w, http.StatusAccepted)
	expectBody(t, w, `{"status":"ignored","reason":"payload not storable as JSONB"}`)
	ev := h.processed(t)
	if len(ev) != 1 || ev[0]["outcome"] != "skipped" || ev[0]["delivery_id"] != "nul-1" {
		t.Errorf("lines = %v", ev)
	}
	if !strings.Contains(h.logs.String(), `"msg":"webhook_payload_unstorable"`) || strings.Contains(h.logs.String(), "webhook_persist_failed") {
		t.Errorf("logs = %s", h.logs.String())
	}
}

// The Splunk wire contract, over every source: each webhook_processed line
// carries the fixed keys and none of the Splunk-reserved ones, and every
// line of any kind starts with the timestamp.
func TestWebhookProcessedSchema(t *testing.T) {
	h := newHarness(t, nil)
	h.bearer(t, "/webhooks/pipeline", checkoutJenkins, fixture(t, "pipeline_jenkins_completed.json"))
	h.bearer(t, "/webhooks/argocd", checkoutArgoCD, fixture(t, "argocd_synced.json"))
	h.bearer(t, "/webhooks/noergler", checkoutNoergler, fixture(t, "noergler_pr_completed_merged.json"))
	h.bitbucket(t, "checkout", checkoutBitbucket, "pr:merged", fixture(t, "bitbucket_pr_merged.json"), nil)
	h.bitbucket(t, "checkout", checkoutBitbucket, "diagnostics:ping", `{}`, nil)
	ignored := fixture(t, "argocd_synced.json")
	ignored["destination_namespace"] = "payments-api-syst"
	h.bearer(t, "/webhooks/argocd", checkoutArgoCD, ignored)

	events := h.processed(t)
	if len(events) != 6 {
		t.Fatalf("webhook_processed lines = %d", len(events))
	}
	sources := map[any]bool{}
	for _, ev := range events {
		sources[ev["webhook_source"]] = true
		for _, k := range []string{"msg", "log_level", "timestamp", "service", "version", "env", "webhook_source", "outcome", "delivery_id", "team"} {
			if v, ok := ev[k]; !ok || v == nil || v == "" {
				t.Errorf("missing %s in %v", k, ev)
			}
		}
		for _, k := range []string{"source", "event", "level", "host", "index", "sourcetype", "noergler_event_type"} {
			if _, ok := ev[k]; ok {
				t.Errorf("reserved %s leaked in %v", k, ev)
			}
		}
	}
	if len(sources) != 4 {
		t.Errorf("sources = %v", sources)
	}
	for _, line := range strings.Split(strings.TrimSpace(h.logs.String()), "\n") {
		if !strings.HasPrefix(line, `{"timestamp":"`) {
			t.Errorf("line does not start with timestamp: %s", line)
		}
	}
}
