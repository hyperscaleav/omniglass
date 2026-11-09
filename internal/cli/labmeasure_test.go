package cli

import (
	"strings"
	"testing"
	"time"
)

func TestLabMeasureOptsCheck(t *testing.T) {
	ok := labMeasureOpts{systems: 100, window: time.Minute, fleetReads: 5, backlog: 1000, drainWait: time.Minute}
	if err := ok.check(); err != nil {
		t.Fatalf("a valid set of flags: %v", err)
	}
	attach := ok
	attach.systems = 0
	attach.backlog = 0
	attach.drainWait = 0
	if err := attach.check(); err != nil {
		t.Fatalf("attaching with the probe skipped needs no drain timeout: %v", err)
	}
	cases := map[string]func(*labMeasureOpts){
		"--systems":       func(o *labMeasureOpts) { o.systems = -1 },
		"--window":        func(o *labMeasureOpts) { o.window = 0 },
		"--warmup":        func(o *labMeasureOpts) { o.warmup = -time.Second },
		"--fleet-reads":   func(o *labMeasureOpts) { o.fleetReads = 0 },
		"--backlog":       func(o *labMeasureOpts) { o.backlog = -1 },
		"--drain-timeout": func(o *labMeasureOpts) { o.drainWait = 0 },
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

// Sampling (#1632) refuses the flag sets a time series cannot run on, each
// with the fix in its text.
func TestLabMeasureSamplingRefusals(t *testing.T) {
	sampling := labMeasureOpts{window: time.Minute, fleetReads: 5, every: 2 * time.Minute, appendPath: "s.jsonl", pageLoads: 20}
	if err := sampling.check(); err != nil {
		t.Fatalf("a valid sampling run: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*labMeasureOpts)
		want   string
	}{
		{"--every with --systems", func(o *labMeasureOpts) { o.systems = 10 },
			"--every samples the lab already running: drop --systems, bring the lab up with `omniglass lab run --scenario corporate --systems N`, then sample it"},
		{"--every with --backlog", func(o *labMeasureOpts) { o.backlog = 1000; o.drainWait = time.Minute },
			"--backlog 1000: the retention probe restarts the server, which a time series must not do; drop --backlog when sampling with --every"},
		{"--every below --window", func(o *labMeasureOpts) { o.every = 30 * time.Second },
			"--every 30s is shorter than --window 1m0s: each sample measures one whole window, so give --every at least the window"},
		{"--append without --every", func(o *labMeasureOpts) { o.every = 0 },
			"--append writes one line a sample: add --every to sample on an interval, or use --json for one measurement"},
		{"--json with --every", func(o *labMeasureOpts) { o.jsonSet = true; o.jsonPath = "x.json" },
			"--json writes one measurement: a sampling run writes JSON lines, so use --append with --every"},
		{"negative --every", func(o *labMeasureOpts) { o.every = -time.Second; o.appendPath = "" },
			"--every -1s: want 0 for one measurement, or the time between samples"},
		{"negative --samples", func(o *labMeasureOpts) { o.samples = -1 },
			"--samples -1: want a count, or 0 to sample until interrupted"},
		{"negative --page-clients", func(o *labMeasureOpts) { o.pageClients = -1 },
			"--page-clients -1: want a count of concurrent clients, or 0 to skip the system page load"},
		{"--samples without --every", func(o *labMeasureOpts) { o.every = 0; o.appendPath = ""; o.samples = 3 },
			"--samples 3 counts a sampling run's samples: add --every to sample on an interval"},
		{"--page-loads below one", func(o *labMeasureOpts) { o.pageClients = 4; o.pageLoads = 0 },
			"--page-loads 0: want at least one timed load a client"},
	}
	for _, c := range cases {
		o := sampling
		c.mutate(&o)
		err := o.check()
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

// A sampling run defaults the retention probe off, since the probe restarts
// the server; a --backlog given on the command line stays, and is refused.
func TestLabMeasureSamplingDefaultsBacklogOff(t *testing.T) {
	cmd := newLabMeasureCmd()
	if err := cmd.ParseFlags([]string{"--every", "2m", "--window", "1m"}); err != nil {
		t.Fatal(err)
	}
	o := labMeasureOptsFrom(cmd)
	if o.backlog != 0 {
		t.Errorf("sampling backlog = %d, want 0", o.backlog)
	}
	cmd = newLabMeasureCmd()
	if err := cmd.ParseFlags([]string{"--every", "2m", "--window", "1m", "--backlog", "500"}); err != nil {
		t.Fatal(err)
	}
	if o := labMeasureOptsFrom(cmd); o.backlog != 500 || o.check() == nil {
		t.Errorf("an explicit --backlog with --every: backlog %d, check %v; want 500 and a refusal", o.backlog, o.check())
	}
	cmd = newLabMeasureCmd()
	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatal(err)
	}
	if o := labMeasureOptsFrom(cmd); o.backlog != 150000 {
		t.Errorf("one measurement backlog = %d, want the 150000 default", o.backlog)
	}
}
