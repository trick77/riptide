package metrics

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
				p.PRID, p.Project, p.Repo, p.MergedAt, p.ChangeType = 4, "portal", "portal/portal-web", agoP(2), s("other")
				p.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: ago(1)}}
			}),
			// approved before it was ready: the wait is unknown, not negative
			pr(func(p *dataset.PR) {
				p.PRID, p.OpenedAt, p.ReadyAt, p.ApprovedAt, p.MergedAt = 5, agoP(5), agoP(2), agoP(3), agoP(4)
				p.GitOps = true
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
		runs = append(runs, r.RunID+":"+r.Kind+":"+r.Verdict)
	}
	if fmt.Sprint(runs) != "[3:main:success 2:main:failure 1:main:success 4:main:success]" {
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
		{"anderes", 1, 0, 0},   // the UI's word for change type "other"
		{"Promotion", 1, 0, 0}, // a GitOps PR's Typ
		{"Feature", 2, 0, 0},   // the Typ column's word; the promotion's hidden type is not searched
		{"master builds", 0, 0, 4},
		{"master-builds", 0, 0, 4}, // the Art column's word, either language
		// both UI languages, whichever the viewer reads
		{"merged", 3, 0, 0},
		{"gemerged", 3, 0, 0},
		{"gemergt", 3, 0, 0}, // the word before, for links shared back then
		{"first deploy", 0, 1, 0},
		{"Other", 1, 0, 0},
		{"fehlgeschlagen", 0, 0, 1}, // FAILURE in the UI's words
		{"erfolgreich", 0, 0, 3},    // SUCCESS, the fixture's default
		{"portal-web:9", 0, 1, 0},
		{"fail", 0, 1, 1},  // the failed sync's phase, the broken run
		{"BUILD", 0, 0, 4}, // build_type and the Art column's "master builds", case-insensitive
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

// Only a status that plainly says succeeded or failed gets the UI's word;
// UNSTABLE is red but keeps the CI's word.
func TestRunOutcome(t *testing.T) {
	for status, want := range map[string]string{
		"SUCCESS": "succeeded", "Succeeded": "succeeded", "Completed": "succeeded",
		"FAILURE": "failed", "Failed": "failed",
		"UNSTABLE": "", "ABORTED": "", "NOT_BUILT": "", "None": "",
	} {
		got := ""
		if o := runOutcome(run(func(r *dataset.Run) { r.Status = s(status) })); o != nil {
			got = *o
		}
		if got != want {
			t.Errorf("%s: %q, want %q", status, got, want)
		}
	}
	if runOutcome(run(func(r *dataset.Run) { r.Status = nil })) != nil {
		t.Error("no status has an outcome")
	}
}

// A search for the pill's words finds the run, whichever words they are.
func TestRunStatusTerms(t *testing.T) {
	for status, want := range map[string]string{
		"SUCCESS":   "Erfolgreich Succeeded",
		"FAILURE":   "Fehlgeschlagen Failed",
		"UNSTABLE":  "UNSTABLE",
		"NOT_BUILT": "NOT BUILT",
	} {
		if got := runStatusTerms(run(func(r *dataset.Run) { r.Status = s(status) })); got != want {
			t.Errorf("%s: %q, want %q", status, got, want)
		}
	}
	if got := runStatusTerms(run(func(r *dataset.Run) { r.Status = nil })); got != "" {
		t.Errorf("no status: %q", got)
	}
}

// uiTerms is ui/src/i18n/terms.json in one language.
type uiTerms struct {
	PRState     map[string]string `json:"prState"`
	PRWaiting   map[string]string `json:"prWaiting"`
	DeployClass map[string]string `json:"deployClass"`
	ChangeType  map[string]string `json:"changeType"`
	Promotion   string            `json:"promotion"`
	RunOutcome  map[string]string `json:"runOutcome"`
	RunKind     map[string]string `json:"runKind"`
}

// The backend searches the words the UI shows, in both languages: a label
// reworded in one place only would make a search for what is on screen
// find nothing.
func TestSearchTermsMatchTheUI(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "ui", "src", "i18n", "terms.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ui map[string]uiTerms
	if err := json.Unmarshal(raw, &ui); err != nil {
		t.Fatal(err)
	}
	// A group the struct does not name would be dropped by Unmarshal and
	// never compared: the file's groups must be exactly these.
	var groups map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil {
		t.Fatal(err)
	}
	for code, g := range groups {
		got := slices.Sorted(maps.Keys(g))
		if want := []string{"changeType", "deployClass", "prState", "prWaiting", "promotion", "runKind", "runOutcome"}; !slices.Equal(got, want) {
			t.Errorf("terms.json %s groups %v, compared %v", code, got, want)
		}
	}
	if len(groups) != 2 {
		t.Errorf("terms.json languages %d, want de and en", len(groups))
	}
	lang := func(m map[string]uiTerm, de bool) map[string]string {
		out := map[string]string{}
		for k, v := range m {
			out[k] = map[bool]string{true: v.De, false: v.En}[de]
		}
		return out
	}
	for code, de := range map[string]bool{"de": true, "en": false} {
		u, ok := ui[code]
		if !ok {
			t.Fatalf("terms.json has no %q", code)
		}
		for name, c := range map[string][2]map[string]string{
			"prState":     {u.PRState, lang(prStateTerm, de)},
			"prWaiting":   {u.PRWaiting, lang(waitTerm, de)},
			"deployClass": {u.DeployClass, lang(deployTerm, de)},
			"changeType":  {u.ChangeType, lang(changeTypeTerm, de)},
			"runOutcome":  {u.RunOutcome, lang(runOutcomeTerm, de)},
			"runKind":     {u.RunKind, lang(runKindTerm, de)},
		} {
			if !maps.Equal(c[0], c[1]) {
				t.Errorf("%s %s: ui %v, backend %v", code, name, c[0], c[1])
			}
		}
		if want := map[bool]string{true: promotionTerm.De, false: promotionTerm.En}[de]; u.Promotion != want {
			t.Errorf("%s promotion: ui %q, backend %q", code, u.Promotion, want)
		}
	}
}

func TestJiraPrefix(t *testing.T) {
	for _, c := range []struct {
		title *string
		keys  []string
		want  string
		more  int
	}{
		{s("KAS-1 Pay by card"), []string{"KAS-1"}, "", 0},
		{s("Pay by card"), []string{"KAS-1", "KAS-2"}, "KAS-1 KAS-2", 0},
		{nil, []string{"A-1", "A-2", "A-3", "A-4", "A-5"}, "A-1 A-2 A-3", 2},
		// KAS-12 in the title does not show KAS-1.
		{s("KAS-12 Pay by card"), []string{"KAS-12", "KAS-1"}, "KAS-1", 0},
	} {
		refs, more := jiraPrefix(c.keys, titleParts(c.title, ""), "")
		var keys []string
		for _, r := range refs {
			keys = append(keys, r.Key)
			if r.URL != nil {
				t.Errorf("%v: url %q without a base URL", c.keys, *r.URL)
			}
		}
		if got := strings.Join(keys, " "); got != c.want || more != c.more {
			t.Errorf("%v: %q +%d, want %q +%d", c.keys, got, more, c.want, c.more)
		}
	}
	refs, _ := jiraPrefix([]string{"KAS-1"}, titleParts(s("Pay"), ""), "https://jira.example.com")
	if len(refs) != 1 || refs[0].URL == nil || *refs[0].URL != "https://jira.example.com/browse/KAS-1" {
		t.Errorf("linked prefix = %+v", refs)
	}
}

func TestTitleParts(t *testing.T) {
	const base = "https://jira.example.com"
	type part struct{ text, url string }
	flat := func(ps []TitlePart) []part {
		out := []part{}
		for _, p := range ps {
			switch {
			case p.Jira == nil:
				out = append(out, part{p.Text, ""})
			case p.Jira.URL == nil:
				out = append(out, part{p.Text, "-"})
			default:
				out = append(out, part{p.Text, *p.Jira.URL})
			}
		}
		return out
	}
	for _, c := range []struct {
		title *string
		base  string
		want  []part
	}{
		{nil, base, []part{}},
		{s("Pay by card"), base, []part{{"Pay by card", ""}}},
		{s("SEP-25360: Smoke"), base, []part{{"SEP-25360", base + "/browse/SEP-25360"}, {": Smoke", ""}}},
		{s("Fix SEP-1 and SEP-2"), base, []part{{"Fix ", ""}, {"SEP-1", base + "/browse/SEP-1"},
			{" and ", ""}, {"SEP-2", base + "/browse/SEP-2"}}},
		{s("aSEP-1 SEP-1x"), base, []part{{"aSEP-1 SEP-1x", ""}}},
		{s("SEP-1"), "", []part{{"SEP-1", "-"}}},
	} {
		if got := flat(titleParts(c.title, c.base)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v: %v, want %v", c.title, got, c.want)
		}
	}
}
