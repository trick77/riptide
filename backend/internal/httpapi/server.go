// Package httpapi is the HTTP plumbing: the mux with kit's probes, kit's
// access log, panic recovery and FastAPI-shaped error bodies. net/http only,
// Go 1.22 method patterns on a ServeMux. Routes live in package api.
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"

	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/ship/kit/logging"
)

// Server holds the mux and the logger.
type Server struct {
	log *slog.Logger
	mux *http.ServeMux
}

// New builds a server with kit's probes mounted: /healthz is liveness,
// /readyz runs checks in order. Both answer loopback callers only.
func New(log *slog.Logger, checks ...httpx.Check) *Server {
	s := &Server{log: log, mux: http.NewServeMux()}
	httpx.Health(s.mux, log, checks...)
	return s
}

// NewUI builds a server without the probes, for the UI listener: the
// collector's listener answers the orchestrator, this one only the UI's
// requests. Same middleware chain and error shape.
func NewUI(log *slog.Logger) *Server {
	return &Server{log: log, mux: http.NewServeMux()}
}

// HandleFunc registers a handler function on the mux.
func (s *Server) HandleFunc(pattern string, h http.HandlerFunc) { s.mux.HandleFunc(pattern, h) }

// Handle registers a handler on the mux.
func (s *Server) Handle(pattern string, h http.Handler) { s.mux.Handle(pattern, h) }

// Handler is the mux wrapped in the middleware chain: the access log
// outermost, so a panic's log line and its 500 carry the request id.
// Senders' X-Request-Id is adopted (TrustRequestID) so one id follows a
// delivery from the sender's log into ours, and every request logs at info:
// webhook traffic is what an operator reads.
func (s *Server) Handler() http.Handler {
	return httpx.Chain(http.HandlerFunc(s.route),
		httpx.AccessLog(s.log, httpx.AccessLogOptions{TrustRequestID: true, RoutineLevel: slog.LevelInfo}),
		bindRequest,
		s.recoverer,
	)
}

// bindRequest puts method and path on every line a handler logs, not only
// on the access line.
func bindRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := logging.With(r.Context(), "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// route dispatches through the mux, but answers the mux's own 404 and 405
// in the {"detail": ...} shape every other error uses. kit's probes refuse
// anyone but a direct local caller with a text/plain 404 (and the mux a
// non-GET with a 405 naming GET): such a caller gets the JSON 404 of a path
// that does not exist, for any method, before the mux.
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	if probePaths[r.URL.Path] && !isLocal(r) {
		WriteDetail(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))
		return
	}
	h, pattern := s.mux.Handler(r)
	if pattern != "" {
		s.mux.ServeHTTP(w, r)
		return
	}
	probe := &probeWriter{header: http.Header{}}
	h.ServeHTTP(probe, r)
	switch probe.status {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		if allow := probe.header.Get("Allow"); allow != "" {
			w.Header().Set("Allow", allow)
		}
		WriteDetail(w, probe.status, http.StatusText(probe.status))
	default:
		// A redirect to the canonical path: let the mux answer it itself.
		h.ServeHTTP(w, r)
	}
}

// probePaths are where httpx.Health mounts the probes.
var probePaths = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range httpx.DefaultProbePaths() {
		m[p] = true
	}
	return m
}()

// isLocal mirrors kit's probe gate: a loopback peer and no forwarding
// header (a sidecar proxy connects from loopback too).
func isLocal(r *http.Request) bool {
	for _, h := range []string{"X-Forwarded-For", "Forwarded", "X-Real-Ip"} {
		if _, ok := r.Header[h]; ok {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}

// probeWriter records what the mux's fallback handler would answer.
type probeWriter struct {
	header http.Header
	status int
}

func (p *probeWriter) Header() http.Header { return p.header }
func (p *probeWriter) WriteHeader(code int) {
	if p.status == 0 {
		p.status = code
	}
}
func (p *probeWriter) Write(b []byte) (int, error) {
	if p.status == 0 {
		p.status = http.StatusOK
	}
	return len(b), nil
}

// WriteJSON renders a JSON body with the status. Bodies are structs, not
// maps, so the key order is stable. Like FastAPI's responses: no trailing
// newline, and <, > and & left unescaped.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	b := []byte(`{"detail":"Internal Server Error"}`)
	if err := enc.Encode(body); err == nil {
		b = bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	} else {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// Detail is FastAPI's error shape.
type Detail struct {
	Detail any `json:"detail"`
}

// WriteDetail writes {"detail": "..."}.
func WriteDetail(w http.ResponseWriter, status int, detail string) {
	WriteJSON(w, status, Detail{Detail: detail})
}

// recoverer turns a panic into a 500 with the stack logged, so one bad
// request never takes the replica down. Not kit's Recover: its 500 body is
// {"error": ...}, and senders read {"detail": ...}.
func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				err, ok := rec.(error)
				if ok && errors.Is(err, http.ErrAbortHandler) {
					panic(rec)
				}
				s.log.ErrorContext(r.Context(), "panic_in_handler", "panic", rec, "stack", string(debug.Stack()))
				WriteDetail(w, http.StatusInternalServerError, "Internal Server Error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
