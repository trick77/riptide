package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestScheduledAnalysisClaim(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	at := time.Date(2030, 1, 7, 6, 0, 0, 0, time.UTC)
	v := AnalysisView{Team: "checkout", Weeks: 4}
	claim := func(at time.Time) (int64, bool) {
		t.Helper()
		id, ok, err := s.ClaimAnalysis(ctx, AnalysisClaim{
			View: v, From: at.AddDate(0, 0, -28), To: time.Date(2030, 1, 7, 0, 0, 0, 0, time.UTC),
			Trigger: "scheduled", Model: "m", At: at, MaxAttempts: 2, StaleAfter: 30 * time.Minute,
		})
		if err != nil {
			t.Fatal(err)
		}
		return id, ok
	}
	id, ok := claim(at)
	if !ok {
		t.Fatal("first claim refused")
	}
	if _, ok := claim(at.Add(time.Minute)); ok {
		t.Fatal("a running week claimed twice")
	}
	// Abandoned: its pod died; claimed again past StaleAfter.
	if again, ok := claim(at.Add(31 * time.Minute)); !ok || again != id {
		t.Fatalf("a stale run not reclaimed: %d %v", again, ok)
	}
	// Abandoned again: a run that kills its pod is not paid for forever.
	if _, ok := claim(at.Add(62 * time.Minute)); ok {
		t.Fatal("a stale run claimed past MaxAttempts")
	}
	msg := "boom"
	if err := s.FailAnalysis(ctx, id, AnalysisResult{Error: &msg, At: at.Add(32 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	// Two attempts are the cap here: the reclaim was the second, failed or
	// abandoned alike.
	if _, ok := claim(at.Add(time.Hour)); ok {
		t.Fatal("failed past MaxAttempts and claimed again")
	}
	a, err := s.ScheduledAttempt(ctx, v, time.Date(2030, 1, 7, 0, 0, 0, 0, time.UTC))
	if err != nil || a == nil || a.Status != "failed" || a.Attempts != 2 || *a.Error != "boom" {
		t.Fatalf("attempt = %+v, %v", a, err)
	}

	// A manual run is a row of its own; done, it is the view's latest.
	mid, ok, err := s.ClaimAnalysis(ctx, AnalysisClaim{View: v, From: at.AddDate(0, 0, -28), To: a.PeriodTo,
		Trigger: "manual", Model: "m", At: at.Add(2 * time.Hour)})
	if err != nil || !ok || mid == id {
		t.Fatalf("manual claim %d %v %v", mid, ok, err)
	}
	out := json.RawMessage(`{"de":{},"en":{}}`)
	if err := s.FinishAnalysis(ctx, mid, AnalysisResult{Output: out, Assessment: json.RawMessage(`{}`),
		At: at.Add(2*time.Hour + time.Minute)}); err != nil {
		t.Fatal(err)
	}
	done, err := s.DoneAnalyses(ctx, v, 5)
	if err != nil || len(done) != 1 || done[0].ID != mid || done[0].FinishedAt == nil {
		t.Fatalf("done = %+v, %v", done, err)
	}
	latest, err := s.LatestAttempt(ctx, v)
	if err != nil || latest.ID != mid || !latest.CreatedAt.Equal(at.Add(2*time.Hour)) {
		t.Fatalf("latest = %+v, %v", latest, err)
	}
	if none, err := s.LatestAttempt(ctx, AnalysisView{Team: "other", Weeks: 4}); err != nil || none != nil {
		t.Errorf("another view's attempt: %+v, %v", none, err)
	}
}

// A done row names the rules it was assessed under; a failed one none,
// whatever it was handed: a binary from before the column may claim and
// finish it again without touching the column.
func TestAnalysisRulesVersion(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	v := AnalysisView{Team: "checkout", Weeks: 4}
	to := time.Date(2030, 1, 7, 0, 0, 0, 0, time.UTC)
	end := func(status string, at time.Time) *Analysis {
		t.Helper()
		id, ok, err := s.ClaimAnalysis(ctx, AnalysisClaim{View: v, From: to.AddDate(0, 0, -28), To: to,
			Trigger: "manual", Model: "m", At: at})
		if err != nil || !ok {
			t.Fatalf("claim %v %v", ok, err)
		}
		r := AnalysisResult{Assessment: json.RawMessage(`{}`), RulesVersion: ptr(2), At: at.Add(time.Minute)}
		if status == "done" {
			err = s.FinishAnalysis(ctx, id, r)
		} else {
			err = s.FailAnalysis(ctx, id, r)
		}
		if err != nil {
			t.Fatal(err)
		}
		a, err := s.LatestAttempt(ctx, v)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	if a := end("done", to.Add(time.Hour)); a.RulesVersion == nil || *a.RulesVersion != 2 {
		t.Errorf("done row's rules = %v", a.RulesVersion)
	}
	if a := end("failed", to.Add(2*time.Hour)); a.RulesVersion != nil {
		t.Errorf("failed row's rules = %v, want none", *a.RulesVersion)
	}
	// A row as a binary from before the column finishes it: none.
	id, _, err := s.ClaimAnalysis(ctx, AnalysisClaim{View: v, From: to.AddDate(0, 0, -28), To: to,
		Trigger: "manual", Model: "m", At: to.Add(3 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE team_analyses SET status = 'done', finished_at = now() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if a, err := s.LatestAttempt(ctx, v); err != nil || a.ID != id || a.RulesVersion != nil {
		t.Errorf("old binary's row = %+v, %v", a, err)
	}
}

// A week's reruns are one week: the limit counts weeks, the newest done run
// with an assessment of each, and the weeks ending at to are left out.
func TestDoneWeeksBefore(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	v := AnalysisView{Team: "checkout", Weeks: 4}
	w := time.Date(2030, 1, 7, 0, 0, 0, 0, time.UTC)
	// kind: done, failed, or bare (done without an assessment).
	run := func(to time.Time, end time.Time, kind string) int64 {
		t.Helper()
		id, ok, err := s.ClaimAnalysis(ctx, AnalysisClaim{View: v, From: to.AddDate(0, 0, -28), To: to,
			Trigger: "manual", Model: "m", At: end.Add(-time.Minute)})
		if err != nil || !ok {
			t.Fatalf("claim %v %v", ok, err)
		}
		r := AnalysisResult{Assessment: json.RawMessage(`{}`), At: end}
		switch kind {
		case "failed":
			err = s.FailAnalysis(ctx, id, r)
		case "bare":
			r.Assessment = nil
			err = s.FinishAnalysis(ctx, id, r)
		default:
			err = s.FinishAnalysis(ctx, id, r)
		}
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	run(w.AddDate(0, 0, -21), w.AddDate(0, 0, -20), "done")
	w2 := run(w.AddDate(0, 0, -14), w.AddDate(0, 0, -13), "done")
	run(w.AddDate(0, 0, -14), w.AddDate(0, 0, -12), "bare")
	// More reruns of one week than the limit.
	for i := range 3 {
		run(w.AddDate(0, 0, -7), w.AddDate(0, 0, -6).Add(time.Duration(i)*time.Hour), "done")
	}
	w1 := run(w.AddDate(0, 0, -7), w.AddDate(0, 0, -5), "done")
	run(w.AddDate(0, 0, -7), w.AddDate(0, 0, -4), "failed")
	run(w, w.AddDate(0, 0, 1), "done")
	got, err := s.DoneWeeksBefore(ctx, v, w, 2)
	if err != nil || len(got) != 2 || got[0].ID != w1 || got[1].ID != w2 {
		t.Fatalf("weeks before = %+v, %v; want %d, %d", got, err, w1, w2)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.DoneWeeksBefore(cancelled, v, w, 2); err == nil {
		t.Error("a failed read returned no error")
	}
}
