package metrics

import (
	"slices"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/dataset"
)

// featureRun is a successful feature build of kasse-api days before the
// period's end, lasting secs.
func featureRun(i int, days float64, secs int64) dataset.Run {
	return mainRun(i, false, days, func(r *dataset.Run) {
		r.PipelineName, r.Branch, r.BranchKind = "kasse-api/feature%2Fx", "feature/x", dataset.BranchFeature
		r.DurationS = n(secs)
	})
}

// A lasting state is red once a unit fired at every week back over two
// sprints: a week its number stood within the hysteresis, never named,
// counts for nothing. Typical feature builds of 13 minutes for weeks (in
// the band under 15), 30 minutes now: a hint, as from 10 minutes.
func TestLastingRedNeedsFiringWeeks(t *testing.T) {
	build := func(before int64) (*dataset.Dataset, *dataset.Dataset) {
		ds := stateData()
		prev := &dataset.Dataset{Meta: ds.Meta, Apps: ds.Apps}
		id := 0
		for i := range 10 {
			id++
			ds.Runs = append(ds.Runs, featureRun(id, 0.5+float64(i)*0.5, 1800))
		}
		for w := 2; w <= 8; w++ {
			for i := range 2 {
				id++
				r := featureRun(id, float64(7*(w-1))+1+float64(i), before)
				if w <= 4 {
					ds.Runs = append(ds.Runs, r)
				} else {
					prev.Runs = append(prev.Runs, r)
				}
			}
		}
		return ds, prev
	}
	for _, secs := range []int64{780, 600} {
		ds, prev := build(secs)
		x := stateSignal(assessBoth(t, ds, prev, nil), StateLongBuilds)
		if x == nil || x.Tone != ToneHint || x.State.Units[0].TwoSprints {
			t.Errorf("first past the threshold now, %d s before = %+v, want a hint", secs, x)
		}
	}
	// Past it at every week back: red, the unit marked.
	ds, prev := build(1200)
	x := stateSignal(assessBoth(t, ds, prev, nil), StateLongBuilds)
	if x == nil || x.Tone != ToneAttention || !x.State.Units[0].TwoSprints {
		t.Errorf("past the threshold for weeks = %+v, want red, the unit marked", x)
	}
}

// One unit held two sprints turns the card red: the card marks it, not the
// fresh ones beside it.
func TestTwoSprintsMarksItsUnit(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Traces["kasse/kasse-web"] = dataset.RepoTrace{DeployedSince: endAgoP(150)}
		d.PRs = []dataset.PR{merged(1, 3), merged(2, 3, func(p *dataset.PR) { p.Repo = "kasse/kasse-web" })}
		d.Approvals = []dataset.Approval{
			// kasse-api: waiting five weeks, touched last week.
			touched(approval("kasse/kasse-api", 10, 35, nil), 4), touched(approval("kasse/kasse-api", 11, 33, nil), 4),
			// kasse-web: waiting since last week.
			approval("kasse/kasse-web", 20, 6, nil), approval("kasse/kasse-web", 21, 5, nil),
		}
	})
	x := stateSignal(assessStates(t, ds, nil, History{}), StateApprovedWaiting)
	if x == nil || x.Tone != ToneAttention || x.State.Count != 2 {
		t.Fatalf("card = %+v, want both repos, red", x)
	}
	for _, u := range x.State.Units {
		if u.TwoSprints != (u.Name == "kasse/kasse-api") {
			t.Errorf("%s two sprints = %v, want only kasse-api marked", u.Name, u.TwoSprints)
		}
	}
}

