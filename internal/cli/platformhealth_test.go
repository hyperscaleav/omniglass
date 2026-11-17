package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// healthyPlatformView is a platform view read from a healthy server: every
// verdict healthy, no alarm, nothing lost end to end.
const healthyPlatformView = `{
  "read_at": "2026-11-17T12:00:00Z",
  "lab": true,
  "figures_read": true,
  "system": {"id": "s1", "name": "omniglass", "verdict": "healthy"},
  "servers": [{
    "component": "c1", "name": "lab-server", "version": "v9.9.9",
    "started_at": "2026-11-17T10:00:00Z", "verdict": "healthy",
    "groups": [
      {"name": "requests", "label": "Requests",
       "keys": [{"name": "db-pool-wait-count", "label": "Pool Waits"},
                {"name": "db-slow-query-count", "label": "Slow Queries"}],
       "figures": [{"key": "db-pool-wait-count", "instance": "", "value": 0, "observed_at": "2026-11-17T11:59:30Z"},
                   {"key": "db-slow-query-count", "instance": "", "value": 0, "observed_at": "2026-11-17T11:59:30Z"}]},
      {"name": "ingest", "label": "Ingest",
       "keys": [{"name": "ingest-lag", "label": "Ingest Lag", "unit": "s"},
                {"name": "ingest-lag-max", "label": "Ingest Lag (max)", "unit": "s"},
                {"name": "push-lane-refusal-count", "label": "Push Lane Refusals"}],
       "figures": [{"key": "ingest-lag", "instance": "", "value": 0.25, "observed_at": "2026-11-17T11:59:30Z"},
                   {"key": "ingest-lag-max", "instance": "", "value": 1.5, "observed_at": "2026-11-17T11:59:30Z"},
                   {"key": "push-lane-refusal-count", "instance": "", "value": 0, "observed_at": "2026-11-17T11:59:30Z"}]},
      {"name": "queues", "label": "Queues",
       "keys": [{"name": "bus-consumer-pending", "label": "Telemetry Backlog"},
                {"name": "bus-consumer-ack-floor", "label": "Telemetry Ack Floor"},
                {"name": "bus-stream-last-seq", "label": "Telemetry Stream Head"},
                {"name": "ingest-lane-busy", "label": "Ingest Lane Busy", "unit": "ratio"}],
       "figures": [{"key": "bus-consumer-pending", "instance": "", "value": 3, "observed_at": "2026-11-17T11:59:30Z"},
                   {"key": "bus-consumer-ack-floor", "instance": "", "value": 1000, "observed_at": "2026-11-17T11:59:30Z"},
                   {"key": "bus-stream-last-seq", "instance": "", "value": 1004, "observed_at": "2026-11-17T11:59:30Z"},
                   {"key": "ingest-lane-busy", "instance": "", "value": 0.125, "observed_at": "2026-11-17T11:59:30Z"}]},
      {"name": "retention", "label": "Retention",
       "keys": [{"name": "database-bytes", "label": "Database Size", "unit": "bytes"},
                {"name": "table-bytes", "label": "Table Size", "unit": "bytes"}],
       "figures": [{"key": "database-bytes", "instance": "", "value": 2147483648, "observed_at": "2026-11-17T11:59:00Z"},
                   {"key": "table-bytes", "instance": "metric_sample", "value": 1048576, "observed_at": "2026-11-17T11:59:00Z"}]}
    ],
    "alarms": []
  }],
  "groups": [],
  "nodes": [
    {"name": "node-a", "label": "Node A", "enrolled": true, "verdict": "healthy", "loss": 0},
    {"name": "node-b", "label": "Node B", "enrolled": true, "verdict": "healthy"}
  ],
  "loss_hops": ["node"],
  "pipeline": {
    "keys": [],
    "hops": [
      {"id": "node", "label": "Node", "description": "", "component_class": "node", "keys": [], "kinds": [],
       "total": {"newest": {"in": 100, "out": 100, "loss": 0}, "last_hour": {"in": 6000, "out": 6000, "loss": 0}},
       "by_component": []},
      {"id": "ingest", "label": "Ingest", "description": "", "component_class": "server", "keys": [], "kinds": [],
       "total": {"newest": {"in": 100, "out": 100, "loss": 0}, "last_hour": {"in": 6000, "out": 6000, "loss": 0}},
       "by_component": []}
    ],
    "end_to_end": [],
    "end_to_end_total": {"newest": 0, "last_hour": 0}
  }
}`

