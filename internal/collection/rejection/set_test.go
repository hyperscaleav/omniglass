package rejection

import (
	"testing"
	"time"
)

// The gate is a set of reasons (#1650): the refused entry keeps today's exact
// form and comes first, every other reason is `<kind> <since> <source>`, the
// entries are joined by ";", and Parse still reads the refused entry alone, so
// a :retry and a restart read what they read before.

func TestASetCarriesTheRefusedEntryFirstAndEveryOtherReason(t *testing.T) {
	since := time.Date(2026, 11, 9, 14, 3, 0, 0, time.UTC)
	refused := Gate{Rung: "authenticated", Class: "auth", Fingerprint: fp}
	unreachable := Reason{Kind: "unreachable", Since: since, Source: "task-1"}

	v := Join(&refused, []Reason{unreachable})
	if want := "authenticated auth " + fp + ";unreachable 2026-11-09T14:03:00Z task-1"; v != want {
		t.Fatalf("Join = %q, want %q", v, want)
	}
	g, ok, others := ParseSet(v)
	if !ok || g != refused || len(others) != 1 || others[0] != unreachable {
		t.Fatalf("ParseSet(%q) = %+v, %v, %+v", v, g, ok, others)
	}
	if got, ok := Parse(v); !ok || got != refused {
		t.Fatalf("Parse(%q) = %+v, %v; want the refused entry alone", v, got, ok)
	}
}

func TestASetWithNoRefusalParsesAsNoGate(t *testing.T) {
	since := time.Date(2026, 11, 9, 14, 3, 0, 0, time.UTC)
	v := Join(nil, []Reason{{Kind: "unreachable", Since: since, Source: "task-1"}})
	if v != "unreachable 2026-11-09T14:03:00Z task-1" {
		t.Fatalf("Join = %q", v)
	}
	if _, ok := Parse(v); ok {
		t.Fatalf("Parse(%q) read a refusal from an unreachable-only set", v)
	}
	if _, ok, others := ParseSet(v); ok || len(others) != 1 || others[0].Kind != "unreachable" {
		t.Fatalf("ParseSet(%q) = %v, %+v", v, ok, others)
	}
}

func TestAnEmptySetIsNone(t *testing.T) {
	if got := Join(nil, nil); got != None {
		t.Fatalf("Join(nil, nil) = %q, want %q", got, None)
	}
	if _, ok, others := ParseSet(None); ok || len(others) != 0 {
		t.Fatalf("ParseSet(none) = %v, %+v", ok, others)
	}
}

// A malformed entry is skipped, never a reason: a bad since, a missing source.
func TestAMalformedReasonIsSkipped(t *testing.T) {
	_, _, others := ParseSet("unreachable yesterday task-1;unreachable 2026-11-09T14:03:00Z")
	if len(others) != 0 {
		t.Fatalf("malformed entries parsed as reasons: %+v", others)
	}
}
