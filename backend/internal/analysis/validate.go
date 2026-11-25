package analysis

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/trick77/toolkit/llmwire"

	"github.com/trick77/riptide/internal/metrics"
)

// Report is what the validator left out and which mandatory signals the
// answer did not cover; the page shows those as "also spotted".
type Report struct {
	Dropped []string `json:"dropped"`
	Missed  []string `json:"missed"`
}

var placeholder = regexp.MustCompile(`\{\{([^{}]+)\}\}`)

// placeholderWidth is what a filled placeholder takes on the page, about:
// "12.9 Tage", "94 %", "3".
const placeholderWidth = 8

// checker validates one answer against the assessment it words, all in
// aliases.
type checker struct {
	facts     map[string]bool
	focus     map[string][]string // alias -> levers
	focusSigs map[string][]string // alias -> signal ids
	cards     map[string][]string // signal id -> levers
	aliases   *aliases
	report    *Report
}

func newChecker(a metrics.Assessment, al *aliases) *checker {
	c := &checker{facts: map[string]bool{}, focus: map[string][]string{}, focusSigs: map[string][]string{},
		cards: map[string][]string{}, aliases: al, report: &Report{Dropped: []string{}, Missed: []string{}}}
	addFacts := func(fs []metrics.Fact) {
		for _, f := range fs {
			c.facts[al.hideID(f.ID)] = true
		}
	}
	addFacts(a.Team)
	bySig := map[string]metrics.Signal{}
	for _, s := range a.Signals {
		addFacts(s.Facts)
		bySig[s.ID] = s
		if cardSignal(s) {
			c.cards[al.hideID(s.ID)] = leversFor(s)
		}
	}
	if a.Question != nil {
		addFacts(a.Question.Facts)
	}
	for _, f := range a.Focus {
		addFacts(f.Facts)
		alias := al.alias[f.Repo]
		for _, id := range f.Signals {
			c.focusSigs[alias] = append(c.focusSigs[alias], al.hideID(id))
			for _, l := range leversFor(bySig[id]) {
				if !slices.Contains(c.focus[alias], l) {
					c.focus[alias] = append(c.focus[alias], l)
				}
			}
		}
	}
	return c
}

// clean reports why s may not be shown, or "" when it may: every
// placeholder a known fact, no digit outside them and the aliases, no sharp
// s in German, at most maxLen characters.
func (c *checker) clean(s string, maxLen int, de bool) string {
	// A placeholder counts as the value the page puts there ("12.9 Tage"),
	// not as its id, which runs to forty characters.
	shown := placeholder.ReplaceAllString(s, strings.Repeat("#", placeholderWidth))
	if utf8.RuneCountInString(shown) > maxLen {
		return fmt.Sprintf("longer than %d", maxLen)
	}
	bad := ""
	rest := placeholder.ReplaceAllStringFunc(s, func(m string) string {
		if id := strings.TrimSpace(m[2 : len(m)-2]); !c.facts[id] && bad == "" {
			bad = "unknown fact " + id
		}
		return " "
	})
	if bad != "" {
		return bad
	}
	for _, a := range c.aliases.alias {
		rest = strings.ReplaceAll(rest, a, " ")
	}
	if strings.ContainsFunc(rest, unicode.IsDigit) {
		return "a digit outside a placeholder"
	}
	if de && strings.ContainsRune(s, 'ß') {
		return "a sharp s"
	}
	return ""
}

func (c *checker) drop(lang, what, why string) {
	c.report.Dropped = append(c.report.Dropped, fmt.Sprintf("%s %s: %s", lang, what, why))
}

