package probekeys

import (
	"slices"
	"testing"
)

// The native probes' sample keys are declared once, here: the probes emit
// these constants and the calc rule's collected-output check reads Native.
func TestNative(t *testing.T) {
	got := Native()
	want := []string{EndpointAuthenticated, EndpointReachable, EndpointResponsive, HTTPResponseTime,
		ICMPRTTAvg, ICMPReachable, SSHHandshakeTime, TCPConnectTime, TCPOpen}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("Native() = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if Native()[0] == "mutated" {
		t.Error("Native shares its backing array")
	}
	if !IsNative(TCPOpen) || IsNative("uptime") {
		t.Error("IsNative disagrees with Native")
	}
}
