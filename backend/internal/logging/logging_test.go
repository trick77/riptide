package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"
)

var pinned = time.Date(2026, 4, 28, 10, 0, 0, 123456000, time.UTC)

func capture(level slog.Level) (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := NewHandler(buf, level, "intg", WithClock(func() time.Time { return pinned }))
	return slog.New(h), buf
}

func decode(t *testing.T, line string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("not JSON: %q", line)
	}
	return m
}

// The first keys are fixed whatever the caller passes: Splunk finds the
// timestamp in the first 128 characters, and a tail -f reads uniformly.
func TestLeadingKeyOrder(t *testing.T) {
	log, buf := capture(slog.LevelInfo)
	log.Info("webhook_processed", "zzz", 1, "team", "checkout")
	line := buf.String()
	want := []string{`{"timestamp":`, `"log_level":"info"`, `"service":"riptide-collector"`, `"version":`, `"env":"intg"`, `"msg":"webhook_processed"`, `"zzz":1`, `"team":"checkout"`}
	pos := -1
	for _, w := range want {
		i := strings.Index(line, w)
		if i <= pos {
			t.Fatalf("%s out of order in %s", w, line)
		}
		pos = i
	}
	if !regexp.MustCompile(`^\{"timestamp":"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{6}Z"`).MatchString(line) || !strings.HasSuffix(line, "}\n") {
		t.Errorf("line = %s", line)
	}
}

func TestReservedKeysAreRenamed(t *testing.T) {
	log, buf := capture(slog.LevelInfo)
	log.Info("x", "source", "jenkins", "host", "h", "index", "i", "sourcetype", "s", "time", "t", "event", "e",
		"msg", "m", "service", "other", "version", "v0", "env", "prod", "log_level", "debug", "timestamp", "ts")
	m := decode(t, buf.String())
	for _, k := range []string{"source", "host", "index", "sourcetype", "time", "event"} {
		if _, ok := m[k]; ok {
			t.Errorf("reserved %s on the wire", k)
		}
		if _, ok := m["splunk_"+k]; !ok {
			t.Errorf("splunk_%s missing", k)
		}
	}
	if m["msg"] != "x" || m["service"] != "riptide-collector" || m["env"] != "intg" || m["splunk_msg"] != "m" || m["splunk_env"] != "prod" {
		t.Errorf("own keys overwritten: %v", m)
	}
	if m["splunk_source"] != "jenkins" {
		t.Errorf("value lost: %v", m)
	}
}

func TestLevelsAndNames(t *testing.T) {
	log, buf := capture(slog.LevelWarn)
	log.Info("dropped")
	log.Warn("kept")
	log.Error("bad")
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 || decode(t, lines[0])["log_level"] != "warning" || decode(t, lines[1])["log_level"] != "error" {
		t.Errorf("lines = %v", lines)
	}
	log, buf = capture(slog.LevelDebug)
	log.Debug("d")
	if decode(t, buf.String())["log_level"] != "debug" {
		t.Error(buf.String())
	}
	for in, want := range map[string]slog.Level{
		"DEBUG": slog.LevelDebug, "info": slog.LevelInfo, "WARNING": slog.LevelWarn, " warn ": slog.LevelWarn,
		"ERROR": slog.LevelError, "critical": slog.LevelError, "nonsense": slog.LevelInfo, "": slog.LevelInfo,
	} {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v", in, got)
		}
	}
}

func TestContextBindingAndValues(t *testing.T) {
	log, buf := capture(slog.LevelInfo)
	ctx := With(context.Background(), "request_id", "rid-1", "team", "a")
	ctx = With(ctx, "team", "b")
	if With(ctx) != ctx {
		t.Error("With with no attrs made a new context")
	}
	var nilStr *string
	s := "val"
	log.With("handler_attr", 1).WithGroup("g").InfoContext(ctx, "x", "team", "record",
		"dur", 1500*time.Millisecond, "err", errors.New("boom"), "at", pinned, "ptr", nilStr, "sptr", &s,
		"u", uint64(7), "f", 1.5, "b", true, slog.Group("grp", "k", "v"))
	m := decode(t, buf.String())
	if m["request_id"] != "rid-1" || m["team"] != "b" || m["handler_attr"] != float64(1) {
		t.Errorf("bound = %v", m)
	}
	if m["g.team"] != "record" || m["g.dur"] != "1.5s" || m["g.err"] != "boom" || m["g.ptr"] != nil || m["g.sptr"] != "val" ||
		m["g.u"] != float64(7) || m["g.f"] != 1.5 || m["g.b"] != true || m["g.at"] != "2026-04-28T10:00:00.123456Z" {
		t.Errorf("values = %v", m)
	}
	if grp, ok := m["g.grp"].(map[string]any); !ok || grp["k"] != "v" {
		t.Errorf("group = %v", m["g.grp"])
	}
	if len(Bound(ctx)) != 3 {
		t.Errorf("bound = %v", Bound(ctx))
	}
	nested := log.WithGroup("a").WithGroup("b").WithGroup("")
	buf.Reset()
	nested.With("k", 1).Info("y")
	if decode(t, buf.String())["a.b.k"] != float64(1) {
		t.Errorf("nested group: %s", buf.String())
	}
}

func TestRecordTimeWins(t *testing.T) {
	buf := &bytes.Buffer{}
	h := NewHandler(buf, slog.LevelInfo, "dev")
	r := slog.NewRecord(time.Date(2020, 1, 1, 0, 0, 0, 0, time.FixedZone("x", 3600)), slog.LevelInfo, "m", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), `{"timestamp":"2019-12-31T23:00:00.000000Z"`) {
		t.Errorf("line = %s", buf.String())
	}
}

func TestUnmarshalableValueDoesNotBreakTheLine(t *testing.T) {
	log, buf := capture(slog.LevelInfo)
	log.Info("x", "ch", make(chan int))
	if !strings.Contains(decode(t, buf.String())["ch"].(string), "unsupported type") {
		t.Errorf("line = %s", buf.String())
	}
}

func TestSetup(t *testing.T) {
	l := Setup("DEBUG", "prod")
	if !l.Enabled(context.Background(), slog.LevelDebug) || slog.Default() != l {
		t.Error("Setup did not install a debug logger")
	}
}
