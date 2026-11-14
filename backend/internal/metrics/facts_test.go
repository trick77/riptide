package metrics

import (
	"fmt"
	"testing"

	"github.com/trick77/riptide/internal/dataset"
)

func factsData() *dataset.Dataset {
	return data(func(ds *dataset.Dataset) {
		ds.PRs = []dataset.PR{
			pr(func(p *dataset.PR) { p.PRID, p.MergedAt = 1, agoP(3) }),
			// open, so ordered by its opening: newest
			pr(func(p *dataset.PR) { p.PRID, p.State, p.MergedAt, p.OpenedAt = 2, "open", nil, agoP(0.5) }),
			pr(func(p *dataset.PR) { p.PRID, p.Bot, p.MergedAt = 3, true, agoP(1) }),
			pr(func(p *dataset.PR) {
				p.PRID, p.Project, p.Repo, p.MergedAt = 4, "portal", "portal/portal-web", agoP(2)
				p.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: ago(1)}}
			}),
			// approved before it was ready: the wait is unknown, not negative
			pr(func(p *dataset.PR) {
				p.PRID, p.OpenedAt, p.ReadyAt, p.ApprovedAt, p.MergedAt = 5, agoP(5), agoP(2), agoP(3), agoP(4)
			}),
		}
		ds.Deploys = []dataset.Deploy{
			deploy(func(d *dataset.Deploy) { d.Revision, d.At = "r1", ago(1) }),
			deploy(func(d *dataset.Deploy) { d.Revision, d.At, d.Kind = "r2", ago(2), s("first_seen") }),
			deploy(func(d *dataset.Deploy) { d.Revision, d.At, d.Kind, d.NewImages = "r3", ago(3), s("sync"), []string{} }),
			deploy(func(d *dataset.Deploy) { d.Revision, d.At, d.Phase, d.Kind = "r4", ago(0.5), "Failed", nil }),
			// a re-sync of an image is found by the image
			deploy(func(d *dataset.Deploy) {
				d.Revision, d.At, d.Kind, d.Images, d.NewImages = "r5", ago(4), s("sync"), []string{"reg/portal-web:9"}, []string{}
			}),
		}
		ds.Runs = []dataset.Run{
			run(func(r *dataset.Run) { r.RunID, r.FinishedAt = "1", agoP(2) }),
			run(func(r *dataset.Run) { r.RunID, r.FinishedAt, r.Status = "2", agoP(1), s("FAILURE") }),
			// unfinished runs order by their start
			run(func(r *dataset.Run) { r.RunID, r.FinishedAt, r.StartedAt = "3", nil, agoP(0.1) }),
			run(func(r *dataset.Run) { r.RunID, r.FinishedAt, r.StartedAt, r.BuildType = "4", nil, nil, nil }),
		}
	})
}

func prIDs(rows []PRFact) []int64 {
	var out []int64
	for _, r := range rows {
		out = append(out, r.PRID)
	}
	return out
}

func TestFacts(t *testing.T) {
	ds := factsData()
	f := ComputeFacts(ds, all(), "")
	if f.ProductionStage != "prod" || f.Limit != FactsLimit {
		t.Errorf("facts = %+v", f)
	}
	// Newest first, bots only on request, no period filter.
	if got := prIDs(f.PRs.Rows); fmt.Sprint(got) != "[2 4 1 5]" || f.PRs.Count != 4 {
		t.Errorf("prs = %v", got)
	}
	if got := prIDs(ComputeFacts(ds, Params{Period: p4(), Bots: true}, "").PRs.Rows); fmt.Sprint(got) != "[2 3 4 1 5]" {
		t.Errorf("prs with bots = %v", got)
	}
	portal := f.PRs.Rows[1]
	if portal.ProdAt == nil || portal.ToProdS == nil || *portal.ToProdS != 86400 || portal.State != "merged" {
		t.Errorf("portal pr = %+v", portal)
	}
	if r := f.PRs.Rows[3]; r.ReviewS != nil || r.CycleS == nil {
		t.Errorf("negative wait = %+v", r)
	}
	if r := f.PRs.Rows[0]; r.MergedAt != nil || r.ToProdS != nil || r.State != "open" {
		t.Errorf("open pr = %+v", r)
	}

	var classes []string
	for _, d := range f.Deploys.Rows {
		classes = append(classes, d.Class+":"+d.Phase)
	}
	if fmt.Sprint(classes) != "[failed:Failed release:Succeeded first_seen:Succeeded sync:Succeeded sync:Succeeded]" {
		t.Errorf("deploys = %v", classes)
	}

	var runs []string
	for _, r := range f.Runs.Rows {
		runs = append(runs, r.RunID+":"+r.Kind)
	}
	if fmt.Sprint(runs) != "[3:main 2:main 1:main 4:main]" {
		t.Errorf("runs = %v", runs)
	}
}

func TestFactsSearch(t *testing.T) {
	ds := factsData()
	for _, c := range []struct {
		q                   string
		prs, deploys, nruns int
	}{
		{"  ", 4, 5, 4},
		{"KAS-1", 4, 0, 0},
		{"#4", 1, 0, 0},
		{"offen", 1, 0, 0},
		{"erstdeploy", 0, 1, 0},
		{"portal-web:9", 0, 1, 0},
		{"fail", 0, 1, 1},  // the failed sync's phase, the broken run
		{"BUILD", 0, 0, 3}, // build_type, case-insensitive
		{"nothing like it", 0, 0, 0},
	} {
		f := ComputeFacts(ds, all(), c.q)
		if f.PRs.Count != c.prs || f.Deploys.Count != c.deploys || f.Runs.Count != c.nruns {
			t.Errorf("%q: %d/%d/%d", c.q, f.PRs.Count, f.Deploys.Count, f.Runs.Count)
		}
	}
	// The product filter applies to every table, as given: one the facts
	// do not know matches nothing (the UI sends the one metrics resolved).
	f := ComputeFacts(ds, product("portal"), "")
	if f.PRs.Count != 1 || f.Deploys.Count != 0 || f.Runs.Count != 0 {
		t.Errorf("portal: %d/%d/%d", f.PRs.Count, f.Deploys.Count, f.Runs.Count)
	}
	if f := ComputeFacts(ds, product("nope"), ""); f.PRs.Count+f.Deploys.Count+f.Runs.Count != 0 {
		t.Errorf("unknown product matched: %+v", f)
	}
}

func TestFactsLimit(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		for i := range FactsLimit + 5 {
			ds.Runs = append(ds.Runs, run(func(r *dataset.Run) { r.RunID = fmt.Sprint(i) }))
		}
	})
	f := ComputeFacts(ds, all(), "")
	if f.Runs.Count != FactsLimit+5 || len(f.Runs.Rows) != FactsLimit {
		t.Errorf("runs %d, rows %d", f.Runs.Count, len(f.Runs.Rows))
	}
}

func TestJiraPrefix(t *testing.T) {
	for _, c := range []struct {
		title *string
		keys  []string
		want  string
	}{
		{s("KAS-1 Pay by card"), []string{"KAS-1"}, ""},
		{s("Pay by card"), []string{"KAS-1", "KAS-2"}, "KAS-1 KAS-2"},
		{nil, []string{"A-1", "A-2", "A-3", "A-4", "A-5"}, "A-1 A-2 A-3 +2"},
	} {
		if got := jiraPrefix(pr(func(p *dataset.PR) { p.Title, p.JiraKeys = c.title, c.keys })); got != c.want {
			t.Errorf("%v: %q, want %q", c.keys, got, c.want)
		}
	}
}
