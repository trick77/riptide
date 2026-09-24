package parse

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validationTypes(t *testing.T, err error) map[string]string {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error %v is not a ValidationError", err)
	}
	out := map[string]string{}
	for _, fe := range ve.Errors {
		out[strings.Join(fe.Loc, ".")] = fe.Type
	}
	return out
}

func expectInvalid(t *testing.T, err error, loc, typ string) {
	t.Helper()
	if err == nil {
		t.Fatalf("accepted; want %s at %s", typ, loc)
	}
	got := validationTypes(t, err)
	if got[loc] != typ {
		t.Fatalf("errors = %v; want %s at %s", got, typ, loc)
	}
}

// --- pipeline ----------------------------------------------------------------

func TestPipelineJenkins(t *testing.T) {
	raw := encode(t, fixture(t, "pipeline_jenkins_completed.json"))
	d, err := Pipeline(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.DeliveryID != "jenkins#payments-api-deploy#1234#COMPLETED" || d.Source != "jenkins" || str(d.Status) != "SUCCESS" {
		t.Errorf("draft = %+v", d)
	}
	if !d.OccurredAt.Equal(*d.FinishedAt) || d.FinishedAt.Sub(d.StartedAt) != 210*time.Second {
		t.Errorf("times = %v %v %v", d.StartedAt, d.FinishedAt, d.OccurredAt)
	}
	if d.ActorHandle != nil || d.ActorAccountKind != nil || d.ImageRef != nil {
		t.Errorf("optional fields = %+v", d)
	}
	if string(d.Payload) != string(raw) {
		t.Error("payload is not the raw body")
	}
}

func TestPipelineFields(t *testing.T) {
	base := func() map[string]any { return fixture(t, "pipeline_jenkins_completed.json") }
	t.Run("image ref and actor declaration", func(t *testing.T) {
		b := base()
		b["image_ref"] = "  registry.example.com/acme/payments-api:2.0.41 "
		b["actor_handle"] = "ci-service"
		d, err := Pipeline(encode(t, b))
		if err != nil {
			t.Fatal(err)
		}
		if str(d.ImageRef) != "registry.example.com/acme/payments-api:2.0.41" || str(d.ActorHandle) != "ci-service" || str(d.ActorAccountKind) != "service" {
			t.Errorf("draft = %+v", d)
		}
	})
	t.Run("whitespace kind is the default", func(t *testing.T) {
		b := base()
		b["actor_handle"] = "ci"
		b["actor_account_kind"] = "  "
		d, err := Pipeline(encode(t, b))
		if err != nil || str(d.ActorAccountKind) != "service" {
			t.Errorf("kind = %s, %v", str(d.ActorAccountKind), err)
		}
	})
	t.Run("declared kind kept", func(t *testing.T) {
		b := base()
		b["actor_handle"] = "renovate"
		b["actor_account_kind"] = "bot"
		d, _ := Pipeline(encode(t, b))
		if str(d.ActorAccountKind) != "bot" {
			t.Errorf("kind = %s", str(d.ActorAccountKind))
		}
	})
	t.Run("empty strings are absent", func(t *testing.T) {
		b := base()
		b["image_ref"] = ""
		b["actor_handle"] = "   "
		b["actor_account_kind"] = ""
		b["status"] = ""
		b["finished_at"] = ""
		d, err := Pipeline(encode(t, b))
		if err != nil {
			t.Fatal(err)
		}
		if d.ImageRef != nil || d.ActorHandle != nil || d.ActorAccountKind != nil || d.Status != nil || d.FinishedAt != nil {
			t.Errorf("draft = %+v", d)
		}
		if !d.OccurredAt.Equal(d.StartedAt) {
			t.Error("occurred_at should fall back to started_at")
		}
	})
	t.Run("commit sha lowercased, naive time is UTC", func(t *testing.T) {
		b := base()
		b["commit_sha"] = "ABC1234567890ABC"
		b["started_at"] = "2026-04-28T10:05:00"
		b["finished_at"] = "2026-04-28T10:06:00"
		d, err := Pipeline(encode(t, b))
		if err != nil {
			t.Fatal(err)
		}
		if d.CommitSHA != "abc1234567890abc" || !d.StartedAt.Equal(time.Date(2026, 4, 28, 10, 5, 0, 0, time.UTC)) {
			t.Errorf("draft = %+v", d)
		}
	})
	t.Run("epoch seconds and milliseconds", func(t *testing.T) {
		b := base()
		b["started_at"] = 1777370700
		b["finished_at"] = 1777370760000
		d, err := Pipeline(encode(t, b))
		if err != nil {
			t.Fatal(err)
		}
		if d.FinishedAt.Sub(d.StartedAt) != time.Minute {
			t.Errorf("times = %v %v", d.StartedAt, d.FinishedAt)
		}
	})
	t.Run("extra fields allowed", func(t *testing.T) {
		b := base()
		b["build_url"] = "https://jenkins/job/1"
		if _, err := Pipeline(encode(t, b)); err != nil {
			t.Fatal(err)
		}
	})
	for _, c := range []struct {
		name, loc, typ string
		mutate         func(map[string]any)
	}{
		{"missing pipeline_name", "body.pipeline_name", "missing", func(b map[string]any) { delete(b, "pipeline_name") }},
		{"null source", "body.source", "missing", func(b map[string]any) { b["source"] = nil }},
		{"empty run_id", "body.run_id", "string_too_short", func(b map[string]any) { b["run_id"] = "" }},
		{"short commit sha", "body.commit_sha", "string_too_short", func(b map[string]any) { b["commit_sha"] = "abc" }},
		{"numeric phase", "body.phase", "string_type", func(b map[string]any) { b["phase"] = 3 }},
		{"bad kind", "body.actor_account_kind", "literal_error", func(b map[string]any) { b["actor_account_kind"] = "robot" }},
		{"missing started_at", "body.started_at", "missing", func(b map[string]any) { delete(b, "started_at") }},
		{"bad started_at", "body.started_at", "datetime_from_date_parsing", func(b map[string]any) { b["started_at"] = "yesterday" }},
		{"bool started_at", "body.started_at", "datetime_type", func(b map[string]any) { b["started_at"] = true }},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := base()
			c.mutate(b)
			_, err := Pipeline(encode(t, b))
			expectInvalid(t, err, c.loc, c.typ)
		})
	}
	t.Run("all errors reported at once", func(t *testing.T) {
		_, err := Pipeline([]byte(`{}`))
		if got := validationTypes(t, err); len(got) != 6 {
			t.Errorf("errors = %v", got)
		}
	})
	t.Run("not an object", func(t *testing.T) {
		_, err := Pipeline([]byte(`[1]`))
		expectInvalid(t, err, "body", "model_attributes_type")
		_, err = Pipeline([]byte(`{bad`))
		expectInvalid(t, err, "body", "json_invalid")
		_, err = Pipeline([]byte(`{"source":"x"}}`))
		expectInvalid(t, err, "body", "json_invalid")
		_, err = Pipeline([]byte("{\"source\":\"\xff\"}"))
		expectInvalid(t, err, "body", "json_invalid")
		_, err = Pipeline([]byte(`{"source":"\u0000"}`))
		expectInvalid(t, err, "body", "json_invalid")
		_, err = Pipeline([]byte(`null`))
		expectInvalid(t, err, "body", "model_attributes_type")
	})
}

