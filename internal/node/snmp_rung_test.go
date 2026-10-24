package node

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// snmp's reachability rung (#821) over a fake getter: a standard get
// answered is the transport rung, an authorizationError a terminal auth
// refusal at the session rung, and beneath that gate no get is sent. The
// real wire is run_snmp_test.go.

type snmpFake struct {
	mu        sync.Mutex
	calls     int
	community []string
	oids      [][]string
	res       map[string]string
	err       error
	delay     time.Duration
}

func (f *snmpFake) Get(_ context.Context, _, community string, oids []string, _ time.Duration) (map[string]string, error) {
	f.mu.Lock()
	f.calls++
	f.community = append(f.community, community)
	f.oids = append(f.oids, oids)
	res, err, delay := f.res, f.err, f.delay
	f.mu.Unlock()
	time.Sleep(delay)
	return res, err
}

func (f *snmpFake) set(res map[string]string, err error) {
	f.mu.Lock()
	f.res, f.err = res, err
	f.mu.Unlock()
}

func (f *snmpFake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func snmpProbeTask(fp string) collection.TaskSpec {
	l := driver.ResolveLadder("snmp", nil, nil)
	return collection.TaskSpec{ID: "s1", Mode: "probe", Transport: "snmp", EndpointID: "ep-s", EndpointName: "snmp",
		EndpointParams: json.RawMessage(`{"target":"10.0.0.9:161","timeout":"1s"}`), Ladder: &l, Fingerprint: fp,
		Secrets: map[string]map[string]string{"community": {"community": "lab-ro"}}, Spec: json.RawMessage(`{}`)}
}

func TestTheSNMPRungIsAStandardGetAnswered(t *testing.T) {
	fake := &snmpFake{res: map[string]string{driver.SysUpTimeOID: "8123456"}, delay: time.Millisecond}
	runner := &collection.Runner{SNMP: fake}
	m := newDatagramManager(t, newCapture())
	task := snmpProbeTask("fp-1")
	m.reconcile([]collection.TaskSpec{task})
	dps, err := collectProbe(t.Context(), runner, task, m)
	if err != nil {
		t.Fatal(err)
	}
	got := samplesByName(dps)
	if r := got[collection.SignalEndpointResponsive]; r.Text != "up" || r.Labels[collection.ReasonLabel] != string(collection.Responded) {
		t.Fatalf("responsive = %+v, want up, responded", r)
	}
	if a := got[collection.SignalEndpointAuthenticated]; a.Text != "yes" {
		t.Fatalf("authenticated = %+v, want yes: an answer means the community was accepted", a)
	}
	if rtt, ok := got[collection.SignalSNMPResponseTime]; !ok || rtt.Value <= 0 {
		t.Fatalf("snmp-response-time = %+v, want the get's time", rtt)
	}
	if _, ok := got[collection.SignalTCPOpen]; ok {
		t.Fatal("an snmp endpoint landed tcp-open")
	}
	if up, ok := collection.EndpointVerdict(dps); !ok || !up {
		t.Fatalf("verdict = %v, %v; the get decides an snmp endpoint's verdict", up, ok)
	}
	if fake.community[0] != "lab-ro" || len(fake.oids[0]) != 1 || fake.oids[0][0] != driver.SysUpTimeOID {
		t.Fatalf("the get sent %v with %v, want sysUpTime.0 with the endpoint's community", fake.oids, fake.community)
	}

	// An agent that answers nothing: a timeout, retryable, no timing, and
	// nothing said of the community (a v2c wrong community is the same
	// silence).
	fake.set(nil, &timeoutErr{})
	dps, err = collectProbe(t.Context(), runner, task, m)
	if err != nil {
		t.Fatal(err)
	}
	got = samplesByName(dps)
	if r := got[collection.SignalEndpointResponsive]; r.Text != "down" || r.Labels[collection.ReasonLabel] != string(collection.Timedout) {
		t.Fatalf("silent responsive = %+v, want down, timeout", r)
	}
	if _, ok := got[collection.SignalEndpointAuthenticated]; ok {
		t.Fatal("a silent agent said something of the community")
	}
	if _, ok := got[collection.SignalSNMPResponseTime]; ok {
		t.Fatal("an unanswered get landed a timing")
	}
	if _, gated := m.blocked("ep-s", rungResponsive); gated {
		t.Fatal("a silent agent gated the endpoint; it is retryable")
	}
}

func TestAnSNMPAuthorizationErrorGatesTheSessionRungAndHoldsTheGet(t *testing.T) {
	fake := &snmpFake{err: &collection.TerminalError{Class: driver.ClassAuth, Reason: "the agent answered authorizationError for the community"}}
	runner := &collection.Runner{SNMP: fake}
	m := newDatagramManager(t, newCapture())
	task := snmpProbeTask("fp-1")
	m.reconcile([]collection.TaskSpec{task})
	dps, err := collectProbe(t.Context(), runner, task, m)
	if err != nil {
		t.Fatal(err)
	}
	got := samplesByName(dps)
	if r := got[collection.SignalEndpointResponsive]; r.Text != "up" {
		t.Fatalf("responsive = %+v, want up: the agent answered", r)
	}
	if a := got[collection.SignalEndpointAuthenticated]; a.Text != "no" {
		t.Fatalf("authenticated = %+v, want no", a)
	}
	gt, ok := m.blocked("ep-s", rungAuthenticated)
	if !ok || gt.rung != rungAuthenticated || gt.class != driver.ClassAuth {
		t.Fatalf("gate = %+v, %v; want the session rung gated, class auth", gt, ok)
	}
	if _, ok := m.blocked("ep-s", rungResponsive); ok {
		t.Fatal("an auth refusal gated the transport rung")
	}
	// Beneath the gate no get is sent under the fingerprint, by the probe or
	// a poll, since every v2c request carries the community.
	sent := fake.count()
	dps, err = collectProbe(t.Context(), runner, task, m)
	if err != nil {
		t.Fatal(err)
	}
	if fake.count() != sent {
		t.Fatal("a get went out beneath the auth gate")
	}
	held := samplesByName(dps)
	if a := held[collection.SignalEndpointAuthenticated]; a.Text != "no" {
		t.Fatalf("held authenticated = %+v, want no", a)
	}
	// Beneath the gate the endpoint is not collectable under its
	// fingerprint, and the agent's state is unobserved: the rung reads down,
	// rejected, so the verdict follows rather than freezing at the last
	// answer (review of #821, finding 1).
	if r := held[collection.SignalEndpointResponsive]; r.Text != "down" || r.Labels[collection.ReasonLabel] != "rejected: auth" {
		t.Fatalf("held responsive = %+v, want down, rejected: auth", r)
	}
	if up, ok := collection.EndpointVerdict(dps); !ok || up {
		t.Fatalf("held verdict = %v, %v, want down", up, ok)
	}
	poll := collection.TaskSpec{ID: "p1", Mode: "poll", Transport: "snmp", EndpointID: "ep-s", EndpointName: "snmp", Fingerprint: "fp-1",
		EndpointParams: json.RawMessage(`{"target":"10.0.0.9:161"}`),
		Secrets:        map[string]map[string]string{"community": {"community": "lab-ro"}},
		Spec: json.RawMessage(`{"driver":"snmp-generic","function":"scalars","request":{"get":["1.3.6.1.2.1.1.3.0"]},
			"emits":[{"name":"uptime","lane":"metric","extract":{"oid":"1.3.6.1.2.1.1.3.0"}}]}`)}
	if _, gated := m.blocked(poll.EndpointID, statelessRung(poll)); !gated {
		t.Fatal("an snmp poll is not held beneath the auth gate")
	}
	// The community rotated: the fingerprint moves, the gate lifts, and the
	// next probe gets again and comes up.
	fake.set(map[string]string{driver.SysUpTimeOID: "1"}, nil)
	rotated := snmpProbeTask("fp-2")
	m.reconcile([]collection.TaskSpec{rotated})
	dps, err = collectProbe(t.Context(), runner, rotated, m)
	if err != nil {
		t.Fatal(err)
	}
	if fake.count() != sent+1 || samplesByName(dps)[collection.SignalEndpointAuthenticated].Text != "yes" {
		t.Fatalf("after the rotation: %d gets, samples %v; want one more get, accepted", fake.count()-sent, names(dps))
	}
}

func TestAnSNMPRungWithNoCommunityIsSkippedByName(t *testing.T) {
	task := snmpProbeTask("fp-1")
	task.Secrets = nil
	_, err := collectProbe(t.Context(), &collection.Runner{SNMP: &snmpFake{}}, task, nil)
	if err == nil || !errors.Is(err, errNoCommunity) {
		t.Fatalf("err = %v, want the missing community named", err)
	}
}

// timeoutErr is a network timeout, as gosnmp reports a silent agent.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "request timeout (after 0 retries)" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
