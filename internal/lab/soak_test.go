package lab

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"
)

var soakStart = time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)

// cleanWeek is a soak that ran its full week, raised nothing and lost nothing:
// each case below breaks one thing in it.
func cleanWeek() SoakReading {
	return SoakReading{
		Build: "v1.4.0",
		Since: soakStart,
		Until: soakStart.Add(SoakMinWindow),
		Losses: []SoakLoss{
			{Where: "outpost hop, last hour", Ratio: 0},
			{Where: "end to end, newest", Ratio: 0},
		},
	}
}

func clearedAfter(raised time.Time, open time.Duration) SoakAlarm {
	cleared := raised.Add(open)
	return SoakAlarm{Component: "lab-server", Rule: "ingest-lag", Severity: "high", RaisedAt: raised, ClearedAt: &cleared}
}

func TestJudgeSoak(t *testing.T) {
	t.Parallel()
	day := 24 * time.Hour
	tests := []struct {
		name   string
		mutate func(*SoakReading)
		pass   bool
		// reason is a fragment one of the verdict's reasons must carry.
		reason string
	}{
		{name: "a clean week passes", mutate: func(*SoakReading) {}, pass: true},
		{
			name: "an alarm that cleared inside the tolerance passes",
			mutate: func(r *SoakReading) {
				r.Alarms = []SoakAlarm{clearedAfter(soakStart.Add(day), SoakAlarmTolerance)}
			},
			pass: true,
		},
		{
			name: "an alarm left open fails",
			mutate: func(r *SoakReading) {
				r.Alarms = []SoakAlarm{{Component: "edge-hq", Rule: "checkin-age", Severity: "high", RaisedAt: soakStart.Add(6 * day)}}
			},
			reason: "still open",
		},
		{
			name: "an alarm raised before the window and still open fails",
			mutate: func(r *SoakReading) {
				r.Alarms = []SoakAlarm{{Component: "edge-hq", Rule: "checkin-age", RaisedAt: soakStart.Add(-day)}}
			},
			reason: "still open",
		},
		{
			name: "an alarm open past the tolerance fails",
			mutate: func(r *SoakReading) {
				r.Alarms = []SoakAlarm{clearedAfter(soakStart.Add(day), SoakAlarmTolerance+time.Minute)}
			},
			reason: "open for",
		},
		{
			name: "an alarm that came and went before the window is not the soak's",
			mutate: func(r *SoakReading) {
				r.Alarms = []SoakAlarm{clearedAfter(soakStart.Add(-2*day), day)}
			},
			pass: true,
		},
		{
			name: "a nonzero loss ratio fails",
			mutate: func(r *SoakReading) {
				r.Losses = append(r.Losses, SoakLoss{Where: "ingest hop, last hour", Ratio: 0.0004})
			},
			reason: "ingest hop, last hour",
		},
		{
			name:   "a window shorter than the week fails",
			mutate: func(r *SoakReading) { r.Until = r.Since.Add(SoakMinWindow - time.Hour) },
			reason: "ran",
		},
		{
			name:   "alarms the API may have cut short fail",
			mutate: func(r *SoakReading) { r.AlarmsTruncated = true },
			reason: "cap",
		},
		{
			name:   "no build fails",
			mutate: func(r *SoakReading) { r.Build = "" },
			reason: "build",
		},
		{
			name:   "no loss ratio to read fails",
			mutate: func(r *SoakReading) { r.Losses = nil },
			reason: "loss",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := cleanWeek()
			tt.mutate(&r)
			v := JudgeSoak(r, SoakAlarmTolerance)
			if v.Pass != tt.pass {
				t.Fatalf("pass = %v, want %v; reasons: %v", v.Pass, tt.pass, v.Reasons)
			}
			if tt.pass {
				if len(v.Reasons) != 0 {
					t.Errorf("a pass carries reasons %v", v.Reasons)
				}
				return
			}
			if !strings.Contains(strings.Join(v.Reasons, "\n"), tt.reason) {
				t.Errorf("reasons %v lack %q", v.Reasons, tt.reason)
			}
		})
	}
}

// Every failing thing is named, not just the first: a soak host is read once a
// week, and a verdict that hid the second problem would cost a second week.
func TestJudgeSoakNamesEveryReason(t *testing.T) {
	t.Parallel()
	r := cleanWeek()
	r.Alarms = []SoakAlarm{
		{Component: "edge-hq", Rule: "checkin-age", RaisedAt: soakStart.Add(time.Hour)},
		clearedAfter(soakStart.Add(2*time.Hour), 3*time.Hour),
	}
	r.Losses = append(r.Losses, SoakLoss{Where: "outpost hop, newest", Ratio: 1})
	v := JudgeSoak(r, SoakAlarmTolerance)
	if v.Pass || len(v.Reasons) != 3 {
		t.Fatalf("verdict = %+v, want a fail with three reasons", v)
	}
}

// The tolerance is the caller's to state, and what it states is what is judged.
func TestJudgeSoakUsesTheStatedTolerance(t *testing.T) {
	t.Parallel()
	r := cleanWeek()
	r.Alarms = []SoakAlarm{clearedAfter(soakStart.Add(time.Hour), 2*time.Hour)}
	if JudgeSoak(r, time.Hour).Pass {
		t.Error("two hours open passed a one hour tolerance")
	}
	if !JudgeSoak(r, 3*time.Hour).Pass {
		t.Error("two hours open failed a three hour tolerance")
	}
}

// The verdict reads only from the platform's API: the files that judge and
// read a soak import neither the database, the Storage Gateway, a process
// runner nor the measurement harness, so no later edit can reach around the
// API without this test saying so.
func TestSoakReadsOnlyFromTheAPI(t *testing.T) {
	t.Parallel()
	banned := []string{"internal/storage", "github.com/jackc/pgx", "database/sql", "os/exec", "internal/labfootprint", "internal/lab/scenario"}
	for _, file := range []string{"soak.go", "soak_read.go", "soak_record.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, b := range banned {
				if strings.Contains(path, b) {
					t.Errorf("%s imports %s: the soak verdict reads the platform through its API alone", file, path)
				}
			}
		}
	}
}
