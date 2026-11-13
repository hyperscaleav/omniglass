package transport_test

import (
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/transport"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The built-in data a transport's endpoint carries is declared on the
// registry (#1371): the lab-level check reads it, so a transport added to the
// registry without its built-in set is a failing test here, not a silent gap
// in the lab.

func TestEveryDeclaredBuiltInKeyIsRuledNodeProbeOrPlatform(t *testing.T) {
	for _, tr := range transport.All() {
		keys := slices.Concat(tr.BuiltIn, tr.Answered)
		for _, k := range keys {
			class, ok := writerclass.Reserved(k)
			if !ok {
				t.Errorf("%s declares %q, which no reserved writer class rules", tr.Name, k)
				continue
			}
			want := writerclass.NodeProbe
			if tr.Push {
				want = writerclass.Platform
			}
			if class != want {
				t.Errorf("%s declares %q, ruled %s, want %s", tr.Name, k, class, want)
			}
		}
	}
}

func TestEveryDialedTransportDeclaresItsBuiltInReachability(t *testing.T) {
	for _, tr := range transport.All() {
		if tr.Push {
			continue
		}
		if !slices.Contains(tr.BuiltIn, probekeys.EndpointReachable) {
			t.Errorf("%s does not declare %s", tr.Name, probekeys.EndpointReachable)
		}
		if len(tr.BuiltIn) < 2 {
			t.Errorf("%s declares %v: a probed transport carries a rung beside its verdict", tr.Name, tr.BuiltIn)
		}
	}
}

func TestAPushTransportDeclaresOnlyPlatformCounts(t *testing.T) {
	tr, _ := transport.ByName("webhook")
	if len(tr.BuiltIn) != 0 {
		t.Errorf("webhook BuiltIn = %v, want none: nothing is dialed", tr.BuiltIn)
	}
	want := []string{writerclass.WebhookPostCount}
	if !slices.Equal(tr.Answered, want) {
		t.Errorf("webhook Answered = %v, want %v", tr.Answered, want)
	}
}

func TestTimingsLandOnlyWhenTheEndpointAnswers(t *testing.T) {
	for name, want := range map[string]string{
		"icmp": probekeys.ICMPRTTAvg, "tcp": probekeys.TCPConnectTime, "ssh": probekeys.SSHHandshakeTime,
		"telnet": probekeys.TelnetResponseTime, "http": probekeys.HTTPResponseTime, "tls": probekeys.TLSHandshakeTime,
		"udp": probekeys.UDPResponseTime, "snmp": probekeys.SNMPResponseTime,
	} {
		tr, _ := transport.ByName(name)
		if !slices.Contains(tr.Answered, want) {
			t.Errorf("%s Answered = %v, want %s among it", name, tr.Answered, want)
		}
		if slices.Contains(tr.BuiltIn, want) {
			t.Errorf("%s declares %s always present, but it lands only when answered", name, want)
		}
	}
}

func TestWhatADriverShapeAddsToTheBuiltInSet(t *testing.T) {
	cases := []struct {
		name      string
		transport string
		shape     transport.DriverShape
		want      []string
	}{
		{"a poller reports a poll duration and its cpu time", "snmp", transport.DriverShape{Polls: true}, []string{probekeys.PollDuration, probekeys.PollCPUTime}},
		{"no driver adds nothing", "ssh", transport.DriverShape{}, nil},
		{"a listener on a held session reports a push count", "tls", transport.DriverShape{Listeners: true}, []string{probekeys.PushMessageCount, probekeys.SessionState}},
		{"a listener on udp reports a push count but holds no session", "udp", transport.DriverShape{Listeners: true}, []string{probekeys.PushMessageCount}},
		{"a bare tcp driver holds no session", "tcp", transport.DriverShape{Polls: true}, []string{probekeys.PollDuration, probekeys.PollCPUTime}},
		{"a tcp driver with a session block holds one", "tcp", transport.DriverShape{Session: true}, []string{probekeys.SessionState}},
		{"http holds no session however its driver reads", "http", transport.DriverShape{Polls: true, Session: true}, []string{probekeys.PollDuration, probekeys.PollCPUTime}},
		{"a webhook's listeners run on the server", "webhook", transport.DriverShape{Polls: true, Listeners: true}, nil},
	}
	for _, c := range cases {
		got := transport.DriverBuiltIn(c.transport, c.shape)
		slices.Sort(got)
		want := slices.Clone(c.want)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: %s over %s = %v, want %v", c.name, "DriverBuiltIn", c.transport, got, want)
		}
	}
}

func TestExpectedBuiltInIsTheTransportSetPlusTheDriverShape(t *testing.T) {
	got := transport.ExpectedBuiltIn("ssh", transport.DriverShape{Polls: true}, true, false)
	for _, k := range []string{probekeys.TCPOpen, probekeys.EndpointReachable, probekeys.SSHHandshakeTime, probekeys.PollDuration, probekeys.SessionState} {
		if !slices.Contains(got, k) {
			t.Errorf("answered ssh poller lacks %s: %v", k, got)
		}
	}
	got = transport.ExpectedBuiltIn("ssh", transport.DriverShape{Polls: true}, false, false)
	if slices.Contains(got, probekeys.SSHHandshakeTime) {
		t.Errorf("an unanswered ssh endpoint expects a handshake time: %v", got)
	}
	if slices.Contains(transport.ExpectedBuiltIn("nope", transport.DriverShape{}, true, false), "") {
		t.Error("an unknown transport expects an empty key")
	}
}

func TestAHeldSessionThatTheProbeYieldsToOwesNoProbeTiming(t *testing.T) {
	shape := transport.DriverShape{Polls: true, Session: true}
	if !transport.ProbeYieldsToSession("telnet") || transport.ProbeYieldsToSession("ssh") || transport.ProbeYieldsToSession("nope") {
		t.Fatal("telnet alone stands down for its up session (#1033)")
	}
	up := transport.ExpectedBuiltIn("telnet", shape, true, true)
	for _, k := range []string{probekeys.TCPConnectTime, probekeys.TelnetResponseTime} {
		if slices.Contains(up, k) {
			t.Errorf("a telnet endpoint with its session up expects %s, which the probe did not take: %v", k, up)
		}
	}
	for _, k := range []string{probekeys.TCPOpen, probekeys.EndpointResponsive, probekeys.PollDuration, probekeys.SessionState} {
		if !slices.Contains(up, k) {
			t.Errorf("a telnet endpoint with its session up lacks %s: %v", k, up)
		}
	}
	if down := transport.ExpectedBuiltIn("telnet", shape, true, false); !slices.Contains(down, probekeys.TelnetResponseTime) {
		t.Errorf("an answered telnet endpoint without a session up owes its response time: %v", down)
	}
	if ssh := transport.ExpectedBuiltIn("ssh", shape, true, true); !slices.Contains(ssh, probekeys.SSHHandshakeTime) {
		t.Errorf("ssh's probe does not yield to its session: %v", ssh)
	}
}
