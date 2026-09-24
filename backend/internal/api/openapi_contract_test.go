package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"

	spec "github.com/trick77/riptide/api"
)

func TestOpenAPISpecIsValid(t *testing.T) {
	if err := loadSpec(t).Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml is not a valid OpenAPI document: %v", err)
	}
}

func TestSpecIsServedWithAnETag(t *testing.T) {
	h := newHarness(t, nil)
	w := h.serve(newRequest(http.MethodGet, "/openapi.yaml", nil))
	expectStatus(t, w, http.StatusOK)
	if !bytes.Equal(w.Body.Bytes(), spec.OpenAPISpec) {
		t.Error("served bytes differ from the embedded spec")
	}
	tag := w.Header().Get("ETag")
	if tag == "" {
		t.Fatal("no ETag")
	}
	r := newRequest(http.MethodGet, "/openapi.yaml", nil)
	r.Header.Set("If-None-Match", tag)
	again := h.serve(r)
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Errorf("conditional request = %d with %d bytes", again.Code, again.Body.Len())
	}
}

func TestMatchesETag(t *testing.T) {
	for _, c := range []struct {
		header, tag string
		want        bool
	}{
		{`"abc"`, `"abc"`, true},
		{`*`, `"abc"`, true},
		{`W/"abc"`, `"abc"`, true},
		{`"other", "abc"`, `"abc"`, true},
		{`"other"`, `"abc"`, false},
		{``, `"abc"`, false},
	} {
		if got := matchesETag(c.header, c.tag); got != c.want {
			t.Errorf("matchesETag(%q, %q) = %v, want %v", c.header, c.tag, got, c.want)
		}
	}
}

func TestDocsPagePointsAtTheSpec(t *testing.T) {
	h := newHarness(t, nil)
	w := h.serve(newRequest(http.MethodGet, "/docs", nil))
	expectStatus(t, w, http.StatusOK)
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "'/openapi.yaml'") {
		t.Errorf("docs page: %s", w.Header().Get("Content-Type"))
	}
}

// The contract test proper: drive the real handler and validate every
// response against the spec.
func TestResponsesMatchOpenAPISpec(t *testing.T) {
	router := specRouter(t)
	h := newHarness(t, nil)
	check := func(method, path string, w *httptest.ResponseRecorder) {
		t.Helper()
		validateResponse(t, router, method, path, w)
	}
	post := http.MethodPost

	check(http.MethodGet, "/health", h.serve(newRequest(http.MethodGet, "/health", nil)))
	check(http.MethodGet, "/ready", h.serve(newRequest(http.MethodGet, "/ready", nil)))
	r := newRequest(http.MethodGet, "/auth/ping", nil)
	r.Header.Set("Authorization", "Bearer "+checkoutArgoCD)
	check(http.MethodGet, "/auth/ping", h.serve(r))
	check(http.MethodGet, "/auth/ping", h.serve(newRequest(http.MethodGet, "/auth/ping", nil)))

	check(post, "/webhooks/pipeline", h.bearer(t, "/webhooks/pipeline", checkoutJenkins, fixture(t, "pipeline_jenkins_completed.json")))
	check(post, "/webhooks/pipeline", h.bearer(t, "/webhooks/pipeline", checkoutJenkins, `{}`))
	check(post, "/webhooks/pipeline", h.bearer(t, "/webhooks/pipeline", "nope", `{}`))
	check(post, "/webhooks/pipeline", h.bearer(t, "/webhooks/pipeline", checkoutJenkins, strings.Repeat(" ", maxOwnedBody+1)))
	check(post, "/webhooks/argocd", h.bearer(t, "/webhooks/argocd", checkoutArgoCD, fixture(t, "argocd_synced.json")))
	ignored := fixture(t, "argocd_synced.json")
	ignored["destination_namespace"] = "x-dev"
	check(post, "/webhooks/argocd", h.bearer(t, "/webhooks/argocd", checkoutArgoCD, ignored))
	check(post, "/webhooks/noergler", h.bearer(t, "/webhooks/noergler", checkoutNoergler, fixture(t, "noergler_feedback.json")))
	check(post, "/webhooks/noergler", h.bearer(t, "/webhooks/noergler", checkoutNoergler, `{"event_type":"x"}`))
	check(post, "/webhooks/bitbucket/checkout", h.bitbucket(t, "checkout", checkoutBitbucket, "pr:merged", fixture(t, "bitbucket_pr_merged.json"), nil))
	check(post, "/webhooks/bitbucket/checkout", h.bitbucket(t, "checkout", checkoutBitbucket, "diagnostics:ping", `{}`, nil))
	check(post, "/webhooks/bitbucket/checkout", h.bitbucket(t, "checkout", "wrong", "pr:merged", `{}`, nil))

	h.mem.fail = errBoom
	check(post, "/webhooks/pipeline", h.bearer(t, "/webhooks/pipeline", checkoutJenkins, fixture(t, "pipeline_jenkins_completed.json")))
	h.mem.pingErr = errBoom
	check(http.MethodGet, "/ready", h.serve(newRequest(http.MethodGet, "/ready", nil)))
}

// Every fixture the handlers accept must also be a valid request under the
// spec, so the documented schemas cannot drift from the real contract.
func TestAcceptedFixturesMatchTheRequestSchemas(t *testing.T) {
	router := specRouter(t)
	for _, c := range []struct{ path, fixture string }{
		{"/webhooks/pipeline", "pipeline_jenkins_completed.json"},
		{"/webhooks/pipeline", "pipeline_tekton_completed.json"},
		{"/webhooks/argocd", "argocd_synced.json"},
		{"/webhooks/noergler", "noergler_pr_completed_merged.json"},
		{"/webhooks/noergler", "noergler_pr_completed_declined.json"},
		{"/webhooks/noergler", "noergler_pr_completed_deleted.json"},
		{"/webhooks/noergler", "noergler_feedback.json"},
	} {
		t.Run(c.fixture, func(t *testing.T) {
			body := jsonBytes(t, fixture(t, c.fixture))
			req := httptest.NewRequest(http.MethodPost, c.path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			route, params, err := router.FindRoute(req)
			if err != nil {
				t.Fatal(err)
			}
			if err := openapi3filter.ValidateRequest(context.Background(), &openapi3filter.RequestValidationInput{
				Request: req, PathParams: params, Route: route,
				Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
			}); err != nil {
				t.Errorf("fixture does not match the spec: %v", err)
			}
		})
	}
}

func specRouter(t *testing.T) routers.Router {
	t.Helper()
	doc := loadSpec(t)
	// httptest's host is not a documented server; match on path alone.
	doc.Servers = nil
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	return router
}

func validateResponse(t *testing.T, router routers.Router, method, path string, w *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	route, params, err := router.FindRoute(req)
	if err != nil {
		t.Fatalf("%s %s: no route in the spec: %v", method, path, err)
	}
	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request: req, PathParams: params, Route: route,
			Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		},
		Status: w.Code,
		Header: w.Header(),
		Options: &openapi3filter.Options{
			IncludeResponseStatus: true,
			AuthenticationFunc:    openapi3filter.NoopAuthenticationFunc,
		},
	}
	input.SetBodyBytes(w.Body.Bytes())
	if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
		t.Errorf("%s %s -> %d does not match the spec: %v\nbody: %s", method, path, w.Code, err, w.Body)
	}
}

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	doc, err := loader.LoadFromData(spec.OpenAPISpec)
	if err != nil {
		t.Fatalf("load openapi.yaml: %v", err)
	}
	return doc
}
