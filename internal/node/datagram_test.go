//go:build linux

package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The node's udp endpoints (#1037) against a real component socket on
// loopback: each endpoint's own bound socket, the request rung over it, polls
// and commands borrowing it, the arm conversation, the gate, and the foreign
// datagram flush. The simulated panel end to end is run_udp_test.go.

// component is a udp peer answering a line protocol, one datagram a line.
type component struct {
	conn     *net.UDPConn
	requests atomic.Int32
	mu       sync.Mutex
	got      []string
	answer   func(string) []string
	drop     func(n int32) bool
	lastFrom *net.UDPAddr
}

func newComponent(t *testing.T, answer func(string) []string) *component {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	p := &component{conn: c, answer: answer}
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			line := strings.TrimRight(string(buf[:n]), "\r\n")
			k := p.requests.Add(1)
			p.mu.Lock()
			p.got = append(p.got, line)
			p.lastFrom = from
			drop, answer := p.drop, p.answer
			p.mu.Unlock()
			if drop != nil && drop(k) {
				continue
			}
			for _, out := range answer(line) {
				_, _ = c.WriteToUDP([]byte(out+"\r\n"), from)
			}
		}
	}()
	return p
}

func (p *component) addr() string { return p.conn.LocalAddr().String() }

func (p *component) set(answer func(string) []string, drop func(int32) bool) {
	p.mu.Lock()
	p.answer, p.drop = answer, drop
	p.mu.Unlock()
}

func (p *component) lines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.got...)
}

func (p *component) count(line string) int {
	n := 0
	for _, l := range p.lines() {
		if l == line {
			n++
		}
	}
	return n
}

func nvpAnswers(line string) []string {
	switch {
	case line == "GET INPUT":
		return []string{"INPUT hdmi1"}
	case line == "SUBSCRIBE INPUT":
		return []string{"OK"}
	case strings.HasPrefix(line, "SET INPUT "):
		return []string{"INPUT " + strings.TrimPrefix(line, "SET INPUT ")}
	}
	return []string{"-ERR unknown"}
}

const udpLadder = `{"ping":true,"transport":"request","request":{"line":"GET INPUT","reply":{"prefix":"INPUT ","error":{"prefix":"-ERR"}}},"session":false}`

func udpProbeTask(id, endpointID, params string) collection.TaskSpec {
	var l driver.EffectiveLadder
	_ = json.Unmarshal([]byte(udpLadder), &l)
	return collection.TaskSpec{ID: id, Mode: "probe", Transport: "udp", EndpointID: endpointID, EndpointName: "udp",
		EndpointParams: json.RawMessage(params), Ladder: &l, Fingerprint: "fp-1"}
}

func udpListenTask(id, endpointID, params string) collection.TaskSpec {
	return collection.TaskSpec{ID: id, Mode: "listen", Transport: "udp", EndpointID: endpointID, EndpointName: "udp",
		EndpointParams: json.RawMessage(params), Fingerprint: "fp-1",
		Spec: json.RawMessage(`{"driver":"newtron-nvp-udp","function":"events","arm":["SUBSCRIBE INPUT"],
			"match":{"prefix":"EVT "},
			"emits":[{"name":"video-input","lane":"property","extract":{"regex":"^EVT INPUT (\\S+)$"}}]}`)}
}

func udpPollTask(id, endpointID, params string) collection.TaskSpec {
	return collection.TaskSpec{ID: id, Mode: "poll", Transport: "udp", EndpointID: endpointID, EndpointName: "udp",
		EndpointParams: json.RawMessage(params), Fingerprint: "fp-1",
		Spec: json.RawMessage(`{"driver":"newtron-nvp-udp","function":"status",
			"request":{"line":"GET INPUT","reply":{"prefix":"INPUT "}},
			"emits":[{"name":"video-input","lane":"property","extract":{"regex":"^INPUT (\\S+)$"}}]}`)}
}

func udpParams(target string, extra ...string) string {
	return `{"target":"` + target + `","timeout":"600ms"` + strings.Join(extra, "") + `}`
}

