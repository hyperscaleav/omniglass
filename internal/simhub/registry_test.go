package simhub

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

func at(s int) time.Time { return time.Unix(1_800_000_000+int64(s), 0).UTC() }

func TestRegistryCountsHitsOnListedDevices(t *testing.T) {
	r := New()
	r.now = func() time.Time { return at(0) }
	r.Pulled("edge-a", collection.SimCapabilities{LowPorts: true}, []string{"c1", "c2"})

	acc, rej := r.Report("edge-a", collection.SimHitBatch{
		Serving: []string{"c1", "c2", "c9"},
		Hits: []collection.SimHit{
			{ComponentID: "c1", Protocol: "http", At: at(1)},
			{ComponentID: "c1", Protocol: "snmp", At: at(3)},
			{ComponentID: "c1", Protocol: "http", At: at(2)},
			{ComponentID: "c9", Protocol: "http", At: at(4)}, // not handed to this node
		},
	})
	if acc != 3 || rej != 1 {
		t.Fatalf("accepted %d rejected %d, want 3 and 1", acc, rej)
	}
	st, ok := r.Status("edge-a")
	if !ok {
		t.Fatal("no status for a node that pulled")
	}
	if !st.Capabilities.LowPorts || !st.LastSeen.Equal(at(0)) {
		t.Fatalf("status = %+v", st)
	}
	c1 := st.Devices["c1"]
	if c1.Count != 3 || c1.ByProtocol["http"] != 2 || c1.ByProtocol["snmp"] != 1 {
		t.Fatalf("c1 = %+v", c1)
	}
	if !c1.LastAt.Equal(at(0)) || c1.LastProtocol != "http" {
		t.Fatalf("c1 last = %v %s, want the batch's last hit at the server's time", c1.LastAt, c1.LastProtocol)
	}
	if _, ok := st.Devices["c9"]; ok {
		t.Fatal("a hit on an unlisted component was recorded")
	}
	if !st.Serving["c1"] || !st.Serving["c2"] || st.Serving["c9"] {
		t.Fatalf("serving = %v, want only the listed devices", st.Serving)
	}
}

func TestRegistryRejectsReportsBeforeAPull(t *testing.T) {
	r := New()
	acc, rej := r.Report("edge-a", collection.SimHitBatch{Hits: []collection.SimHit{{ComponentID: "c1", Protocol: "http"}}})
	if acc != 0 || rej != 1 {
		t.Fatalf("accepted %d rejected %d before any pull", acc, rej)
	}
	if _, ok := r.Status("edge-a"); ok {
		t.Fatal("status exists for a node that never pulled")
	}
}

func TestRegistryForgetsDevicesDroppedFromTheList(t *testing.T) {
	r := New()
	r.Pulled("edge-a", collection.SimCapabilities{}, []string{"c1", "c2"})
	r.Report("edge-a", collection.SimHitBatch{Hits: []collection.SimHit{{ComponentID: "c2", Protocol: "http", At: at(1)}}})
	r.Pulled("edge-a", collection.SimCapabilities{}, []string{"c1"})
	st, _ := r.Status("edge-a")
	if _, ok := st.Devices["c2"]; ok {
		t.Fatal("c2 kept after the node stopped listing it")
	}
	if acc, _ := r.Report("edge-a", collection.SimHitBatch{Hits: []collection.SimHit{{ComponentID: "c2", Protocol: "http"}}}); acc != 0 {
		t.Fatal("hit accepted for a dropped device")
	}
}

func TestRegistryStatusIsACopy(t *testing.T) {
	r := New()
	r.Pulled("edge-a", collection.SimCapabilities{}, []string{"c1"})
	r.Report("edge-a", collection.SimHitBatch{Hits: []collection.SimHit{{ComponentID: "c1", Protocol: "http", At: at(1)}}})
	st, _ := r.Status("edge-a")
	st.Devices["c1"].ByProtocol["http"] = 99
	again, _ := r.Status("edge-a")
	if again.Devices["c1"].ByProtocol["http"] != 1 {
		t.Fatal("mutating a status reached the registry")
	}
}

// A simulator's report cannot grow the registry without bound or pin its
// view: only known protocols count, a batch is capped, and the server
// stamps when it heard a hit.
func TestRegistryBoundsWhatAReportCanDo(t *testing.T) {
	r := New()
	r.now = func() time.Time { return at(100) }
	r.Pulled("edge-a", collection.SimCapabilities{}, []string{"c1"})

	acc, rej := r.Report("edge-a", collection.SimHitBatch{Hits: []collection.SimHit{
		{ComponentID: "c1", Protocol: "http", At: at(100_000_000)}, // a client clock far ahead
		{ComponentID: "c1", Protocol: "made-up-protocol-1"},
		{ComponentID: "c1", Protocol: "made-up-protocol-2"},
	}})
	if acc != 1 || rej != 2 {
		t.Fatalf("accepted %d rejected %d, want only the http hit", acc, rej)
	}
	st, _ := r.Status("edge-a")
	if len(st.Devices["c1"].ByProtocol) != 1 {
		t.Fatalf("by protocol = %v, want http alone", st.Devices["c1"].ByProtocol)
	}
	if !st.Devices["c1"].LastAt.Equal(at(100)) {
		t.Fatalf("last hit at %v, want the server's time", st.Devices["c1"].LastAt)
	}

	big := collection.SimHitBatch{}
	for i := 0; i < MaxHitsPerBatch+10; i++ {
		big.Hits = append(big.Hits, collection.SimHit{ComponentID: "c1", Protocol: "tcp"})
	}
	acc, rej = r.Report("edge-a", big)
	if acc != MaxHitsPerBatch || rej != 10 {
		t.Fatalf("oversized batch: accepted %d rejected %d, want the cap", acc, rej)
	}
}
