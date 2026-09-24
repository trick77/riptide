package config

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Sources a team key can be registered under. A leaked secret is scoped to
// one source: an argocd key cannot authenticate /webhooks/pipeline.
const (
	SourceBitbucket = "bitbucket"
	SourceArgoCD    = "argocd"
	SourceJenkins   = "jenkins"
	SourceNoergler  = "noergler"
)

var knownSources = map[string]bool{
	SourceBitbucket: true, SourceArgoCD: true, SourceJenkins: true, SourceNoergler: true,
}

// Keys is a parsed team-keys.json: team -> source -> raw secret. The file is
// mounted from a Secret and never committed. The bearer IS the team identity.
type Keys struct {
	teams map[string]map[string]string
	names []string // sorted, computed once: lookups run per request
}

// ParseKeys validates the file contents: a non-empty object of known source
// to non-empty token per team, and no token shared by two teams, where it
// would make the caller's identity ambiguous.
func ParseKeys(data []byte) (*Keys, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil || top == nil {
		var typeErr *json.UnmarshalTypeError
		if err == nil || errors.As(err, &typeErr) {
			return nil, errors.New("team-keys file must be a JSON object at the top level")
		}
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	k := &Keys{teams: map[string]map[string]string{}}
	owner := map[string]string{}
	for _, team := range sortedKeys(top) {
		if team == "" {
			return nil, errors.New("team key must be a non-empty string")
		}
		var sources map[string]json.RawMessage
		if err := json.Unmarshal(top[team], &sources); err != nil || len(sources) == 0 {
			return nil, fmt.Errorf("team %q value must be a non-empty object of source → raw token", team)
		}
		k.teams[team] = map[string]string{}
		for _, source := range sortedKeys(sources) {
			if !knownSources[source] {
				return nil, fmt.Errorf("team %q has unknown source %q; allowed: [argocd bitbucket jenkins noergler]", team, source)
			}
			var token string
			if err := json.Unmarshal(sources[source], &token); err != nil || token == "" {
				return nil, fmt.Errorf("team %q source %q must be a non-empty string", team, source)
			}
			if other, dup := owner[token]; dup && other != team {
				return nil, fmt.Errorf("teams %q and %q share a token; every team needs its own", other, team)
			}
			owner[token] = team
			k.teams[team][source] = token
		}
	}
	k.names = sortedKeys(k.teams)
	return k, nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TeamNames returns the teams that have keys, sorted. Callers must not
// modify the slice.
func (k *Keys) TeamNames() []string { return k.names }

// Has reports whether team has an entry at all.
func (k *Keys) Has(team string) bool {
	_, ok := k.teams[team]
	return ok
}

// Secret returns team's secret for source.
func (k *Keys) Secret(team, source string) (string, bool) {
	s, ok := k.teams[team][source]
	return s, ok
}

// Lookup returns the team whose source secret equals token. Every candidate
// is compared in constant time and the loop never exits early, so the wall
// time does not tell which team came closest. Comparison is on bytes: a
// non-ASCII token is simply a mismatch.
func (k *Keys) Lookup(token, source string) (string, bool) {
	if token == "" || !knownSources[source] {
		return "", false
	}
	match := ""
	for _, team := range k.names {
		stored, ok := k.teams[team][source]
		if !ok {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(stored)) == 1 {
			match = team
		}
	}
	return match, match != ""
}

// LookupAnySource returns the team for which token matches any of its
// secrets. /auth/ping uses it: the caller proves it holds one of its team's
// secrets, whichever.
func (k *Keys) LookupAnySource(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	match := ""
	for _, team := range k.names {
		for _, stored := range k.teams[team] {
			if subtle.ConstantTimeCompare([]byte(token), []byte(stored)) == 1 {
				match = team
			}
		}
	}
	return match, match != ""
}

// CrossValidate checks a config against a key file: every configured team
// needs keys. It returns the teams that have keys but no config entry, which
// is allowed (a key can land before its config entry) but worth a warning.
func CrossValidate(cfg *Config, keys *Keys) (extra []string, err error) {
	var missing []string
	for _, t := range cfg.TeamNames() {
		if !keys.Has(t) {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("team-keys file is missing entries for teams in the config: %v", missing)
	}
	for _, t := range keys.TeamNames() {
		if _, ok := cfg.Teams[t]; !ok {
			extra = append(extra, t)
		}
	}
	return extra, nil
}
