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