// An approved PR still open stays counted however long nobody touches it:
// the forgotten approved PR turns red and is never "gelöst" while it waits.
func TestApprovedUntouchedStaysCounted(t *testing.T) {
	for _, age := range []float64{20, 31, 32, 60} {
		ds := stateData(func(d *dataset.Dataset) {
			d.PRs = []dataset.PR{merged(1, 3)}
			d.Approvals = []dataset.Approval{
				approval("kasse/kasse-api", 10, age, nil), approval("kasse/kasse-api", 11, age, nil),
			}
		})
		a := assessStates(t, ds, nil, History{})
		// Red once waiting past the threshold two sprints, a hint before.
		want := ToneAttention
		if age < 2*SprintDays {
			want = ToneHint
		}
		if x := stateSignal(a, StateApprovedWaiting); x == nil || x.Tone != want {
			t.Errorf("two PRs approved %v days back, untouched = %+v, want %s", age, x, want)
		}
		if hasSignal(a, SignalResolved+":"+SignalState+":"+StateApprovedWaiting, ToneGood) {
			t.Errorf("approved %v days back, still open: announced gelöst", age)
		}
	}
	// Merged now: "gelöst".
	ds := stateData(func(d *dataset.Dataset) {
		d.PRs = []dataset.PR{merged(1, 3)}
		d.Approvals = []dataset.Approval{
			approval("kasse/kasse-api", 10, 32, ptr(2.0)), approval("kasse/kasse-api", 11, 32, ptr(2.0)),
		}
	})
	if a := assessStates(t, ds, nil, History{}); stateSignal(a, StateApprovedWaiting) != nil ||
		!hasSignal(a, SignalResolved+":"+SignalState+":"+StateApprovedWaiting, ToneGood) {
		t.Errorf("both merged = %v, want gelöst", sigIDs(a.Signals))
	}
}

// Another reviewer's needs work on a PR that waited a week before: not
// waiting for its merge now, nor merged, closed or unapproved. No card, no
// "gelöst", no "nicht mehr messbar".
func TestApprovedHeldByNeedsWorkIsNoNews(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.PRs = []dataset.PR{merged(1, 3)}
		d.Approvals = []dataset.Approval{
			approval("kasse/kasse-api", 10, 12, nil), needsWork(approval("kasse/kasse-api", 11, 11, nil), 3),
		}
	})
	a := assessStates(t, ds, nil, History{})
	if slices.ContainsFunc(a.Signals, func(s Signal) bool {
		return s.ID == SignalState+":"+StateApprovedWaiting || s.ID == SignalResolved+":"+SignalState+":"+StateApprovedWaiting ||
			s.ID == SignalState+":"+StateUnmeasurable+":"+StateApprovedWaiting
	}) {
		t.Errorf("signals %v, want nothing of approved_waiting", sigIDs(a.Signals))
	}
}

// pauseWeeks is kasse merging 8 PRs in every week back to 14 weeks before
// the period's end, but in the week skip weeks back (1: the period's last
// week), with three PRs approved on its Tuesday, open until days before the
// period's end (nil: still open).
func pauseWeeks(skip int, mergedDays *float64) *dataset.Dataset {
	return stateData(func(d *dataset.Dataset) {
		id := int64(0)
		for w := 1; w <= 14; w++ {
			if w == skip {
				continue
			}
			for i := range 8 {
				id++
				d.PRs = append(d.PRs, merged(id, float64(7*(w-1))+0.5+float64(i)*0.3))
			}
		}
		for i := range 3 {
			d.Approvals = append(d.Approvals, approval("kasse/kasse-api", int64(100+i), float64(7*(skip-1))+6, mergedDays))
		}
	})
}

