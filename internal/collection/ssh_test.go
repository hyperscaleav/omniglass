package collection_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The ssh ladder's sample shapes over a faked prober; the real handshake path
// is the integration test's job (the capability carve-out). The auth rung's
// contract is the one worth pinning hardest: endpoint-authenticated exists
// ONLY when a credential was actually tried, so an unauthenticated probe can
// never read as a failed login.

type fakeSSHProber struct{ res collection.SSHResult }

func (f fakeSSHProber) Probe(context.Context, string, collection.SSHCredential, time.Duration) (collection.SSHResult, error) {
	return f.res, nil
}

func sshSamplesByName(t *testing.T, res collection.SSHResult) map[string]collection.Sample {
	t.Helper()
	r := &collection.Runner{SSH: fakeSSHProber{res: res}}
	dps, err := r.CollectSSH(context.Background(), collection.SSHTask{Target: "10.0.0.1:22"})
	if err != nil {
		t.Fatalf("CollectSSH: %v", err)
	}
	out := map[string]collection.Sample{}
	for _, d := range dps {
		out[d.Name] = d
	}
	return out
}

func TestCollectSSHRespondedNoCredential(t *testing.T) {
	dps := sshSamplesByName(t, collection.SSHResult{
		Dialed: true, Responded: true, DialMS: 1.0, HandshakeMS: 9.0,
		Reason: collection.Responded,
	})
	if dps["endpoint-responsive"].Text != "up" {
		t.Errorf("endpoint-responsive = %+v, want up", dps["endpoint-responsive"])
	}
	if dps["ssh-handshake-time"].Value != 9.0 {
		t.Errorf("ssh-handshake-time = %v, want 9.0", dps["ssh-handshake-time"].Value)
	}
	if _, ok := dps["endpoint-authenticated"]; ok {
		t.Error("endpoint-authenticated emitted with no credential tried; absence is the fact")
	}
}

func TestCollectSSHRespondedButNotAuthenticated(t *testing.T) {
	dps := sshSamplesByName(t, collection.SSHResult{
		Dialed: true, Responded: true, AuthAttempted: true, Authenticated: false,
		DialMS: 1.0, HandshakeMS: 12.0, Reason: collection.Responded,
	})
	if dps["endpoint-responsive"].Text != "up" {
		t.Errorf("responded-but-not-authenticated must keep the responds rung up, got %+v", dps["endpoint-responsive"])
	}
	if dps["endpoint-authenticated"].Text != "no" {
		t.Errorf("endpoint-authenticated = %+v, want no", dps["endpoint-authenticated"])
	}
}

func TestCollectSSHAuthenticated(t *testing.T) {
	dps := sshSamplesByName(t, collection.SSHResult{
		Dialed: true, Responded: true, AuthAttempted: true, Authenticated: true,
		DialMS: 1.0, HandshakeMS: 15.0, Reason: collection.Responded,
	})
	if dps["endpoint-authenticated"].Text != "yes" {
		t.Errorf("endpoint-authenticated = %+v, want yes", dps["endpoint-authenticated"])
	}
}

func TestCollectSSHReachedButNotResponsive(t *testing.T) {
	dps := sshSamplesByName(t, collection.SSHResult{
		Dialed: true, Responded: false, DialMS: 1.0, Reason: collection.Timedout,
	})
	if dps["tcp-open"].Value != 1.0 {
		t.Errorf("tcp-open = %v, want 1: the port accepted", dps["tcp-open"].Value)
	}
	if dps["endpoint-responsive"].Text != "down" {
		t.Errorf("endpoint-responsive = %+v, want down", dps["endpoint-responsive"])
	}
	if _, ok := dps["ssh-handshake-time"]; ok {
		t.Error("ssh-handshake-time emitted with no completed exchange")
	}
}