func TestDesiredDatagrams(t *testing.T) {
	a := udpParams("10.0.0.5:51327", `,"listen-port":"51400"`)
	badPort := udpParams("10.0.0.6:51327", `,"listen-port":"a lot"`)
	tasks := []collection.TaskSpec{
		udpPollTask("p1", "ep-1", a),
		udpListenTask("l2", "ep-1", a),
		udpListenTask("l1", "ep-1", a),
		listenTask("t1", "ep-2", "10.0.0.7:51325"), // tcp: a held session, never a socket
		udpPollTask("b1", "ep-3", badPort),
		udpPollTask("n1", "ep-4", `{}`),
		udpPollTask("x1", "", a), // an older server: no endpoint id
	}
	want, refused := desiredDatagrams(tasks)
	if len(want) != 1 {
		t.Fatalf("desired = %+v, want one socket (ep-1)", want)
	}
	got := want["ep-1"]
	if got.conn.target != "10.0.0.5:51327" || got.conn.listenPort != 51400 || got.taskID != "l1" {
		t.Fatalf("ep-1 socket = %+v, want its target, its listen port, and its lowest task", got)
	}
	if len(got.listeners) != 2 || strings.Join(got.arm, ",") != "SUBSCRIBE INPUT,SUBSCRIBE INPUT" {
		t.Fatalf("ep-1 listeners %d arm %v, want both listen tasks and their arm lines", len(got.listeners), got.arm)
	}
	if got.timeout != 600*time.Millisecond {
		t.Fatalf("ep-1 timeout = %s", got.timeout)
	}
	if !strings.Contains(refused["ep-3"], "listen-port") || refused["ep-4"] == "" || len(refused) != 2 {
		t.Fatalf("refused = %v, want the bad listen port and the empty target named", refused)
	}
	// A listen port given as a number reads the same.
	num, _ := desiredDatagrams([]collection.TaskSpec{udpPollTask("p", "ep", udpParams("10.0.0.5:1", `,"listen-port":51401`))})
	if num["ep"].conn.listenPort != 51401 {
		t.Fatalf("a numeric listen port = %+v", num["ep"])
	}
}

func newDatagramManager(t *testing.T, c *capture) *sessionManager {
	t.Helper()
	m := newSessionManager(map[string]collection.SessionDialer{}, c.hooks(&transitionLog{}))
	t.Cleanup(m.closeAll)
	return m
}

func samplesByName(dps []collection.Sample) map[string]collection.Sample {
	out := map[string]collection.Sample{}
	for _, d := range dps {
		out[d.Name] = d
	}
	return out
}

func TestTheUDPRequestRungIsTheTransportRung(t *testing.T) {
	peer := newComponent(t, nvpAnswers)
	c := newCapture()
	m := newDatagramManager(t, c)
	task := udpProbeTask("r1", "ep-1", udpParams(peer.addr()))
	m.reconcile([]collection.TaskSpec{task})
	runner := &collection.Runner{}

	dps, err := collectProbe(t.Context(), runner, task, m)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	got := samplesByName(dps)
	if r := got[collection.SignalEndpointResponsive]; r.Text != "up" || r.Labels[collection.ReasonLabel] != string(collection.Responded) {
		t.Fatalf("responsive = %+v, want up, responded", r)
	}
	if rtt, ok := got[collection.SignalUDPResponseTime]; !ok || rtt.Value <= 0 {
		t.Fatalf("udp-response-time = %+v, want the answer's time", rtt)
	}
	if _, ok := got[collection.SignalTCPOpen]; ok {
		t.Fatalf("a udp endpoint landed tcp-open: %v", names(dps))
	}
	if up, ok := collection.EndpointVerdict(dps); !ok || !up {
		t.Fatalf("verdict = %v, %v; the request rung decides a udp endpoint's verdict", up, ok)
	}

	// The component falls silent: not responsive, timed out, no timing.
	peer.set(nvpAnswers, func(int32) bool { return true })
	dps, err = collectProbe(t.Context(), runner, task, m)
	if err != nil {
		t.Fatal(err)
	}
	got = samplesByName(dps)
	if r := got[collection.SignalEndpointResponsive]; r.Text != "down" || r.Labels[collection.ReasonLabel] != string(collection.Timedout) {
		t.Fatalf("silent responsive = %+v, want down, timeout", r)
	}
	if _, ok := got[collection.SignalUDPResponseTime]; ok {
		t.Fatal("a lost request landed a timing")
	}
	if up, ok := collection.EndpointVerdict(dps); !ok || up {
		t.Fatalf("silent verdict = %v, %v, want down", up, ok)
	}
}

