package node

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The identity on the wire (#1386): the node's batch builder copies a
// sample's identity onto its metric, property or event sample, never onto a
// log line; and a plain poll's change memory tells two records' occurrences
// apart, so each room's event lands when that room's changes.

func TestBuildBatchCarriesIdentity(t *testing.T) {
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	r1 := &driver.Identity{Key: "ucc-room-id", Value: "r1"}
	dps := []collection.Sample{
		{Name: "call-state", Text: "in-call", IsText: true, TS: at, Identity: r1},
		{Name: "people-count", Value: 3, TS: at, Identity: r1},
		{Name: "call-started", Lane: driver.LaneEvent, Text: "started", IsText: true, TS: at, Identity: r1},
		{Lane: driver.LaneLog, Text: "raw", IsText: true, TS: at},
		{Name: "uptime", Value: 9, TS: at},
	}
	b := buildBatch("t1", "node-a", dps)
	if p := b.GetProperties()[0]; p.GetIdentity().GetKey() != "ucc-room-id" || p.GetIdentity().GetValue() != "r1" || p.GetInstance() != "" {
		t.Fatalf("property %v, want identity ucc-room-id=r1 and the instance left to the server", p)
	}
	if m := b.GetMetrics()[0]; m.GetIdentity().GetValue() != "r1" {
		t.Fatalf("metric %v carries no identity", m)
	}
	if m := b.GetMetrics()[1]; m.GetIdentity() != nil {
		t.Fatalf("an unidentified metric %v carries an identity", m)
	}
	if e := b.GetEvents()[0]; e.GetIdentity().GetValue() != "r1" {
		t.Fatalf("event %v carries no identity", e)
	}
}

func TestChangeMemoryTellsRecordsApart(t *testing.T) {
	s := &pollSchedule{occurred: map[string]map[string]string{}}
	ev := func(room, text string) collection.Sample {
		return collection.Sample{Name: "call-started", Lane: driver.LaneEvent, Text: text, IsText: true,
			Identity: &driver.Identity{Key: "ucc-room-id", Value: room}}
	}
	first := []collection.Sample{ev("r1", "in-call"), ev("r2", "idle")}
	if got := s.changed("t1", first); len(got) != 2 {
		t.Fatalf("first run landed %d, want both rooms", len(got))
	}
	s.commitChanged("t1", first)
	if got := s.changed("t1", []collection.Sample{ev("r1", "in-call"), ev("r2", "idle")}); len(got) != 0 {
		t.Fatalf("an unchanged run landed %+v, want nothing", got)
	}
}
