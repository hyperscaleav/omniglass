package main

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/lab"
)

// `make lab-footprint` with no arguments measures the ladder's smoke rung,
// corporate at 20 systems (#1719); a size is a count, never negative.
func TestParseArgs(t *testing.T) {
	o, err := parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.cfg.Scenario != lab.MeasureScenario || o.cfg.Systems != lab.SmokeSystems {
		t.Fatalf("no arguments measure %s at %d systems, want %s at %d", o.cfg.Scenario, o.cfg.Systems, lab.MeasureScenario, lab.SmokeSystems)
	}
	if o.cfg.Cadence != lab.CadenceLab || o.cfg.Windows != 5 || len(o.cfg.ReadHours) != 2 {
		t.Fatalf("defaults %+v, want the lab cadence, 5 windows and two read ranges", o.cfg)
	}
	o, err = parseArgs([]string{"-scenario", "dev", "-systems", "0", "-json", "out.json"})
	if err != nil {
		t.Fatal(err)
	}
	if o.cfg.Scenario != "dev" || o.cfg.Systems != 0 || o.jsonOut != "out.json" {
		t.Fatalf("flags %+v json %q, want dev at no size written to out.json", o.cfg, o.jsonOut)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"-systems", "-1"}, "-systems -1"},
		{[]string{"-windows", "0"}, "-windows 0"},
		{[]string{"-read-hours", "24,x"}, "-read-hours"},
	} {
		if _, err := parseArgs(c.args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: err %v, want one naming %q", c.args, err, c.want)
		}
	}
}
