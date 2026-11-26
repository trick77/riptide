// Package config loads riptide.json: teams, org-wide automation rules and the
// environment settings. It is config, not data: edited by PR, hot-reloaded by
// the Runtime in this package, never moved into Postgres.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// DefaultProductionStage is the stage suffix that means production when the
// config does not say otherwise.
const DefaultProductionStage = "prod"

// Team is one entry of `teams`.
type Team struct {
	Name       string
	GroupEmail string
}

// AutomationSource is one entry of `automation`, in file order.
type AutomationSource struct {
	Name           string
	Authors        []string
	BranchPrefixes []string
}

// Environments is the `environments` block.
type Environments struct {
	ProductionStage string
	IgnoredStages   map[string]bool
}

// Config is a parsed, validated riptide.json. Immutable once built.
type Config struct {
	Teams        map[string]Team
	Automation   []AutomationSource
	Environments Environments
	// ExcludedProjects are Bitbucket project keys, lowercased, the metrics
	// leave out: shared libraries and tooling, not a team's product. Applied
	// on read, so the rows stay and the exclusion is retroactive.
	ExcludedProjects map[string]bool
	// JiraBaseURL is `jira.base_url` without a trailing slash; a key links
	// to <JiraBaseURL>/browse/<KEY>. Empty: keys are shown, not linked.
	JiraBaseURL string
	// Thresholds are the `thresholds` block over its starting values: the
	// team's agreements the assessment's states measure against.
	Thresholds Thresholds
}

// ExcludedProject reports whether the metrics leave the project key out.
func (c *Config) ExcludedProject(key string) bool {
	return c.ExcludedProjects[strings.ToLower(key)]
}

// TeamNames returns the configured team names, sorted.
func (c *Config) TeamNames() []string {
	out := make([]string, 0, len(c.Teams))
	for n := range c.Teams {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Parse validates the file contents. Unknown top-level keys are ignored; the
// keys riptide reads are checked for type, so a malformed file fails here
// instead of as a runtime error on the first webhook.
func Parse(data []byte) (*Config, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return nil, errors.New("config must be a JSON object at the top level")
		}
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if top == nil {
		return nil, errors.New("config must be a JSON object at the top level")
	}
	cfg := &Config{Teams: map[string]Team{}, ExcludedProjects: map[string]bool{}}

	if raw, ok := top["teams"]; ok && !isNull(raw) {
		var teams []json.RawMessage
		if err := json.Unmarshal(raw, &teams); err != nil {
			return nil, errors.New("`teams` must be a list")
		}
		for i, rawTeam := range teams {
			var t map[string]json.RawMessage
			if err := json.Unmarshal(rawTeam, &t); err != nil || t == nil {
				return nil, fmt.Errorf("teams[%d] must be an object", i)
			}
			name, ok := stringField(t, "name")
			if !ok || name == "" {
				return nil, errors.New("team is missing `name`")
			}
			if _, dup := cfg.Teams[name]; dup {
				return nil, fmt.Errorf("duplicate team name: %q", name)
			}
			email, _ := stringField(t, "group_email")
			if !validEmail(email) {
				return nil, fmt.Errorf("team %q has invalid `group_email`: %q", name, email)
			}
			cfg.Teams[name] = Team{Name: name, GroupEmail: email}
		}
	}

	if raw, ok := top["automation"]; ok && !isNull(raw) {
		entries, err := orderedObject(raw)
		if err != nil {
			return nil, errors.New("`automation` must be an object")
		}
		seen := map[string]bool{}
		for _, e := range entries {
			if seen[e.key] {
				return nil, fmt.Errorf("duplicate automation source: %q", e.key)
			}
			seen[e.key] = true
			var src map[string]json.RawMessage
			if err := json.Unmarshal(e.value, &src); err != nil || src == nil {
				return nil, fmt.Errorf("automation.%s must be an object", e.key)
			}
			authors, err := stringList(src, "authors")
			if err != nil {
				return nil, fmt.Errorf("automation.%s.authors %w", e.key, err)
			}
			prefixes, err := stringList(src, "branch_prefixes")
			if err != nil {
				return nil, fmt.Errorf("automation.%s.branch_prefixes %w", e.key, err)
			}
			// Branch names are lowercased at ingest, so the prefixes are
			// matched lowercased too; an uppercase prefix would never match.
			for i, p := range prefixes {
				prefixes[i] = strings.ToLower(p)
			}
			cfg.Automation = append(cfg.Automation, AutomationSource{Name: e.key, Authors: authors, BranchPrefixes: prefixes})
		}
	}

	env, err := parseEnvironments(top["environments"])
	if err != nil {
		return nil, err
	}
	cfg.Environments = env

	excluded, err := stringList(top, "excluded_projects")
	if err != nil {
		return nil, fmt.Errorf("`excluded_projects` %w", err)
	}
	for _, k := range excluded {
		// Bitbucket shows keys uppercase, ingest stores them lowercased
		// (repo_full_name): a key typed as Bitbucket shows it must match.
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			return nil, errors.New("`excluded_projects` entries must be non-empty strings")
		}
		cfg.ExcludedProjects[k] = true
	}

	jira, err := parseJiraBaseURL(top["jira"])
	if err != nil {
		return nil, err
	}
	cfg.JiraBaseURL = jira

	if cfg.Thresholds, err = parseThresholds(top["thresholds"]); err != nil {
		return nil, err
	}
	return cfg, nil
}

