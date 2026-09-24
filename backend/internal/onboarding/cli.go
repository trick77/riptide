package onboarding

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"
)

const usageText = `usage: riptide onboard-bitbucket CONFIG.json [--name riptide] [--dry-run] [--remove] [--env-file PATH] [-v]
       riptide onboard-bitbucket --discover --bitbucket-url https://bitbucket.example.com [--env-file PATH]

Creates or updates the riptide webhook on every repo in CONFIG.json
(idempotent). --remove deletes it instead. --discover lists, as JSON on
stdout, every repo the token's user administers, grouped by project.

Secrets BITBUCKET_TOKEN and RIPTIDE_TEAM_KEY (the team's bitbucket key from
team-keys.json) come from --env-file, then ./.env, then the environment; the
first match wins. Write permission is checked implicitly: without it the
webhook write fails with a 403.
`

type options struct {
	config       string
	name         string
	dryRun       bool
	remove       bool
	envFile      string
	discover     bool
	bitbucketURL string
	verbose      bool
}

// parseArgs accepts flags before and after the config path, as argparse did.
func parseArgs(args []string, stderr io.Writer) (*options, error) {
	o := &options{}
	fs := flag.NewFlagSet("onboard-bitbucket", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { _, _ = io.WriteString(stderr, usageText) }
	fs.StringVar(&o.name, "name", DefaultWebhookName, "webhook name")
	fs.BoolVar(&o.dryRun, "dry-run", false, "print planned changes without writing to Bitbucket")
	fs.BoolVar(&o.remove, "remove", false, "delete the webhook instead of creating/updating it")
	fs.StringVar(&o.envFile, "env-file", "", "additional .env file to read secrets from")
	fs.BoolVar(&o.discover, "discover", false, "list the repos the token's user administers")
	fs.StringVar(&o.bitbucketURL, "bitbucket-url", "", "base URL for --discover")
	fs.BoolVar(&o.verbose, "verbose", false, "debug logging")
	fs.BoolVar(&o.verbose, "v", false, "debug logging")
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(positional) > 1 {
		return nil, fmt.Errorf("unexpected arguments: %v", positional[1:])
	}
	if len(positional) == 1 {
		o.config = positional[0]
	}
	return o, nil
}

// Main runs the command and returns the exit code: 0 ok, 1 a repo failed,
// 2 usage or configuration error.
func Main(args []string, stdout, stderr io.Writer) int {
	o, err := parseArgs(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		return 2
	}
	logf := func(format string, a ...any) {
		_, _ = fmt.Fprintf(stderr, "%s INFO %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, a...))
	}
	errf := func(format string, a ...any) {
		_, _ = fmt.Fprintf(stderr, "%s ERROR %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, a...))
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if o.discover {
		return runDiscover(ctx, o, stdout, stderr, logf, errf)
	}
	if o.config == "" {
		_, _ = io.WriteString(stderr, "ERROR: config path is required (or pass --discover)\n")
		return 2
	}
	secrets, err := resolveEnv(o.envFile, "BITBUCKET_TOKEN", "RIPTIDE_TEAM_KEY")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "ERROR: %v\n", err)
		return 2
	}
	in, err := LoadInput(o.config)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "ERROR: %v\n", err)
		return 2
	}
	token, teamKey := secrets["BITBUCKET_TOKEN"], secrets["RIPTIDE_TEAM_KEY"]

	logf("Bitbucket URL: %s", in.BitbucketURL)
	logf("Webhook URL:   %s", in.WebhookURL)
	logf("Team:          %s", in.Team)
	if u, _ := url.Parse(in.WebhookURL); u != nil && u.Scheme == "http" {
		logf("WARNING webhook_url is http://: deliveries, and their signatures, travel unencrypted")
	}
	logf("Bitbucket token loaded: %s", mask(token))
	logf("Riptide team key loaded: %s", mask(teamKey))
	keys := make([]string, len(in.Repos))
	for i, r := range in.Repos {
		keys[i] = r.Key()
	}
	logf("Target repos (%d): %s", len(in.Repos), strings.Join(keys, ", "))
	if o.remove {
		logf("REMOVE mode: will delete the %q webhook from each repo", o.name)
	}
	if o.dryRun {
		logf("DRY-RUN: no writes will be issued")
	}

	ob := &Onboarder{
		Client: NewClient(in.BitbucketURL, token), WebhookURL: in.WebhookURL, Team: in.Team,
		TeamKey: teamKey, Name: o.name, DryRun: o.dryRun, Log: logf,
	}
	action := ob.Onboard
	if o.remove {
		action = ob.Remove
	}
	var results []Result
	for i, spec := range in.Repos {
		logf("--- %s ---", spec.Key())
		res := action(ctx, spec)
		results = append(results, res)
		if res.Status == "failed" {
			errf("[%s] aborting: %s", spec.Key(), res.Detail)
			if rest := keys[i+1:]; len(rest) > 0 {
				errf("not processed: %s", strings.Join(rest, ", "))
			}
			break
		}
	}
	printSummary(stdout, results)
	for _, r := range results {
		if r.Status == "failed" {
			return 1
		}
	}
	return 0
}

