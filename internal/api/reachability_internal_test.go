package api

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
)

// TestParseRejection holds the endpoint-rejection read (#1059): a gate reads
// its rung and class and never its fingerprint; a key refusal reads both keys
// as data (#1078); none, a malformed value, and a rung the ladder does not
// have read as no gate at all.
func TestParseRejection(t *testing.T) {
	t.Parallel()
	pinned, offered := "SHA256:"+strings.Repeat("A", 43), "SHA256:"+strings.Repeat("B", 43)
	cases := []struct {
		in              string
		rung, class     string
		pinned, offered string
	}{
		{in: "authenticated auth 9f86d081", rung: "authenticated", class: "auth"},
		{in: "responsive host-key 9f86d081", rung: "responsive", class: "host-key"},
		{in: "reachable protocol 9f86d081", rung: "reachable", class: "protocol"},
		{in: "responsive host-key 9f86d081 " + pinned + " " + offered, rung: "responsive", class: "host-key", pinned: pinned, offered: offered},
		{in: "none"},
		{in: ""},
		{in: "authenticated auth"},
		{in: "authenticated auth 9f86d081 extra"},
		{in: "session auth 9f86d081"},
	}
	for _, c := range cases {
		got := parseRejection(c.in)
		if c.rung == "" {
			if got != nil {
				t.Errorf("parseRejection(%q) = %+v, want no gate", c.in, got)
			}
			continue
		}
		if got == nil || got.Rung != c.rung || got.Class != c.class || got.PinnedKey != c.pinned || got.OfferedKey != c.offered {
			t.Errorf("parseRejection(%q) = %+v, want %s %s %q %q", c.in, got, c.rung, c.class, c.pinned, c.offered)
		}
	}
}

// TestCapReason holds the session reason a read serves to a bound (#1059): a
// device's refusal text rides it verbatim and a line answer may run to 64 KiB,
// which the console renders on every row that shows it. A reason within the
// bound is untouched; a longer one is cut on a rune boundary and says so.
// A refusal of a changed key (#1035) carries the key the endpoint pins and
// the key the component offered after the fingerprint: the read serves both
// as data (so a surface offering to trust the new key never parses a reason),
// and still never the fingerprint.
func TestParseRejectionCarriesTheRefusedKeys(t *testing.T) {
	t.Parallel()
	pinned, offered := "SHA256:"+strings.Repeat("A", 43), "SHA256:"+strings.Repeat("B", 43)
	got := parseRejection("responsive certificate 9f86d081 " + pinned + " " + offered)
	if got == nil || got.Rung != "responsive" || got.Class != "certificate" || got.PinnedKey != pinned || got.OfferedKey != offered {
		t.Fatalf("parseRejection = %+v, want the rung, class and both keys", got)
	}
	if got := parseRejection("responsive certificate 9f86d081"); got == nil || got.PinnedKey != "" || got.OfferedKey != "" {
		t.Fatalf("a refusal naming no key = %+v, want no keys", got)
	}
	if got := parseRejection("responsive certificate 9f86d081 " + pinned); got != nil {
		t.Fatalf("a malformed value = %+v, want no gate", got)
	}
}

func TestCapReason(t *testing.T) {
	t.Parallel()
	if got := capReason("auth: auth rejected"); got != "auth: auth rejected" {
		t.Fatalf("a short reason changed: %q", got)
	}
	long := strings.Repeat("é", maxReasonRunes+40)
	got := capReason(long)
	if !utf8.ValidString(got) {
		t.Fatalf("the cut split a rune: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != maxReasonRunes+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("capReason kept %d runes (want %d and an ellipsis): %q", n, maxReasonRunes+1, got[len(got)-8:])
	}
}

// TestReachabilityReadsOnlyNodeOnlyKeys ties the reachability read to #1080's
// ruling: every key it serves as the node's account (the verdict, its rungs,
// the probe layers and their timings, a held session's state and reason, the
// gate) is one the push lane refuses, so no push can stand in for the node
// here. A new key read here that is not ruled node-only fails this test.
func TestReachabilityReadsOnlyNodeOnlyKeys(t *testing.T) {
	t.Parallel()
	keys := []string{verdictKey, responsiveKey, authKey, sessionStateKey, sessionReasonKey, rejectionKey,
		tlsKeyKey, tlsExpiryKey, tlsDaysLeftKey, tlsHandshakeKey, udpResponseKey, snmpResponseKey}
	for _, l := range reachLayers {
		keys = append(keys, l.SignalKey, l.TimingKey)
	}
	for _, key := range keys {
		if !probekeys.IsNodeOnly(key) {
			t.Errorf("the reachability read serves %s as the node's, but probekeys does not rule it node-only", key)
		}
	}
}

// The gate is a reason set (#1650): the read serves every reason beside the
// refusal, its kind, since and source, and an endpoint held unreachable alone
// carries no refusal.
func TestParseGateReasonsServesEveryReasonBesideTheRefusal(t *testing.T) {
	t.Parallel()
	both := "authenticated auth fp-a;unreachable 2026-11-09T14:03:00Z probe-1"
	if got := parseRejection(both); got == nil || got.Rung != "authenticated" || got.Class != "auth" {
		t.Fatalf("parseRejection(%q) = %+v, want the refusal", both, got)
	}
	got := parseGateReasons(both)
	if len(got) != 1 || got[0].Kind != "unreachable" || !got[0].Since.Equal(time.Date(2026, 11, 9, 14, 3, 0, 0, time.UTC)) || got[0].Source != "probe-1" {
		t.Fatalf("parseGateReasons(%q) = %+v, want unreachable since the first down", both, got)
	}
	alone := "unreachable 2026-11-09T14:03:00Z probe-1"
	if got := parseRejection(alone); got != nil {
		t.Fatalf("unreachable alone read as a refusal: %+v", got)
	}
	if got := parseGateReasons(alone); len(got) != 1 {
		t.Fatalf("parseGateReasons(%q) = %+v, want the one reason", alone, got)
	}
	if got := parseGateReasons("none"); got != nil {
		t.Fatalf("an open gate served reasons %+v", got)
	}
}

// TestPendingReason holds why a verdict is null (#981): an endpoint no node
// probes is waiting on an assignment, an assigned one on its first probe, and a
// row with a verdict is waiting on nothing, so the field is absent.
func TestPendingReason(t *testing.T) {
	t.Parallel()
	verdict := &reachVerdictBody{Value: "up"}
	cases := []struct {
		name    string
		node    string
		verdict *reachVerdictBody
		want    string
	}{
		{"no node, no verdict", "", nil, pendingNoNode},
		{"node assigned, no verdict", "edge-1", nil, pendingAwaitingProbe},
		{"node assigned, verdict", "edge-1", verdict, ""},
		{"no node, verdict", "", verdict, ""},
	}
	for _, c := range cases {
		if got := pendingReason(c.node, c.verdict); got != c.want {
			t.Errorf("%s: pendingReason(%q, %v) = %q, want %q", c.name, c.node, c.verdict, got, c.want)
		}
	}
}
