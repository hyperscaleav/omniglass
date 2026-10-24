package rejection

import (
	"strings"
	"testing"
)

// The endpoint-rejection value (ADR-0158, #1078): the gate a terminal refusal
// holds, written by the node and read back by the node (a restart adopts it),
// the server (a :retry checks a trusted key against it) and the reachability
// read (the console names both keys from it). One shape, read in one place.

const fp = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

var (
	pinned  = "SHA256:" + strings.Repeat("A", 43)
	offered = "SHA256:" + strings.Repeat("B", 43)
)

func TestAGateRendersItsRungClassAndFingerprint(t *testing.T) {
	g := Gate{Rung: "authenticated", Class: "auth", Fingerprint: fp}
	if got, want := g.String(), "authenticated auth "+fp; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestAKeyRefusalCarriesBothKeysAsData(t *testing.T) {
	g := Gate{Rung: "responsive", Class: "host-key", Fingerprint: fp, PinnedKey: pinned, OfferedKey: offered}
	v := g.String()
	if want := "responsive host-key " + fp + " " + pinned + " " + offered; v != want {
		t.Fatalf("String() = %q, want %q", v, want)
	}
	back, ok := Parse(v)
	if !ok || back != g {
		t.Fatalf("Parse(%q) = %+v, %v; want the gate back", v, back, ok)
	}
}

// Evidence is both keys or neither: a pinned key alone names nothing to
// trust, and an offered key alone nothing it replaces.
func TestHalfAKeyPairIsNoEvidence(t *testing.T) {
	for _, g := range []Gate{
		{Rung: "responsive", Class: "host-key", Fingerprint: fp, PinnedKey: pinned},
		{Rung: "responsive", Class: "host-key", Fingerprint: fp, OfferedKey: offered},
	} {
		if got, want := g.String(), "responsive host-key "+fp; got != want {
			t.Fatalf("String() = %q, want the gate without evidence %q", got, want)
		}
	}
}

func TestParseReadsWhatTheNodePublishes(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want Gate
	}{
		{in: "authenticated auth " + fp, ok: true, want: Gate{Rung: "authenticated", Class: "auth", Fingerprint: fp}},
		{in: "responsive host-key " + fp, ok: true, want: Gate{Rung: "responsive", Class: "host-key", Fingerprint: fp}},
		{in: "reachable protocol " + fp, ok: true, want: Gate{Rung: "reachable", Class: "protocol", Fingerprint: fp}},
		{in: "  responsive   certificate " + fp + " " + pinned + "  " + offered + " ", ok: true,
			want: Gate{Rung: "responsive", Class: "certificate", Fingerprint: fp, PinnedKey: pinned, OfferedKey: offered}},
		{in: None},
		{in: ""},
		{in: "authenticated auth"},
		{in: "authenticated auth " + fp + " " + pinned},
		{in: "authenticated auth " + fp + " " + pinned + " " + offered + " extra"},
		{in: "session auth " + fp},
	}
	for _, c := range cases {
		got, ok := Parse(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}