// --- argocd ------------------------------------------------------------------

func TestArgoCD(t *testing.T) {
	base := func() map[string]any { return fixture(t, "argocd_synced.json") }
	t.Run("synced", func(t *testing.T) {
		raw := encode(t, base())
		d, err := ArgoCD(raw, fixedNow)
		if err != nil {
			t.Fatal(err)
		}
		if d.DeliveryID != "payments-api-prod#abc1234567890abc1234567890abc1234567890a#2026-04-28T10:09:00+00:00#Succeeded" {
			t.Errorf("delivery id = %q", d.DeliveryID)
		}
		if str(d.Environment) != "prod" || str(d.DestinationNamespace) != "payments-prod" || !d.OccurredAt.Equal(*d.FinishedAt) {
			t.Errorf("draft = %+v", d)
		}
	})
	t.Run("uppercase revision lowercased", func(t *testing.T) {
		b := base()
		b["revision"] = "ABCDEF1234567"
		d, _ := ArgoCD(encode(t, b), fixedNow)
		if d.Revision != "abcdef1234567" {
			t.Errorf("revision = %q", d.Revision)
		}
	})
	t.Run("no namespace, no environment", func(t *testing.T) {
		b := base()
		delete(b, "destination_namespace")
		d, _ := ArgoCD(encode(t, b), fixedNow)
		if d.DestinationNamespace != nil || d.Environment != nil {
			t.Errorf("draft = %+v", d)
		}
	})
	t.Run("namespace without suffix stored, environment null", func(t *testing.T) {
		b := base()
		b["destination_namespace"] = " payments "
		d, _ := ArgoCD(encode(t, b), fixedNow)
		if str(d.DestinationNamespace) != "payments" || d.Environment != nil {
			t.Errorf("draft = %+v", d)
		}
	})
	t.Run("running sync renders finished_at empty", func(t *testing.T) {
		b := base()
		b["operation_phase"] = "Running"
		b["finished_at"] = ""
		b["sync_status"] = ""
		d, err := ArgoCD(encode(t, b), fixedNow)
		if err != nil {
			t.Fatal(err)
		}
		if d.FinishedAt != nil || d.SyncStatus != nil || !d.OccurredAt.Equal(*d.StartedAt) {
			t.Errorf("draft = %+v", d)
		}
	})
	t.Run("no timestamps: now, and unknown in the key", func(t *testing.T) {
		b := base()
		delete(b, "started_at")
		delete(b, "finished_at")
		delete(b, "operation_phase")
		d, _ := ArgoCD(encode(t, b), fixedNow)
		if !d.OccurredAt.Equal(fixedNow) || !strings.HasSuffix(d.DeliveryID, "#unknown#unknown") {
			t.Errorf("draft = %+v", d)
		}
	})
	t.Run("empty images allowed", func(t *testing.T) {
		b := base()
		b["images"] = []any{}
		if _, err := ArgoCD(encode(t, b), fixedNow); err != nil {
			t.Fatal(err)
		}
	})
	for _, c := range []struct {
		name, loc, typ string
		mutate         func(map[string]any)
	}{
		{"missing revision", "body.revision", "missing", func(b map[string]any) { delete(b, "revision") }},
		{"short revision", "body.revision", "string_too_short", func(b map[string]any) { b["revision"] = "abc" }},
		{"missing images", "body.images", "missing", func(b map[string]any) { delete(b, "images") }},
		{"images not a list", "body.images", "list_type", func(b map[string]any) { b["images"] = "x" }},
		{"image not a string", "body.images.0", "string_type", func(b map[string]any) { b["images"] = []any{1} }},
		{"empty app", "body.app_name", "string_too_short", func(b map[string]any) { b["app_name"] = "" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := base()
			c.mutate(b)
			_, err := ArgoCD(encode(t, b), fixedNow)
			expectInvalid(t, err, c.loc, c.typ)
		})
	}
	t.Run("not an object", func(t *testing.T) {
		_, err := ArgoCD([]byte(`"x"`), fixedNow)
		expectInvalid(t, err, "body", "model_attributes_type")
	})
}