func TestTheUDPRequestRungReadsAClosedPortRefused(t *testing.T) {
	// Port 9 on a claimed loopback address: nothing can be bound there, so
	// the host refuses the request (a pick-and-release port could be bound
	// by a concurrent run first).
	closed := net.JoinHostPort(simtest.ClaimLoopback(t), "9")
	m := newDatagramManager(t, newCapture())
	task := udpProbeTask("r1", "ep-1", udpParams(closed))
	m.reconcile([]collection.TaskSpec{task})
	dps, err := collectProbe(t.Context(), &collection.Runner{}, task, m)
	if err != nil {
		t.Fatal(err)
	}
	if r := samplesByName(dps)[collection.SignalEndpointResponsive]; r.Text != "down" || r.Labels[collection.ReasonLabel] != string(collection.Refused) {
		t.Fatalf("closed port responsive = %+v, want down, refused", r)
	}
}

// A terminal reply gates the endpoint at the transport rung (nothing logs in
// over udp), and beneath the gate no request is sent under that fingerprint.
func TestAUDPTerminalReplyGatesTheEndpoint(t *testing.T) {
	peer := newComponent(t, func(string) []string { return []string{"-ERR locked"} })
	m := newDatagramManager(t, newCapture())
	task := udpProbeTask("r1", "ep-1", udpParams(peer.addr()))
	task.Session = &driver.SessionSpec{Terminal: []driver.TerminalRule{{Class: driver.ClassAuth, Match: driver.Match{Prefix: "-ERR locked"}}}}
	m.reconcile([]collection.TaskSpec{task})
	runner := &collection.Runner{}
	if _, err := collectProbe(t.Context(), runner, task, m); err != nil {
		t.Fatal(err)
	}
	gt, ok := m.blocked("ep-1", rungResponsive)
	if !ok || gt.class != driver.ClassAuth || gt.rung != rungResponsive {
		t.Fatalf("gate = %+v, %v; want the transport rung gated, class auth", gt, ok)
	}
	sent := peer.requests.Load()
	dps, err := collectProbe(t.Context(), runner, task, m)
	if err != nil {
		t.Fatal(err)
	}
	if peer.requests.Load() != sent {
		t.Fatal("a request went out beneath the gate")
	}
	if r := samplesByName(dps)[collection.SignalEndpointResponsive]; r.Text != "down" {
		t.Fatalf("gated responsive = %+v, want down", r)
	}
	// A poll and a command beneath the gate fail fast, sending nothing.
	poll := udpPollTask("p1", "ep-1", udpParams(peer.addr()))
	m.reconcile([]collection.TaskSpec{task, poll})
	if _, out := executeDelivery(t.Context(), runner, collection.CommandDelivery{ID: 1, Transport: "udp", EndpointID: "ep-1", Line: "SET INPUT hdmi2"}, m); !strings.Contains(out, "gated") {
		t.Fatalf("a gated command = %q, want it refused as gated", out)
	}
	if peer.requests.Load() != sent {
		t.Fatal("a command went out beneath the gate")
	}
}

