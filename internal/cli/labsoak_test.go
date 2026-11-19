package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab"
)

// soakAPI serves fixed bodies by path, the platform's side of the read.
func soakAPI(view, alarms string) lab.SoakGetter {
	return func(_ context.Context, path string, into any) error {
		switch {
		case path == "/api/v1/views/platform":
			return json.Unmarshal([]byte(view), into)
		case strings.HasSuffix(path, "/alarms"):
			return json.Unmarshal([]byte(alarms), into)
		}
		return fmt.Errorf("unexpected read %s", path)
	}
}

const soakCleanView = `{"figures_read": true, "system": {"name": "platform"},
 "servers": [{"name": "lab-server", "version": "v1.4.0"}],
 "outposts": [{"name": "edge-hq", "loss": 0}],
 "pipeline": {"hops": [{"id": "outpost", "total": {"newest": {"loss": 0}, "last_hour": {"loss": 0}}}],
   "end_to_end_total": {"newest": 0, "last_hour": 0}}}`

func TestRunSoakVerdictWritesTheRecord(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 28, 8, 0, 0, 0, time.UTC)
	o := soakVerdictOpts{since: "2026-11-20T08:00:00Z", tolerance: lab.SoakAlarmTolerance, recordDir: t.TempDir()}
	var out strings.Builder
	if err := runSoakVerdict(context.Background(), &out, soakAPI(soakCleanView, `{"alarms": []}`), o, now, "soak-1 linux/amd64 16 cpu"); err != nil {
		t.Fatalf("a clean week: %v\n%s", err, out.String())
	}
	if !strings.HasPrefix(out.String(), "PASS") {
		t.Errorf("output = %q, want PASS first", out.String())
	}
	rec, err := os.ReadFile(filepath.Join(o.recordDir, "v1.4.0.md"))
	if err != nil {
		t.Fatalf("the record: %v", err)
	}
	for _, want := range []string{"verdict: pass", "build: v1.4.0", "host: soak-1 linux/amd64 16 cpu", "since: 2026-11-20T08:00:00Z"} {
		if !strings.Contains(string(rec), want) {
			t.Errorf("record lacks %q:\n%s", want, rec)
		}
	}
}

func TestRunSoakVerdictFailsOnAnOpenAlarm(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 28, 8, 0, 0, 0, time.UTC)
	o := soakVerdictOpts{since: "2026-11-20T08:00:00Z", tolerance: lab.SoakAlarmTolerance}
	alarms := `{"alarms": [{"component": "edge-hq", "raised_by_rule": "checkin-age", "severity": "high", "raised_at": "2026-11-27T01:00:00Z"}]}`
	var out strings.Builder
	err := runSoakVerdict(context.Background(), &out, soakAPI(soakCleanView, alarms), o, now, "h")
	if err == nil {
		t.Fatal("an open alarm passed: want an error so the exit status is 1")
	}
	if !strings.HasPrefix(out.String(), "FAIL") || !strings.Contains(out.String(), "still open") {
		t.Errorf("output = %q, want FAIL naming the open alarm", out.String())
	}
}

func TestSoakVerdictOptsWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 28, 8, 0, 0, 0, time.UTC)
	for name, o := range map[string]soakVerdictOpts{
		"no since":         {},
		"a bad since":      {since: "yesterday"},
		"a bad until":      {since: "2026-11-20T08:00:00Z", until: "soon"},
		"until before":     {since: "2026-11-20T08:00:00Z", until: "2026-11-19T08:00:00Z"},
		"a negative bound": {since: "2026-11-20T08:00:00Z", tolerance: -time.Minute},
	} {
		if _, _, err := o.window(now); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
	since, until, err := soakVerdictOpts{since: "2026-11-20T08:00:00Z"}.window(now)
	if err != nil || !since.Equal(time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)) || !until.Equal(now) {
		t.Errorf("window = %v, %v, %v; want since parsed and until now", since, until, err)
	}
}