// --- noergler ----------------------------------------------------------------

func TestNoerglerPRCompleted(t *testing.T) {
	base := func() map[string]any { return fixture(t, "noergler_pr_completed_merged.json") }
	t.Run("merged", func(t *testing.T) {
		d, err := Noergler(encode(t, base()))
		if err != nil {
			t.Fatal(err)
		}
		if d.DeliveryID != "pr_completed#proj/payments-api#42#merged" || d.PRKey != "proj/payments-api#42" || str(d.Repo) != "acme/payments-api" {
			t.Errorf("draft = %+v", d)
		}
		if str(d.CommitSHA) != "abc1234567890abc1234567890abc1234567890a" || str(d.MergeCommitSHA) != "def4567890abc1234567890abc1234567890abcd" {
			t.Errorf("shas = %s %s", str(d.CommitSHA), str(d.MergeCommitSHA))
		}
		if *d.LinesAdded != 320 || *d.TotalRuns != 3 || *d.PromptTokens != 38420 || str(d.CostUSD) != "0.382100" || d.ModelsUsed[0] != "gpt-4o-2024-08-06" {
			t.Errorf("draft = %+v", d)
		}
		if !d.OccurredAt.Equal(time.Date(2026, 4, 29, 18, 42, 0, 0, time.UTC)) || d.FirstReviewAt == nil {
			t.Errorf("times = %v %v", d.OccurredAt, d.FirstReviewAt)
		}
		if d.ReviewerHandle != nil || d.ReviewerAccountKind != nil || d.FindingID != nil {
			t.Errorf("draft = %+v", d)
		}
	})
	t.Run("declined and deleted", func(t *testing.T) {
		for _, f := range []string{"noergler_pr_completed_declined.json", "noergler_pr_completed_deleted.json"} {
			d, err := Noergler(encode(t, fixture(t, f)))
			if err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			if d.MergeCommitSHA != nil || !strings.HasSuffix(d.DeliveryID, "#"+*d.Outcome) {
				t.Errorf("%s: %+v", f, d)
			}
		}
	})
	t.Run("naive closed_at is UTC", func(t *testing.T) {
		b := base()
		b["closed_at"] = "2026-04-29T18:42:00"
		d, _ := Noergler(encode(t, b))
		if !d.OccurredAt.Equal(time.Date(2026, 4, 29, 18, 42, 0, 0, time.UTC)) {
			t.Errorf("closed = %v", d.OccurredAt)
		}
	})
	t.Run("reviewer handle, case preserved, kind default bot", func(t *testing.T) {
		b := base()
		b["reviewer_handle"] = " Rop "
		d, _ := Noergler(encode(t, b))
		if str(d.ReviewerHandle) != "Rop" || str(d.ReviewerAccountKind) != "bot" {
			t.Errorf("reviewer = %s/%s", str(d.ReviewerHandle), str(d.ReviewerAccountKind))
		}
		b["reviewer_account_kind"] = "human"
		d, _ = Noergler(encode(t, b))
		if str(d.ReviewerAccountKind) != "human" {
			t.Errorf("kind = %s", str(d.ReviewerAccountKind))
		}
	})
	t.Run("empty reviewer handle is absent", func(t *testing.T) {
		b := base()
		b["reviewer_handle"] = ""
		b["reviewer_account_kind"] = "service"
		d, _ := Noergler(encode(t, b))
		if d.ReviewerHandle != nil || d.ReviewerAccountKind != nil {
			t.Errorf("reviewer = %+v", d)
		}
	})
	t.Run("cost optional, numbers and strings, rounding and range", func(t *testing.T) {
		b := base()
		delete(b, "total_cost_usd")
		if d, err := Noergler(encode(t, b)); err != nil || d.CostUSD != nil {
			t.Fatalf("no cost: %v %v", d, err)
		}
		for in, want := range map[any]string{
			0.25: "0.250000", "1e-3": "0.001000", 0.30000000000000004: "0.300000", "999999.9999994": "999999.999999",
		} {
			b["total_cost_usd"] = in
			d, err := Noergler(encode(t, b))
			if err != nil || str(d.CostUSD) != want {
				t.Errorf("cost %v = %s, %v", in, str(d.CostUSD), err)
			}
		}
	})
	t.Run("integral floats and numeric strings are integers", func(t *testing.T) {
		b := base()
		b["lines_added"] = 5.0
		b["total_runs"] = "2"
		d, err := Noergler(encode(t, b))
		if err != nil || *d.LinesAdded != 5 || *d.TotalRuns != 2 {
			t.Fatalf("draft = %+v, %v", d, err)
		}
	})
	for _, c := range []struct {
		name, loc, typ string
		mutate         func(map[string]any)
	}{
		{"negative tokens", "body.total_prompt_tokens", "greater_than_equal", func(b map[string]any) { b["total_prompt_tokens"] = -1 }},
		{"zero runs", "body.total_runs", "greater_than_equal", func(b map[string]any) { b["total_runs"] = 0 }},
		{"fractional count", "body.lines_added", "int_parsing", func(b map[string]any) { b["lines_added"] = 1.5 }},
		{"beyond int64", "body.total_elapsed_ms", "int_parsing", func(b map[string]any) { b["total_elapsed_ms"] = "99999999999999999999" }},
		{"bool count", "body.files_changed", "int_type", func(b map[string]any) { b["files_changed"] = true }},
		{"missing count", "body.total_findings_count", "missing", func(b map[string]any) { delete(b, "total_findings_count") }},
		{"empty models", "body.models_used", "too_short", func(b map[string]any) { b["models_used"] = []any{} }},
		{"blank model", "body.models_used", "value_error", func(b map[string]any) { b["models_used"] = []any{"m", " "} }},
		{"merged without merge sha", "body.merge_commit_sha", "value_error", func(b map[string]any) { delete(b, "merge_commit_sha") }},
		{"merged with empty merge sha", "body.merge_commit_sha", "value_error", func(b map[string]any) { b["merge_commit_sha"] = "" }},
		{"short merge sha", "body.merge_commit_sha", "string_too_short", func(b map[string]any) { b["merge_commit_sha"] = "abc" }},
		{"negative cost", "body.total_cost_usd", "greater_than_equal", func(b map[string]any) { b["total_cost_usd"] = "-1.0" }},
		{"cost too large", "body.total_cost_usd", "less_than_equal", func(b map[string]any) { b["total_cost_usd"] = 1000000 }},
		{"cost not a number", "body.total_cost_usd", "decimal_parsing", func(b map[string]any) { b["total_cost_usd"] = "cheap" }},
		{"cost wrong type", "body.total_cost_usd", "decimal_type", func(b map[string]any) { b["total_cost_usd"] = []any{} }},
		{"bad outcome", "body.outcome", "literal_error", func(b map[string]any) { b["outcome"] = "abandoned" }},
		{"bad kind", "body.reviewer_account_kind", "literal_error", func(b map[string]any) { b["reviewer_handle"] = "svc"; b["reviewer_account_kind"] = "robot" }},
		{"unknown field", "body.surprise", "extra_forbidden", func(b map[string]any) { b["surprise"] = 1 }},
		{"missing first review", "body.first_review_at", "missing", func(b map[string]any) { delete(b, "first_review_at") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := base()
			c.mutate(b)
			_, err := Noergler(encode(t, b))
			expectInvalid(t, err, c.loc, c.typ)
		})
	}
	t.Run("declined with merge sha", func(t *testing.T) {
		b := fixture(t, "noergler_pr_completed_declined.json")
		b["merge_commit_sha"] = "def4567890abc1234567890abc1234567890abcd"
		_, err := Noergler(encode(t, b))
		expectInvalid(t, err, "body.merge_commit_sha", "value_error")
	})
}