// parseJiraBaseURL reads the optional `jira` block. Bitbucket payloads name
// no Jira instance, so the link target has to be configured.
func parseJiraBaseURL(raw json.RawMessage) (string, error) {
	if raw == nil || isNull(raw) {
		return "", nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return "", errors.New("`jira` must be an object")
	}
	rawURL, ok := obj["base_url"]
	if !ok || isNull(rawURL) {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(rawURL, &s); err != nil {
		return "", errors.New("`jira.base_url` must be a string")
	}
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	u, err := url.Parse(s)
	// A bare `?` or `#` parses as an empty query or fragment and would
	// swallow /browse/<KEY>; userinfo would land in every href.
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" ||
		strings.ContainsAny(s, "?#") || u.User != nil {
		return "", fmt.Errorf("`jira.base_url` must be an absolute http(s) URL without credentials, query or fragment, got %q", s)
	}
	return u.String(), nil
}

func parseEnvironments(raw json.RawMessage) (Environments, error) {
	env := Environments{ProductionStage: DefaultProductionStage, IgnoredStages: map[string]bool{}}
	if raw == nil || isNull(raw) {
		return env, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return env, errors.New("`environments` must be an object")
	}
	if rawStage, ok := obj["production_stage"]; ok {
		var stage string
		if err := json.Unmarshal(rawStage, &stage); err != nil || strings.TrimSpace(stage) == "" {
			return env, errors.New("`environments.production_stage` must be a non-empty string")
		}
		env.ProductionStage = strings.ToLower(strings.TrimSpace(stage))
	}
	if rawIgnored, ok := obj["ignored_stages"]; ok {
		var items []json.RawMessage
		if err := json.Unmarshal(rawIgnored, &items); err != nil || isNull(rawIgnored) {
			return env, errors.New("`environments.ignored_stages` must be a list of strings")
		}
		for _, it := range items {
			var s string
			if err := json.Unmarshal(it, &s); err != nil || strings.TrimSpace(s) == "" {
				return env, errors.New("`environments.ignored_stages` entries must be non-empty strings")
			}
			env.IgnoredStages[strings.ToLower(strings.TrimSpace(s))] = true
		}
	}
	if env.IgnoredStages[env.ProductionStage] {
		return env, fmt.Errorf("`environments.production_stage` (%q) cannot also appear in `environments.ignored_stages`", env.ProductionStage)
	}
	return env, nil
}

// validEmail accepts an RFC 5322 address ("a@b.c" or "Team <a@b.c>") whose
// domain has a dot. group_email is contact metadata; nothing sends to it.
func validEmail(s string) bool {
	if s == "" {
		return false
	}
	addr, err := mail.ParseAddress(s)
	if err != nil {
		return false
	}
	_, domain, ok := strings.Cut(addr.Address, "@")
	return ok && strings.Contains(domain, ".")
}

// botShapedRE is the name-shape fallback for accounts nobody reports:
// `*-bot`, `*[bot]`, `bot-*`.
var botShapedRE = regexp.MustCompile(`(?i)^(.*-bot|.*\[bot\]|bot-.*)$`)

// LooksBotShaped reports whether a handle has the shape of a bot account.
func LooksBotShaped(handle string) bool {
	return handle != "" && botShapedRE.MatchString(handle)
}

// DetectAutomationSource classifies an event's author. Order, first match
// wins:
//
//  1. a configured source whose `authors` list names the login or the
//     display name (case-insensitive): a bot provisioned as an ordinary user
//     account is often only recognisable by its display name;
//  2. a configured source whose `branch_prefixes` prefixes the branch;
//  3. "service-account" when the git host flags the account SERVICE: a
//     stated fact beats a guess from the name;
//  4. "other-bot" when either name has a bot shape.
//
// Empty means human. Written at ingest (automation_source), so it stays
// what it was when the event came: a rule here that caught a person would
// do so for good. The read path adds AutomatedAtRead's name rule.
func (c *Config) DetectAutomationSource(author, displayName, branch string, serviceAccount bool) string {
	var handles []string
	for _, h := range []string{author, displayName} {
		if h != "" {
			handles = append(handles, h)
		}
	}
	for _, h := range handles {
		for _, src := range c.Automation {
			for _, known := range src.Authors {
				if strings.EqualFold(h, known) {
					return src.Name
				}
			}
		}
	}
	if branch != "" {
		for _, src := range c.Automation {
			for _, p := range src.BranchPrefixes {
				if strings.HasPrefix(branch, p) {
					return src.Name
				}
			}
		}
	}
	if serviceAccount {
		return "service-account"
	}
	for _, h := range handles {
		if LooksBotShaped(h) {
			return "other-bot"
		}
	}
	return ""
}

// AutomatedAtRead reports whether a commit's or PR opener's author is
// automation as the read path sees it: DetectAutomationSource without a
// branch or the SERVICE flag (a commit on master has neither), plus a
// configured source whose name is a whole word of either name (namedAfter).
// Read time only: the name rule is a guess, and fixing a config that
// guessed wrong corrects every number at once.
func (c *Config) AutomatedAtRead(author, displayName string) bool {
	if c.DetectAutomationSource(author, displayName, "", false) != "" {
		return true
	}
	return c.namedAfter([]string{author, displayName}) != ""
}

// BotBranch reports whether a branch, lowercased, starts with a configured
// source's branch_prefixes: a bot's branch, whose CI runs are no build of
// the team's.
func (c *Config) BotBranch(branch string) bool {
	return slices.ContainsFunc(c.BotPrefixes(), func(p string) bool { return strings.HasPrefix(branch, p) })
}

// BotPrefixes are every configured source's non-empty branch_prefixes,
// lowercased as loaded: the read path's bot branches (dataset.Params).
func (c *Config) BotPrefixes() []string {
	out := []string{}
	for _, src := range c.Automation {
		for _, p := range src.BranchPrefixes {
			if p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// MinSourceNameLetters is the fewest letters a source name needs to name a
// bot by its words (namedAfter): a shorter one is likely a word of a
// person's name too.
const MinSourceNameLetters = 4

// ShortSourceNames are the configured sources whose name has fewer than
// MinSourceNameLetters letters: their name catches every person with that
// word in a name. A warning at load, not a refusal: the name may be meant.
func (c *Config) ShortSourceNames() []string {
	var out []string
	for _, src := range c.Automation {
		if n := len([]rune(strings.Join(letterWords(src.Name), ""))); n > 0 && n < MinSourceNameLetters {
			out = append(out, src.Name)
		}
	}
	return out
}

// namedAfter is the configured source whose name is a whole word of one of
// the handles: a dependency bot run under an ordinary user account is named
// after its tool and the team it serves ("renovate_kasse", "Renovate
// Kasse"), without a bot shape, and its pushes to the trunk would read as a
// person's. Words split on every non-letter, so a person whose name merely
// holds the tool's letters inside a longer word is none; a source name of
// several words matches them in a row. A source named like an ordinary word
// would catch people of that name: name it after its tool
// (ShortSourceNames warns of the likeliest).
func (c *Config) namedAfter(handles []string) string {
	for _, h := range handles {
		if h == "" {
			continue
		}
		words := letterWords(h)
		for _, src := range c.Automation {
			if name := letterWords(src.Name); len(name) > 0 && containsRun(words, name) {
				return src.Name
			}
		}
	}
	return ""
}

// letterWords splits s into its runs of letters, lowercased.
func letterWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) })
}

// containsRun reports whether run occurs in words as consecutive words.
func containsRun(words, run []string) bool {
	for i := 0; i+len(run) <= len(words); i++ {
		if slices.Equal(words[i:i+len(run)], run) {
			return true
		}
	}
	return false
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func stringField(obj map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := obj[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// stringList reads an optional list of strings; absent or null is empty.
func stringList(obj map[string]json.RawMessage, key string) ([]string, error) {
	raw, ok := obj[key]
	if !ok || isNull(raw) {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("must be a list of strings")
	}
	for _, s := range out {
		if s == "" {
			return nil, errors.New("entries must be non-empty strings")
		}
	}
	return out, nil
}

type entry struct {
	key   string
	value json.RawMessage
}

// orderedObject decodes a JSON object keeping key order: automation sources
// are matched in file order, and a Go map would shuffle them.
func orderedObject(raw json.RawMessage) ([]entry, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not an object")
	}
	var out []entry
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, entry{key: key, value: v})
	}
	if _, err := dec.Token(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return out, nil
}
