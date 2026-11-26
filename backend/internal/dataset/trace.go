package dataset

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Tracing a merged PR to the deploys that shipped it (docs/dora-lead-time.md):
// a deploy ships a stretch of its repos' trunks, (From, To] in push time,
// and a PR is on an environment from the first deploy there whose stretch
// holds its arrival on the trunk (PR.TrunkAt). Read once per page request
// (LoadShared), over all history: a deploy's stretch depends on the deploys
// and pushes before it, never on a window.
//
// A deploy's stretch of a repo comes from the first of these that resolves:
//
//  1. compare links in its GitOps release commit: the new end where it
//     reached the trunk; the old end likewise, or the tip before the
//     repo's first push riptide holds (its fromHash), both exact; an old
//     end no payload names predates the data: everything since the
//     repo's first trunk push, a lower bound
//  2. its images: a deployed reference equal to one a CI run published
//     (docs/correlating-deploys-to-commits.md); the run's commit is the
//     head, placed where it reached the trunk, but never after the run
//     started (a UI job pushes its own release commit minutes into the
//     run: what landed meanwhile is not in it); the newest head of the
//     image repository's tags before, in this App and environment, is the
//     old end (exact); without one the deploy that ran them (a lower bound:
//     nothing pushed after it was in them); an App's first deploy of the
//     image repository starts after what another App's deploy of it
//     shipped, else at the repo's first trunk push (lower bound).
//
// Neither resolving, the deploy is unresolved for the repo. A stretch's end
// never lies after the deploy: a commit pushed after it was not in it.

// Routes a stretch was found by.
const (
	RouteCompare = "compare"
	RouteImage   = "image"
)

// compareLink is docs/correlating-deploys-to-commits.md's fallback, a
// release commit's link from the previous to the new App-repo commit. The
// project key, when the link names one, tells two repos of one slug apart.
var compareLink = regexp.MustCompile(
	`(?:projects/([A-Za-z0-9_.~-]+)/)?repos/([a-z0-9._-]+)/compare/diff\?targetBranch=([0-9a-f]{40})&sourceBranch=([0-9a-f]{40})`)

// traceDeploy is a row of sql/trace_deploys.sql.
type traceDeploy struct {
	ID          int64     `db:"id"`
	App         string    `db:"app"`
	Environment string    `db:"environment"`
	Revision    string    `db:"revision"`
	At          time.Time `db:"at"`
	Images      []string  `db:"images"`
}

// traceBody is a row of sql/trace_releases.sql.
type traceBody struct {
	Revision string `db:"revision"`
	Body     string `db:"body"`
}

// traceRun is a row of sql/trace_runs.sql.
type traceRun struct {
	ID        int64     `db:"id"`
	CommitSHA string    `db:"commit_sha"`
	StartedAt time.Time `db:"started_at"`
	ImageRefs []string  `db:"image_refs"`
	Repo      *string   `db:"repo"`
}

// tracePosition is a row of sql/trace_positions.sql.
type tracePosition struct {
	Kind   string    `db:"kind"`
	SHA    *string   `db:"sha"`
	Repo   string    `db:"repo"`
	Branch string    `db:"branch"`
	At     time.Time `db:"at"`
}

// traceInput is what the tracing reads; buildTrace makes stretches of it.
type traceInput struct {
	deploys   []traceDeploy
	bodies    []traceBody
	runs      []traceRun
	positions []tracePosition
}

// loadTrace reads the team's tracing input: its deploys, release commits
// and image runs, then where the SHAs they name reached the trunk.
func loadTrace(ctx context.Context, tx pgx.Tx, team string) (*traceInput, error) {
	in := &traceInput{}
	var err error
	if in.deploys, err = all[traceDeploy](ctx, tx, "trace_deploys", traceDeploysSQL, team); err != nil {
		return nil, err
	}
	if in.bodies, err = all[traceBody](ctx, tx, "trace_releases", traceReleasesSQL, team); err != nil {
		return nil, err
	}
	if in.runs, err = all[traceRun](ctx, tx, "trace_runs", traceRunsSQL, team); err != nil {
		return nil, err
	}
	shas := map[string]bool{}
	for _, b := range in.bodies {
		for _, l := range links(b.Body) {
			shas[l.old], shas[l.new] = true, true
		}
	}
	for _, r := range in.runs {
		shas[r.CommitSHA] = true
	}
	if in.positions, err = all[tracePosition](ctx, tx, "trace_positions", tracePositionsSQL, team,
		slices.Sorted(maps.Keys(shas))); err != nil {
		return nil, err
	}
	return in, nil
}

