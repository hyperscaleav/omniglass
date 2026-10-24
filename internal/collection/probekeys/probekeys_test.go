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
		ICMPRTTAvg, ICMPReachable, SSHHandshakeTime, TelnetResponseTime, TCPConnectTime, TCPOpen, SessionState, SessionReason, SSHHostKey, EndpointRejection,
		TLSHandshakeTime, TLSCertificate, TLSCertificateExpiry, TLSCertificateDaysLeft}
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

// The key a tls endpoint trusted steers what the node attempts, as the ssh
// host key does (#1035): only the node's own collection writes it.
func TestTheTLSPinIsANodeControlKey(t *testing.T) {
	if !IsNodeControl(TLSCertificate) || !IsNodeControl(SSHHostKey) {
		t.Fatal("a pin key is pushable")
	}
	if IsNodeControl(TLSCertificateExpiry) || IsNodeControl(TLSCertificateDaysLeft) {
		t.Fatal("a certificate fact that steers nothing is refused to the push lane")
	}
}