func TestAUDPPollRidesTheEndpointsSocketAndRetries(t *testing.T) {
	peer := newComponent(t, nvpAnswers)
	peer.set(nvpAnswers, func(n int32) bool { return n <= 2 })
	m := newDatagramManager(t, newCapture())
	poll := udpPollTask("p1", "ep-1", udpParams(peer.addr()))
	m.reconcile([]collection.TaskSpec{poll})
	dps, faults := collectDriverTask(t.Context(), &collection.Runner{}, poll, m)
	if len(faults) != 0 {
		t.Fatalf("faults: %v", faults)
	}
	if v := samplesByName(dps)["video-input"]; v.Text != "hdmi1" {
		t.Fatalf("poll samples = %+v, want video-input through the retries", dps)
	}
	if peer.count("GET INPUT") != 3 {
		t.Fatalf("the component saw %v; a poll re-sends a lost request twice by default", peer.lines())
	}
	// No socket for the endpoint: the poll faults by name.
	if _, faults := collectDriverTask(t.Context(), &collection.Runner{}, udpPollTask("p2", "ep-9", udpParams(peer.addr())), m); len(faults) != 1 || !strings.Contains(faults[0].Error(), "udp") {
		t.Fatalf("faults = %v, want the missing socket named", faults)
	}
}

func TestAUDPCommandIsSentOnceUnlessItDeclaresRetries(t *testing.T) {
	peer := newComponent(t, nvpAnswers)
	m := newDatagramManager(t, newCapture())
	m.reconcile([]collection.TaskSpec{udpPollTask("p1", "ep-1", udpParams(peer.addr()))})
	runner := &collection.Runner{}
	d := collection.CommandDelivery{ID: 1, Transport: "udp", EndpointID: "ep-1", Line: "SET INPUT hdmi2", Reply: &driver.Reply{Prefix: "INPUT "}}
	reply, out := executeDelivery(t.Context(), runner, d, m)
	if out != "" || reply != "INPUT hdmi2" {
		t.Fatalf("command = %q, %q; want the component's answer", reply, out)
	}
	peer.set(nvpAnswers, func(int32) bool { return true })
	d.ID, d.Line = 2, "SET INPUT hdmi3"
	if _, out := executeDelivery(t.Context(), runner, d, m); out == "" {
		t.Fatal("a lost command reported success")
	}
	time.Sleep(50 * time.Millisecond)
	if n := peer.count("SET INPUT hdmi3"); n != 1 {
		t.Fatalf("the component saw SET INPUT hdmi3 %d times; with no retries a command is sent once", n)
	}
	base := peer.requests.Load()
	peer.set(nvpAnswers, func(n int32) bool { return n == base+1 }) // the first send is lost
	d.ID, d.Line, d.Retries = 3, "SET INPUT dp1", 2
	if reply, out := executeDelivery(t.Context(), runner, d, m); out != "" || reply != "INPUT dp1" {
		t.Fatalf("a command declaring retries = %q, %q; want it answered on a retry", reply, out)
	}
	// A refusal carries the component's text.
	peer.set(func(string) []string { return []string{"-ERR no such input"} }, nil)
	d.ID, d.Line, d.Retries = 4, "SET INPUT vga", 0
	d.Reply = &driver.Reply{Prefix: "INPUT ", Error: &driver.Match{Prefix: "-ERR"}}
	if reply, out := executeDelivery(t.Context(), runner, d, m); reply != "-ERR no such input" || !strings.Contains(out, "-ERR no such input") {
		t.Fatalf("a refused command = %q, %q", reply, out)
	}
}