// platformFixture is the healthy view decoded, for a test to change one fact.
func platformFixture(t *testing.T) platformView {
	t.Helper()
	v, err := decodePlatformView([]byte(healthyPlatformView))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// platformServer serves view as the platform view and a one-system fleet
// whose system page reads all answer, counting the platform view's reads.
func platformServer(t *testing.T, view func(n int64) string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var reads atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/views/platform":
			n := reads.Add(1)
			_, _ = w.Write([]byte(view(n)))
		case r.URL.Path == "/api/v1/views/fleet":
			_, _ = w.Write([]byte(`{"systems":[{"id":"sys-1","name":"room-1"}]}`))
		case strings.HasSuffix(r.URL.Path, "/metrics"):
			_, _ = w.Write([]byte(`{"metrics":[]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &reads
}

func staticView(body string) func(int64) string { return func(int64) string { return body } }

// runPlatformHealth runs `platform health` against server with its own
// stdout and stderr, so a test can parse the JSON on stdout alone.
func runPlatformHealth(t *testing.T, server string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("OMNIGLASS_TOKEN", "")
	t.Setenv("OMNIGLASS_SERVER", "")
	var stdout, stderr bytes.Buffer
	root := Root("test")
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"platform", "health", "--server", server}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestPlatformHealthReadsAHealthyPlatform(t *testing.T) {
	r := readPlatformHealth(platformFixture(t))
	if !r.Healthy || len(r.Reasons) != 0 {
		t.Fatalf("healthy view read as healthy=%v reasons=%v", r.Healthy, r.Reasons)
	}
	if len(r.NotVisible) != 0 {
		t.Fatalf("not visible = %v, want none", r.NotVisible)
	}
	if r.System == nil || r.System.Name != "omniglass" || r.System.Verdict != "healthy" {
		t.Fatalf("system = %+v", r.System)
	}
	if len(r.Servers) != 1 {
		t.Fatalf("servers = %d, want 1", len(r.Servers))
	}
	s := r.Servers[0]
	if s.Version != "v9.9.9" || s.UptimeSeconds == nil || *s.UptimeSeconds != 7200 {
		t.Fatalf("server identity = version %q uptime %v, want v9.9.9 and 7200 s", s.Version, s.UptimeSeconds)
	}
	if s.Backlog == nil || *s.Backlog != 4 {
		t.Fatalf("backlog = %v, want the stream head less the ack floor, 4", s.Backlog)
	}
	got := map[string]float64{}
	for _, f := range s.Figures {
		got[f.Key] = f.Value
	}
	for key, want := range map[string]float64{"ingest-lag": 0.25, "ingest-lag-max": 1.5, "ingest-lane-busy": 0.125, "bus-consumer-pending": 3, "database-bytes": 2147483648, "db-pool-wait-count": 0} {
		if v, ok := got[key]; !ok || v != want {
			t.Errorf("figure %s = %v (present %v), want %v", key, v, ok, want)
		}
	}
	if _, ok := got["table-bytes"]; ok {
		t.Errorf("table-bytes is not a headline figure; got it in %v", got)
	}
	if r.Nodes.Count != 2 || len(r.Nodes.Lossy) != 0 || len(r.Nodes.NotHealthy) != 0 {
		t.Fatalf("nodes = %+v", r.Nodes)
	}
	if r.Pipeline == nil || len(r.Pipeline.Hops) != 2 || r.Pipeline.Hops[0].ID != "node" || r.Pipeline.Hops[0].Newest.In != 100 || r.Pipeline.Hops[1].LastHour.Out != 6000 {
		t.Fatalf("pipeline = %+v", r.Pipeline)
	}
	if r.Pipeline.EndToEnd.Newest == nil || *r.Pipeline.EndToEnd.Newest != 0 {
		t.Fatalf("end to end = %+v, want newest 0", r.Pipeline.EndToEnd)
	}
}

func TestPlatformHealthBacklogNeedsBothPositions(t *testing.T) {
	v := platformFixture(t)
	figs := v.Servers[0].Groups[2].Figures
	v.Servers[0].Groups[2].Figures = figs[:1] // pending only: no head, no floor
	if b := readPlatformHealth(v).Servers[0].Backlog; b != nil {
		t.Fatalf("backlog = %v with no stream positions, want absent", *b)
	}
}

func TestPlatformHealthPicksNonzeroFailures(t *testing.T) {
	v := platformFixture(t)
	v.Servers[0].Groups[0].Figures[1].Value = 2 // db-slow-query-count
	v.Servers[0].Groups[1].Figures[2].Value = 5 // push-lane-refusal-count
	v.Servers[0].Groups[1].Figures[0].Value = 9 // ingest-lag: not a failure figure
	v.Nodes[0].Figures = map[string]platformViewNodeFigure{"command-expired-count": {Value: 1}, "command-pending-count": {Value: 7}}
	r := readPlatformHealth(v)
	got := map[string]float64{}
	for _, f := range r.Failures {
		got[f.Source+" "+f.Key] = f.Value
	}
	want := map[string]float64{
		"lab-server db-slow-query-count":     2,
		"lab-server push-lane-refusal-count": 5,
		"node-a command-expired-count":       1,
	}
	if len(got) != len(want) {
		t.Fatalf("failures = %v, want %v", got, want)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("failure %s = %v, want %v", k, got[k], w)
		}
	}
}

func TestPlatformHealthRollsUpNodes(t *testing.T) {
	v := platformFixture(t)
	loss := 0.02
	v.Nodes[0].Loss = &loss
	v.Nodes[1].Verdict = "degraded"
	v.Nodes = append(v.Nodes, platformViewNode{Name: "node-c"}) // never reported: no verdict
	r := readPlatformHealth(v)
	if r.Nodes.Count != 3 {
		t.Fatalf("count = %d, want 3", r.Nodes.Count)
	}
	if len(r.Nodes.Lossy) != 1 || r.Nodes.Lossy[0].Name != "node-a" || r.Nodes.Lossy[0].Loss != 0.02 {
		t.Fatalf("lossy = %+v", r.Nodes.Lossy)
	}
	if len(r.Nodes.NotHealthy) != 1 || r.Nodes.NotHealthy[0].Name != "node-b" || r.Nodes.NotHealthy[0].Verdict != "degraded" {
		t.Fatalf("not healthy = %+v", r.Nodes.NotHealthy)
	}
}

func TestPlatformHealthReasons(t *testing.T) {
	loss := 0.1
	cases := []struct {
		name   string
		change func(*platformView)
		reason string
	}{
		{"system verdict", func(v *platformView) { v.System.Verdict = "outage" }, "the platform system reads outage"},
		{"server verdict", func(v *platformView) { v.Servers[0].Verdict = "degraded" }, "server lab-server reads degraded"},
		{"node verdict", func(v *platformView) { v.Nodes[1].Verdict = "outage" }, "node node-b reads outage"},
		{"alarm", func(v *platformView) {
			v.Servers[0].Alarms = []platformViewAlarm{{Rule: "ingest-lag-high", Severity: "warning", Message: "lag"}}
		}, "alarm open on lab-server: ingest-lag-high (warning)"},
		{"hand-raised alarm", func(v *platformView) {
			v.Servers[0].Alarms = []platformViewAlarm{{Severity: "high", Message: "disk swap"}}
		}, "alarm open on lab-server: disk swap (high)"},
		{"end to end loss", func(v *platformView) { v.Pipeline.EndToEndTotal.Newest = &loss }, "the newest windows lost 0.1 of their work end to end"},
		{"figures not visible", func(v *platformView) { v.FiguresRead = false }, "the platform's figures are not visible to you"},
		{"system not visible", func(v *platformView) { v.System = nil }, "the platform system's verdict is not visible to you"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := platformFixture(t)
			c.change(&v)
			r := readPlatformHealth(v)
			if r.Healthy {
				t.Fatal("read as healthy")
			}
			if len(r.Reasons) != 1 || r.Reasons[0] != c.reason {
				t.Fatalf("reasons = %q, want [%q]", r.Reasons, c.reason)
			}
		})
	}
}

func TestPlatformHealthNamesWhatIsNotVisible(t *testing.T) {
	v := platformFixture(t)
	v.FiguresRead = false
	v.System = nil
	v.Servers = nil
	v.Pipeline = nil
	r := readPlatformHealth(v)
	for _, part := range []string{"system verdict", "servers", "pipeline", "node figures"} {
		if !contains(r.NotVisible, part) {
			t.Errorf("not visible = %v, want it to name %q", r.NotVisible, part)
		}
	}
	var out bytes.Buffer
	r.table(&out)
	if !strings.Contains(out.String(), "not visible to you") {
		t.Fatalf("table does not say what is not visible:\n%s", out.String())
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestPlatformWindowDeltasAndRates(t *testing.T) {
	start := platformFixture(t)
	end := platformFixture(t)
	end.ReadAt = start.ReadAt.Add(2 * time.Minute)
	for gi := range end.Servers[0].Groups {
		for fi := range end.Servers[0].Groups[gi].Figures {
			f := &end.Servers[0].Groups[gi].Figures[fi]
			f.ObservedAt = f.ObservedAt.Add(2 * time.Minute)
			switch f.Key {
			case "database-bytes":
				f.Value += 2 * 1048576
			case "bus-stream-last-seq":
				f.Value += 600
			}
		}
	}
	w := platformWindowFigures(start, end)
	if w.Seconds != 120 {
		t.Fatalf("seconds = %v, want 120", w.Seconds)
	}
	got := map[string]platformWindowFigure{}
	for _, f := range w.Figures {
		got[f.Key+"|"+f.Instance] = f
	}
	db := got["database-bytes|"]
	if db.Delta != 2*1048576 || db.PerMinute == nil || *db.PerMinute != 1048576 {
		t.Fatalf("database-bytes = %+v, want delta 2 MiB at 1 MiB a minute", db)
	}
	seq := got["bus-stream-last-seq|"]
	if seq.Delta != 600 || seq.PerMinute == nil || *seq.PerMinute != 300 {
		t.Fatalf("stream head = %+v, want 600 at 300 a minute", seq)
	}
	if tb, ok := got["table-bytes|metric_sample"]; !ok || tb.Delta != 0 {
		t.Fatalf("table-bytes = %+v (present %v), want a zero delta per table", tb, ok)
	}
}

func TestPlatformWindowWithNoNewSampleHasNoRate(t *testing.T) {
	v := platformFixture(t)
	end := platformFixture(t)
	end.ReadAt = v.ReadAt.Add(time.Minute)
	for _, f := range platformWindowFigures(v, end).Figures {
		if f.PerMinute != nil {
			t.Fatalf("%s rate = %v over an unchanged sample, want absent", f.Key, *f.PerMinute)
		}
	}
}

func TestPlatformHealthOptsCheck(t *testing.T) {
	cases := []struct {
		name string
		o    platformHealthOpts
		want string
	}{
		{"negative window", platformHealthOpts{window: -time.Second}, "--window -1s: want 0 for one reading"},
		{"negative every", platformHealthOpts{every: -time.Second}, "--every -1s: want 0 for one reading"},
		{"append alone", platformHealthOpts{appendPath: "x"}, "--append writes one line a sample"},
		{"samples alone", platformHealthOpts{samples: 2}, "--samples 2 counts a sampling run's samples"},
		{"negative samples", platformHealthOpts{every: time.Minute, samples: -1}, "--samples -1: want the number of samples"},
		{"negative samples alone", platformHealthOpts{samples: -1}, "--samples -1: want the number of samples"},
		{"every under window", platformHealthOpts{every: time.Second, window: time.Minute}, "--every 1s is shorter than --window 1m0s"},
		{"negative clients", platformHealthOpts{pageClients: -1}, "--page-clients -1: want a count"},
		{"no loads", platformHealthOpts{pageClients: 1, pageLoads: 0}, "--page-loads 0: want at least one"},
		{"json with every", platformHealthOpts{every: time.Minute, json: true}, "--json reads once"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.o.check()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("check() = %v, want %q", err, c.want)
			}
		})
	}
	if err := (platformHealthOpts{pageLoads: 1}).check(); err != nil {
		t.Fatalf("defaults refused: %v", err)
	}
}

func TestPlatformHealthCommandHealthyExitsZero(t *testing.T) {
	srv, _ := platformServer(t, staticView(healthyPlatformView))
	out, _, err := runPlatformHealth(t, srv.URL)
	if err != nil {
		t.Fatalf("healthy platform exited with %v\n%s", err, out)
	}
	for _, want := range []string{"healthy", "lab-server", "v9.9.9", "Ingest Lag", "2.00 GiB", "end to end"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestPlatformHealthCommandUnhealthyExitsNonzero(t *testing.T) {
	v := platformFixture(t)
	v.Servers[0].Alarms = []platformViewAlarm{{Rule: "retention-behind", Severity: "warning"}}
	body, _ := json.Marshal(v)
	srv, _ := platformServer(t, staticView(string(body)))
	out, _, err := runPlatformHealth(t, srv.URL)
	if err == nil || !strings.Contains(err.Error(), "alarm open on lab-server: retention-behind (warning)") {
		t.Fatalf("err = %v, want the alarm named", err)
	}
	if !strings.Contains(out, "retention-behind") {
		t.Fatalf("the reading is not printed before the exit:\n%s", out)
	}
}

func TestPlatformHealthCommandJSON(t *testing.T) {
	srv, _ := platformServer(t, staticView(healthyPlatformView))
	out, _, err := runPlatformHealth(t, srv.URL, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var r platformReading
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("--json is not one JSON reading: %v\n%s", err, out)
	}
	if !r.Healthy || r.Servers[0].Name != "lab-server" {
		t.Fatalf("reading = %+v", r)
	}
}

func TestPlatformHealthCommandRefusedRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"missing permission node:read"}`, http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	_, _, err := runPlatformHealth(t, srv.URL)
	if err == nil || !strings.Contains(err.Error(), "GET /api/v1/views/platform: 403 Forbidden") || !strings.Contains(err.Error(), "node:read") {
		t.Fatalf("err = %v, want the refused read and its reason", err)
	}
}

func TestPlatformHealthCommandWindow(t *testing.T) {
	srv, reads := platformServer(t, func(n int64) string {
		if n == 1 {
			return healthyPlatformView
		}
		return strings.Replace(strings.Replace(healthyPlatformView,
			`"value": 1004, "observed_at": "2026-11-17T11:59:30Z"`, `"value": 1064, "observed_at": "2026-11-17T12:00:30Z"`, 1),
			`"read_at": "2026-11-17T12:00:00Z"`, `"read_at": "2026-11-17T12:01:00Z"`, 1)
	})
	out, _, err := runPlatformHealth(t, srv.URL, "--window", "20ms", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 2 {
		t.Fatalf("read the view %d times, want a start and an end", reads.Load())
	}
	var r platformReading
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Window == nil || r.Window.Seconds != 60 {
		t.Fatalf("window = %+v, want 60 s between the reads", r.Window)
	}
	for _, f := range r.Window.Figures {
		if f.Key == "bus-stream-last-seq" {
			if f.Delta != 60 || f.PerMinute == nil || *f.PerMinute != 60 {
				t.Fatalf("stream head = %+v, want 60 at 60 a minute", f)
			}
			return
		}
	}
	t.Fatalf("window lacks the stream head: %+v", r.Window.Figures)
}

func TestPlatformHealthCommandPageLoads(t *testing.T) {
	srv, _ := platformServer(t, staticView(healthyPlatformView))
	out, _, err := runPlatformHealth(t, srv.URL, "--page-clients", "2", "--page-loads", "2", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var r platformReading
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Page == nil || r.Page.Clients != 2 || r.Page.Ms.N != 4 || r.Page.Errors != 0 {
		t.Fatalf("page = %+v, want 2 clients and 4 loads that all answered", r.Page)
	}
}

func TestPlatformHealthCommandPageSystemUnknown(t *testing.T) {
	srv, _ := platformServer(t, staticView(healthyPlatformView))
	_, _, err := runPlatformHealth(t, srv.URL, "--page-clients", "1", "--page-system", "nowhere")
	if err == nil || !strings.Contains(err.Error(), `--page-system "nowhere"`) {
		t.Fatalf("err = %v, want the unknown system named", err)
	}
}

func TestPlatformHealthCommandSamplesToAFile(t *testing.T) {
	calls := 0
	srv, _ := platformServer(t, func(int64) string {
		calls++
		if calls == 2 {
			return `not json`
		}
		return healthyPlatformView
	})
	path := filepath.Join(t.TempDir(), "health.jsonl")
	_, stderr, err := runPlatformHealth(t, srv.URL, "--every", "10ms", "--samples", "3", "--append", path)
	if err != nil {
		t.Fatalf("a sampling run with a failed sample exited %v", err)
	}
	if !strings.Contains(stderr, "appended to "+path) {
		t.Fatalf("stderr = %q, want where the lines went", stderr)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []platformHealthSample
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var s platformHealthSample
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		lines = append(lines, s)
	}
	if len(lines) != 3 {
		t.Fatalf("%d lines, want 3", len(lines))
	}
	for i, s := range lines {
		if s.Seq != i+1 || s.At.IsZero() {
			t.Errorf("line %d: seq %d at %v", i, s.Seq, s.At)
		}
	}
	if lines[0].platformReading == nil || !lines[0].Healthy {
		t.Fatalf("line 1 = %+v, want a healthy reading", lines[0])
	}
	if lines[1].Error == "" || lines[1].platformReading != nil {
		t.Fatalf("line 2 = %+v, want an error and no reading", lines[1])
	}
}

func TestPlatformHealthCommandSamplesToStdout(t *testing.T) {
	srv, _ := platformServer(t, staticView(healthyPlatformView))
	out, _, err := runPlatformHealth(t, srv.URL, "--every", "10ms", "--samples", "2")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.TrimSpace(out), "\n") + 1; n != 2 {
		t.Fatalf("%d lines on stdout, want 2:\n%s", n, out)
	}
}

func TestFormatPlatformValue(t *testing.T) {
	cases := []struct {
		v    float64
		unit string
		want string
	}{
		{2147483648, "bytes", "2.00 GiB"},
		{5 * 1048576, "bytes", "5 MiB"},
		{3 * 1024, "bytes", "3 KiB"},
		{12, "bytes", "12 B"},
		{1.5, "s", "1.5 s"},
		{0.1234567, "s", "0.123 s"},
		{0.125, "ratio", "0.125"},
		{42, "", "42"},
	}
	for _, c := range cases {
		if got := formatPlatformValue(c.v, c.unit); got != c.want {
			t.Errorf("formatPlatformValue(%v, %q) = %q, want %q", c.v, c.unit, got, c.want)
		}
	}
}
