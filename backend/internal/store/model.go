package store

// Activity is how many events one sender identifier produced: a count per
// source, identifier and team over the four event tables.
//
// Table: -
type Activity struct {
	Source     string // bitbucket | pipeline | argocd | noergler
	Identifier string // repo_full_name / pipeline_name / app_name / repo
	Team       string
	Events     int64
}