func TestAUDPEndpointArmsAtOpenAndOnRecovery(t *testing.T) {
	peer := newComponent(t, nvpAnswers)
	c := newCapture()
	m := newDatagramManager(t, c)
	p := udpParams(peer.addr())
	probe, listen := udpProbeTask("r1", "ep-1", p), udpListenTask("l1", "ep-1", p)
	m.reconcile([]collection.TaskSpec{probe, listen})
	eventually(t, "the arm at open", func() bool { return peer.count("SUBSCRIBE INPUT") == 1 })
	runner := &collection.Runner{}
	if _, err := collectProbe(t.Context(), runner, probe, m); err != nil {
		t.Fatal(err)
	}
	if peer.count("SUBSCRIBE INPUT") != 1 {
		t.Fatal("an endpoint already up re-armed")
	}
	// The component goes away and comes back (a reboot forgot its
	// subscriber): the rung's recovery re-arms.
	peer.set(nvpAnswers, func(int32) bool { return true })
	if _, err := collectProbe(t.Context(), runner, probe, m); err != nil {
		t.Fatal(err)
	}
	peer.set(nvpAnswers, nil)
	if _, err := collectProbe(t.Context(), runner, probe, m); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the re-arm on recovery", func() bool { return peer.count("SUBSCRIBE INPUT") >= 2 })

	// The component's push lands under the listen task.
	peer.mu.Lock()
	to := peer.lastFrom
	peer.mu.Unlock()
	if _, err := peer.conn.WriteToUDP([]byte("EVT INPUT dp1\r\n"), to); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the push", func() bool {
		for _, s := range c.samplesOf("l1") {
			if s.Name == "video-input" && s.Text == "dp1" {
				return true
			}
		}
		return false
	})
}

func TestAUDPEndpointDropsAndLogsAnotherSourcesDatagramsOncePerTick(t *testing.T) {
	peer := newComponent(t, nvpAnswers)
	c := newCapture()
	m := newDatagramManager(t, c)
	m.reconcile([]collection.TaskSpec{udpListenTask("l1", "ep-1", udpParams(peer.addr()))})
	held := m.datagrams["ep-1"]
	if held == nil {
		t.Fatal("no socket held for the endpoint")
	}
	stranger := newComponent(t, nil)
	for i := 0; i < 2; i++ {
		if _, err := stranger.conn.WriteTo([]byte("EVT INPUT dp1\r\n"), held.sock.LocalAddr()); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "both counted", func() bool { return held.sock.ForeignCount() == 2 })
	var buf bytes.Buffer
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	m.flushUnclaimed()
	m.flushUnclaimed()
	slog.SetDefault(prior)
	if n := strings.Count(buf.String(), "foreign datagrams dropped"); n != 1 || !strings.Contains(buf.String(), "count=2") || !strings.Contains(buf.String(), stranger.addr()) {
		t.Fatalf("log = %q, want one line counting both, naming the source", buf.String())
	}
	if got := c.samplesOf("l1"); len(got) != 0 {
		t.Fatalf("a stranger's datagram published %+v", got)
	}
}

func TestAUDPSocketRebindsOnlyWhenWhatItDependsOnChanges(t *testing.T) {
	peer := newComponent(t, nvpAnswers)
	other := newComponent(t, nvpAnswers)
	m := newDatagramManager(t, newCapture())
	free, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	port := free.LocalAddr().(*net.UDPAddr).Port
	_ = free.Close()
	p := udpParams(peer.addr(), fmt.Sprintf(`,"listen-port":"%d"`, port))
	m.reconcile([]collection.TaskSpec{udpListenTask("l1", "ep-1", p)})
	first := m.datagrams["ep-1"].sock
	if got := first.LocalAddr().(*net.UDPAddr).Port; got != port {
		t.Fatalf("bound %d, want the listen port %d", got, port)
	}
	// A listener re-hash (a new task id) keeps the socket.
	m.reconcile([]collection.TaskSpec{udpListenTask("l9", "ep-1", p)})
	if m.datagrams["ep-1"].sock != first {
		t.Fatal("a re-hash rebound the socket")
	}
	// A new target rebinds.
	m.reconcile([]collection.TaskSpec{udpListenTask("l9", "ep-1", udpParams(other.addr(), fmt.Sprintf(`,"listen-port":"%d"`, port)))})
	if m.datagrams["ep-1"].sock == first {
		t.Fatal("a new target kept the old socket")
	}
	// The endpoint leaves the worklist: its socket closes.
	m.reconcile(nil)
	if len(m.datagrams) != 0 {
		t.Fatalf("held %v after the endpoint left", m.datagrams)
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
