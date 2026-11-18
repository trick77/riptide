// Package onboarding puts riptide's webhook on Bitbucket Data Center projects
// and repositories, reports its state and takes it off again: the domain
// behind POST /onboard/{team}. No HTTP handler here.
//
// A project target gets one project webhook, which covers every repo in it,
// new ones included. A repo target gets a repo webhook: for a personal
// project (~USER) or a project only partly the team's. A repo hook next to the
// project hook would deliver every event twice (each delivery has its own id,
// so nothing dedupes them): onboarding a project prunes the team's repo hooks
// below it, and a repo target under a project hook is skipped.
//
// The webhook is HMAC-signed: `configuration.secret` is the team's bitbucket
// key, and Bitbucket sends `X-Hub-Signature: sha256=<hex>`. Basic auth
// through the webhook's `credentials` block does not work over REST (Bitbucket
// DC drops `credentials.password` on POST/PUT), so it is not offered.
//
// Every Bitbucket call runs on the caller's own token, for one request only.
package onboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WebhookName is the name riptide's hook is found by.
const WebhookName = "riptide"

// RequiredEvents are the events riptide extracts data from: PR lifecycle,
// reviewer activity (all five, since reviewers often approve silently and a
// comment-only signal misses them), `pr:modified` (only its draft flips are
// kept) and pushes.
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

// repoHookConcurrency bounds the repo hook listings in flight under one
// project: one listing per repo, and a project has dozens.
const repoHookConcurrency = 4

// Action is what Run does with each target.
type Action string

const (
	// ActionStatus reports each target's webhook, writing nothing.
	ActionStatus Action = "status"
	// ActionOnboard creates or updates the webhook, and under a project
	// prunes the team's repo hooks.
	ActionOnboard Action = "onboard"
	// ActionRemove deletes the team's webhook from the target.
	ActionRemove Action = "remove"
)

// Target is a project (Repo == "": one project webhook) or one repo.
type Target struct {
	Project, Repo string
}

// IsProject reports a whole-project target.
func (t Target) IsProject() bool { return t.Repo == "" }

// Key is `PROJECT` or `PROJECT/repo`.
func (t Target) Key() string {
	if t.IsProject() {
		return t.Project
	}
	return t.Project + "/" + t.Repo
}

// Level is "project" or "repo".
func (t Target) Level() string {
	if t.IsProject() {
		return "project"
	}
	return "repo"
}

// Row is one target's outcome.
type Row struct {
	Target string `json:"target"`
	Level  string `json:"level"`
	// Status: with status ok, stale, missing, foreign, double or failed;
	// with onboard and remove ok, skipped or failed.
	Status string   `json:"status"`
	Detail string   `json:"detail"`
	Diff   []string `json:"diff"`
	// RepoHooks: under a project target, the repos with a hook of the
	// team's own besides the project hook: to prune (status) or pruned
	// (onboard).
	RepoHooks []string `json:"repo_hooks"`
	// Foreign: hooks named riptide that deliver to another team, left alone.
	Foreign []string `json:"foreign"`
}

func row(t Target, status, detail string) Row {
	return Row{Target: t.Key(), Level: t.Level(), Status: status, Detail: detail,
		Diff: []string{}, RepoHooks: []string{}, Foreign: []string{}}
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

// status is err's HTTP status, 0 for a transport failure.
func status(err error) int {
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status
	}
	return 0
}

