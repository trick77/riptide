package metrics

import (
	"cmp"
	"slices"
)

// Findings come only from the rules the page already applies (the stage
// that takes longest, flaky and slow repos), never from new thresholds of
// their own, plus one fixed question to talk about.
const (
	// FindingsMax caps the findings before the question to talk about.
	FindingsMax = 3
	// StageAttention: a stage that takes this part of the time to
	// production or more needs attention; below it the longest stage is
	// only a hint, as some stage always takes longest. A starting value.
	StageAttention = 0.5
)

// Finding kinds and tones.
const (
	FindingStage      = "stage"
	FindingFlaky      = "flaky"
	FindingSlow       = "slow"
	FindingFirstTouch = "first_touch"

	ToneAttention = "attention"
	ToneHint      = "hint"
	ToneTalk      = "talk"
)

// Finding is one item of the page's "what now". The UI words it from Kind
// and reads its numbers off the section it names: the stage in the lead
// chain, the repo in the run kind's hotspots, the review's first-touch rate.
type Finding struct {
	Kind    string  `json:"kind"`
	Tone    string  `json:"tone"`
	Stage   *string `json:"stage"`
	RunKind *string `json:"run_kind"`
	Repo    *string `json:"repo"`
}

// findings are the attention ones first (a stage past StageAttention on
// enough PRs, then
// flaky repos by wasted CI time, then slow ones by factor, a repo once), a
// stage hint after them, at most FindingsMax, then the first-touch rate to
// talk about.
func findings(c Chain, b Build, r Review) []Finding {
	out := []Finding{}
	var hot *Stage
	for i := range c.Stages {
		if c.Stages[i].Hot {
			hot = &c.Stages[i]
		}
	}
	stage := func(tone string) Finding {
		return Finding{Kind: FindingStage, Tone: tone, Stage: ptr(hot.Key)}
	}
	// A median of fewer than 10 PRs is too weak to ask for attention.
	attention := hot != nil && hot.Share != nil && *hot.Share >= StageAttention && !hot.LowSample
	if attention {
		out = append(out, stage(ToneAttention))
	}

	type repo struct {
		kind string
		h    RepoHotspot
	}
	var flaky, slow []repo
	for _, k := range b.Hotspots.Kinds {
		for _, h := range k.Repos {
			switch {
			case h.Flaky:
				flaky = append(flaky, repo{k.Kind, h})
			case h.Slow:
				slow = append(slow, repo{k.Kind, h})
			}
		}
	}
	slices.SortStableFunc(flaky, func(a, b repo) int { return cmp.Compare(b.h.WastedSeconds, a.h.WastedSeconds) })
	slices.SortStableFunc(slow, func(a, b repo) int { return cmp.Compare(*b.h.Factor, *a.h.Factor) })
	seen := map[string]bool{}
	add := func(kind string, rs []repo) {
		for _, x := range rs {
			if !seen[x.h.Repo] {
				seen[x.h.Repo] = true
				out = append(out, Finding{Kind: kind, Tone: ToneAttention, RunKind: ptr(x.kind), Repo: ptr(x.h.Repo)})
			}
		}
	}
	add(FindingFlaky, flaky)
	add(FindingSlow, slow)
	if hot != nil && !attention {
		out = append(out, stage(ToneHint))
	}
	out = out[:min(len(out), FindingsMax)]
	if r.Approved > 0 {
		out = append(out, Finding{Kind: FindingFirstTouch, Tone: ToneTalk})
	}
	return out
}
