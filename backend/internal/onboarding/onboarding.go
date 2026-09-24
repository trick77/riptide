// Package onboarding is `riptide onboard-bitbucket`: it creates, updates or
// removes the riptide webhook on Bitbucket Data Center repositories.
//
// The webhook is HMAC-signed: `configuration.secret` is the team's bitbucket
// key, and Bitbucket sends `X-Hub-Signature: sha256=<hex>`. Basic auth
// through the webhook's `credentials` block does not work over REST (Bitbucket
// DC drops `credentials.password` on POST/PUT), so it is not offered. The
// webhook URL is `<webhook_url>/<team>`, because riptide identifies the
// caller by path before it checks the signature.
package onboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultWebhookName is the name the webhook is found by.
const DefaultWebhookName = "riptide"

// RequiredEvents are the events riptide extracts data from: PR lifecycle,
// reviewer activity (all five, since reviewers often approve silently and a
// comment-only signal misses them), `pr:modified` (only its draft->ready flip
// is kept, as the pickup-clock start) and pushes.
var RequiredEvents = []string{
	"pr:opened",
	"pr:modified",
	"pr:from_ref_updated",
	"pr:comment:added",
	"pr:reviewer:approved",
	"pr:reviewer:unapproved",
	"pr:reviewer:needs_work",
	"pr:reviewer:updated",
	"pr:merged",
	"pr:declined",
	"pr:deleted",
	"repo:refs_changed",
}

// RepoSpec is one target repository.
type RepoSpec struct {
	Project, Repo string
}

// Key is `PROJECT/repo`.
func (r RepoSpec) Key() string { return r.Project + "/" + r.Repo }

// Input is the onboarding JSON file.
type Input struct {
	BitbucketURL string
	WebhookURL   string
	Team         string
	Repos        []RepoSpec
}

// LoadInput reads and validates the onboarding JSON.
func LoadInput(path string) (*Input, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-supplied path
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	return ParseInput(data)
}

// ParseInput validates the onboarding JSON.
func ParseInput(data []byte) (*Input, error) {
	var raw struct {
		BitbucketURL any `json:"bitbucket_url"`
		WebhookURL   any `json:"webhook_url"`
		Team         any `json:"team"`
		Projects     any `json:"projects"`
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil || top == nil {
		return nil, errors.New("config root must be a JSON object")
	}
	_ = json.Unmarshal(data, &raw)

	bb, err := requireURL(raw.BitbucketURL, "bitbucket_url", true)
	if err != nil {
		return nil, err
	}
	hook, err := requireURL(raw.WebhookURL, "webhook_url", false)
	if err != nil {
		return nil, err
	}
	team, ok := raw.Team.(string)
	if !ok || strings.TrimSpace(team) == "" {
		return nil, errors.New("'team' must be a non-empty string")
	}
	projects, ok := raw.Projects.([]any)
	if !ok || len(projects) == 0 {
		return nil, errors.New("'projects' must be a non-empty list")
	}
	in := &Input{BitbucketURL: strings.TrimRight(bb, "/"), WebhookURL: hook, Team: strings.TrimSpace(team)}
	seen := map[RepoSpec]bool{}
	for i, p := range projects {
		entry, ok := p.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("projects[%d] must be an object", i)
		}
		project, ok := entry["project"].(string)
		if !ok || strings.TrimSpace(project) == "" {
			return nil, fmt.Errorf("projects[%d].project must be a non-empty string", i)
		}
		repos, ok := entry["repos"].([]any)
		if !ok || len(repos) == 0 {
			return nil, fmt.Errorf("projects[%d].repos must be a non-empty list of repo slugs", i)
		}
		for j, r := range repos {
			slug, ok := r.(string)
			if !ok || strings.TrimSpace(slug) == "" {
				return nil, fmt.Errorf("projects[%d].repos[%d] must be a non-empty string", i, j)
			}
			spec := RepoSpec{Project: strings.TrimSpace(project), Repo: strings.TrimSpace(slug)}
			if seen[spec] {
				return nil, fmt.Errorf("duplicate repo entry: %s", spec.Key())
			}
			seen[spec] = true
			in.Repos = append(in.Repos, spec)
		}
	}
	return in, nil
}

func requireURL(v any, field string, httpsOnly bool) (string, error) {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("'%s' must be a non-empty string", field)
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "https" && (httpsOnly || u.Scheme != "http")) {
		if httpsOnly {
			return "", fmt.Errorf("'%s' must be an https URL", field)
		}
		return "", fmt.Errorf("'%s' must be an http(s) URL", field)
	}
	return s, nil
}

