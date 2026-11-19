package dataset

import (
	"strings"
	"testing"
)

// Every placeholder is filled and every fragment is written into a query: a
// typo in either name would otherwise ship a query that fails, or one that
// silently lost its shared part, only against Postgres. Fragments nest no
// placeholder and hold no -- comment (see expand).
func TestFragmentsExpanded(t *testing.T) {
	for name, q := range queries {
		if i := strings.Index(q, "{{"); i >= 0 {
			t.Errorf("%s: placeholder left: %.80q", name, q[i:])
		}
	}
	raw := readSQL("sql/*.sql")
	frags := fragments()
	if len(frags) == 0 {
		t.Fatal("no fragments embedded")
	}
	for name, body := range frags {
		if strings.Contains(body, "/*{{") {
			t.Errorf("%s nests a placeholder", name)
		}
		if strings.Contains(body, "--") {
			t.Errorf("%s holds a -- comment", name)
		}
		used := false
		for _, q := range raw {
			used = used || strings.Contains(q, placeholder(name))
		}
		if !used {
			t.Errorf("%s is in no query", name)
		}
	}
}
