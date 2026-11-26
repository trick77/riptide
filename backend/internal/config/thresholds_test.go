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
	} {
		_, err := Parse([]byte(c.in))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.in, err, c.want)
		}
	}
}
