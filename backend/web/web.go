// Package web serves the built UI out of the binary, on the UI listener.
package web

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/trick77/ship/kit/spa"
)

// The SPA is built into dist/ by `npm run build` (ui/vite.config.ts points
// its outDir here). A fresh clone has no build, so dist/ holds a tracked
// .gitkeep: `//go:embed all:dist` fails at compile time on a missing or
// empty directory, which would break `go build` without the UI build.
//
//go:embed all:dist
var distFS embed.FS

// placeholderHTML ships outside dist/: vite runs with emptyOutDir off so the
// tracked .gitkeep survives a build, and a placeholder inside dist/ would be
// overwritten by the first build.
//
//go:embed placeholder.html
var placeholderHTML []byte

// isRoute is the UI's one page: team, product and window live in the query
// string, sections are anchors. Anything else is a real 404, not the shell.
func isRoute(p string) bool { return p == "/" }

func dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err) // unreachable: the embed pattern guarantees the directory
	}
	return sub
}

// Handler serves the SPA, registered on "GET /" as the catch-all; the
// explicit /api routes win over it.
func Handler() http.Handler { return handler(dist()) }

// handler is kit's SPA server with the route allowlist: a missing /assets/
// file stays a 404, the shell is revalidated on every load, and an unbuilt
// binary answers its route with the placeholder and a 503.
func handler(sub fs.FS) http.Handler {
	return spa.Handler(sub, spa.Options{Routes: isRoute, Placeholder: placeholderHTML})
}