// text checks one language; a bad headline fails the answer, anything
// else is dropped on its own.
func (c *checker) text(lang string, t Text) (Text, error) {
	de := lang == "de"
	t.Headline = strings.TrimSpace(t.Headline)
	if t.Headline == "" {
		return t, fmt.Errorf("%s headline empty", lang)
	}
	if why := c.clean(t.Headline, MaxHeadline, de); why != "" {
		return t, fmt.Errorf("%s headline: %s", lang, why)
	}
	field := func(name string, s *string, maxLen int) {
		*s = strings.TrimSpace(*s)
		if why := c.clean(*s, maxLen, de); why != "" {
			c.drop(lang, name, why)
			*s = ""
		}
	}
	field("trend", &t.Trend, MaxTrend)
	field("question", &t.Question, MaxQuestion)
	retro := func(name string, r *string) *string {
		if r == nil || strings.TrimSpace(*r) == "" {
			return nil
		}
		v := strings.TrimSpace(*r)
		if why := c.clean(v, MaxRetro, de); why != "" {
			c.drop(lang, name+" retro", why)
			return nil
		}
		return &v
	}
	focus := []FocusText{}
	for _, f := range t.Focus {
		name := "focus " + f.Repo
		levers, ok := c.focus[f.Repo]
		switch {
		case !ok:
			c.drop(lang, name, "not a candidate")
		case slices.ContainsFunc(focus, func(x FocusText) bool { return x.Repo == f.Repo }):
			c.drop(lang, name, "twice")
		case len(focus) == MaxFocus:
			c.drop(lang, name, "past the cap")
		case !slices.Contains(levers, f.Lever):
			c.drop(lang, name, "lever "+f.Lever+" not its own")
		default:
			f.Take = strings.TrimSpace(f.Take)
			if why := c.clean(f.Take, MaxTake, de); why != "" || f.Take == "" {
				c.drop(lang, name, "take: "+cmp.Or(why, "empty"))
				continue
			}
			f.Retro = retro(name, f.Retro)
			focus = append(focus, f)
		}
	}
	t.Focus = focus
	cards := []CardText{}
	for _, k := range t.Cards {
		name := "card " + k.Signal
		levers, ok := c.cards[k.Signal]
		switch {
		case !ok:
			c.drop(lang, name, "no team-wide signal")
		case slices.ContainsFunc(cards, func(x CardText) bool { return x.Signal == k.Signal }):
			c.drop(lang, name, "twice")
		case len(cards) == MaxCards:
			c.drop(lang, name, "past the cap")
		// A signal without levers (a resolution) takes none: no step without
		// a finding behind it.
		case len(levers) == 0 && k.Lever != "", len(levers) > 0 && !slices.Contains(levers, k.Lever):
			c.drop(lang, name, "lever "+k.Lever+" not its own")
		default:
			k.Take = strings.TrimSpace(k.Take)
			if why := c.clean(k.Take, MaxTake, de); why != "" || k.Take == "" {
				c.drop(lang, name, "take: "+cmp.Or(why, "empty"))
				continue
			}
			k.Retro = retro(name, k.Retro)
			cards = append(cards, k)
		}
	}
	t.Cards = cards
	return t, nil
}

// cardSignal reports whether s is worded as a team-wide card: no gate, no
// repo (that is the focus's) unless resolved. The validator and the layout
// both ask it.
func cardSignal(s metrics.Signal) bool {
	return s.Tone != metrics.ToneGate && (s.Repo == nil || s.Kind == metrics.SignalResolved)
}

