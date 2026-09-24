// Package buildinfo carries the version stamped into the binary.
package buildinfo

import "os"

// Set at link time:
// -ldflags "-X github.com/trick77/riptide/internal/buildinfo.version=1.2.3".
var (
	version string
	commit  string
)

// Version is the release the binary runs as. OPENSHIFT_BUILD_COMMIT wins:
// a cluster-side BuildConfig sets it and it is the source of truth there.
// Then the ldflags value, then RIPTIDE_VERSION (the image's fallback), then
// "dev".
func Version() string {
	if v := os.Getenv("OPENSHIFT_BUILD_COMMIT"); v != "" {
		return v
	}
	if version != "" {
		return version
	}
	if v := os.Getenv("RIPTIDE_VERSION"); v != "" {
		return v
	}
	return "dev"
}

// Commit is the short git hash, empty when not stamped.
func Commit() string { return commit }