// An episode (a week's merge pause, a run of red trunk builds) is "gelöst"
// the instant after it ended, not when its week leaves the view: four weeks
// late in a 4-week view, twelve in a 12-week one.
func TestEpisodeResolvedTheWeekAfter(t *testing.T) {
	pause := SignalResolved + ":" + SignalState + ":" + StateMergePause
	// The pause in the period's second-to-last week, the last week normal:
	// still on its card (dated), and over.
	a := assessStates(t, pauseWeeks(2, ptr(1.0)), nil, History{})
	if x := stateSignal(a, StateMergePause); x == nil || x.Weight == tierNow || !hasSignal(a, pause, ToneGood) {
		t.Errorf("pause the week before = %v, want its dated card and gelöst", sigIDs(a.Signals))
	}
	// Still paused in the last week: no resolution.
	if a := assessStates(t, pauseWeeks(1, nil), nil, History{}); hasSignal(a, pause, ToneGood) {
		t.Errorf("a running pause announced gelöst: %v", sigIDs(a.Signals))
	}
	// The pause in the week before the period, merges normal since: left the
	// view, no news.
	if a := assessStates(t, pauseWeeks(5, ptr(26.0)), nil, History{}); hasSignal(a, pause, ToneGood) {
		t.Errorf("a pause four weeks over announced gelöst now: %v", sigIDs(a.Signals))
	}

	trunk := SignalResolved + ":" + SignalState + ":" + StateTrunkFailing
	// Red from the week before's Monday to its end, green again on this
	// week's Tuesday.
	ds := stateData(func(d *dataset.Dataset) {
		d.Runs = []dataset.Run{mainRun(1, false, 15), mainRun(2, true, 14), mainRun(3, true, 12), mainRun(4, true, 10),
			mainRun(5, false, 5)}
	})
	a = assessStates(t, ds, nil, History{})
	if x := stateSignal(a, StateTrunkFailing); x == nil || x.Weight == tierNow || !hasSignal(a, trunk, ToneGood) {
		t.Errorf("trunk green again = %v, want its dated card and gelöst", sigIDs(a.Signals))
	}
	// Red in the week before the period, green since: no news now.
	ds = stateData()
	prev := &dataset.Dataset{Meta: ds.Meta, Apps: ds.Apps}
	prev.Runs = []dataset.Run{mainRun(1, false, 39), mainRun(2, true, 36), mainRun(3, true, 35.5), mainRun(4, true, 33),
		mainRun(5, false, 32)}
	for i := range 6 {
		ds.Runs = append(ds.Runs, mainRun(10+i, false, 25-float64(i)*4))
	}
	if a := assessBoth(t, ds, prev, nil); hasSignal(a, trunk, ToneGood) {
		t.Errorf("a trunk green for weeks announced gelöst now: %v", sigIDs(a.Signals))
	}
}

// A feature branch's run of failures ended weeks ago is no news when its
// week leaves the view; one running a week ago and green since is
// "gelöst".
func TestBranchFailingResolvedTheWeekAfter(t *testing.T) {
	id := SignalResolved + ":" + SignalState + ":" + StateBranchFailing
	feature := func(r *dataset.Run) {
		r.PipelineName, r.Branch, r.BranchKind = "kasse-api/feature%2Fkas-7", "feature/kas-7", dataset.BranchFeature
	}
	streak := func(from float64) []dataset.Run {
		var rs []dataset.Run
		for i := range 6 {
			rs = append(rs, mainRun(i, true, from-float64(i)*0.5, feature))
		}
		return rs
	}
	// Ended five weeks back, green builds since in the period.
	ds := stateData()
	prev := &dataset.Dataset{Meta: ds.Meta, Apps: ds.Apps}
	prev.Runs = append(streak(33.5), mainRun(20, false, 30, feature))
	ds.Runs = []dataset.Run{mainRun(21, false, 20, feature), mainRun(22, false, 10, feature)}
	if a := assessBoth(t, ds, prev, nil); hasSignal(a, id, ToneGood) {
		t.Errorf("a run over for weeks announced gelöst now: %v", sigIDs(a.Signals))
	}
	// Failing a week ago, green since.
	ds = stateData(func(d *dataset.Dataset) {
		d.Runs = append(streak(11), mainRun(20, false, 4, feature))
	})
	if a := assessStates(t, ds, nil, History{}); !hasSignal(a, id, ToneGood) {
		t.Errorf("a run over since this week = %v, want gelöst", sigIDs(a.Signals))
	}
}

