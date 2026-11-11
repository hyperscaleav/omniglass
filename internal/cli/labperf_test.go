package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/labfootprint"
)

// A size resolves to its committed baseline's path and the inputs its runs
// measure with; an override moves the inputs and the device set with them.
func TestLabPerfOptsResolveTheSize(t *testing.T) {
	o := labPerfOpts{size: "ci", runs: 3, settleWait: time.Minute}
	in, err := o.inputs()
	if err != nil {
		t.Fatal(err)
	}
	if in.Scenario != "corporate" || in.Systems != 300 || in.WindowS != 300 || in.WarmupS != 120 || in.FleetReads != 100 || in.PlanDigest == "" {
		t.Fatalf("ci inputs = %+v", in)
	}
	if got, want := o.baselinePath(), filepath.Join("internal", "labfootprint", "baseline", "corporate-300.ci.json"); got != want {
		t.Fatalf("baseline path = %s, want %s", got, want)
	}
	o.baseline = "/tmp/x.json"
	if o.baselinePath() != "/tmp/x.json" {
		t.Fatal("--baseline does not name the file")
	}

	small := labPerfOpts{size: "ci", runs: 1, systems: 4, warmup: 30 * time.Second, window: time.Minute, fleetReads: 3, settleWait: time.Minute}
	sin, err := small.inputs()
	if err != nil {
		t.Fatal(err)
	}
	if sin.Systems != 4 || sin.WarmupS != 30 || sin.WindowS != 60 || sin.FleetReads != 3 || sin.PlanDigest == in.PlanDigest {
		t.Fatalf("overridden inputs = %+v, want 4 systems, 30s, 1m, 3 reads and a new device set", sin)
	}
}

func TestLabPerfOptsCheck(t *testing.T) {
	ok := labPerfOpts{size: "ci", runs: 3, settleWait: time.Minute}
	if err := ok.check(); err != nil {
		t.Fatalf("a valid set of flags: %v", err)
	}
	cases := map[string]func(*labPerfOpts){
		"--size":           func(o *labPerfOpts) { o.size = "huge" },
		"--runs":           func(o *labPerfOpts) { o.runs = 0 },
		"--systems":        func(o *labPerfOpts) { o.systems = -1 },
		"--window":         func(o *labPerfOpts) { o.window = -time.Minute },
		"--settle-timeout": func(o *labPerfOpts) { o.settleWait = 0 },
		"--last":           func(o *labPerfOpts) { o.last = 2 },
		"--image":          func(o *labPerfOpts) { o.samples = "s.jsonl"; o.image = "omniglass:x" },
	}
	for flag, mutate := range cases {
		o := ok
		mutate(&o)
		err := o.check()
		if err == nil || !strings.Contains(err.Error(), flag) {
			t.Errorf("%s: err = %v, want one naming the flag", flag, err)
		}
	}
}

// Each run brings up its own throwaway lab of the inputs' fleet, with the
// retention probe and the page load off: neither is gated, and the probe
// restarts the server.
func TestLabPerfScaleConfigIsTheInputs(t *testing.T) {
	in := labfootprint.PerfInputs{Scenario: "corporate", Systems: 300, WarmupS: 120, WindowS: 300, FleetReads: 20}
	o := labPerfOpts{image: "omniglass:goal", settleWait: 2 * time.Minute}
	cfg := o.scaleConfig(in, "/bin/omniglass", "unix:///var/run/docker.sock")
	if cfg.Binary != "/bin/omniglass" || cfg.Scenario != "corporate" || cfg.Systems != 300 ||
		cfg.Warmup != 2*time.Minute || cfg.Window != 5*time.Minute || cfg.FleetReads != 20 ||
		cfg.Backlog != 0 || cfg.PageClients != 0 || cfg.Image != "omniglass:goal" || cfg.SettleTimeout != 2*time.Minute {
		t.Fatalf("scale config = %+v", cfg)
	}
}