// detail renders a failed step for a row: the status and the start of
// Bitbucket's answer, never the URL (it carries nothing the row lacks).
func detail(step string, err error) string {
	var he *HTTPError
	if errors.As(err, &he) {
		return fmt.Sprintf("%s: HTTP %d: %s", step, he.Status, truncate(strings.TrimSpace(he.Body), 200))
	}
	return fmt.Sprintf("%s: %v", step, err)
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

// defaultHTTP: 30 s per call. The default transport honours the proxy
// environment and SSL_CERT_FILE.
var defaultHTTP = &http.Client{Timeout: 30 * time.Second}

// NewClient builds a client; hc nil is the 30 s default.
func NewClient(baseURL, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = defaultHTTP
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: hc}
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

func projectPath(project string) string {
	return "/rest/api/1.0/projects/" + url.PathEscape(project)
}

func hooksPath(t Target) string {
	if t.IsProject() {
		return projectPath(t.Project) + "/webhooks"
	}
	return projectPath(t.Project) + "/repos/" + url.PathEscape(t.Repo) + "/webhooks"
}

func hookPath(t Target, id int64) string {
	return hooksPath(t) + "/" + strconv.FormatInt(id, 10)
}

// Webhook is the part of a Bitbucket webhook riptide reads. Bitbucket
// redacts the secret on read-back, so it is not here.
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
func paginate[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var out []T
	start := 0
	for {
		q := url.Values{"start": {strconv.Itoa(start)}, "limit": {"100"}}
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

// ListWebhooks returns every webhook on the target.
func (c *Client) ListWebhooks(ctx context.Context, t Target) ([]Webhook, error) {
	return paginate[Webhook](ctx, c, hooksPath(t))
}

// ListRepos returns the slugs of a project's repos, in Bitbucket's order.
func (c *Client) ListRepos(ctx context.Context, project string) ([]string, error) {
	type repo struct {
		Slug string `json:"slug"`
	}
	repos, err := paginate[repo](ctx, c, projectPath(project)+"/repos")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(repos))
	for _, r := range repos {
		if r.Slug != "" {
			out = append(out, r.Slug)
		}
	}
	return out, nil
}

// Onboarder applies one team's webhook, for one request.
type Onboarder struct {
	Client *Client
	Team   string
	// WebhookURL is the team's: `<public url>/webhooks/bitbucket/<team>`.
	WebhookURL string
	// Secret is the team's bitbucket key, the hook's HMAC secret.
	Secret string
	DryRun bool
	Log    *slog.Logger

	// projectHooks caches projectHook per project for the request: repo
	// targets of one project ask once. Run is sequential, no lock.
	projectHooks map[string]*Webhook
}

func (o *Onboarder) log() *slog.Logger {
	if o.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return o.Log
}

// ours reports a hook of this team's: named riptide and delivering to this
// team's path, on whatever host and under whatever prefix. A host move
// (riptide-collector to riptide, 2026-11-14) left every hook on the old
// host, failing for days: matching on the host would call those foreign and
// never move them. A hook named riptide for another team is foreign: a
// shared project.
func (o *Onboarder) ours(h Webhook) bool {
	if h.Name != WebhookName {
		return false
	}
	u, err := url.Parse(h.URL)
	return err == nil && strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/webhooks/bitbucket/"+o.Team)
}

// find picks the team's riptide hook among hooks, else another team's (or
// nil): Bitbucket does not keep names unique, and two teams on one shared
// repo both name theirs riptide.
func (o *Onboarder) find(hooks []Webhook) (own, foreign *Webhook) {
	for i := range hooks {
		switch {
		case o.ours(hooks[i]):
			return &hooks[i], nil
		case hooks[i].Name == WebhookName && foreign == nil:
			foreign = &hooks[i]
		}
	}
	return nil, foreign
}

func (o *Onboarder) body() map[string]any {
	return map[string]any{
		"name":                    WebhookName,
		"url":                     o.WebhookURL,
		"active":                  true,
		"events":                  RequiredEvents,
		"configuration":           map[string]any{"secret": o.Secret},
		"sslVerificationRequired": true,
	}
}

// diff lists what an existing hook of the team's lacks. The secret is not
// compared: Bitbucket never returns it, so onboard rewrites it every time.
func (o *Onboarder) diff(h Webhook) []string {
	d := []string{}
	if h.URL != o.WebhookURL {
		d = append(d, fmt.Sprintf("url: %s -> %s", h.URL, o.WebhookURL))
	}
	var missing, extra []string
	for _, e := range RequiredEvents {
		if !slices.Contains(h.Events, e) {
			missing = append(missing, e)
		}
	}
	for _, e := range h.Events {
		if !slices.Contains(RequiredEvents, e) {
			extra = append(extra, e)
		}
	}
	slices.Sort(missing)
	slices.Sort(extra)
	if len(missing) > 0 {
		d = append(d, "events missing: "+strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		d = append(d, "events extra: "+strings.Join(extra, ", "))
	}
	if h.Active != nil && !*h.Active {
		d = append(d, "active: false -> true")
	}
	return d
}

// projectHook is the team's hook on t's project, nil when there is none or
// the project's hooks cannot be read: no project admin (401, 403), or a
// project without project hooks (a personal ~USER one may answer 400 or
// 404). Then there is nothing to check.
func (o *Onboarder) projectHook(ctx context.Context, t Target) (*Webhook, error) {
	if h, ok := o.projectHooks[t.Project]; ok {
		return h, nil
	}
	hooks, err := o.Client.ListWebhooks(ctx, Target{Project: t.Project})
	if err != nil {
		switch status(err) {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			hooks = nil
		default:
			return nil, err
		}
	}
	own, _ := o.find(hooks)
	if o.projectHooks == nil {
		o.projectHooks = map[string]*Webhook{}
	}
	o.projectHooks[t.Project] = own
	return own, nil
}

type repoHook struct {
	repo Target
	hook Webhook
}

// repoHooks lists, under a project, the repos' hooks named riptide: the
// team's (stray next to a project hook) and other teams' (foreign). In
// Bitbucket's repo order: the listings run concurrently, the result does not
// depend on which answered first. A repo gone since the repo list (404) has
// none; any other failure stops the listings still to run.
func (o *Onboarder) repoHooks(ctx context.Context, project string) (stray []repoHook, foreign []string, err error) {
	slugs, err := o.Client.ListRepos(ctx, project)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	own := make([]*Webhook, len(slugs))
	other := make([]*Webhook, len(slugs))
	errs := make([]error, len(slugs))
	sem := make(chan struct{}, repoHookConcurrency)
	var wg sync.WaitGroup
	for i, slug := range slugs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			hooks, err := o.Client.ListWebhooks(ctx, Target{Project: project, Repo: slug})
			if err != nil && status(err) != http.StatusNotFound {
				errs[i] = err
				cancel()
				return
			}
			own[i], other[i] = o.find(hooks)
		})
	}
	wg.Wait()
	// The first real failure in repo order; the listings it cancelled
	// failed for that reason only.
	var first error
	for _, err := range errs {
		if err != nil && (first == nil || errors.Is(first, context.Canceled)) {
			first = err
		}
	}
	if first != nil {
		return nil, nil, first
	}
	for i, slug := range slugs {
		t := Target{Project: project, Repo: slug}
		switch {
		case own[i] != nil:
			stray = append(stray, repoHook{t, *own[i]})
		case other[i] != nil:
			foreign = append(foreign, t.Key()+" -> "+other[i].URL)
		}
	}
	return stray, foreign, nil
}

