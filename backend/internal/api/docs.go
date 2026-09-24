package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	spec "github.com/trick77/riptide/api"
)

// specETag is the spec's hash, computed once: the bytes are embedded in the
// binary and cannot change while it runs.
var specETag = func() string {
	sum := sha256.Sum256(spec.OpenAPISpec)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}()

// openapiSpec serves the contract itself.
func (d Deps) openapiSpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("ETag", specETag)
	w.Header().Set("Cache-Control", "public, max-age=300")
	if matchesETag(r.Header.Get("If-None-Match"), specETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(spec.OpenAPISpec)
}

// matchesETag reports whether an If-None-Match header covers the tag: "*",
// a comma-separated list, or a weak "W/" prefix.
func matchesETag(header, tag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == tag {
			return true
		}
	}
	return false
}

// docsHTML is Swagger UI, loaded from a CDN and pointed at the spec route.
// The script is third-party and runs on this origin, and "Try it out" is on:
// a bearer typed into that panel is readable by it. That token is the
// caller's own team key, so the page is served anyway, on the same reasoning
// as noergler's.
const docsHTML = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>riptide-collector — webhook API</title>
    <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css" />
  </head>
  <body>
    <div id="app"></div>
    <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
    <script>
      window.ui = SwaggerUIBundle({ url: '/openapi.yaml', dom_id: '#app' })
    </script>
  </body>
</html>
`

// docs serves the API reference page.
func (d Deps) docs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write([]byte(docsHTML))
}
