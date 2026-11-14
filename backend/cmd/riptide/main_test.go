package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/ship/kit/httpx"
)

// The probe waits out /readyz's whole check budget, and maps the answer to
// the exit code with the reason on stderr.
func TestHealthcheck(t *testing.T) {
	if probeTimeout <= httpx.CheckTimeout {
		t.Fatalf("probe timeout %s does not outlast the check budget %s", probeTimeout, httpx.CheckTimeout)
	}
	var status atomic.Int32
	status.Store(http.StatusOK)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			time.Sleep(3500 * time.Millisecond) // past kit's 3s client, inside the budget
		}
		if r.URL.Path == "/moved" {
			http.Redirect(w, r, "/readyz", http.StatusFound)
			return
		}
		w.WriteHeader(int(status.Load()))
	}))
	defer srv.Close()
	var stderr bytes.Buffer
	if code := healthcheck(srv.URL+"/readyz", &stderr); code != 0 || stderr.Len() != 0 {
		t.Errorf("2xx: code %d, stderr %q", code, stderr.String())
	}
	if code := healthcheck(srv.URL+"/slow", &stderr); code != 0 {
		t.Errorf("slow 2xx: code %d, stderr %q", code, stderr.String())
	}
	if code := healthcheck(srv.URL+"/moved", &stderr); code != 1 || !strings.Contains(stderr.String(), "302") {
		t.Errorf("redirect: code %d, stderr %q", code, stderr.String())
	}
	status.Store(http.StatusServiceUnavailable)
	stderr.Reset()
	if code := healthcheck(srv.URL+"/readyz", &stderr); code != 1 || !strings.Contains(stderr.String(), "503") {
		t.Errorf("503: code %d, stderr %q", code, stderr.String())
	}
	stderr.Reset()
	if code := healthcheck("http://127.0.0.1:1/readyz", &stderr); code != 1 || !strings.Contains(stderr.String(), "riptide: probe:") {
		t.Errorf("unreachable: code %d, stderr %q", code, stderr.String())
	}
}

// One listener failing stops the others: a UI listener that cannot bind must
// not leave the collector running alone, nor the reverse.
func TestServeAllStopsEveryServerWhenOneFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	free := httpx.NewServer("127.0.0.1:0", http.NotFoundHandler())
	taken := httpx.NewServer(ln.Addr().String(), http.NotFoundHandler()) // already bound: fails at once
	done := make(chan error, 1)
	go func() { done <- serveAll(context.Background(), free, taken) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "address already in use") {
			t.Errorf("err = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serveAll kept running after a listener failed")
	}
}

// A clean shutdown of the shared context stops every listener without error.
func TestServeAllStopsOnContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveAll(ctx, httpx.NewServer("127.0.0.1:0", http.NotFoundHandler()),
			httpx.NewServer("127.0.0.1:0", http.NotFoundHandler()))
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("err = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serveAll did not stop")
	}
}

// The self-probe always dials loopback: kit's probes 404 any other peer.
func TestProbeURL(t *testing.T) {
	for _, c := range []struct{ addr, want string }{
		{":8000", "http://127.0.0.1:8000/healthz"},
		{"0.0.0.0:8000", "http://127.0.0.1:8000/healthz"},
		{"[::]:8000", "http://127.0.0.1:8000/healthz"},
		{"10.1.2.3:8000", "http://127.0.0.1:8000/healthz"},
		{"[2001:db8::1]:9000", "http://127.0.0.1:9000/healthz"},
		{"127.0.0.1:8000", "http://127.0.0.1:8000/healthz"},
		{"[::1]:8000", "http://[::1]:8000/healthz"},
		{"localhost:8000", "http://localhost:8000/healthz"},
		{"garbage", "http://garbage:80/healthz"},
	} {
		if got := probeURL(c.addr, "/healthz"); got != c.want {
			t.Errorf("probeURL(%q) = %q, want %q", c.addr, got, c.want)
		}
	}
}
