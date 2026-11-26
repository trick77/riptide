package metrics

import (
	"testing"

	"github.com/trick77/riptide/internal/dataset"
)

// The lead time's base and coverage's on production are the same PRs: one
// shipped without a start (opened before Bitbucket reported, no pushed
// commit) is counted as unmeasured, never dropped silently.
func TestLeadTimeCountsTheUnmeasured(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Traces = map[string]dataset.RepoTrace{"kasse/kasse-api": {DeployedSince: agoP(50)}}
		ds.PRs = []dataset.PR{
			shipped(3600, func(p *dataset.PR) { p.PRID = 1 }),
			shipped(7200, func(p *dataset.PR) { p.PRID = 2 }),
			shipped(0, func(p *dataset.PR) { p.PRID, p.FirstCommitAt, p.OpenedAt = 3, nil, nil }),
		}
	})
	m := Compute(ds, noPrevious(ds), nil, all())
	l := m.Delivery.LeadTime
	if l.Human.N != 2 || l.Unmeasured != 1 || l.Human.N+l.Unmeasured != m.Coverage.OnProd {
		t.Errorf("lead time %+v, on prod %d", l, m.Coverage.OnProd)
	}
}

// A period whose lead times rest on lower bounds for more than a fifth of
// its PRs reads long by what tracing missed: no change is judged on it.
func TestLeadChangeLowerBounds(t *testing.T) {
	lead := func(p50 float64, n, lower int) LeadTimes {
		return LeadTimes{Human: Spread{N: n, P50: &p50}, LowerBound: lower}
	}
	for _, c := range []struct {
		name      string
		cur, prev LeadTimes
		judged    bool
	}{
		{"exact on both sides", lead(4e5, 12, 0), lead(2e5, 12, 0), true},
		{"a few lower bounds", lead(4e5, 12, 2), lead(2e5, 12, 1), true},
		{"most lower bounds now", lead(4e5, 12, 7), lead(2e5, 12, 0), false},
		{"most lower bounds before", lead(4e5, 12, 0), lead(2e5, 12, 9), false},
	} {
		got := leadChange(c.cur, c.prev, false)
		if judged := got.Reason == nil; judged != c.judged {
			t.Errorf("%s: change %+v, reason %v", c.name, got, str(got.Reason))
		}
		if !c.judged && got.Reason != nil && *got.Reason != ReasonIncomplete {
			t.Errorf("%s: reason %s", c.name, *got.Reason)
		}
	}
}
