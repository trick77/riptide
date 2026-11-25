// Package analysis words an assessment (metrics.Assess) with an LLM: what
// runs well, what is stuck, the two or three repos that move most, a
// question for the retro. The model gets the assessment's signals and
// numbers, never raw events, and judges nothing riptide has not judged:
// numbers reach its text only as {{fact}} placeholders the UI fills in,
// levers only as glossary keys, repos only under an alias. The analysis is
// text about computed numbers, never the source of one: stored for its
// cost, its history and the follow-up, not as a metric.
package analysis

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/trick77/riptide/internal/metrics"
)

// Text is the analysis in one language.
type Text struct {
	// Headline is the one sentence read out in the daily.
	Headline string `json:"headline"`
	// Trend is the team as a whole, one sentence.
	Trend string      `json:"trend"`
	Focus []FocusText `json:"focus"`
	Cards []CardText  `json:"cards"`
	// Question is the one question for the team.
	Question string `json:"question"`
}

// FocusText words a focus repo.
type FocusText struct {
	Repo  string  `json:"repo"`
	Take  string  `json:"take"`
	Retro *string `json:"retro"`
	Lever string  `json:"lever"`
}

// CardText words a team-wide signal.
type CardText struct {
	Signal string  `json:"signal"`
	Take   string  `json:"take"`
	Retro  *string `json:"retro"`
	Lever  string  `json:"lever"`
}

// Output is the model's answer: both languages from one call, so switching
// the language never switches the content.
type Output struct {
	DE Text `json:"de"`
	EN Text `json:"en"`
}

// digestSignal is a signal as the model reads it.
type digestSignal struct {
	metrics.Signal
	Means  string   `json:"means"`
	Levers []string `json:"levers"`
	// Series shadows the signal's weeks, always nil (omitted): the weeks
	// are the page's picture, the model words the facts.
	Series *struct{} `json:"series,omitempty"`
}

// Digest is what the model reads: the assessment, every repo under an
// alias, with the glossary of what each signal and number means.
type Digest struct {
	WindowWeeks int    `json:"window_weeks"`
	Weeks       string `json:"calendar_weeks"`
	// Mandatory are the signals the answer must cover, by ID.
	Mandatory []string            `json:"mandatory"`
	Gaps      []string            `json:"gaps"`
	Team      []metrics.Fact      `json:"team"`
	Signals   []digestSignal      `json:"signals"`
	Focus     []metrics.FocusRepo `json:"focus_candidates"`
	Active    int                 `json:"active_repos"`
	// Calm, Unjudged and Idle as the assessment's: calm only where a rule
	// could judge the repo, so the model never calls the rest fine.
	Calm     int             `json:"calm_repos"`
	Unjudged int             `json:"unjudged_repos"`
	Idle     int             `json:"idle_repos"`
	Question *metrics.Signal `json:"question"`
	// Quiet and QuietReasons decide whether the text may say that nothing
	// stands out: Go decides it, the model words it.
	Quiet        bool              `json:"quiet"`
	QuietReasons []string          `json:"quiet_reasons"`
	Limits       []metrics.Limit   `json:"limits"`
	Labels       map[string]string `json:"labels"`
	Levers       map[string]string `json:"levers"`
}

// MandatoryTop is how many of the ranked signals, gates aside, the answer
// must cover: the rest may go to "also spotted" without words.
const MandatoryTop = 3

// aliases maps every repo of an assessment to repo_a, repo_b, ... in order
// of first appearance; the model never sees a repo's name.
type aliases struct {
	real  []string
	alias map[string]string
}

func aliasName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('a'+(i-1)%26)) + s
	}
	return "repo_" + s
}

func newAliases(a metrics.Assessment) *aliases {
	al := &aliases{alias: map[string]string{}}
	add := func(r *string) {
		if r != nil && *r != "" && al.alias[*r] == "" {
			al.alias[*r] = aliasName(len(al.real))
			al.real = append(al.real, *r)
		}
	}
	for _, f := range a.Focus {
		add(&f.Repo)
	}
	for _, s := range a.Signals {
		add(s.Repo)
		for _, d := range s.Drivers {
			add(&d.Repo)
		}
	}
	return al
}

// longestFirst sorts names longest first, so a name containing another is
// replaced whole (kasse-api-v2 before kasse-api).
func longestFirst(names []string) []string {
	out := slices.Clone(names)
	slices.SortFunc(out, func(x, y string) int { return cmp.Or(cmp.Compare(len(y), len(x)), cmp.Compare(x, y)) })
	return out
}

// swapSegment replaces from by to where from is an ID's repo segment:
// after a ':' and at the end or before a '.' ("pr_jam:R", "pr_jam:R.long",
// "change:review_time.driver.R"). Never a plain substring: a repo named
// "ci" or "review" must not touch "ci_seconds" or "review_time".
func swapSegment(id, from, to string) (string, bool) {
	for _, pre := range []string{":", "."} {
		if strings.HasSuffix(id, pre+from) {
			return id[:len(id)-len(from)] + to, true
		}
	}
	if i := strings.Index(id, ":"+from+"."); i >= 0 {
		return id[:i+1] + to + id[i+1+len(from):], true
	}
	return id, false
}

// hideID is a signal or fact ID with its repo segment aliased.
func (al *aliases) hideID(id string) string {
	for _, r := range longestFirst(al.real) {
		if out, ok := swapSegment(id, r, al.alias[r]); ok {
			return out
		}
	}
	return id
}

