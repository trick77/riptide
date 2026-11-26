package config

import (
	"strings"
	"testing"
)

func TestThresholdsDefault(t *testing.T) {
	c := mustParse(t, validConfig)
	if c.Thresholds != DefaultThresholds() {
		t.Errorf("thresholds without the block = %+v, want the starting values", c.Thresholds)
	}
	d := DefaultThresholds()
	if d.ReleaseBreachDays != 28 || d.ReleaseHintDays != 14 || d.NotOnProdDays != 14 || d.BotLoopFailures != 10 ||
		d.ApprovedWaitDays != 1 || d.MergePauseShare != 0.2 || d.HysteresisShare != 0.8 {
		t.Errorf("starting values = %+v", d)
	}
}

func TestThresholdsOverride(t *testing.T) {
	c := mustParse(t, `{"thresholds": {"release_breach_days": 21, "approved_wait_days": 1.5, "flaky_share": 0.5}}`)
	want := DefaultThresholds()
	want.ReleaseBreachDays, want.ApprovedWaitDays, want.FlakyShare = 21, 1.5, 0.5
	if c.Thresholds != want {
		t.Errorf("thresholds = %+v, want %+v", c.Thresholds, want)
	}
	if c := mustParse(t, `{"thresholds": null}`); c.Thresholds != DefaultThresholds() {
		t.Errorf("null block = %+v", c.Thresholds)
	}
}

func TestThresholdsInvalid(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`{"thresholds": []}`, "must be an object"},
		{`{"thresholds": {"release_breach_dayz": 21}}`, "not a known threshold"},
		{`{"thresholds": {"release_breach_days": 0}}`, "above 0"},
		{`{"thresholds": {"release_breach_days": -3}}`, "above 0"},
		{`{"thresholds": {"release_breach_days": "28"}}`, "above 0"},
		{`{"thresholds": {"release_breach_days": null}}`, "above 0"},
		{`{"thresholds": {"release_breach_days": 2.5}}`, "whole number"},
		{`{"thresholds": {"flaky_share": 1.5}}`, "at most 1"},
		{`{"thresholds": {"release_hint_days": 30}}`, "below `release_breach_days`"},
		{`{"thresholds": {"branch_silent_days": 90}}`, "below `branch_silent_max_days`"},
		// Past the states' look-back nothing could ever fire; a day count
		// past it, a loop over each day, hung every assessment.
		{`{"thresholds": {"silent_builds_days": 5000}}`, "silent_builds_days` must be at most 15"},
		// Past the runs the shortest view reads, the span before it is not
		// read: the state could never judge a repo.
		{`{"thresholds": {"silent_builds_days": 16}}`, "silent_builds_days` must be at most 15"},
		// Dropped: a key no longer known refuses to start.
		{`{"thresholds": {"merge_pause_wait_days": 0.5}}`, "not a known threshold"},
		{`{"thresholds": {"not_on_prod_days": 200}}`, "at most 182"},
		{`{"thresholds": {"merge_pause_weeks": 40}}`, "at most 26"},
		{`{"thresholds": {"build_minutes": 2000}}`, "at most 1440"},
		{`{"thresholds": {"bot_loop_failures": 20000}}`, "at most 10000"},
		// Two bad keys: the first by name, every time.
		{`{"thresholds": {"release_breach_days": 0, "approved_wait_days": -1}}`, "approved_wait_days"},
	} {
		for range 5 {
			_, err := Parse([]byte(c.in))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: err = %v, want %q", c.in, err, c.want)
			}
		}
	}
}