// mirror keeps only what both languages kept: switching the language must
// never switch the content, so an item or sentence dropped in one is
// dropped in the other.
func (c *checker) mirror(out *Output) {
	both := func(a, b *string, name string) {
		if (*a == "") != (*b == "") {
			c.drop("both", name, "dropped in one language")
			*a, *b = "", ""
		}
	}
	both(&out.DE.Trend, &out.EN.Trend, "trend")
	both(&out.DE.Question, &out.EN.Question, "question")

	focus := func(t Text) map[string]bool {
		m := map[string]bool{}
		for _, f := range t.Focus {
			m[f.Repo] = true
		}
		return m
	}
	cards := func(t Text) map[string]bool {
		m := map[string]bool{}
		for _, k := range t.Cards {
			m[k.Signal] = true
		}
		return m
	}
	fd, fe, cd, ce := focus(out.DE), focus(out.EN), cards(out.DE), cards(out.EN)
	keepF := func(t *Text, other map[string]bool) {
		kept := []FocusText{}
		for _, f := range t.Focus {
			if other[f.Repo] {
				kept = append(kept, f)
			} else {
				c.drop("both", "focus "+f.Repo, "missing in the other language")
			}
		}
		t.Focus = kept
	}
	keepC := func(t *Text, other map[string]bool) {
		kept := []CardText{}
		for _, k := range t.Cards {
			if other[k.Signal] {
				kept = append(kept, k)
			} else {
				c.drop("both", "card "+k.Signal, "missing in the other language")
			}
		}
		t.Cards = kept
	}
	keepF(&out.DE, fe)
	keepF(&out.EN, fd)
	keepC(&out.DE, ce)
	keepC(&out.EN, cd)
	// The retro question too: a question in one language only is none. The
	// lever is the German one in both: a key, worded by the page.
	for i := range out.DE.Focus {
		for j := range out.EN.Focus {
			d, e := &out.DE.Focus[i], &out.EN.Focus[j]
			if d.Repo != e.Repo {
				continue
			}
			if (d.Retro == nil) != (e.Retro == nil) {
				d.Retro, e.Retro = nil, nil
			}
			e.Lever = d.Lever
		}
	}
	for i := range out.DE.Cards {
		for j := range out.EN.Cards {
			d, e := &out.DE.Cards[i], &out.EN.Cards[j]
			if d.Signal != e.Signal {
				continue
			}
			if (d.Retro == nil) != (e.Retro == nil) {
				d.Retro, e.Retro = nil, nil
			}
			e.Lever = d.Lever
		}
	}
}

// covered are the signal IDs a text words, through a focus repo or a card.
func (c *checker) covered(t Text) map[string]bool {
	out := map[string]bool{}
	for _, f := range t.Focus {
		for _, id := range c.focusSigs[f.Repo] {
			out[id] = true
		}
	}
	for _, k := range t.Cards {
		out[k.Signal] = true
	}
	return out
}

// show maps every alias of a checked text back to its repo.
func (al *aliases) showText(t Text) Text {
	t.Headline, t.Trend, t.Question = al.show(t.Headline), al.show(t.Trend), al.show(t.Question)
	showP := func(p *string) *string {
		if p == nil {
			return nil
		}
		v := al.show(*p)
		return &v
	}
	for i := range t.Focus {
		f := &t.Focus[i]
		f.Repo, f.Take, f.Retro = al.show(f.Repo), al.show(f.Take), showP(f.Retro)
	}
	for i := range t.Cards {
		k := &t.Cards[i]
		k.Signal, k.Take, k.Retro = al.showID(k.Signal), al.show(k.Take), showP(k.Retro)
	}
	return t
}

// errNoAnswer: the model answered no JSON object.
var errNoAnswer = errors.New("no JSON object in the answer")

// validate checks the model's raw answer against the assessment, both
// languages, and returns it with the repos shown again. A bad headline or
// unparsable answer fails it; a bad item is dropped and reported.
func validate(raw string, a metrics.Assessment, al *aliases) (Output, Report, error) {
	obj, ok := llmwire.JSONObject(raw)
	if !ok {
		return Output{}, Report{}, errNoAnswer
	}
	var out Output
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return Output{}, Report{}, fmt.Errorf("answer: %w", err)
	}
	c := newChecker(a, al)
	var err error
	if out.DE, err = c.text("de", out.DE); err != nil {
		return Output{}, *c.report, err
	}
	if out.EN, err = c.text("en", out.EN); err != nil {
		return Output{}, *c.report, err
	}
	c.mirror(&out)
	got := c.covered(out.DE)
	for _, id := range mandatory(a) {
		if !got[al.hideID(id)] {
			c.report.Missed = append(c.report.Missed, id)
		}
	}
	out.DE, out.EN = al.showText(out.DE), al.showText(out.EN)
	return out, *c.report, nil
}
