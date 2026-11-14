package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func built() fstest.MapFS {
	return fstest.MapFS{
		"index.html":          {Data: []byte("<!doctype html><div id=root>")},
		"assets/index-abc.js": {Data: []byte("console.log(1)")},
	}
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// The one page gets the shell; any other path is a real 404, so a typo is
// not a blank page.
func TestOnlyTheRootGetsTheShell(t *testing.T) {
	h := handler(built())
	if w := get(h, "/"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "id=root") {
		t.Errorf("/: %d %q", w.Code, w.Body.String())
	}
	for _, path := range []string{"/nope", "/teams/x", "/assets/missing.js", "/index.html/x"} {
		if code := get(h, path).Code; code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, code)
		}
	}
	if w := get(h, "/assets/index-abc.js"); w.Code != http.StatusOK || w.Body.String() != "console.log(1)" {
		t.Errorf("asset: %d %q", w.Code, w.Body.String())
	}
}

// An unbuilt dist answers with the placeholder and a 503, never a blank 200.
func TestUnbuiltServesThePlaceholder(t *testing.T) {
	w := get(handler(fstest.MapFS{".gitkeep": {}}), "/")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "make fe-build") {
		t.Errorf("unbuilt: %d %q", w.Code, w.Body.String())
	}
	if code := get(Handler(), "/").Code; code != http.StatusOK && code != http.StatusServiceUnavailable {
		t.Errorf("embedded: %d", code)
	}
}
