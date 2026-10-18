package storage

import "testing"

func TestTargetHost(t *testing.T) {
	for in, want := range map[string]string{
		"10.0.0.5:80": "10.0.0.5",
		"10.0.0.5":    "10.0.0.5",
		"[::1]:161":   "::1",
		"host.lan:22": "host.lan",
		"":            "",
	} {
		if got := TargetHost(in); got != want {
			t.Errorf("TargetHost(%q) = %q, want %q", in, got, want)
		}
	}
}