// HTTPError is a non-2xx answer from Bitbucket.
type HTTPError struct {
	Status int
	Body   string
	URL    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d for %s: %s", e.Status, e.URL, truncate(e.Body, 200))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Client is a minimal Bitbucket Data Center REST client.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// defaultHTTP is the transport every client uses; tests swap it for one that
// trusts their TLS server.
var defaultHTTP = &http.Client{Timeout: 30 * time.Second}

// NewClient builds a client with a 30 s timeout.
func NewClient(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: defaultHTTP}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &HTTPError{Status: resp.StatusCode, Body: string(data), URL: u}
	}
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func repoPath(p RepoSpec) string {
	return "/rest/api/1.0/projects/" + url.PathEscape(p.Project) + "/repos/" + url.PathEscape(p.Repo)
}

// Webhook is the part of a Bitbucket webhook the onboarder reads.
type Webhook struct {
	ID     int64    `json:"id"`
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Events []string `json:"events"`
	Active *bool    `json:"active"`
}

type page[T any] struct {
	Values        []T   `json:"values"`
	IsLastPage    *bool `json:"isLastPage"`
	NextPageStart *int  `json:"nextPageStart"`
}

// paginate walks a paged list until isLastPage or a non-advancing cursor.
func paginate[T any](ctx context.Context, c *Client, path string, query url.Values) ([]T, error) {
	var out []T
	start := 0
	for {
		q := url.Values{}
		for k, v := range query {
			q[k] = v
		}
		q.Set("start", strconv.Itoa(start))
		q.Set("limit", "100")
		var p page[T]
		if err := c.do(ctx, http.MethodGet, path, q, nil, &p); err != nil {
			return nil, err
		}
		out = append(out, p.Values...)
		if p.IsLastPage == nil || *p.IsLastPage || p.NextPageStart == nil || *p.NextPageStart <= start {
			return out, nil
		}
		start = *p.NextPageStart
	}
}

// ListWebhooks returns every webhook on the repo.
func (c *Client) ListWebhooks(ctx context.Context, p RepoSpec) ([]Webhook, error) {
	return paginate[Webhook](ctx, c, repoPath(p)+"/webhooks", nil)
}

// ListAdminRepos returns every repo the token's user administers.
func (c *Client) ListAdminRepos(ctx context.Context) ([]RepoSpec, error) {
	type repo struct {
		Slug    string `json:"slug"`
		Project struct {
			Key string `json:"key"`
		} `json:"project"`
	}
	repos, err := paginate[repo](ctx, c, "/rest/api/1.0/repos", url.Values{"permission": {"REPO_ADMIN"}})
	if err != nil {
		return nil, err
	}
	out := make([]RepoSpec, 0, len(repos))
	for _, r := range repos {
		if r.Project.Key != "" && r.Slug != "" {
			out = append(out, RepoSpec{Project: r.Project.Key, Repo: r.Slug})
		}
	}
	return out, nil
}

// Result is the outcome for one repository.
type Result struct {
	Repo   RepoSpec
	Status string // ok | failed | skipped
	Detail string
	Diff   []string
}

// Onboarder applies the webhook to repositories.
type Onboarder struct {
	Client     *Client
	WebhookURL string
	Team       string
	TeamKey    string
	Name       string
	DryRun     bool
	Log        func(format string, args ...any)
}

func (o *Onboarder) logf(format string, args ...any) {
	if o.Log != nil {
		o.Log(format, args...)
	}
}

func (o *Onboarder) teamWebhookURL() string {
	return strings.TrimRight(o.WebhookURL, "/") + "/" + o.Team
}

func (o *Onboarder) body() map[string]any {
	return map[string]any{
		"name":                    o.Name,
		"url":                     o.teamWebhookURL(),
		"active":                  true,
		"events":                  RequiredEvents,
		"configuration":           map[string]any{"secret": o.TeamKey},
		"sslVerificationRequired": true,
	}
}

func redacted(body map[string]any) string {
	cp := map[string]any{}
	for k, v := range body {
		cp[k] = v
	}
	cp["configuration"] = map[string]any{"secret": "***"}
	b, _ := json.Marshal(cp)
	return string(b)
}