// Status reports one target's webhook; it writes nothing.
func (o *Onboarder) Status(ctx context.Context, t Target) Row {
	hooks, err := o.Client.ListWebhooks(ctx, t)
	if err != nil {
		return row(t, "failed", detail("list webhooks", err))
	}
	h, other := o.find(hooks)
	var r Row
	switch {
	case h == nil && other == nil:
		r = row(t, "missing", "no riptide webhook")
	case h == nil:
		r = row(t, "foreign", "only another team's riptide webhook: "+other.URL+"; onboard adds the team's own")
	default:
		if d := o.diff(*h); len(d) > 0 {
			r = row(t, "stale", fmt.Sprintf("webhook id=%d differs; onboard rewrites it", h.ID))
			r.Diff = d
		} else {
			r = row(t, "ok", fmt.Sprintf("webhook id=%d up to date", h.ID))
		}
	}

	if !t.IsProject() {
		ph, err := o.projectHook(ctx, t)
		switch {
		case err != nil:
			r.Status, r.Detail = "failed", detail("project webhook check", err)
		case ph != nil && r.Status == "missing":
			r.Status, r.Detail = "ok", fmt.Sprintf("covered by the project webhook id=%d", ph.ID)
		case ph != nil && r.Status != "foreign":
			r.Status = "double"
			r.Detail = fmt.Sprintf("the project webhook id=%d delivers every event a second time; "+
				"onboard the project to prune this one", ph.ID)
		}
		return r
	}

	stray, foreign, err := o.repoHooks(ctx, t.Project)
	if err != nil {
		r.Status, r.Detail = "failed", detail("repo webhook check", err)
		return r
	}
	for _, s := range stray {
		r.RepoHooks = append(r.RepoHooks, s.repo.Repo)
	}
	r.Foreign = append(r.Foreign, foreign...)
	if len(stray) > 0 && r.Status == "ok" {
		r.Status = "double"
		r.Detail += fmt.Sprintf("; %d repo webhook(s) deliver every event a second time, onboard prunes them", len(stray))
	}
	return r
}

