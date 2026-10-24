package api

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
)

// TestParseRejection holds the endpoint-rejection read (#1059): a gate reads
// its rung and class and never its fingerprint; a key refusal reads both keys
// as data (#1078); none, a malformed value, and a rung the ladder does not
// have read as no gate at all.
func TestParseRejection(t *testing.T) {
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
	keys := []string{verdictKey, responsiveKey, authKey, sessionStateKey, sessionReasonKey, rejectionKey,
		tlsKeyKey, tlsExpiryKey, tlsDaysLeftKey, tlsHandshakeKey, udpResponseKey}
	for _, l := range reachLayers {
		keys = append(keys, l.SignalKey, l.TimingKey)
	}
	for _, key := range keys {
		if !probekeys.IsNodeOnly(key) {
			t.Errorf("the reachability read serves %s as the node's, but probekeys does not rule it node-only", key)
		}
	}
}
