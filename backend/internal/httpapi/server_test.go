package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/logging"
)

func newServer() (*Server, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	s := New(slog.New(logging.NewHandler(buf, slog.LevelInfo, "test")))
	s.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { WriteJSON(w, 200, map[string]string{"status": "ok"}) })
	s.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	s.HandleFunc("POST /things", func(w http.ResponseWriter, r *http.Request) {
		slog.New(logging.NewHandler(buf, slog.LevelInfo, "test")).InfoContext(r.Context(), "inside")
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
	if len(ls) != 2 || ls[0]["msg"] != "inside" || ls[0]["request_id"] != "test-rid-abc" {
		t.Fatalf("lines = %v", ls)
	}
	ev := ls[1]
	if ev["msg"] != "http_request" || ev["request_id"] != "test-rid-abc" || ev["path"] != "/things" || ev["method"] != "POST" ||
		ev["status_code"] != float64(418) {
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
		if ev := lines(t, buf)[0]; ev["request_id"] != got || ev["status_code"] != float64(200) {
			t.Errorf("line = %v", ev)
		}
	}
	buf.Reset()
	do(s, http.MethodGet, "/write", nil)
	if lines(t, buf)[0]["status_code"] != float64(200) {
		t.Error("write without WriteHeader not logged as 200")
	}
}

func TestProbesAreNotLogged(t *testing.T) {
	s, buf := newServer()
	for _, p := range []string{"/health", "/ready", "/health/", "/ready/"} {
		do(s, http.MethodGet, p, nil)
	}
	if buf.Len() != 0 {
		t.Errorf("probe logged: %s", buf.String())
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
	if ls[0]["msg"] != "panic_in_handler" || ls[0]["panic"] != "kaboom" || ls[1]["status_code"] != float64(500) {
		t.Errorf("lines = %v", ls)
	}
	defer func() {
		if rec, _ := recover().(error); !errors.Is(rec, http.ErrAbortHandler) {
			t.Errorf("ErrAbortHandler swallowed: %v", rec)
		}
	}()
	do(s, http.MethodGet, "/abort", nil)
}

func TestRunServesAndShutsDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	s, _ := newServer()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, addr, s.Handler(), slog.New(slog.DiscardHandler)) }()
	var resp *http.Response
	for i := 0; i < 50; i++ {
		resp, err = http.Get("http://" + addr + "/health") //nolint:noctx // test
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := Run(context.Background(), "bad-address", s.Handler(), slog.New(slog.DiscardHandler)); err == nil {
		t.Error("bad address accepted")
	}
}
