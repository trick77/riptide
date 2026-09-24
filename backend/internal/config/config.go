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
	"regexp"
	"sort"
	"strings"
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
	cfg := &Config{Teams: map[string]Team{}}

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
	return cfg, nil
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
// Empty means human.
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