// showID is hideID undone.
func (al *aliases) showID(id string) string {
	for _, r := range al.real {
		if out, ok := swapSegment(id, al.alias[r], r); ok {
			return out
		}
	}
	return id
}

// aliasWord finds an alias in the model's prose, whole.
var aliasWord = regexp.MustCompile(`\brepo_[a-z]+\b`)

// show undoes the aliases in the model's text: placeholders get their real
// fact ID, aliases in prose their repo.
func (al *aliases) show(s string) string {
	byAlias := map[string]string{}
	for _, r := range al.real {
		byAlias[al.alias[r]] = r
	}
	s = placeholder.ReplaceAllStringFunc(s, func(m string) string {
		return "{{" + al.showID(strings.TrimSpace(m[2:len(m)-2])) + "}}"
	})
	return aliasWord.ReplaceAllStringFunc(s, func(a string) string {
		if r, ok := byAlias[a]; ok {
			return r
		}
		return a
	})
}

// hideFacts are facts with their IDs aliased, a copy.
func (al *aliases) hideFacts(fs []metrics.Fact) []metrics.Fact {
	out := make([]metrics.Fact, len(fs))
	for i, f := range fs {
		f.ID = al.hideID(f.ID)
		out[i] = f
	}
	return out
}

// hideRepo is a repo's alias.
func (al *aliases) hideRepo(r string) string {
	if a, ok := al.alias[r]; ok {
		return a
	}
	return r
}

// hideSignal is s with every repo aliased: its ID, repo, metric, facts and
// drivers. A copy: the stored assessment keeps the names.
func (al *aliases) hideSignal(s metrics.Signal) metrics.Signal {
	s.ID = al.hideID(s.ID)
	if s.Repo != nil {
		s.Repo = ptr(al.hideRepo(*s.Repo))
	}
	if s.Metric != nil {
		s.Metric = ptr(al.hideID(*s.Metric))
	}
	s.Facts = al.hideFacts(s.Facts)
	ds := make([]metrics.Driver, len(s.Drivers))
	for i, d := range s.Drivers {
		d.Repo = al.hideRepo(d.Repo)
		ds[i] = d
	}
	s.Drivers = ds
	return s
}

func ptr[T any](v T) *T { return &v }

// mandatory are the ranked signals the answer must word: the top
// MandatoryTop that are no gate (a gate is shown as is), and the strongest
// gain, which ranks last and would never be among them. A resolution is
// never added that way: it has a slot of its own and shows unworded too.
func mandatory(a metrics.Assessment) []string {
	out, good := []string{}, ""
	for _, s := range a.Signals {
		if s.Tone == metrics.ToneGate {
			continue
		}
		if good == "" && s.Tone == metrics.ToneGood && s.Kind == metrics.SignalChange {
			good = s.ID
		}
		if len(out) < MandatoryTop {
			out = append(out, s.ID)
		}
	}
	if good != "" && !slices.Contains(out, good) {
		out = append(out, good)
	}
	return out
}

// weekRange names the period's calendar weeks, "KW 43-46".
func weekRange(p metrics.Period) string {
	if len(p.Weeks) == 0 {
		return ""
	}
	return fmt.Sprintf("KW %d-%d", p.Weeks[0].KW, p.Weeks[len(p.Weeks)-1].KW)
}

// buildDigest is the model's input, the repos hidden, and the aliases to
// show them again.
func buildDigest(a metrics.Assessment) ([]byte, *aliases, error) {
	al := newAliases(a)
	// Every repo field aliased where it is, never a text replacement over
	// the JSON: a repo named like a key or a word would rewrite those.
	mand := mandatory(a)
	for i, id := range mand {
		mand[i] = al.hideID(id)
	}
	focus := make([]metrics.FocusRepo, len(a.Focus))
	for i, f := range a.Focus {
		f.Repo = al.hideRepo(f.Repo)
		sigs := make([]string, len(f.Signals))
		for j, id := range f.Signals {
			sigs[j] = al.hideID(id)
		}
		f.Signals, f.Facts = sigs, al.hideFacts(f.Facts)
		focus[i] = f
	}
	d := Digest{
		WindowWeeks: len(a.Period.Weeks), Weeks: weekRange(a.Period), Mandatory: mand,
		Gaps: a.Gaps, Team: a.Team, Focus: focus, Active: a.Active, Calm: a.Calm, Unjudged: a.Unjudged, Idle: a.Idle,
		Question: a.Question, Quiet: a.Quiet, QuietReasons: a.QuietReasons, Limits: a.Limits,
		Labels: labels, Levers: leverText,
	}
	for _, s := range a.Signals {
		d.Signals = append(d.Signals, digestSignal{Signal: al.hideSignal(s), Means: kinds[s.Kind].means, Levers: leversFor(s)})
	}
	raw, err := json.Marshal(d)
	return raw, al, err
}

// inputHash identifies a call: the same digest to the same model under
// the same rules is the same answer, and is not asked again. The rules
// version too: raised without a change to the digest or the prompt, a
// click must still word the view anew, or its outdated analysis stays.
func inputHash(model string, digest []byte) string {
	h := sha256.New()
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(metrics.RulesVersion)))
	h.Write([]byte{0})
	h.Write(digest)
	h.Write([]byte{0})
	h.Write([]byte(promptText))
	return hex.EncodeToString(h.Sum(nil))
}