func runDiscover(ctx context.Context, o *options, stdout, stderr io.Writer, logf, errf func(string, ...any)) int {
	if o.bitbucketURL == "" {
		_, _ = io.WriteString(stderr, "ERROR: --discover requires --bitbucket-url\n")
		return 2
	}
	if u, err := url.Parse(o.bitbucketURL); err != nil || u.Scheme != "https" || u.Host == "" {
		_, _ = io.WriteString(stderr, "ERROR: --bitbucket-url must be an https URL\n")
		return 2
	}
	secrets, err := resolveEnv(o.envFile, "BITBUCKET_TOKEN")
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "ERROR: %v\n", err)
		return 2
	}
	token := secrets["BITBUCKET_TOKEN"]
	logf("Bitbucket URL: %s", o.bitbucketURL)
	logf("Bitbucket token loaded: %s", mask(token))
	repos, err := NewClient(o.bitbucketURL, token).ListAdminRepos(ctx)
	if err != nil {
		errf("discover: %v", err)
		return 1
	}
	byProject := map[string][]string{}
	for _, r := range repos {
		byProject[r.Project] = append(byProject[r.Project], r.Repo)
	}
	type project struct {
		Project string   `json:"project"`
		Repos   []string `json:"repos"`
	}
	out := []project{}
	total := 0
	for _, k := range sortedKeys(byProject) {
		rs := byProject[k]
		sort.Strings(rs)
		total += len(rs)
		out = append(out, project{Project: k, Repos: rs})
	}
	logf("discover: %d repos across %d projects", total, len(out))
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	return 0
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func printSummary(w io.Writer, results []Result) {
	width := 10
	for _, r := range results {
		if n := len(r.Repo.Key()); n > width {
			width = n
		}
	}
	header := fmt.Sprintf("%-*s  status   detail", width, "repo")
	_, _ = fmt.Fprintf(w, "\n%s\n%s\n", header, strings.Repeat("-", len(header)))
	for _, r := range results {
		_, _ = fmt.Fprintf(w, "%-*s  %-7s  %s\n", width, r.Repo.Key(), r.Status, r.Detail)
	}
}

func mask(secret string) string {
	if len(secret) <= 4 {
		return "****"
	}
	return secret[:4] + "-****"
}

// resolveEnv reads the required variables. Precedence, first match wins:
// --env-file, then ./.env, then the process environment. An explicit file
// beats the environment so a stale exported value in the operator's shell
// does not silently override it.
func resolveEnv(envFile string, required ...string) (map[string]string, error) {
	merged := map[string]string{}
	for _, k := range required {
		if v := os.Getenv(k); v != "" {
			merged[k] = v
		}
	}
	files := []string{".env"}
	if envFile != "" {
		files = append(files, envFile)
	}
	for _, f := range files {
		vals, err := readEnvFile(f, envFile != "" && f == envFile)
		if err != nil {
			return nil, err
		}
		for k, v := range vals {
			merged[k] = v
		}
	}
	var missing []string
	for _, k := range required {
		if merged[k] == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variable(s): %s\nSet them in the process env, a .env in CWD, or pass --env-file", strings.Join(missing, ", "))
	}
	return merged, nil
}

// readEnvFile parses KEY=VALUE lines; a missing ./.env is fine, a missing
// --env-file is an error.
func readEnvFile(path string, mustExist bool) (map[string]string, error) {
	f, err := os.Open(path) //nolint:gosec // operator-supplied path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !mustExist {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) == "" {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, sc.Err()
}
