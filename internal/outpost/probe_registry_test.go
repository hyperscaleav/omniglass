package node

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/transport"
)

// The transport registry declares the keys each built transport's probe lands
// (ProbeKeys, #1087), and the collection page renders that declaration. These
// tests bind it to the probes: each is driven through the node's own dispatch
// (collectProbe, then the verdict the collector rolls its samples into) against
// a fake wire, once passing and once refused, and the keys that landed must be
// the declared ones, no more (a probe that lands a key its entry omits) and no
// fewer (an entry that declares a key no probe run lands).

// probeRun is one drive of a probe: the runner holding the fake wire, the task,
// and the node's session manager where the transport needs one (udp's socket).
type probeRun struct {
	runner   *collection.Runner
	task     collection.TaskSpec
	sessions *sessionManager
}

// probeRig builds the two runs of one transport's probe.
type probeRig struct {
	pass, refused func(t *testing.T) probeRun
}

func probeTask(transport, params string) collection.TaskSpec {
	return collection.TaskSpec{ID: "t-" + transport, Mode: "probe", Transport: transport, EndpointID: "ep-" + transport,
		EndpointName: transport, EndpointParams: json.RawMessage(params), Fingerprint: "fp-1"}
}

const probeTarget = `"target":"10.0.0.9:80","timeout":"1s"`

type pingFake struct{ res collection.PingResult }

func (f pingFake) Ping(context.Context, string, int, time.Duration) (collection.PingResult, error) {
	return f.res, nil
}

type dialFake struct {
	ms    float64
	reach collection.Reachability
}

func (f dialFake) Dial(context.Context, string, time.Duration) (float64, collection.Reachability, error) {
	return f.ms, f.reach, nil
}

type httpProbeFake struct{ res collection.HTTPResult }

func (f httpProbeFake) Probe(context.Context, collection.HTTPTask) (collection.HTTPResult, error) {
	return f.res, nil
}

type telnetFake struct{ res collection.TelnetResult }

func (f telnetFake) Probe(context.Context, string, time.Duration) (collection.TelnetResult, error) {
	return f.res, nil
}

type sshProbeFake struct{ res collection.SSHResult }

func (f sshProbeFake) Probe(context.Context, string, collection.SSHCredential, time.Duration) (collection.SSHResult, error) {
	return f.res, nil
}

type tlsProbeFake struct{ res collection.TLSResult }

func (f tlsProbeFake) Probe(context.Context, string, collection.TLSTrust, time.Duration) (collection.TLSResult, error) {
	return f.res, nil
}

func always(run probeRun) func(*testing.T) probeRun {
	return func(*testing.T) probeRun { return run }
}

// httpRig is the rig of http and websocket (one GET /) and, with tls set, of
// https and wss (the handshake, the certificate, then the GET /).
func httpRig(transport string, tls bool) probeRig {
	task := probeTask(transport, `{`+probeTarget+`}`)
	pass := collection.HTTPResult{Dialed: true, Responded: true, DialMS: 2, ResponseMS: 5, Reason: collection.Responded}
	if tls {
		pass.Handshaken, pass.Accepted, pass.HandshakeMS = true, true, 3
		pass.Facts.NotAfter = time.Now().Add(90 * 24 * time.Hour)
	}
	return probeRig{
		pass:    always(probeRun{runner: &collection.Runner{HTTP: httpProbeFake{pass}}, task: task}),
		refused: always(probeRun{runner: &collection.Runner{HTTP: httpProbeFake{collection.HTTPResult{Reason: collection.Refused}}}, task: task}),
	}
}

