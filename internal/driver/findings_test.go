package driver_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

func specJSON(t *testing.T, s *driver.Spec) []byte {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A spec that validates has no findings: the same answer a save would give.
func TestFindingsOfAValidSpecAreNone(t *testing.T) {
	if got := driver.Findings(specJSON(t, validSpec()), catalog()); len(got) != 0 {
		t.Fatalf("findings of a valid spec = %+v, want none", got)
	}
}

// A spec that does not parse is one finding naming why, of kind parse.
func TestFindingsOfAnUnparseableSpec(t *testing.T) {
	got := driver.Findings([]byte(`{"version":1,"transport":"tcp","pols":[]}`), catalog())
	if len(got) != 1 || got[0].Kind != "parse" || !strings.Contains(got[0].Message, "pols") {
		t.Fatalf("findings = %+v, want one parse finding naming the unknown field", got)
	}
}

// The validator stops at a spec's first fault, so a save names one; the
// findings name each broken function's own fault, so an author (or an agent)
// fixes them in one pass. The first is always the fault a save would name.
func TestFindingsNameEachBrokenFunction(t *testing.T) {
	s := validSpec()
	s.Polls[0].Emits[0].Name = "no-such-key"
	s.Polls = append(s.Polls, driver.Poll{Name: "second", Schedule: driver.Schedule{Every: "soon"}, Request: driver.Request{Line: "GET X"}})
	s.Commands[0].CommandType = "no-such-command"
	raw := specJSON(t, s)

	saveFault := s.Validate(catalog())
	if saveFault == nil {
		t.Fatal("the broken spec validates")
	}
	got := driver.Findings(raw, catalog())
	if len(got) != 3 {
		t.Fatalf("findings = %+v, want three (the bad emit, the bad schedule, the bad command)", got)
	}
	if got[0].Message != saveFault.Error() {
		t.Errorf("first finding %q, want the fault a save names: %q", got[0].Message, saveFault.Error())
	}
	byFn := map[string]driver.Finding{}
	for _, f := range got {
		byFn[f.Function] = f
	}
	for fn, kind := range map[string]string{"status": "poll", "second": "poll", "no-such-command": "command"} {
		f, ok := byFn[fn]
		if !ok || f.Kind != kind {
			t.Errorf("finding for %s = %+v, want kind %s", fn, f, kind)
		}
	}
	if !strings.Contains(byFn["second"].Message, "schedule") {
		t.Errorf("the schedule finding = %q", byFn["second"].Message)
	}
}

// A fault of the spec itself (no function to pin it on) is one finding of
// kind spec, never repeated per function.
func TestASpecLevelFaultIsOneFinding(t *testing.T) {
	s := validSpec()
	s.Transport = "carrier-pigeon"
	got := driver.Findings(specJSON(t, s), catalog())
	if len(got) != 1 || got[0].Kind != "spec" || got[0].Function != "" || !strings.Contains(got[0].Message, "carrier-pigeon") {
		t.Fatalf("findings = %+v, want one spec finding naming the transport", got)
	}
}

// Two functions sharing a name is a fault only the whole spec shows; it is
// still found.
func TestADuplicateFunctionIsFound(t *testing.T) {
	s := validSpec()
	s.Polls = append(s.Polls, s.Polls[0])
	got := driver.Findings(specJSON(t, s), catalog())
	if len(got) == 0 || !strings.Contains(got[0].Message, "duplicate") {
		t.Fatalf("findings = %+v, want the duplicate named first", got)
	}
}