// diff lists what differs between the existing webhook and the one wanted.
// It always includes the secret: Bitbucket redacts it on read-back, so it is
// rewritten on every run rather than left possibly stale.
func (o *Onboarder) diff(existing Webhook) []string {
	var d []string
	if want := o.teamWebhookURL(); existing.URL != want {
		d = append(d, fmt.Sprintf("url: %q -> %q", existing.URL, want))
	}
	have := map[string]bool{}
	for _, e := range existing.Events {
		have[e] = true
	}
	want := map[string]bool{}
	var missing, extra []string
	for _, e := range RequiredEvents {
		want[e] = true
		if !have[e] {
			missing = append(missing, e)
		}
	}
	for e := range have {
		if !want[e] {
			extra = append(extra, e)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		d = append(d, fmt.Sprintf("events: missing=%v extra=%v", missing, extra))
	}
	if existing.Active != nil && !*existing.Active {
		d = append(d, "active: false -> true")
	}
	return append(d, "configuration.secret: (rewriting, Bitbucket redacts it on read-back)")
}

func find(hooks []Webhook, name string) *Webhook {
	for i := range hooks {
		if hooks[i].Name == name {
			return &hooks[i]
		}
	}
	return nil
}

// Onboard checks read access, then creates or updates the webhook. The
// pull-request list call proves PR-read scope, which some roles lack even
// with repo-read; webhook write scope shows as a 403 on the write.
func (o *Onboarder) Onboard(ctx context.Context, p RepoSpec) Result {
	if err := o.Client.do(ctx, http.MethodGet, repoPath(p), nil, nil, nil); err != nil {
		return failed(p, "permission check", err)
	}
	if err := o.Client.do(ctx, http.MethodGet, repoPath(p)+"/pull-requests", url.Values{"limit": {"1"}}, nil, nil); err != nil {
		return failed(p, "permission check", err)
	}
	o.logf("[%s] read permissions OK", p.Key())

	hooks, err := o.Client.ListWebhooks(ctx, p)
	if err != nil {
		return failed(p, "upsert webhook", err)
	}
	body := o.body()
	existing := find(hooks, o.Name)
	if existing == nil {
		o.logf("[%s] creating webhook %q", p.Key(), o.Name)
		if o.DryRun {
			o.logf("[%s] DRY-RUN body=%s", p.Key(), redacted(body))
			return Result{Repo: p, Status: "ok", Detail: "dry-run", Diff: []string{"create"}}
		}
		if err := o.Client.do(ctx, http.MethodPost, repoPath(p)+"/webhooks", nil, body, nil); err != nil {
			return failed(p, "upsert webhook", err)
		}
		return Result{Repo: p, Status: "ok", Detail: "webhook configured", Diff: []string{"create"}}
	}
	d := o.diff(*existing)
	o.logf("[%s] updating webhook id=%d changes=%v", p.Key(), existing.ID, d)
	if o.DryRun {
		o.logf("[%s] DRY-RUN body=%s", p.Key(), redacted(body))
		return Result{Repo: p, Status: "ok", Detail: "dry-run", Diff: d}
	}
	path := repoPath(p) + "/webhooks/" + strconv.FormatInt(existing.ID, 10)
	if err := o.Client.do(ctx, http.MethodPut, path, nil, body, nil); err != nil {
		return failed(p, "upsert webhook", err)
	}
	return Result{Repo: p, Status: "ok", Detail: "webhook configured", Diff: d}
}

// Remove deletes the webhook; a repo without one is skipped.
func (o *Onboarder) Remove(ctx context.Context, p RepoSpec) Result {
	hooks, err := o.Client.ListWebhooks(ctx, p)
	if err != nil {
		return failed(p, "list webhooks", err)
	}
	existing := find(hooks, o.Name)
	if existing == nil {
		o.logf("[%s] no %q webhook found, nothing to remove", p.Key(), o.Name)
		return Result{Repo: p, Status: "skipped", Detail: fmt.Sprintf("no %q webhook found", o.Name)}
	}
	if o.DryRun {
		o.logf("[%s] DRY-RUN would delete webhook id=%d", p.Key(), existing.ID)
		return Result{Repo: p, Status: "ok", Detail: fmt.Sprintf("dry-run: would remove webhook id=%d", existing.ID)}
	}
	path := repoPath(p) + "/webhooks/" + strconv.FormatInt(existing.ID, 10)
	if err := o.Client.do(ctx, http.MethodDelete, path, nil, nil, nil); err != nil {
		return failed(p, "delete webhook", err)
	}
	o.logf("[%s] removed webhook id=%d", p.Key(), existing.ID)
	return Result{Repo: p, Status: "ok", Detail: fmt.Sprintf("webhook removed: id=%d", existing.ID)}
}

func failed(p RepoSpec, step string, err error) Result {
	var he *HTTPError
	if errors.As(err, &he) {
		return Result{Repo: p, Status: "failed", Detail: fmt.Sprintf("%s HTTP %d: %s", step, he.Status, truncate(he.Body, 200))}
	}
	return Result{Repo: p, Status: "failed", Detail: fmt.Sprintf("%s: %v", step, err)}
}
