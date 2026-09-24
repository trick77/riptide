// Package httpapi is the HTTP plumbing: the mux, the access log, panic
// recovery, FastAPI-shaped error bodies and graceful shutdown. net/http only,
// Go 1.22 method patterns on a ServeMux. Routes live in package api.
package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/trick77/riptide/internal/logging"
)

// Server holds the mux and the logger.
type Server struct {
	log *slog.Logger
	mux *http.ServeMux
}

// New builds an empty server.
func New(log *slog.Logger) *Server {
	return &Server{log: log, mux: http.NewServeMux()}
}

// HandleFunc registers a handler function on the mux.
func (s *Server) HandleFunc(pattern string, h http.HandlerFunc) { s.mux.HandleFunc(pattern, h) }

// Handler is the mux wrapped in the middleware chain: request id and access
// log outermost, so a panic's log line and its 500 carry the request id.
func (s *Server) Handler() http.Handler {
	return s.accessLog(s.recoverer(http.HandlerFunc(s.route)))
}

// route dispatches through the mux, but answers the mux's own 404 and 405
// in the {"detail": ...} shape every other error uses.
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
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

// silent holds the probe paths: polled every few seconds, they would bury
// real traffic. A trailing slash is silenced too.
func silent(path string) bool {
	p := strings.TrimRight(path, "/")
	return p == "/health" || p == "/ready"
}

// requestIDRE bounds a caller-supplied X-Request-Id: untrusted input must not
// become an indexed field (newlines, huge strings).
var requestIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// accessLog binds request_id, method and path into the context, so every line
// of the request carries them, echoes the request id back, and emits one
// `http_request` line with status_code and duration_ms. Probes pass through
// unobserved.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if silent(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		id := r.Header.Get("X-Request-Id")
		if !requestIDRE.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := logging.With(r.Context(), "request_id", id, "method", r.Method, "path", r.URL.Path)
		sw := &statusWriter{ResponseWriter: w}
		started := time.Now()
		defer func() {
			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			ms := float64(time.Since(started).Microseconds()) / 1000
			s.log.InfoContext(ctx, "http_request", "status_code", status, "duration_ms", float64(int(ms*10+0.5))/10)
		}()
		next.ServeHTTP(sw, r.WithContext(ctx))
	})
}

// recoverer turns a panic into a 500 with the stack logged, so one bad
// request never takes the replica down.
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

// Run serves until ctx is cancelled, then drains for up to 15 seconds.
func Run(ctx context.Context, addr string, h http.Handler, log *slog.Logger) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