// link is one compare link: the repo's project (lowercased, "" when the
// link names none), its slug, and the old and the new end.
type link struct{ project, slug, old, new string }

// links are body's compare links, each once.
func links(body string) []link {
	var out []link
	for _, m := range compareLink.FindAllStringSubmatch(body, -1) {
		l := link{project: strings.ToLower(m[1]), slug: m[2], old: m[3], new: m[4]}
		if !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

// Stretch is the part of one repo's trunk branch one deploy shipped: the
// commits that arrived on it in (From, To]. LowerBound: From is no known
// old end (the stretch holds at least these; the deploy may have shipped
// more, or some of these first went out with a deploy riptide could not
// resolve).
type Stretch struct {
	DeployID    int64
	App         string
	Environment string
	At          time.Time
	Repo        string
	Branch      string
	From, To    time.Time
	Route       string
	LowerBound  bool
}

// holds reports whether the stretch shipped a PR that reached branch at at
// (branch "": the PR's payload named no target, any trunk branch).
func (s Stretch) holds(at time.Time, branch string) bool {
	return (branch == "" || branch == s.Branch) && at.After(s.From) && !at.After(s.To)
}

// RepoTrace is what tracing knows of one repo: since when an App is known
// to deploy it, and every release of those Apps.
type RepoTrace struct {
	// DeployedSince is the first deploy that showed an App deploys the
	// repo (its compare link, its run's image, an image named after it);
	// nil: none ever did, nothing tells where its PRs went.
	DeployedSince *time.Time
	// Apps are the Apps known to deploy it, each since the first deploy that
	// showed it (DeployedSince is the earliest).
	Apps map[string]time.Time
	// Releases are its Apps' deploys that brought new images (or an App's
	// first), oldest first: the deploys that could have shipped its PRs.
	Releases []TraceRelease
}

// TraceRelease is one release of an App that deploys a repo, and what
// tracing could tell of the repo in it.
type TraceRelease struct {
	App         string
	Environment string
	At          time.Time
	// Known is when a deploy first showed the App deploys the repo
	// (RepoTrace.Apps), possibly after this release: a closed period reads
	// the release only when that came before its end.
	Known time.Time
	// Stretches are the repo's stretches this release resolved;
	// Unchanged: it shipped nothing of the repo (its compare links name
	// other repos only, or the repo's images stayed the same).
	Stretches []Stretch
	Unchanged bool
	// Silence: a lost Bitbucket silence (Silence.Lost) lay within
	// LostCommitReach before it: its GitOps commit or the commits it
	// shipped may be what was lost.
	Silence bool
}

// Resolved reports whether tracing knows if the release shipped a PR that
// reached the repo's trunk branch ("": any) at at: it shipped nothing of
// the repo, or a stretch of it holds at, ends before at, or starts at a
// known old end. A stretch whose start is a lower bound past at may have
// shipped it, or an earlier release did: unknown.
func (r TraceRelease) Resolved(at time.Time, branch string) bool {
	if r.Unchanged {
		return true
	}
	return slices.ContainsFunc(r.Stretches, func(s Stretch) bool {
		return (branch == "" || branch == s.Branch) && (s.holds(at, branch) || s.To.Before(at) || !s.LowerBound)
	})
}

// ShippedBefore reports whether a stretch of the release proves a PR that
// reached the repo's trunk branch ("": any) at at was out before it: the
// stretch starts at a known old end at or after at, so the deploy the App
// ran before this one already held it, whether riptide saw that deploy or
// not.
func (r TraceRelease) ShippedBefore(at time.Time, branch string) bool {
	return slices.ContainsFunc(r.Stretches, func(s Stretch) bool {
		return !s.LowerBound && (branch == "" || branch == s.Branch) && !s.From.Before(at)
	})
}

// trace is buildTrace's result: every stretch by repo (deploy time, then
// id), each repo's trace, and the deploys some stretch resolved.
type trace struct {
	stretches map[string][]Stretch
	repos     map[string]*RepoTrace
	traced    map[int64]bool
	// releases are every App's releases per environment, oldest first.
	releases []AppRelease
}

// AppRelease is an App's deploy to an environment that ran other images
// than its deploy there before, or its first deploy naming images (First):
// what the sprint rule counts, over all history (dataset.Releases). A
// re-sync of the same images is none.
type AppRelease struct {
	App         string
	Environment string
	At          time.Time
	// First: the App's first deploy to the environment riptide holds. Inside
	// FirstDeployGrace it is no release anywhere else (the App may have run
	// those images long before): the last release was then, or earlier.
	First bool
}

// head is a deployed image's run commit where it reached the trunk: the
// stretch's end it gives.
type head struct {
	repo, branch string
	at           time.Time
}

// imageSet is the tags of one image repository a deploy ran, and their
// heads by repo (the newest).
type imageSet struct {
	refs  []string
	at    time.Time
	heads map[string]head
}

// imageRepository is ref without its digest, then its tag: what a new
// version of the same image keeps. A registry's port is no tag.
func imageRepository(ref string) string {
	ref, _, _ = strings.Cut(ref, "@")
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	return ref
}

// noSHA is the fromHash of a branch's first push.
const noSHA = "0000000000000000000000000000000000000000"

// justBefore is the instant before t (Postgres keeps microseconds): a
// stretch starting there holds the push at t.
func justBefore(t time.Time) time.Time { return t.Add(-time.Microsecond) }

// buildTrace resolves every deploy's stretches (in deploy order, as each
// leans on the ones before it), which Apps deploy which repo, and their
// releases. silences mark the releases lost Bitbucket events may explain.
func buildTrace(in *traceInput, silences []Silence) *trace {
	t := &trace{stretches: map[string][]Stretch{}, repos: map[string]*RepoTrace{}, traced: map[int64]bool{}}

	// Where SHAs reached the trunk, by repo; each trunk branch's first push.
	at := map[string][]tracePosition{}
	type start struct {
		at   time.Time
		from string
	}
	starts := map[string]map[string]start{}
	repos := map[string]bool{}
	for _, p := range in.positions {
		repos[p.Repo] = true
		switch p.Kind {
		case "at":
			at[*p.SHA] = append(at[*p.SHA], p)
		case "start":
			if starts[p.Repo] == nil {
				starts[p.Repo] = map[string]start{}
			}
			s := start{at: p.At}
			if p.SHA != nil && *p.SHA != noSHA {
				s.from = *p.SHA
			}
			starts[p.Repo][p.Branch] = s
		}
	}
	for _, r := range in.runs {
		if r.Repo != nil {
			repos[*r.Repo] = true
		}
	}
	bySlug := map[string][]string{}
	for repo := range repos {
		_, slug, _ := strings.Cut(repo, "/")
		bySlug[strings.ToLower(slug)] = append(bySlug[strings.ToLower(slug)], repo)
	}
	// A link's repo: its slug's, the project telling two apart.
	repoOf := func(l link) string {
		var hit []string
		for _, r := range bySlug[l.slug] {
			if l.project == "" || strings.EqualFold(strings.SplitN(r, "/", 2)[0], l.project) {
				hit = append(hit, r)
			}
		}
		if len(hit) == 1 {
			return hit[0]
		}
		return ""
	}
	// Where sha reached repo's trunk; ok false when it did not.
	reached := func(sha, repo string) (tracePosition, bool) {
		for _, p := range at[sha] {
			if p.Repo == repo {
				return p, true
			}
		}
		return tracePosition{}, false
	}
	// The trunk branch of repo whose first push's fromHash is sha.
	firstFrom := func(sha, repo string) (string, start, bool) {
		for _, b := range slices.Sorted(maps.Keys(starts[repo])) {
			if s := starts[repo][b]; s.from == sha {
				return b, s, true
			}
		}
		return "", start{}, false
	}
	// The repo's trunk data start on branch: a stretch from it holds its
	// first push.
	dataStart := func(repo, branch string) (time.Time, bool) {
		s, ok := starts[repo][branch]
		return justBefore(s.at), ok
	}

	linksOf := map[string][]link{}
	for _, b := range in.bodies {
		for _, l := range links(b.Body) {
			if !slices.Contains(linksOf[b.Revision], l) {
				linksOf[b.Revision] = append(linksOf[b.Revision], l)
			}
		}
	}
	runsOf := map[string][]traceRun{}
	for _, r := range in.runs {
		for _, ref := range r.ImageRefs {
			runsOf[ref] = append(runsOf[ref], r)
		}
	}
	// What a run's image tells: the repo it was built in (Bitbucket's
	// sighting of its commit, else where the commit reached a trunk) and
	// its head, when the commit reached the trunk before the deploy.
	headOf := func(ref string, deployed time.Time) (repo string, h *head) {
		var run *traceRun
		for i, r := range runsOf[ref] {
			// The latest run started before the deploy: a tag pushed again
			// names the rebuild (docs/correlating-deploys-to-commits.md).
			if !r.StartedAt.After(deployed) && (run == nil || r.StartedAt.After(run.StartedAt) ||
				(r.StartedAt.Equal(run.StartedAt) && r.ID > run.ID)) {
				run = &runsOf[ref][i]
			}
		}
		if run == nil {
			return "", nil
		}
		if run.Repo != nil {
			repo = *run.Repo
		}
		var pos *tracePosition
		for i, p := range at[run.CommitSHA] {
			if p.Repo == repo {
				pos = &at[run.CommitSHA][i]
				break
			}
		}
		if repo == "" && len(at[run.CommitSHA]) == 1 {
			// Bitbucket saw the commit nowhere else: the one trunk it reached
			// (as imageRepos tells a run's repo; of several, none).
			pos = &at[run.CommitSHA][0]
		}
		if pos == nil {
			return repo, nil
		}
		if pos.At.After(deployed) {
			return pos.Repo, nil
		}
		end := pos.At
		if run.StartedAt.Before(end) {
			end = run.StartedAt
		}
		return pos.Repo, &head{repo: pos.Repo, branch: pos.Branch, at: end}
	}
	// The repos an image is named after (a path segment equal to a slug):
	// a deployable repo's image is, even when its CI does not report.
	namedAfter := func(ref string) []string {
		var out []string
		for _, s := range imageSegments(ref) {
			out = append(out, bySlug[s]...)
		}
		return out
	}

	// Evidence that an App deploys a repo, first instant.
	since := map[string]map[string]time.Time{}
	evidence := func(repo, app string, when time.Time) {
		if repo == "" {
			return
		}
		if since[repo] == nil {
			since[repo] = map[string]time.Time{}
		}
		if s, ok := since[repo][app]; !ok || when.Before(s) {
			since[repo][app] = when
		}
	}
	// The image repositories each repo's runs published, each since the
	// first run's start: what a release knew of them is what ran before it,
	// so a closed period's releases read the same whatever a later run
	// publishes.
	imageRepos := map[string]map[string]time.Time{}
	for _, r := range in.runs {
		repo := ""
		if r.Repo != nil {
			repo = *r.Repo
		} else if ps := at[r.CommitSHA]; len(ps) == 1 {
			repo = ps[0].Repo
		}
		if repo == "" {
			continue
		}
		if imageRepos[repo] == nil {
			imageRepos[repo] = map[string]time.Time{}
		}
		for _, ref := range r.ImageRefs {
			ir := imageRepository(ref)
			if s, ok := imageRepos[repo][ir]; !ok || r.StartedAt.Before(s) {
				imageRepos[repo][ir] = r.StartedAt
			}
		}
	}
	// Whether a run of repo started by when had published image repository
	// ir ("": any).
	published := func(repo, ir string, when time.Time) bool {
		if ir != "" {
			s, ok := imageRepos[repo][ir]
			return ok && !s.After(when)
		}
		for _, s := range imageRepos[repo] {
			if !s.After(when) {
				return true
			}
		}
		return false
	}

	// Whether image repository ir is repo's as of when: one its runs had
	// published, or one named after it.
	owns := func(repo, ir string, when time.Time) bool {
		if published(repo, ir, when) {
			return true
		}
		_, slug, _ := strings.Cut(repo, "/")
		return slices.Contains(imageSegments(ir), strings.ToLower(slug))
	}

	type appEnv struct{ app, env string }
	type release struct {
		d         traceDeploy
		stretches map[string][]Stretch
		unchanged map[string]bool
		links     bool
		changed   map[string]bool
		// changedIRs are changed's keys, sorted.
		changedIRs []string
	}
	last := map[appEnv]map[string]*imageSet{}
	previous := map[appEnv][]string{}
	// Where a run's head cut a link's new end back (an App's deploy, the
	// repo, the new end's SHA): the next link from that SHA starts there, or
	// what landed between the head and the new end's push is in no stretch.
	type cut struct {
		appEnv
		repo, sha string
	}
	capped := map[cut]time.Time{}
	// Per environment and image repository, what an earlier deploy of any
	// App shipped: the newest head by repo, and when one last ran it.
	envHead := map[string]map[string]map[string]head{}
	envLast := map[string]map[string]time.Time{}
	var releases []release

	for _, d := range in.deploys {
		key := appEnv{d.App, d.Environment}
		sets := map[string]*imageSet{}
		for _, ref := range d.Images {
			ir := imageRepository(ref)
			if sets[ir] == nil {
				sets[ir] = &imageSet{at: d.At, heads: map[string]head{}}
			}
			s := sets[ir]
			s.refs = append(s.refs, ref)
			repo, h := headOf(ref, d.At)
			evidence(repo, d.App, d.At)
			if h != nil {
				if old, ok := s.heads[h.repo]; !ok || h.at.After(old.at) {
					s.heads[h.repo] = *h
				}
			}
			for _, r := range namedAfter(ref) {
				evidence(r, d.App, d.At)
			}
		}
		prev, seen := previous[key]
		isRelease := len(d.Images) > 0 && (!seen || !slices.Equal(prev, d.Images))
		if len(d.Images) > 0 {
			previous[key] = d.Images
		}
		rel := release{d: d, stretches: map[string][]Stretch{}, unchanged: map[string]bool{}, changed: map[string]bool{}}

		// Route 1: the compare links.
		byRepo := map[string]*Stretch{}
		// The new end of the link that gave each repo's stretch its end.
		endSHA := map[string]string{}
		named := map[string]bool{}
		for _, l := range linksOf[d.Revision] {
			rel.links = true
			repo := repoOf(l)
			if repo == "" {
				// Resolve the slug through the new end's repo.
				for _, p := range at[l.new] {
					if strings.HasSuffix(p.Repo, "/"+l.slug) {
						repo = p.Repo
						break
					}
				}
			}
			if repo == "" {
				// Not told apart: the link may be any of its slug's repos'.
				for _, r := range bySlug[l.slug] {
					if l.project == "" || strings.EqualFold(strings.SplitN(r, "/", 2)[0], l.project) {
						named[r] = true
					}
				}
				continue
			}
			named[repo] = true
			evidence(repo, d.App, d.At)
			var branch string
			var to time.Time
			if p, ok := reached(l.new, repo); ok && !p.At.After(d.At) {
				branch, to = p.Branch, p.At
			} else if b, s, ok := firstFrom(l.new, repo); ok && !s.at.After(d.At) {
				branch, to = b, justBefore(s.at)
			} else {
				continue
			}
			var from time.Time
			lower := false
			if c, ok := capped[cut{key, repo, l.old}]; ok {
				from = c
			} else if p, ok := reached(l.old, repo); ok {
				from = p.At
			} else if _, s, ok := firstFrom(l.old, repo); ok {
				from = justBefore(s.at)
			} else if s, ok := dataStart(repo, branch); ok {
				from, lower = s, true
			} else {
				continue
			}
			st := byRepo[repo]
			if st == nil {
				byRepo[repo] = &Stretch{DeployID: d.ID, App: d.App, Environment: d.Environment, At: d.At, Repo: repo,
					Branch: branch, From: from, To: to, Route: RouteCompare, LowerBound: lower}
				endSHA[repo] = l.new
				continue
			}
			// Several links of one repo (Apps of the product on different
			// versions): their union, a lower bound when its start is one.
			switch {
			case from.Before(st.From):
				st.From, st.LowerBound = from, lower
			case from.Equal(st.From):
				st.LowerBound = st.LowerBound && lower
			}
			if to.After(st.To) {
				st.To, st.Branch = to, branch
				endSHA[repo] = l.new
			}
		}
		// A run's head caps a link's new end: a release tool's commits pushed
		// after the build started are not in the image.
		capOf := map[string]time.Time{}
		for _, s := range sets {
			for repo, h := range s.heads {
				if c, ok := capOf[repo]; !ok || h.at.After(c) {
					capOf[repo] = h.at
				}
			}
		}
		for repo, st := range byRepo {
			if c, ok := capOf[repo]; ok && c.Before(st.To) && c.After(st.From) {
				st.To = c
				capped[cut{key, repo, endSHA[repo]}] = c
			}
		}

		// Route 2: the images, for the repos no link resolved.
		image := map[string]*Stretch{}
		for ir, s := range sets {
			slices.Sort(s.refs)
			prevSet := last[key][ir]
			changed := prevSet == nil || !slices.Equal(prevSet.refs, s.refs)
			if changed {
				rel.changed[ir] = true
			}
			for repo, h := range s.heads {
				if !changed || byRepo[repo] != nil {
					continue
				}
				var from time.Time
				lower := false
				switch {
				case prevSet != nil:
					if ph, ok := prevSet.heads[repo]; ok {
						from = ph.at
					} else {
						from, lower = prevSet.at, true
					}
				case envHead[d.Environment][ir] != nil && envHead[d.Environment][ir][repo].repo != "":
					from = envHead[d.Environment][ir][repo].at
				case !envLast[d.Environment][ir].IsZero():
					from, lower = envLast[d.Environment][ir], true
				default:
					ds, ok := dataStart(repo, h.branch)
					if !ok {
						continue
					}
					from, lower = ds, true
				}
				st := &Stretch{DeployID: d.ID, App: d.App, Environment: d.Environment, At: d.At, Repo: repo,
					Branch: h.branch, From: from, To: h.at, Route: RouteImage, LowerBound: lower}
				if o := image[repo]; o != nil {
					// Two image repositories of one repo: their union, a lower
					// bound when its start is one.
					switch {
					case o.From.Before(st.From):
						st.From, st.LowerBound = o.From, o.LowerBound
					case o.From.Equal(st.From):
						st.LowerBound = st.LowerBound && o.LowerBound
					}
					if o.To.After(st.To) {
						st.To, st.Branch = o.To, o.Branch
					}
				}
				image[repo] = st
			}
		}
		for repo, st := range image {
			byRepo[repo] = st
		}
		for _, repo := range slices.Sorted(maps.Keys(byRepo)) {
			st := *byRepo[repo]
			if st.To.Before(st.From) && !st.LowerBound {
				// A rollback: an older head than the one before ships nothing.
				// A head older than a lower bound start is no rollback: what
				// the images before held is unknown, and the stretch, empty,
				// leaves the release unresolved for what came before its head.
				st.To = st.From
			}
			t.stretches[repo] = append(t.stretches[repo], st)
			t.traced[d.ID] = true
			rel.stretches[repo] = append(rel.stretches[repo], st)
		}
		// Shipped nothing of a repo: the links name others only, and none of
		// its own images changed (a repo whose image changed while no link
		// names it is no proof of anything).
		changedIRs := slices.Sorted(maps.Keys(rel.changed))
		rel.changedIRs = changedIRs
		if rel.links {
			for repo := range repos {
				if !named[repo] && !slices.ContainsFunc(changedIRs, func(ir string) bool {
					return owns(repo, ir, d.At)
				}) {
					rel.unchanged[repo] = true
				}
			}
		}

		// Remember the sets, for the next deploy of this App and of others.
		// A re-sync of the same tags keeps the set as it first ran: what
		// landed after that was not in it.
		if last[key] == nil {
			last[key] = map[string]*imageSet{}
		}
		for ir, s := range sets {
			if p := last[key][ir]; p != nil && slices.Equal(p.refs, s.refs) {
				s = p
			}
			last[key][ir] = s
			if envHead[d.Environment] == nil {
				envHead[d.Environment] = map[string]map[string]head{}
				envLast[d.Environment] = map[string]time.Time{}
			}
			if envHead[d.Environment][ir] == nil {
				envHead[d.Environment][ir] = map[string]head{}
			}
			for repo, h := range s.heads {
				if o, ok := envHead[d.Environment][ir][repo]; !ok || h.at.After(o.at) {
					envHead[d.Environment][ir][repo] = h
				}
			}
			envLast[d.Environment][ir] = s.at
		}
		if isRelease {
			releases = append(releases, rel)
			t.releases = append(t.releases, AppRelease{App: d.App, Environment: d.Environment, At: d.At.UTC(), First: !seen})
		}
	}

	// Which repos each App deploys, and since when.
	apps := map[string]map[string]time.Time{}
	for repo, byApp := range since {
		rt := &RepoTrace{Apps: map[string]time.Time{}}
		for app, when := range byApp {
			if rt.DeployedSince == nil || when.Before(*rt.DeployedSince) {
				rt.DeployedSince = ptr(when)
			}
			rt.Apps[app] = when.UTC()
			if apps[app] == nil {
				apps[app] = map[string]time.Time{}
			}
			apps[app][repo] = when
		}
		t.repos[repo] = rt
	}
	// Each repo's Apps' releases, and what tracing could tell of it there.
	for _, rel := range releases {
		silence := slices.ContainsFunc(silences, func(s Silence) bool {
			return s.Lost && !s.From.After(rel.d.At) && !s.To.Before(rel.d.At.Add(-LostCommitReach))
		})
		// Whether it shipped nothing of the repos its App was known to deploy
		// by then (one learnt later must not reopen a closed period).
		all, some := true, false
		for repo, known := range apps[rel.d.App] {
			unchanged := rel.unchanged[repo]
			if !unchanged && len(rel.stretches[repo]) == 0 {
				// Its images stayed the same: the repo's image repositories
				// (its runs' before the release, or named after it) none of
				// the changed ones.
				own := func(ir string) bool { return owns(repo, ir, rel.d.At) }
				knownIRs := published(repo, "", rel.d.At) || slices.ContainsFunc(rel.d.Images, func(ref string) bool {
					return own(imageRepository(ref))
				})
				unchanged = knownIRs && !slices.ContainsFunc(rel.changedIRs, own)
			}
			if !known.After(rel.d.At) {
				all, some = all && unchanged, true
			}
			t.repos[repo].Releases = append(t.repos[repo].Releases, TraceRelease{
				App: rel.d.App, Environment: rel.d.Environment, At: rel.d.At.UTC(), Known: known.UTC(),
				Stretches: rel.stretches[repo], Unchanged: unchanged, Silence: silence,
			})
		}
		if all && some {
			// It shipped nothing of any repo its App deploys (a sidecar's or
			// a base image's new tag): resolved, nothing to tie to PRs.
			t.traced[rel.d.ID] = true
		}
	}
	for _, rt := range t.repos {
		slices.SortStableFunc(rt.Releases, func(a, b TraceRelease) int { return a.At.Compare(b.At) })
	}
	return t
}

// place gives every merged PR its first deploy per environment whose
// stretch holds its arrival on the trunk (or onward on master or main), and
// the dataset its repos' traces. A PR an umbrella's merge marked merged
// (ImplicitMerge) arrives with the umbrella. After markImplicitMerges.
func place(d *Dataset, t *trace) {
	d.Traces = map[string]RepoTrace{}
	for i := range d.PRs {
		pr := &d.PRs[i]
		envs := []EnvironmentDeploy{}
		if pr.TrunkAt != nil {
			first := map[string]EnvironmentDeploy{}
			for _, s := range t.stretches[pr.Repo] {
				if !s.holds(*pr.TrunkAt, deref(pr.TrunkBranch)) &&
					(pr.OnwardAt == nil || !s.holds(*pr.OnwardAt, deref(pr.OnwardBranch))) {
					continue
				}
				e, ok := first[s.Environment]
				// The earliest; of two at once the exact one.
				if !ok || s.At.Before(e.FirstDeployedAt) || (s.At.Equal(e.FirstDeployedAt) && e.LowerBound && !s.LowerBound) {
					first[s.Environment] = EnvironmentDeploy{Environment: s.Environment, FirstDeployedAt: s.At,
						Route: s.Route, LowerBound: s.LowerBound}
				}
			}
			envs = slices.AppendSeq(envs, maps.Values(first))
		}
		slices.SortFunc(envs, func(a, b EnvironmentDeploy) int {
			return cmp.Or(a.FirstDeployedAt.Compare(b.FirstDeployedAt), cmp.Compare(a.Environment, b.Environment))
		})
		pr.Environments = envs
		if rt, ok := t.repos[pr.Repo]; ok {
			d.Traces[pr.Repo] = *rt
		}
	}
	for i := range d.Deploys {
		d.Deploys[i].Traced = t.traced[d.Deploys[i].ID]
	}
}

// String names a stretch for a test's failure.
func (s Stretch) String() string {
	return fmt.Sprintf("%s %s@%s %s %s:%s (%s, %s] lower=%v", s.App, s.Environment, s.At.UTC().Format(time.RFC3339),
		s.Route, s.Repo, s.Branch, s.From.UTC().Format(time.RFC3339Nano), s.To.UTC().Format(time.RFC3339Nano), s.LowerBound)
}