func TestNoerglerFeedback(t *testing.T) {
	base := func() map[string]any { return fixture(t, "noergler_feedback.json") }
	d, err := Noergler(encode(t, base()))
	if err != nil {
		t.Fatal(err)
	}
	if d.DeliveryID != "feedback#finding-2026-04-29-0001#disagreed" || str(d.Actor) != "alice@example.com" ||
		d.PRKey != "proj/payments-api#42" || str(d.Verdict) != "disagreed" || d.Outcome != nil || d.ModelsUsed != nil {
		t.Errorf("draft = %+v", d)
	}
	b := base()
	delete(b, "commit_sha")
	b["repo"] = ""
	if d, err = Noergler(encode(t, b)); err != nil || d.CommitSHA != nil || d.Repo != nil {
		t.Errorf("optional fields: %+v, %v", d, err)
	}
	b = base()
	b["verdict"] = "shrug"
	_, err = Noergler(encode(t, b))
	expectInvalid(t, err, "body.verdict", "literal_error")
	b = base()
	b["total_runs"] = 1
	_, err = Noergler(encode(t, b))
	expectInvalid(t, err, "body.total_runs", "extra_forbidden")
}

func TestNoerglerEventType(t *testing.T) {
	for _, c := range []struct{ body, typ string }{
		{`{"event_type": "surprise"}`, "union_tag_invalid"},
		{`{"event_type": "completed"}`, "union_tag_invalid"},
		{`{"event_type": ""}`, "union_tag_invalid"},
		{`{}`, "union_tag_not_found"},
		{`{"event_type": 1}`, "string_type"},
	} {
		_, err := Noergler([]byte(c.body))
		expectInvalid(t, err, "body.event_type", c.typ)
	}
	_, err := Noergler([]byte(`[]`))
	expectInvalid(t, err, "body", "model_attributes_type")
}

func TestValidationErrorMessage(t *testing.T) {
	_, err := ArgoCD([]byte(`{}`), fixedNow)
	if !strings.Contains(err.Error(), "body.app_name: Field required") {
		t.Errorf("message = %q", err.Error())
	}
}
