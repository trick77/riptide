package testdb

import (
	"fmt"
	"strings"
	"testing"
)

// fatalTB records Fatalf and stops DSN there, as a real test would.
type fatalTB struct {
	testing.TB
	fatal string
}

func (f *fatalTB) Helper() {}

func (f *fatalTB) Fatalf(format string, a ...any) {
	f.fatal = fmt.Sprintf(format, a...)
	panic(f)
}

// The pre-rename variable fails loudly instead of skipping every DB test.
func TestOldDSNNameFails(t *testing.T) {
	t.Setenv("SHIP_TEST_DSN", "")
	t.Setenv("RIPTIDE_TEST_DSN", "postgres://x/y")
	f := &fatalTB{TB: t}
	defer func() {
		if recover() != f || !strings.Contains(f.fatal, "renamed to SHIP_TEST_DSN") {
			t.Errorf("fatal = %q", f.fatal)
		}
	}()
	DSN(f)
}