// A bot's loop is an episode alike: over for weeks, no news when it leaves
// the view; looping a week ago and green since, "gelöst".
func TestBotLoopResolvedTheWeekAfter(t *testing.T) {
	id := SignalResolved + ":" + SignalState + ":" + StateBotLoop
	at := func(days float64) func(*dataset.Run) {
		return func(r *dataset.Run) {
			r.StartedAt, r.FinishedAt = endAgoP(days), endAgoP(days-0.01)
			r.OccurredAt = *r.FinishedAt
		}
	}
	loop := func(from float64) []dataset.Run {
		var rs []dataset.Run
		for i := range 12 {
			rs = append(rs, botRun(i, true, at(from-float64(i)*0.1)))
		}
		return rs
	}
	// Looped five weeks back, green builds since in the period.
	ds := stateData()
	prev := &dataset.Dataset{Meta: ds.Meta, Apps: ds.Apps}
	prev.Runs = append(loop(33.5), botRun(20, false, at(30)))
	ds.Runs = []dataset.Run{botRun(21, false, at(20)), botRun(22, false, at(10))}
	if a := assessBoth(t, ds, prev, nil); hasSignal(a, id, ToneGood) {
		t.Errorf("a loop over for weeks announced gelöst now: %v", sigIDs(a.Signals))
	}
	// Looping a week ago, green since.
	ds = stateData(func(d *dataset.Dataset) {
		d.Runs = append(loop(10), botRun(30, false, at(4)))
	})
	if a := assessStates(t, ds, nil, History{}); !hasSignal(a, id, ToneGood) {
		t.Errorf("a loop over since this week = %v, want gelöst", sigIDs(a.Signals))
	}
}

// Whether a pause went on, a week Bitbucket lost cannot tell: "nicht mehr
// messbar", never "gelöst".
func TestEpisodeAfterAnUnjudgedWeek(t *testing.T) {
	ds := pauseWeeks(2, ptr(1.0))
	// Lost from Monday noon of the last week to Sunday noon: under a fifth
	// of its working time reported.
	ds.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: endAgo(6.5), To: endAgo(0.5), Lost: true}}
	a := assessStates(t, ds, nil, History{})
	if !hasSignal(a, SignalState+":"+StateUnmeasurable+":"+StateMergePause, ToneLimit) ||
		hasSignal(a, SignalResolved+":"+SignalState+":"+StateMergePause, ToneGood) {
		t.Errorf("signals %v, want the pause unmeasurable, never resolved", sigIDs(a.Signals))
	}
}

// A repo whose PRs go to develop builds master too: each trunk branch is a
// sequence of its own. A green master build between two red develop builds
// hides nothing.
func TestTrunkFailingPerBranch(t *testing.T) {
	mk := func(withMaster bool) *dataset.Dataset {
		return stateData(func(d *dataset.Dataset) {
			dev := func(r *dataset.Run) {
				r.PipelineName, r.Branch, r.BranchKind = "kasse-api/develop", "develop", dataset.BranchKind("develop")
			}
			d.Runs = []dataset.Run{mainRun(1, false, 9, dev), mainRun(2, true, 7, dev), mainRun(3, true, 6, dev),
				mainRun(5, false, 4, dev)}
			if withMaster {
				d.Runs = append(d.Runs, mainRun(6, false, 6.5))
			}
			for i := range 3 {
				d.PRs = append(d.PRs, merged(int64(i+1), 3, func(p *dataset.PR) { p.TrunkBranch = s("develop") }))
			}
		})
	}
	for _, withMaster := range []bool{false, true} {
		if x := stateSignal(assessStates(t, mk(withMaster), nil, History{}), StateTrunkFailing); x == nil ||
			*x.State.Units[0].Count != 2 {
			t.Errorf("develop red twice over a day, master build between %v = %+v, want a card", withMaster, x)
		}
	}
}