// probeRigs is one rig per built transport that dials; a transport added to the
// registry without a rig here fails TestEveryBuiltTransportHasAProbeRig.
var probeRigs = map[string]probeRig{
	"icmp": {
		pass: always(probeRun{runner: &collection.Runner{Ping: pingFake{collection.PingResult{Received: 1, AvgRTT: 2 * time.Millisecond, Reason: collection.Responded}}},
			task: probeTask("icmp", `{"target":"10.0.0.9"}`)}),
		refused: always(probeRun{runner: &collection.Runner{Ping: pingFake{collection.PingResult{Reason: collection.Timedout}}},
			task: probeTask("icmp", `{"target":"10.0.0.9"}`)}),
	},
	"tcp": {
		pass:    always(probeRun{runner: &collection.Runner{TCP: dialFake{4, collection.Responded}}, task: probeTask("tcp", `{`+probeTarget+`}`)}),
		refused: always(probeRun{runner: &collection.Runner{TCP: dialFake{0, collection.Refused}}, task: probeTask("tcp", `{`+probeTarget+`}`)}),
	},
	"http":      httpRig("http", false),
	"websocket": httpRig("websocket", false),
	"https":     httpRig("https", true),
	"wss":       httpRig("wss", true),
	"telnet": {
		pass:    always(probeRun{runner: &collection.Runner{Telnet: telnetFake{collection.TelnetResult{Dialed: true, Responded: true, DialMS: 2, ResponseMS: 4, Reason: collection.Responded}}}, task: probeTask("telnet", `{`+probeTarget+`}`)}),
		refused: always(probeRun{runner: &collection.Runner{Telnet: telnetFake{collection.TelnetResult{Reason: collection.Refused}}}, task: probeTask("telnet", `{`+probeTarget+`}`)}),
	},
	"ssh": {
		// A credential rides the params, so the auth rung runs: the pass is
		// accepted, the refusal answers the exchange and rejects it.
		pass: always(probeRun{runner: &collection.Runner{SSH: sshProbeFake{collection.SSHResult{Dialed: true, Responded: true, AuthAttempted: true, Authenticated: true, DialMS: 2, HandshakeMS: 9, Reason: collection.Responded}}},
			task: probeTask("ssh", `{`+probeTarget+`,"username":"u","password":"p"}`)}),
		refused: always(probeRun{runner: &collection.Runner{SSH: sshProbeFake{collection.SSHResult{Dialed: true, Responded: true, AuthAttempted: true, DialMS: 2, HandshakeMS: 9, Reason: collection.Responded}}},
			task: probeTask("ssh", `{`+probeTarget+`,"username":"u","password":"wrong"}`)}),
	},
	"tls": {
		pass: always(probeRun{runner: &collection.Runner{TLS: tlsProbeFake{collection.TLSResult{Dialed: true, Responded: true, Accepted: true, DialMS: 2, HandshakeMS: 7, Reason: collection.Responded,
			Facts: collection.TLSFacts{NotAfter: time.Now().Add(90 * 24 * time.Hour)}}}}, task: probeTask("tls", `{`+probeTarget+`}`)}),
		refused: always(probeRun{runner: &collection.Runner{TLS: tlsProbeFake{collection.TLSResult{Reason: collection.Refused}}}, task: probeTask("tls", `{`+probeTarget+`}`)}),
	},
	"udp": {
		// A real component socket on loopback, as the udp tests run it: the
		// request rung rides the endpoint's own bound socket, which a fake
		// would not exercise. The refusal is a closed port no process can bind.
		pass: func(t *testing.T) probeRun {
			peer := newComponent(t, nvpAnswers)
			return udpRun(t, peer.addr())
		},
		refused: func(t *testing.T) probeRun { return udpRun(t, "127.0.0.1:1") },
	},
	"snmp": {
		pass: func(t *testing.T) probeRun {
			return snmpRun(t, &snmpFake{res: map[string]string{"1.3.6.1.2.1.1.3.0": "42"}})
		},
		refused: func(t *testing.T) probeRun {
			return snmpRun(t, &snmpFake{err: &collection.TerminalError{Class: driver.ClassAuth, Reason: "the agent answered authorizationError for the community"}})
		},
	},
}

func udpRun(t *testing.T, addr string) probeRun {
	t.Helper()
	m := newDatagramManager(t, newCapture())
	task := udpProbeTask("t-udp", "ep-udp", udpParams(addr))
	m.reconcile([]collection.TaskSpec{task})
	return probeRun{runner: &collection.Runner{}, task: task, sessions: m}
}

func snmpRun(t *testing.T, fake *snmpFake) probeRun {
	t.Helper()
	m := newDatagramManager(t, newCapture())
	task := snmpProbeTask("fp-1")
	m.reconcile([]collection.TaskSpec{task})
	return probeRun{runner: &collection.Runner{SNMP: fake}, task: task, sessions: m}
}

// landedKeys drives one run as the collector does: the probe, then the
// endpoint verdict rolled up from its samples.
func landedKeys(t *testing.T, run probeRun) []string {
	t.Helper()
	dps, err := collectProbe(t.Context(), run.runner, run.task, run.sessions)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	dps = appendVerdict(dps, run.task.ID, map[string]string{})
	keys := make([]string, 0, len(dps))
	for _, d := range dps {
		keys = append(keys, d.Name)
	}
	return keys
}

// probedTransports are the registry's built transports a node dials.
func probedTransports() []transport.Transport {
	var out []transport.Transport
	for _, tr := range transport.All() {
		if tr.Built && !tr.Push {
			out = append(out, tr)
		}
	}
	return out
}

func TestEveryBuiltTransportHasAProbeRig(t *testing.T) {
	t.Parallel()
	for _, tr := range probedTransports() {
		if _, ok := probeRigs[tr.Name]; !ok {
			t.Errorf("transport %q is built and dialed but has no probe rig: add one so its probe is bound to its declared keys", tr.Name)
		}
	}
	for name := range probeRigs {
		if tr, ok := transport.ByName(name); !ok || !tr.Built || tr.Push {
			t.Errorf("probe rig %q is not a built, dialed transport in the registry", name)
		}
	}
}

// A probe, passing and refused, lands exactly the keys its transport declares.
func TestABuiltTransportsProbeLandsTheKeysItsRegistryEntryDeclares(t *testing.T) {
	t.Parallel()
	for _, tr := range probedTransports() {
		rig, ok := probeRigs[tr.Name]
		if !ok {
			continue // reported by TestEveryBuiltTransportHasAProbeRig
		}
		t.Run(tr.Name, func(t *testing.T) {
			t.Parallel()
			if len(tr.ProbeKeys) == 0 {
				t.Fatalf("the registry declares no probe keys for %q", tr.Name)
			}
			landed := map[string]bool{}
			for _, run := range []struct {
				name  string
				build func(*testing.T) probeRun
			}{{"pass", rig.pass}, {"refusal", rig.refused}} {
				for _, k := range landedKeys(t, run.build(t)) {
					if !slices.Contains(tr.ProbeKeys, k) {
						t.Errorf("the %s run landed %q, which the %s entry does not declare in ProbeKeys %v", run.name, k, tr.Name, tr.ProbeKeys)
					}
					landed[k] = true
				}
			}
			for _, k := range tr.ProbeKeys {
				if !landed[k] {
					t.Errorf("the %s entry declares %q but neither a passing nor a refused probe landed it", tr.Name, k)
				}
			}
		})
	}
}
