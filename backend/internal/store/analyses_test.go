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
