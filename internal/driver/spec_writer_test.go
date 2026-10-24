package driver_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// reservedEmits are names a driver may never emit: every native probe key
// (read from probekeys, so a key added there is covered here) and the health
// rollup's key. Each is written by one writer, never by a driver's emits.
func reservedEmits() []string {
	return append(probekeys.Native(), "health")
}

// TestValidateRefusesAnEmitAnotherWriterOwns holds a driver to the keys it may
// write (ADR-0161): a spec emitting a node-probe or platform key is refused at
// write, naming the key, with or without a catalog (the refusal is the
// writer-class ruling, not a catalog lookup), on a poll and on a listener.
func TestValidateRefusesAnEmitAnotherWriterOwns(t *testing.T) {
	cat := catalog()
	for _, key := range reservedEmits() {
		cat.lanes[key] = "property"
	}
	for _, key := range reservedEmits() {
		for family, mutate := range map[string]func(*driver.Spec){
			"poll":     func(s *driver.Spec) { s.Polls[0].Emits[0].Name = key },
			"listener": func(s *driver.Spec) { s.Listeners[0].Emits[0].Name = key },
		} {
			s := validSpec()
			mutate(s)
			for _, c := range []driver.Catalog{cat, nil} {
				err := s.Validate(c)
				if err == nil {
					t.Fatalf("a %s emitting %s validated (catalog %v); want it refused", family, key, c != nil)
				}
				if !strings.Contains(err.Error(), key) {
					t.Fatalf("refusal %q does not name %s", err, key)
				}
			}
		}
	}
}

// TestInterpretRefusesAReservedEmit is the node's half: a baked function
// carrying an emit a driver may not write (one derived before the write-time
// refusal existed) yields a fault naming the key, never a sample, so a stored
// legacy task cannot land a node-probe value through a driver.
func TestInterpretRefusesAReservedEmit(t *testing.T) {
	for _, key := range reservedEmits() {
		fn := &driver.BakedFunction{
			Driver: "rogue", Function: "status",
			Emits: []driver.BakedEmit{
				{Name: key, Lane: "property", Extract: driver.Extract{Regex: `STATE (\S+)`}},
				{Name: "video-input", Lane: "property", Extract: driver.Extract{Regex: `INPUT (\S+)`}},
			},
		}
		emitted, faults := fn.Interpret(driver.Payload{Text: "STATE up INPUT hdmi1"}, time.Now())
		for _, e := range emitted {
			if e.Name == key {
				t.Fatalf("a driver emit of %s interpreted to a sample %+v; want a fault", key, e)
			}
		}
		if len(faults) != 1 || !strings.Contains(faults[0].Error(), key) {
			t.Fatalf("faults for %s = %v, want one naming it", key, faults)
		}
	}
}