// A branch left lying and pushed again starts over: open once worked on
// for BranchOpenDays since, never for the weeks it lay silent.
func TestBranchOpenAfterSilence(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Branches = []dataset.BranchLife{branchLife("feature/kas-77", 60, 1, func(b *dataset.BranchLife) {
			b.PushTimes = []time.Time{endAgo(60), endAgo(1)}
			b.Pushes = 2
		})}
	})
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateBranchOpen); x != nil {
		t.Errorf("pushed yesterday after 59 silent days = %+v, want no open branch", x.State.Units)
	}
	// Worked on again for 20 days since, a push every few days: open for 20
	// days, since 14 days after the new start; a hint.
	ds.Branches[0].PushTimes = []time.Time{endAgo(60), endAgo(20), endAgo(16), endAgo(11), endAgo(6), endAgo(1)}
	x := stateSignal(assessStates(t, ds, nil, History{}), StateBranchOpen)
	if x == nil || x.Tone != ToneHint || x.State.Units[0].Value != 20 || x.State.Since == nil ||
		!x.State.Since.Equal(endAgo(20).AddDate(0, 0, 14)) {
		t.Errorf("worked on 20 days after a silence = %+v, want a hint, 20 days, since day 14 of the run", x)
	}
}

// An epic branch is a PR's target from the first PR into it: not a branch
// of work at an earlier instant, though a PR came from it only now.
func TestEpicBranchIsNoWork(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Branches = []dataset.BranchLife{
			branchLife("epic/kas-big", 40, 1, func(b *dataset.BranchLife) {
				b.PushTimes = []time.Time{endAgo(40), endAgo(30), endAgo(20), endAgo(10), endAgo(8), endAgo(1)}
				b.TargetAt, b.PRAt = endAgoP(38), endAgoP(2)
			}),
			branchLife("feature/kas-3", 5, 3),
		}
	})
	a := assessStates(t, ds, nil, History{})
	if stateSignal(a, StateBranchOpen) != nil || hasSignal(a, SignalResolved+":"+SignalState+":"+StateBranchOpen, ToneGood) {
		t.Errorf("signals %v, want the epic never judged as work", sigIDs(a.Signals))
	}
}

// A held unit's chain is read four weeks back: one in the band longer drops
// out silently (no card, no "gelöst") while under the threshold; past it
// again, it fires.
func TestHeldBeyondFourWeeks(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Releases = []dataset.AppRelease{release("kasse-prod", 2, false)}
		d.PRs = []dataset.PR{
			merged(1, 51, shippedOn(33.5)), merged(2, 40.5, shippedOn(27)), merged(3, 33.5, shippedOn(20)),
			merged(4, 26.5, shippedOn(13)), merged(5, 19.5, shippedOn(6)), merged(6, 12.5),
		}
	})
	a := assessStates(t, ds, nil, History{})
	if stateSignal(a, StateNotOnProd) != nil || hasSignal(a, SignalResolved+":"+SignalState+":"+StateNotOnProd, ToneGood) {
		t.Errorf("in the band for five weeks = %v, want neither card nor gelöst", sigIDs(a.Signals))
	}
	ds.PRs[5] = merged(6, 15)
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateNotOnProd); x == nil || x.State.Units[0].Held {
		t.Errorf("past the threshold again = %+v, want it firing", x)
	}
}

// The merge pause's threshold names no span: the waiting PRs count however
// long they waited (config drops merge_pause_wait_days).
func TestMergePauseThresholdHasNoSpan(t *testing.T) {
	th := config.DefaultThresholds()
	th.MergePauseWaiting = 2
	x := stateSignal(assessStates(t, pauseData(2), &th, History{}), StateMergePause)
	if x == nil || x.State.Threshold.WithinKey != nil || *x.State.Threshold.Min != 2 {
		t.Errorf("pause = %+v, want the waiting count as its only minimum", x)
	}
}