// Onboard creates or updates the team's webhook on one target, and under a
// project prunes the team's repo hooks.
func (o *Onboarder) Onboard(ctx context.Context, t Target) Row {
	if !t.IsProject() {
		ph, err := o.projectHook(ctx, t)
		if err != nil {
			return row(t, "failed", detail("project webhook check", err))
		}
		if ph != nil {
			return row(t, "skipped", fmt.Sprintf("the project webhook id=%d covers this repo already; "+
				"a repo webhook next to it would deliver every event twice", ph.ID))
		}
	}

	hooks, err := o.Client.ListWebhooks(ctx, t)
	if err != nil {
		return row(t, "failed", detail("list webhooks", err))
	}
	// Another team's riptide hook stays; ours goes next to it (a shared
	// repo or project delivers to both teams).
	h, other := o.find(hooks)
	if o.projectHooks != nil && t.IsProject() && !o.DryRun {
		delete(o.projectHooks, t.Project)
	}
	prefix := ""
	if o.DryRun {
		prefix = "dry run: "
	}
	var r Row
	if h == nil {
		id := int64(-1)
		if !o.DryRun {
			var created Webhook
			if err := o.Client.do(ctx, http.MethodPost, hooksPath(t), nil, o.body(), &created); err != nil {
				return row(t, "failed", detail("create webhook", err))
			}
			id = created.ID
		}
		o.log().InfoContext(ctx, "onboard_webhook_created", "team", o.Team, "target", t.Key(), "dry_run", o.DryRun)
		r = row(t, "ok", fmt.Sprintf("%swebhook created (id=%d)", prefix, id))
		r.Diff = []string{"create"}
	} else {
		// Always written: the secret cannot be compared, so a rotated key
		// reaches Bitbucket only this way.
		d := o.diff(*h)
		if !o.DryRun {
			if err := o.Client.do(ctx, http.MethodPut, hookPath(t, h.ID), nil, o.body(), nil); err != nil {
				return row(t, "failed", detail("update webhook", err))
			}
		}
		o.log().InfoContext(ctx, "onboard_webhook_updated", "team", o.Team, "target", t.Key(),
			"changes", len(d), "dry_run", o.DryRun)
		if len(d) > 0 {
			r = row(t, "ok", fmt.Sprintf("%swebhook updated (id=%d)", prefix, h.ID))
			r.Diff = d
		} else {
			r = row(t, "ok", fmt.Sprintf("%swebhook up to date (id=%d), secret rewritten", prefix, h.ID))
		}
	}
	if other != nil {
		r.Foreign = append(r.Foreign, t.Key()+" -> "+other.URL)
	}
	if !t.IsProject() {
		return r
	}

	stray, foreign, err := o.repoHooks(ctx, t.Project)
	if err != nil {
		r.Detail += "; " + detail("repo webhook prune", err)
		r.Status = "failed"
		return r
	}
	r.Foreign = append(r.Foreign, foreign...)
	for _, s := range stray {
		if !o.DryRun {
			if err := o.Client.do(ctx, http.MethodDelete, hookPath(s.repo, s.hook.ID), nil, nil, nil); err != nil {
				r.Detail += "; " + detail("prune "+s.repo.Key(), err)
				r.Status = "failed"
				return r
			}
			o.log().InfoContext(ctx, "onboard_repo_webhook_pruned", "team", o.Team, "target", s.repo.Key())
		}
		r.RepoHooks = append(r.RepoHooks, s.repo.Repo)
	}
	switch {
	case len(stray) > 0 && o.DryRun:
		r.Detail += fmt.Sprintf(", would prune %d repo webhook(s)", len(stray))
	case len(stray) > 0:
		r.Detail += fmt.Sprintf(", %d repo webhook(s) pruned", len(stray))
	}
	return r
}

