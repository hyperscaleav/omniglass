package node

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A reachability probe's cadence (#1373): the node's own --probe-every when it
// is set (a lab's, a test's), else the platform default the worklist carries,
// else every heartbeat (an older server that sends none).
func TestProbeCadenceResolves(t *testing.T) {
	wl := collection.WorklistReply{ProbeSeconds: 90}
	if got := probeCadence(10*time.Second, wl); got != 10*time.Second {
		t.Fatalf("a node flag: %s, want the flag's 10s", got)
	}
	if got := probeCadence(0, wl); got != 90*time.Second {
		t.Fatalf("no flag: %s, want the platform's 90s", got)
	}
	if got := probeCadence(0, collection.WorklistReply{}); got != 0 {
		t.Fatalf("no flag and no platform default: %s, want 0 (every heartbeat)", got)
	}
}

// A driver poll's cadence (#1373): the driver's declared schedule.every, else
// the platform's default for a poll that declares none, never every
// heartbeat while the platform says otherwise.
func TestPollCadenceResolves(t *testing.T) {
	baked := func(every string) collection.TaskSpec {
		fn := driver.BakedFunction{Driver: "d", Function: "f", Schedule: &driver.Schedule{Every: every},
			Request: &driver.Request{}, Emits: []driver.BakedEmit{}}
		if every == "" {
			fn.Schedule = nil
		}
		b, err := json.Marshal(fn)
		if err != nil {
			t.Fatal(err)
		}
		return collection.TaskSpec{ID: "t", Mode: "poll", Spec: b}
	}
	wl := collection.WorklistReply{PollSeconds: 300}
	if got := pollCadence(baked("45s"), wl); got != 45*time.Second {
		t.Fatalf("a declared schedule: %s, want the driver's 45s", got)
	}
	if got := pollCadence(baked(""), wl); got != 300*time.Second {
		t.Fatalf("no declared schedule: %s, want the platform's 300s", got)
	}
	if got := pollCadence(baked(""), collection.WorklistReply{}); got != 0 {
		t.Fatalf("no schedule and no platform default: %s, want 0 (every heartbeat)", got)
	}
}
