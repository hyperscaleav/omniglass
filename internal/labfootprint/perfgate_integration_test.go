//go:build labintegration

package labfootprint

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestIntegrationPerfGate drives `omniglass lab perf` as an author does:
// a baseline from one throwaway lab, an immediate check against it from a
// second that passes, and the same check's runs read again against a baseline
// with rows a minute halved, which fails and names the figure. The fleet is
// the ci size's scenario at 20 systems, so the two labs fit the lab tier's
// time; the window stays the size's 5 minutes, a whole number of every poll
// interval. OMNIGLASS_LAB_IMAGE names the image the labs run, for a host
// whose omniglass:dev is shared.
func TestIntegrationPerfGate(t *testing.T) {
	if os.Getenv("OMNIGLASS_LAB_TEST") != "1" {
		t.Skip("the perf gate runs through make test-lab (it needs the image make builds)")
	}
	dir := t.TempDir()
	baseline := filepath.Join(dir, "baseline.json")
	checked := filepath.Join(dir, "check-runs.jsonl")
	var image []string
	if img := os.Getenv("OMNIGLASS_LAB_IMAGE"); img != "" {
		image = []string{"--image", img}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()

	out, err := perfCmd(ctx, append([]string{"baseline", "--size", "ci", "--runs", "1",
		"--systems", "20", "--warmup", "1m", "--fleet-reads", "4", "--out", baseline}, image...)...)
	if err != nil {
		t.Fatalf("lab perf baseline: %v\n%s", err, out)
	}
	base := readTestBaseline(t, baseline)
	if base.Inputs.Systems != 20 || base.Runs != 1 || len(base.Figures) == 0 || base.Host.CPU == "" {
		t.Fatalf("the baseline = %+v, want 20 systems, 1 run, its figures and this host", base)
	}

	verdict := filepath.Join(dir, "verdict.json")
	out, err = perfCmd(ctx, append([]string{"check", "--size", "ci", "--runs", "1",
		"--baseline", baseline, "--append", checked, "--json", verdict}, image...)...)
	if err != nil {
		t.Fatalf("lab perf check against its own baseline: %v\n%s", err, out)
	}
	v := readTestVerdict(t, verdict)
	if v.Status != PerfPass {
		t.Fatalf("the immediate check read %s, want pass:\n%s", v.Status, out)
	}
	// A healthy lab loses nothing: every delivery row the run read is an
	// exact pass at 0, and every boot settled (#1695).
	losses := 0
	for _, f := range v.Figures {
		if strings.HasPrefix(f.Name, "delivery_loss/") || f.Name == "delivery_unsettled_boots" {
			if f.Status != PerfPass || f.Observed != 0 {
				t.Errorf("%s = %+v, want a pass at 0", f.Name, f)
			}
			if strings.HasPrefix(f.Name, "delivery_loss/") {
				losses++
			}
		}
	}
	if losses == 0 {
		t.Errorf("the verdict holds no delivery_loss row, want one a kind a hop:\n%s", out)
	}

	const rows = "metric_rows_per_minute_per_system"
	halved := false
	for i, f := range base.Figures {
		if f.Name == rows {
			base.Figures[i].Median /= 2
			for j := range f.Values {
				base.Figures[i].Values[j] /= 2
			}
			halved = true
		}
	}
	if !halved {
		t.Fatalf("the baseline carries no %s figure: %+v", rows, base.Figures)
	}
	perturbed := filepath.Join(dir, "perturbed.json")
	writeTestBaseline(t, perturbed, base)
	out, err = perfCmd(ctx, "check", "--size", "ci", "--runs", "1",
		"--baseline", perturbed, "--samples", checked)
	if err == nil {
		t.Fatalf("lab perf check against a baseline with rows a minute halved exited 0, want nonzero:\n%s", out)
	}
	failed := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, rows) && strings.Contains(line, string(PerfFail)) {
			failed = true
		}
	}
	if !failed {
		t.Errorf("the failing check does not name %s as failed:\n%s", rows, out)
	}
}

func perfCmd(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "../../bin/omniglass", append([]string{"lab", "perf"}, args...)...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}

func readTestBaseline(t *testing.T, path string) Baseline {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, err := ReadBaseline(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeTestBaseline(t *testing.T, path string, b Baseline) {
	t.Helper()
	var buf bytes.Buffer
	if err := b.Write(&buf); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestVerdict(t *testing.T, path string) PerfVerdict {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v PerfVerdict
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