// Remove deletes the team's webhook from one target; another team's is left
// alone.
func (o *Onboarder) Remove(ctx context.Context, t Target) Row {
	hooks, err := o.Client.ListWebhooks(ctx, t)
	if err != nil {
		return row(t, "failed", detail("list webhooks", err))
	}
	h, other := o.find(hooks)
	switch {
	case h == nil && other == nil:
		return row(t, "skipped", "no riptide webhook")
	case h == nil:
		return row(t, "skipped", "only another team's riptide webhook ("+other.URL+"); left alone")
	case o.DryRun:
		return row(t, "ok", fmt.Sprintf("dry run: would remove webhook id=%d", h.ID))
	}
	if t.IsProject() {
		delete(o.projectHooks, t.Project)
	}
	if err := o.Client.do(ctx, http.MethodDelete, hookPath(t, h.ID), nil, nil, nil); err != nil {
		return row(t, "failed", detail("delete webhook", err))
	}
	o.log().InfoContext(ctx, "onboard_webhook_removed", "team", o.Team, "target", t.Key())
	return row(t, "ok", fmt.Sprintf("webhook removed (id=%d)", h.ID))
}

// Run applies action to every target in order, one failure never stopping
// the rest; one write at a time against Bitbucket. Healthy: with status
// every row ok, otherwise none failed.
func Run(ctx context.Context, o *Onboarder, action Action, targets []Target) (rows []Row, healthy bool) {
	step := o.Status
	switch action {
	case ActionOnboard:
		step = o.Onboard
	case ActionRemove:
		step = o.Remove
	}
	rows = make([]Row, 0, len(targets))
	healthy = true
	for _, t := range targets {
		r := step(ctx, t)
		rows = append(rows, r)
		if r.Status == "failed" || (action == ActionStatus && r.Status != "ok") {
			healthy = false
		}
	}
	return rows, healthy
}

// Render is the rows as a plain table, for a reader of the raw response.
func Render(rows []Row) string {
	width := 0
	for _, r := range rows {
		width = max(width, len(r.Target)+len(r.Level)+3)
	}
	var b strings.Builder
	for _, r := range rows {
		label := r.Target + " (" + r.Level + ")"
		fmt.Fprintf(&b, "%-*s  %-8s  %s\n", width, label, r.Status, r.Detail)
		for _, d := range r.Diff {
			fmt.Fprintf(&b, "%-*s  %-8s    %s\n", width, "", "", d)
		}
		if len(r.RepoHooks) > 0 {
			fmt.Fprintf(&b, "%-*s  %-8s    repo webhooks: %s\n", width, "", "", strings.Join(r.RepoHooks, ", "))
		}
		for _, f := range r.Foreign {
			fmt.Fprintf(&b, "%-*s  %-8s    other team: %s\n", width, "", "", f)
		}
	}
	return b.String()
}
