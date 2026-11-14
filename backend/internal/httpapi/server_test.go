package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/ship/kit/logging"
)

func newServer(checks ...httpx.Check) (*Server, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	log := slog.New(logging.NewSplunkHandler(buf, slog.LevelInfo, "riptide", "test"))
	s := New(log, checks...)
	s.HandleFunc("POST /things", func(w http.ResponseWriter, r *http.Request) {
		log.InfoContext(r.Context(), "inside")
		WriteDetail(w, http.StatusTeapot, "short and stout")
	})
	s.HandleFunc("GET /silent", func(http.ResponseWriter, *http.Request) {})
	s.HandleFunc("GET /write", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("x")) })
	s.HandleFunc("GET /panic", func(http.ResponseWriter, *http.Request) { panic("kaboom") })
	s.HandleFunc("GET /abort", func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	s.HandleFunc("GET /dir/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	return s, buf
}

func do(s *Server, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// local is a request from this host: kit's probes answer nobody else.
func local(s *Server, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.RemoteAddr = "127.0.0.1:40000"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func TestAccessLog(t *testing.T) {
	s, buf := newServer()
	w := do(s, http.MethodPost, "/things", map[string]string{"X-Request-Id": "test-rid-abc"})
	if w.Code != http.StatusTeapot || strings.TrimSpace(w.Body.String()) != `{"detail":"short and stout"}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w.Header().Get("X-Request-Id") != "test-rid-abc" {
		t.Error("request id not echoed")
	}
	ls := lines(t, buf)
	if len(ls) != 2 {
		t.Fatalf("lines = %v", ls)
	}
	if in := ls[0]; in["msg"] != "inside" || in["request_id"] != "test-rid-abc" || in["method"] != "POST" || in["path"] != "/things" {
		t.Errorf("handler line = %v", in)
	}
	ev := ls[1]
	if ev["msg"] != "http_request" || ev["request_id"] != "test-rid-abc" || ev["path"] != "/things" || ev["method"] != "POST" ||
		ev["status"] != float64(418) || ev["log_level"] != "warning" {
		t.Errorf("line = %v", ev)
	}
	if _, ok := ev["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms = %v", ev["duration_ms"])
	}
}

func TestRequestIDIsGeneratedWhenMissingOrUnsafe(t *testing.T) {
	s, buf := newServer()
	for _, id := range []string{"", "has space", strings.Repeat("a", 65), "new\nline"} {
		buf.Reset()
		w := do(s, http.MethodGet, "/silent", map[string]string{"X-Request-Id": id})
		got := w.Header().Get("X-Request-Id")
		if got == id || len(got) != 32 {
			t.Errorf("id %q -> %q", id, got)
		}
		if ev := lines(t, buf)[0]; ev["request_id"] != got || ev["status"] != float64(200) || ev["log_level"] != "info" {
			t.Errorf("line = %v", ev)
		}
	}
	buf.Reset()
	do(s, http.MethodGet, "/write", nil)
	if lines(t, buf)[0]["status"] != float64(200) {
		t.Error("write without WriteHeader not logged as 200")
	}
}

func TestProbes(t *testing.T) {
	var down error
	s, buf := newServer(httpx.Check{Name: "database", Run: func(context.Context) error { return down }})
	if w := local(s, "/healthz"); w.Code != http.StatusOK {
		t.Errorf("healthz = %d", w.Code)
	}
	if w := local(s, "/readyz"); w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"status":"ok"}` {
		t.Errorf("readyz = %d %s", w.Code, w.Body)
	}
	if buf.Len() != 0 {
		t.Errorf("probe logged: %s", buf.String())
	}
	down = errors.New("boom")
	if w := local(s, "/readyz"); w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"check":"database"`) {
		t.Errorf("readyz down = %d %s", w.Code, w.Body)
	}
	if ls := lines(t, buf); len(ls) != 1 || ls[0]["check"] != "database" || ls[0]["err"] != "boom" {
		t.Errorf("readiness transition = %v", ls)
	}
	// Anyone but this host: as if the probes did not exist.
	if w := do(s, http.MethodGet, "/readyz", nil); w.Code != http.StatusNotFound {
		t.Errorf("remote readyz = %d", w.Code)
	}
}

// A caller kit's probes refuse gets the JSON 404 of a path that does not
// exist, for any method: not kit's text/plain 404, not a 405 naming GET.
func TestProbesRefuseRemoteCallersInTheDetailShape(t *testing.T) {
	s, _ := newServer()
	for _, c := range []struct {
		method, path, remote string
		hdr                  map[string]string
	}{
		{http.MethodGet, "/readyz", "192.0.2.1:1234", nil},
		{http.MethodPost, "/readyz", "192.0.2.1:1234", nil},
		{http.MethodGet, "/healthz", "[2001:db8::1]:1234", nil},
		{http.MethodGet, "/healthz", "127.0.0.1:40000", map[string]string{"X-Forwarded-For": "192.0.2.1"}},
		{http.MethodGet, "/healthz", "not-an-addr", nil},
	} {
		r := httptest.NewRequest(c.method, c.path, nil)
		r.RemoteAddr = c.remote
		for k, v := range c.hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusNotFound || w.Body.String() != `{"detail":"Not Found"}` ||
			w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Allow") != "" {
			t.Errorf("%s %s from %s %v: %d %q %v", c.method, c.path, c.remote, c.hdr, w.Code, w.Body, w.Header())
		}
	}
	if w := local(s, "/healthz"); w.Code != http.StatusOK || w.Body.String() != "ok\n" {
		t.Errorf("loopback healthz = %d %q", w.Code, w.Body)
	}
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	r.RemoteAddr = "[::1]:40000"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("IPv6 loopback healthz = %d", w.Code)
	}
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	s, _ := newServer()
	w := do(s, http.MethodGet, "/nope", nil)
	if w.Code != 404 || strings.TrimSpace(w.Body.String()) != `{"detail":"Not Found"}` || w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("404: %d %s", w.Code, w.Body)
	}
	w = do(s, http.MethodGet, "/things", nil)
	if w.Code != 405 || strings.TrimSpace(w.Body.String()) != `{"detail":"Method Not Allowed"}` || !strings.Contains(w.Header().Get("Allow"), "POST") {
		t.Errorf("405: %d %s %v", w.Code, w.Body, w.Header())
	}
	// The mux's own redirect to the canonical path still happens.
	w = do(s, http.MethodGet, "/dir", nil)
	if w.Code != http.StatusMovedPermanently && w.Code != http.StatusTemporaryRedirect {
		t.Errorf("redirect: %d", w.Code)
	}
}

func TestRecoverer(t *testing.T) {
	s, buf := newServer()
	w := do(s, http.MethodGet, "/panic", nil)
	if w.Code != 500 || strings.TrimSpace(w.Body.String()) != `{"detail":"Internal Server Error"}` {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	ls := lines(t, buf)
	if ls[0]["msg"] != "panic_in_handler" || ls[0]["panic"] != "kaboom" || ls[0]["request_id"] == nil || ls[1]["status"] != float64(500) {
		t.Errorf("lines = %v", ls)
	}
	defer func() {
		if rec, _ := recover().(error); !errors.Is(rec, http.ErrAbortHandler) {
			t.Errorf("ErrAbortHandler swallowed: %v", rec)
		}
	}()
	do(s, http.MethodGet, "/abort", nil)
}

func TestWriteJSONFallsBackOnAnUnencodableBody(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusOK, func() {})
	if w.Code != 500 || w.Body.String() != `{"detail":"Internal Server Error"}` {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}
