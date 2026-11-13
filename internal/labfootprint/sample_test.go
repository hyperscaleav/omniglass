package labfootprint

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fullReport is a report with every figure set, so a path that went missing
// from a sample line would show.
func fullReport() ScaleReport {
	age := 3.0
	gap := NewLagGap(LagAges{CommitAgeS: &age}, time.Minute)
	return SummarizeScale(ScaleRun{
		Build: "v1", Scenario: "corporate", Started: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
		Warmup: time.Minute, Window: time.Minute, Systems: 3, Sites: 1, Components: 9, Tasks: 40,
		Measured:  Window{MetricRowsPerMinute: 10, SamplesLanded: 5, WALBytes: 50, Containers: map[string]ContainerWindow{"server": {Millicores: 5}}},
		Lag:       []float64{0.2, 0.4},
		LagGap:    &gap,
		FleetView: []time.Duration{time.Millisecond},
		Page:      PageRun{Clients: 2, Loads: []PageLoad{{Took: 4 * time.Millisecond, Bytes: 100}, {Err: errors.New("503")}}},
		Absolutes: Absolutes{DatabaseBytes: 1, MetricPartitions: 3, MetricOldestDay: "2026-11-08", DeadTuples: DeadTuples{Property: 2}},
	})
}

// A sample is one line of compact JSON carrying the one-shot report's fields
// at the same paths as `lab measure --json` writes them, plus its own seq,
// at, uptime_seconds and error.
func TestScaleSampleLineKeepsTheReportPaths(t *testing.T) {
	rep := fullReport()
	up := 125.5
	line, err := ScaleSample{Seq: 1, At: time.Date(2026, 11, 9, 12, 2, 0, 0, time.UTC), UptimeS: &up, ScaleReport: &rep}.Line()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(line, []byte("\n")) || bytes.Count(line, []byte("\n")) != 1 {
		t.Fatalf("a sample line holds an interior newline or no final one: %q", line)
	}
	var sample, report map[string]any
	if err := json.Unmarshal(line, &sample); err != nil {
		t.Fatal(err)
	}
	indented, _ := json.MarshalIndent(rep, "", "  ")
	if err := json.Unmarshal(indented, &report); err != nil {
		t.Fatal(err)
	}
	for k, v := range report {
		if !reflect.DeepEqual(sample[k], v) {
			t.Errorf("sample .%s = %v, want the --json report's %v", k, sample[k], v)
		}
	}
	for _, k := range []string{"seq", "at", "uptime_seconds"} {
		if _, ok := sample[k]; !ok {
			t.Errorf("sample has no .%s", k)
		}
	}
	if _, ok := sample["error"]; ok {
		t.Error("a measured sample carries an error key")
	}
	if sample["at"] != "2026-11-09T12:02:00Z" {
		t.Errorf("at = %v, want RFC 3339", sample["at"])
	}
}

// fakeMeasure measures sample seq instantly, or fails the ones in fail.
func fakeMeasure(fail map[int]bool) SampleFunc {
	return func(_ context.Context, seq int) (SampleResult, error) {
		if fail[seq] {
			return SampleResult{}, errors.New("the database counters: connection refused")
		}
		rep := fullReport()
		return SampleResult{Report: rep, End: time.Now(), UptimeS: float64(seq)}, nil
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

// Appending three samples to a file that already holds a run leaves its
// earlier bytes as they were and yields four lines, each one JSON.
func TestRunSamplesAppendsToAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	earlier := []byte(`{"seq":1,"at":"2026-11-08T00:00:00Z","error":"from the run before"}` + "\n")
	if err := os.WriteFile(path, earlier, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := OpenSampleLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := RunSamples(context.Background(), SampleLoop{Every: time.Millisecond, Samples: 3, Measure: fakeMeasure(nil), Out: f}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.HasPrefix(got, earlier) {
		t.Fatalf("the earlier line changed: %q", got[:len(earlier)])
	}
	lines := readLines(t, path)
	if len(lines) != 4 {
		t.Fatalf("%d lines, want 4", len(lines))
	}
	for i, l := range lines {
		var v map[string]any
		if err := json.Unmarshal([]byte(l), &v); err != nil {
			t.Errorf("line %d does not parse: %v", i+1, err)
		}
		if i > 0 && v["seq"] != float64(i) {
			t.Errorf("line %d seq = %v, want %d", i+1, v["seq"], i)
		}
	}
}

// A sample that fails writes a line with its error and no figures, and the
// run goes on to the next.
func TestRunSamplesWritesAFailedSampleAndGoesOn(t *testing.T) {
	var out bytes.Buffer
	if err := RunSamples(context.Background(), SampleLoop{Every: time.Millisecond, Samples: 3, Measure: fakeMeasure(map[int]bool{2: true}), Out: &out}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("%d lines, want 3: %s", len(lines), out.String())
	}
	var failed map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &failed); err != nil {
		t.Fatal(err)
	}
	if failed["seq"] != float64(2) || failed["error"] != "the database counters: connection refused" {
		t.Errorf("failed line = %v, want seq 2 and the error", failed)
	}
	for _, k := range []string{"ingest_lag_ms", "system_page_ms", "absolutes", "uptime_seconds", "build"} {
		if _, ok := failed[k]; ok {
			t.Errorf("a failed sample carries the figure .%s", k)
		}
	}
	var third map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &third); err != nil || third["seq"] != float64(3) || third["error"] != nil {
		t.Errorf("the sample after a failure = %v (%v), want seq 3 measured", third, err)
	}
}

// Stopping the run between samples ends it without a further sample; the
// sample in flight when the stop arrives is finished and written.
func TestRunSamplesStopsAfterTheSampleInFlight(t *testing.T) {
	stop, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	measure := func(ctx context.Context, seq int) (SampleResult, error) {
		if seq == 2 {
			cancel()
			if ctx.Err() != nil {
				return SampleResult{}, errors.New("the in-flight sample saw the stop")
			}
		}
		return fakeMeasure(nil)(ctx, seq)
	}
	if err := RunSamples(stop, SampleLoop{Every: time.Hour, Samples: 0, Measure: measure, Out: &out, wait: func(ctx context.Context, d time.Duration) error {
		return ctx.Err()
	}}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 2 || strings.Contains(out.String(), `"error":`) {
		t.Fatalf("stopped during sample 2: %d lines, want 2 measured:\n%s", len(lines), out.String())
	}
}
