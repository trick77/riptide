package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/httpapi"
	"github.com/trick77/riptide/internal/onboarding"
)

// Onboarding is what POST /onboard needs; either URL empty switches it off.
type Onboarding struct {
	// PublicURL is riptide's base URL as senders reach it.
	PublicURL string
	// BitbucketURL is the Bitbucket Data Center base URL.
	BitbucketURL string
	// HTTP is the client for Bitbucket; nil is the onboarding default.
	HTTP *http.Client
}

// onboardRequest is the body. Decoded strictly: an unknown field is an
// operator's typo, refused rather than dropped.
type onboardRequest struct {
	Action   string           `json:"action"`
	Projects []onboardProject `json:"projects"`
	DryRun   bool             `json:"dry_run"`
}

type onboardProject struct {
	Key   string    `json:"key"`
	Repos *[]string `json:"repos"`
}

type onboardResponse struct {
	Team       string           `json:"team"`
	Action     string           `json:"action"`
	DryRun     bool             `json:"dry_run"`
	WebhookURL string           `json:"webhook_url"`
	Healthy    bool             `json:"healthy"`
	Rows       []onboarding.Row `json:"rows"`
	Text       string           `json:"text"`
}

// onboardTimeout bounds one request's Bitbucket work, short of the route's
// 300 s in front (infra) so riptide answers before the router cuts.
const onboardTimeout = 270 * time.Second

// onboard puts the team's Bitbucket webhook on the projects and repos in the
// body, reports it or removes it. Authenticated by the team's bitbucket key
// as a bearer; the Bitbucket calls run on the caller's own token
// (X-Bitbucket-Token), used for this request only, never logged or stored.
// A Bitbucket failure is a row, not an error status: the other targets
// still run.
func (d Deps) onboard(w http.ResponseWriter, r *http.Request) {
	team, ok := d.bearer(w, r, config.SourceBitbucket)
	if !ok {
		return
	}
	// The bearer just proved equal to the team's bitbucket key, the hooks'
	// HMAC secret: no second lookup a key reload could answer with "".
	secret, _ := bearerToken(r)
	// Same answer as a wrong key: a valid key does not reveal other teams.
	if team != r.PathValue("team") {
		httpapi.WriteDetail(w, http.StatusUnauthorized, "Invalid credentials.")
		return
	}
	if d.Onboarding.PublicURL == "" || d.Onboarding.BitbucketURL == "" {
		httpapi.WriteDetail(w, http.StatusServiceUnavailable,
			"onboarding is off on this instance: BACKEND_PUBLIC_URL and BACKEND_BITBUCKET_URL must be set")
		return
	}
	token := strings.TrimSpace(r.Header.Get("X-Bitbucket-Token"))
	if token == "" {
		httpapi.WriteDetail(w, http.StatusBadRequest,
			"X-Bitbucket-Token required: your own Bitbucket HTTP access token with admin on the projects or repos")
		return
	}
	body, ok := readBody(w, r, maxOwnedBody)
	if !ok {
		return
	}
	req, targets, err := parseOnboard(body)
	if err != nil {
		httpapi.WriteDetail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	o := &onboarding.Onboarder{
		Client:     onboarding.NewClient(d.Onboarding.BitbucketURL, token, d.Onboarding.HTTP),
		Team:       team,
		WebhookURL: d.Onboarding.PublicURL + "/webhooks/bitbucket/" + team,
		Secret:     secret,
		DryRun:     req.DryRun,
		Log:        d.Log,
	}
	// Not the request's context: the collector's ReadTimeout cancels it 60 s
	// in, and a project of a hundred repos takes longer. Its own deadline,
	// so a run is never cut halfway by a gone caller or that timeout.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), onboardTimeout)
	defer cancel()
	rows, healthy := onboarding.Run(ctx, o, onboarding.Action(req.Action), targets)
	d.Log.InfoContext(r.Context(), "onboard_processed", "team", team, "action", req.Action,
		"dry_run", req.DryRun, "targets", len(targets), "healthy", healthy)
	httpapi.WriteJSON(w, http.StatusOK, onboardResponse{
		Team: team, Action: req.Action, DryRun: req.DryRun, WebhookURL: o.WebhookURL,
		Healthy: healthy, Rows: rows, Text: onboarding.Render(rows),
	})
}

// parseOnboard decodes and checks the body: the action (status when absent),
// at least one project, each key set, repos when given not empty, no target
// twice. A project without repos is one project target.
func parseOnboard(body []byte) (onboardRequest, []onboarding.Target, error) {
	var req onboardRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, nil, fmt.Errorf("invalid body: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return req, nil, errors.New("invalid body: one JSON object expected")
	}
	if req.Action == "" {
		req.Action = string(onboarding.ActionStatus)
	}
	if !slices.Contains(onboarding.Actions, onboarding.Action(req.Action)) {
		names := make([]string, len(onboarding.Actions))
		for i, a := range onboarding.Actions {
			names[i] = string(a)
		}
		return req, nil, fmt.Errorf("action must be one of %s", strings.Join(names, ", "))
	}
	if len(req.Projects) == 0 {
		return req, nil, errors.New("projects must list at least one project")
	}
	var targets []onboarding.Target
	seen := map[onboarding.Target]bool{}
	add := func(t onboarding.Target) error {
		if seen[t] {
			return fmt.Errorf("%s is listed twice", t.Key())
		}
		seen[t] = true
		targets = append(targets, t)
		return nil
	}
	for i, p := range req.Projects {
		key := strings.TrimSpace(p.Key)
		if key == "" {
			return req, nil, fmt.Errorf("projects[%d].key must not be empty", i)
		}
		if p.Repos == nil {
			if err := add(onboarding.Target{Project: key}); err != nil {
				return req, nil, err
			}
			continue
		}
		if len(*p.Repos) == 0 {
			return req, nil, fmt.Errorf("projects[%d].repos must not be empty when given; leave it out for the whole project", i)
		}
		for j, slug := range *p.Repos {
			slug = strings.TrimSpace(slug)
			if slug == "" {
				return req, nil, fmt.Errorf("projects[%d].repos[%d] must not be empty", i, j)
			}
			if err := add(onboarding.Target{Project: key, Repo: slug}); err != nil {
				return req, nil, err
			}
		}
	}
	// A repo of a project also listed whole: the project's webhook covers
	// it, and onboarding both would create a repo hook only to prune it.
	for _, t := range targets {
		if !t.IsProject() && seen[onboarding.Target{Project: t.Project}] {
			return req, nil, fmt.Errorf("%s: %s is listed whole too, its project webhook covers the repo", t.Key(), t.Project)
		}
	}
	return req, targets, nil
}
