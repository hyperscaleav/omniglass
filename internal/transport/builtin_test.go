package transport_test

import (
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/transport"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// Each transport declares the built-in keys its endpoints always carry (#1371):
// every one a key only a node's probes and sessions write (node-probe) or the
// server's own webhook count (platform), never a collected key; never one
// that lands only when nonzero or only when something fails; and every
// dialed transport's set says whether the endpoint is reachable.
func TestEveryTransportDeclaresItsBuiltInKeys(t *testing.T) {
	sparse := []string{
		probekeys.PollFailureCount, probekeys.PushUnclaimedCount, probekeys.CollectionFailed,
		probekeys.EndpointRejection, probekeys.EndpointRejectionReason, writerclass.WebhookRefusalCount,
	}
	for _, tr := range transport.All() {
		sets := map[string][]string{
			"built_in": tr.BuiltIn, "session_built_in": tr.SessionBuiltIn, "credential_built_in": tr.CredentialBuiltIn,
			"poll_built_in": tr.PollBuiltIn, "listen_built_in": tr.ListenBuiltIn,
		}
		for field, keys := range sets {
			for _, k := range keys {
				if c := writerclass.Of(k, false); c != writerclass.NodeProbe && c != writerclass.Platform {
					t.Errorf("%s %s declares %s, of writer class %s: built-in data is node-probe or platform", tr.Name, field, k, c)
				}
				if slices.Contains(sparse, k) {
					t.Errorf("%s %s declares %s, which lands only when nonzero or on a failure", tr.Name, field, k)
				}
			}
		}
		switch {
		case tr.Push && len(tr.BuiltIn) != 0:
			t.Errorf("%s is pushed to, nothing dialed: it has no probe keys, got %v", tr.Name, tr.BuiltIn)
		case !tr.Push && !slices.Contains(tr.BuiltIn, probekeys.EndpointReachable):
			t.Errorf("%s does not declare %s", tr.Name, probekeys.EndpointReachable)
		}
		if !tr.Held && tr.Name != "udp" && len(tr.ListenBuiltIn) > 0 && !tr.Push {
			t.Errorf("%s holds no session or socket, yet declares listener keys %v", tr.Name, tr.ListenBuiltIn)
		}
	}
}

// What an endpoint must carry is its transport's set plus what its attach
// adds: a session's state where a driver holds one, the authentication
// verdict where the endpoint carries a credential, the poll timing where its
// driver polls, the pushed-message count where it listens.
func TestTheExpectedBuiltInSetFollowsTheAttach(t *testing.T) {
	get := func(n string) transport.Transport {
		tr, ok := transport.ByName(n)
		if !ok {
			t.Fatalf("no %s", n)
		}
		return tr
	}
	has := func(set []string, keys ...string) bool {
		for _, k := range keys {
			if !slices.Contains(set, k) {
				return false
			}
		}
		return true
	}
	tcp := get("tcp")
	bare := tcp.Expected(transport.Attach{})
	if has(bare, probekeys.SessionState) || has(bare, probekeys.PollDuration) || !has(bare, probekeys.TCPOpen) {
		t.Errorf("a bare tcp probe expects %v: its probe keys alone", bare)
	}
	driven := tcp.Expected(transport.Attach{Driver: true, Polls: true, Listeners: true})
	if !has(driven, probekeys.SessionState, probekeys.PollDuration, probekeys.PushMessageCount) || has(driven, probekeys.EndpointAuthenticated) {
		t.Errorf("a tcp driver that polls and listens, with no credential, expects %v", driven)
	}
	if got := get("snmp").Expected(transport.Attach{Driver: true, Credential: true, Polls: true}); !has(got, probekeys.EndpointAuthenticated, probekeys.PollDuration) || has(got, probekeys.SessionState) {
		t.Errorf("an snmp attach with a community expects %v", got)
	}
	if got := get("https").Expected(transport.Attach{Driver: true, Polls: true}); has(got, probekeys.SessionState) || !has(got, probekeys.TLSCertificateDaysLeft) {
		t.Errorf("an https driver that only polls holds no session: %v", got)
	}
	if got := get("https").Expected(transport.Attach{Driver: true, Listeners: true}); !has(got, probekeys.SessionState, probekeys.PushMessageCount) {
		t.Errorf("an https driver that listens holds its event stream: %v", got)
	}
	if got := get("webhook").Expected(transport.Attach{Driver: true, Credential: true, Listeners: true}); len(got) != 1 || got[0] != writerclass.WebhookPostCount {
		t.Errorf("a webhook endpoint expects its accepted posts alone, got %v", got)
	}
}
