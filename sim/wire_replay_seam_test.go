//go:build linux

package sim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The replay seam works (#2837): a small transcript, written the way a capture
// from a real device would be, replays clean against a real family
// (borealedge), and an expectation altered by one byte is reported by step.
// This is the one proof the seam keeps; a family's own behavior is tested in
// its own package and the driver contract test.

const edgeTranscript = `{
 "product": "boreal-edge-55",
 "source": "written by hand for the replay seam test",
 "http": [
  {"Name": "a refused power value", "Method": "POST", "Path": "/api/display/power", "Body": "{\"power\": \"bogus\"}",
   "status": 400, "headers": {"Content-Type": ["application/json"]},
   "response": "{\"error\":\"validation failed\",\"fields\":[{\"path\":\"status.device.power\",\"value\":\"bogus\",\"enum\":[\"on\",\"off\",\"standby\"]}]}",
   "state_change": false},
  {"Name": "volume 33", "Method": "POST", "Path": "/api/audio/volume", "Body": "{\"volume\": 33}",
   "status": 200, "headers": {"Content-Type": ["application/json"]},
   "response": "{\"ok\": true, \"volume\": 33}\n",
   "state_change": true},
  {"Name": "an unknown path", "Method": "GET", "Path": "/nope",
   "status": 404, "headers": {"Content-Type": ["text/plain; charset=utf-8"], "X-Content-Type-Options": ["nosniff"]},
   "response": "404 page not found\n",
   "state_change": false}
 ]
}`

func edgeReplayFamily() wireFamily {
	return wireFamily{Name: "boreal-edge", Engine: func(t testing.TB) *Engine { return shippedEngine(t) }}
}

func TestWireReplayHoldsAFamilyToATranscript(t *testing.T) {
	fam := edgeReplayFamily()
	fam.Dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(fam.Dir, "boreal-edge-55.json"), []byte(edgeTranscript), 0o644); err != nil {
		t.Fatal(err)
	}
	replayWire(t, fam, "boreal-edge-55")
}

func TestWireReplayReportsAnAlteredExpectation(t *testing.T) {
	altered := strings.Replace(edgeTranscript, `\"volume\": 33}\n`, `\"volume\": 34}\n`, 1)
	if altered == edgeTranscript {
		t.Fatal("the test did not alter the transcript")
	}
	want, err := parseTranscript([]byte(altered))
	if err != nil {
		t.Fatal(err)
	}
	diffs := replayTranscript(t, edgeReplayFamily(), want)
	if len(diffs) != 1 || !strings.Contains(diffs[0], "volume 33: body differs") {
		t.Fatalf("want one report naming the step, got %q", diffs)
	}
}
