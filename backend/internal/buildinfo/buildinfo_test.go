package buildinfo

import "testing"

func TestVersionPrecedence(t *testing.T) {
	t.Setenv("OPENSHIFT_BUILD_COMMIT", "")
	t.Setenv("RIPTIDE_VERSION", "")
	version, commit = "", ""
	if Version() != "dev" || Commit() != "" {
		t.Errorf("default = %q", Version())
	}
	t.Setenv("RIPTIDE_VERSION", "1.2.3")
	if Version() != "1.2.3" {
		t.Errorf("env = %q", Version())
	}
	version = "2.0.0"
	defer func() { version = "" }()
	if Version() != "2.0.0" {
		t.Errorf("ldflags = %q", Version())
	}
	t.Setenv("OPENSHIFT_BUILD_COMMIT", "abc123")
	if Version() != "abc123" {
		t.Errorf("openshift = %q", Version())
	}
}
