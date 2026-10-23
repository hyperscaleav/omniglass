package api

import "testing"

// TestParseRejection holds the endpoint-rejection read (#1059): a gate reads
// its rung and class and never its fingerprint; none, a malformed value, and
// a rung the ladder does not have read as no gate at all.
func TestParseRejection(t *testing.T) {
	cases := []struct {
		in          string
		rung, class string
	}{
		{in: "authenticated auth 9f86d081", rung: "authenticated", class: "auth"},
		{in: "responsive host-key 9f86d081", rung: "responsive", class: "host-key"},
		{in: "reachable protocol 9f86d081", rung: "reachable", class: "protocol"},
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
		if got == nil || got.Rung != c.rung || got.Class != c.class {
			t.Errorf("parseRejection(%q) = %+v, want %s %s", c.in, got, c.rung, c.class)
		}
	}
}
