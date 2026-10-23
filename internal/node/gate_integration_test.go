package node

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The gate across the node's connection paths (#1056 ruling): held sessions,
// the ssh probe's credential try (#1060), stateless polls and commands.

type rejectionLog struct {
	mu  sync.Mutex
	got []string
}

func (l *rejectionLog) record(_, _ string, value string) {
	l.mu.Lock()
	l.got = append(l.got, value)
	l.mu.Unlock()
}

func (l *rejectionLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.got...)
}

func withFingerprint(task collection.TaskSpec, fp, rejection string) collection.TaskSpec {
	task.Fingerprint, task.Rejection = fp, rejection
	return task
}

// A rejection is published under its fingerprint, and a fingerprint that
// changes and then passes publishes none.
func TestSessionManagerPublishesItsRejection(t *testing.T) {
	dialer := &authFakeDialer{}
	log, rej := &transitionLog{}, &rejectionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record, rejection: rej.record})
	defer m.closeAll()
	m.reconcile([]collection.TaskSpec{withFingerprint(pollTaskWithLogin("p1", "ep-1", "wrong", 0), "fp-1", "")})
	log.waitFor(t, "p1", collection.SessionRejected)
	waitFor(t, func() bool { got := rej.all(); return len(got) > 0 && got[len(got)-1] == "authenticated auth fp-1" }, "the rejection published under fp-1")

	m.reconcile([]collection.TaskSpec{withFingerprint(pollTaskWithLogin("p1", "ep-1", "newtron", 0), "fp-2", "authenticated auth fp-1")})
	log.waitFor(t, "p1", collection.SessionUp)
	waitFor(t, func() bool { got := rej.all(); return len(got) > 0 && got[len(got)-1] == rejectionNone }, "none published once the new fingerprint passed")
}

// A restarted node honours the rejection the worklist carries back: zero
// attempts while the fingerprint is unchanged, one as soon as it moves.
func TestARestartedNodeMakesNoAttemptAgainstAnUnchangedFingerprint(t *testing.T) {
	dialer := &authFakeDialer{}
	log := &transitionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record})
	defer m.closeAll()
	task := withFingerprint(pollTaskWithLogin("p1", "ep-1", "wrong", 0), "fp-1", "authenticated auth fp-1")
	m.reconcile([]collection.TaskSpec{task})
	log.waitFor(t, "p1", collection.SessionRejected)
	for i := 0; i < 5; i++ {
		m.reconcile([]collection.TaskSpec{task})
	}
	time.Sleep(30 * time.Millisecond)
	if n := dialer.connectCount(); n != 0 {
		t.Fatalf("a restarted node made %d attempts against an unchanged fingerprint", n)
	}
	moved := withFingerprint(pollTaskWithLogin("p1", "ep-1", "wrong", 1), "fp-retried", "authenticated auth fp-1")
	m.reconcile([]collection.TaskSpec{moved})
	waitFor(t, func() bool { return dialer.connectCount() == 1 }, "one attempt once the fingerprint moved")
}

// recordingSSHProber records the credential each probe carried and refuses
// any password but "right".
type recordingSSHProber struct {
	mu    sync.Mutex
	creds []collection.SSHCredential
}

func (p *recordingSSHProber) Probe(_ context.Context, _ string, cred collection.SSHCredential, _ time.Duration) (collection.SSHResult, error) {
	p.mu.Lock()
	p.creds = append(p.creds, cred)
	p.mu.Unlock()
	res := collection.SSHResult{Dialed: true, Responded: true, Reason: collection.Responded}
	if cred.Password != "" {
		res.AuthAttempted = true
		res.Authenticated = cred.Password == "right"
	}
	return res, nil
}

// #1060: a credential the ssh probe had refused is not tried again while the
// fingerprint is unchanged; the key exchange still runs, so the responds
// rung stays observed. A changed fingerprint (a fixed credential) tries again.
func TestSSHProbeCredentialIsGated(t *testing.T) {
	prober := &recordingSSHProber{}
	runner := &collection.Runner{SSH: prober}
	rej := &rejectionLog{}
	m := newSessionManager(nil, sessionHooks{rejection: rej.record})
	probe := func(password, fp string) collection.TaskSpec {
		return collection.TaskSpec{ID: "r1", Mode: "poll", Transport: "ssh", EndpointID: "ep-1", EndpointName: "ssh",
			EndpointParams: json.RawMessage(`{"target":"10.0.0.5:22","username":"probe","password":"` + password + `"}`),
			Spec:           json.RawMessage(`{}`), Fingerprint: fp}
	}
	for i := 0; i < 3; i++ {
		m.reconcile([]collection.TaskSpec{probe("wrong", "fp-1")})
		if _, err := collectProbe(t.Context(), runner, probe("wrong", "fp-1"), m); err != nil {
			t.Fatal(err)
		}
	}
	prober.mu.Lock()
	creds := append([]collection.SSHCredential(nil), prober.creds...)
	prober.mu.Unlock()
	if len(creds) != 3 || creds[0].Password != "wrong" || creds[1].Password != "" || creds[2].Password != "" {
		t.Fatalf("probe credentials = %+v, want the refused one tried once", creds)
	}
	if got := rej.all(); len(got) == 0 || !strings.HasPrefix(got[0], "authenticated auth ") {
		t.Fatalf("published rejections = %v", got)
	}
	m.reconcile([]collection.TaskSpec{probe("right", "fp-2")})
	if _, err := collectProbe(t.Context(), runner, probe("right", "fp-2"), m); err != nil {
		t.Fatal(err)
	}
	prober.mu.Lock()
	last := prober.creds[len(prober.creds)-1]
	prober.mu.Unlock()
	if last.Password != "right" {
		t.Fatalf("a changed fingerprint did not try the credential again: %+v", last)
	}
}

// A command on a gated endpoint fails fast with the refusal, whatever path
// it would ride.
func TestExecuteDeliveryFailsFastOnAGatedEndpoint(t *testing.T) {
	m := newSessionManager(nil, sessionHooks{})
	m.reconcile([]collection.TaskSpec{withFingerprint(collection.TaskSpec{ID: "r1", Transport: "tcp", EndpointID: "ep-1", EndpointName: "tcp",
		EndpointParams: json.RawMessage(`{"target":"10.0.0.5:23"}`), Spec: json.RawMessage(`{}`)}, "fp-1", "responsive host-key fp-1")})
	throwaway := &lineRecorder{}
	_, out := executeDelivery(t.Context(), &collection.Runner{Line: throwaway}, collection.CommandDelivery{
		ID: 1, Transport: "tcp", Target: "10.0.0.5:23", EndpointID: "ep-1", Line: "SET INPUT hdmi2"}, m)
	if !strings.Contains(out, "host-key") || len(throwaway.sent) != 0 {
		t.Fatalf("outcome %q, sent %v: want the gate's refusal and no dial", out, throwaway.sent)
	}
}

func waitFor(t *testing.T, ok func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// The ladder's request rung (ADR-0157): where an open port cannot be
// detected, the driver's named request answered is the transport proven.
// The line client carries it here; snmp's own request is #821.
func TestRequestRungIsTheTransportProven(t *testing.T) {
	task := collection.TaskSpec{ID: "r1", Transport: "tcp", EndpointID: "ep-1", EndpointName: "tcp",
		EndpointParams: json.RawMessage(`{"target":"10.0.0.5:51325"}`), Spec: json.RawMessage(`{}`),
		Ladder: &driver.EffectiveLadder{Transport: driver.RungRequest,
			Request: &driver.Request{Line: "PING", Reply: &driver.Reply{Prefix: "OK"}}}}
	line := &lineRecorder{}
	dps, err := collectProbe(t.Context(), &collection.Runner{Line: line}, task, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]collection.Sample{}
	for _, d := range dps {
		got[d.Name] = d
	}
	if len(line.sent) != 1 || line.sent[0] != "PING" || got["endpoint-responsive"].Text != "up" || got["tcp-open"].Value != 1 {
		t.Fatalf("sent %v, samples %+v: want the named request answered as the transport rung", line.sent, got)
	}
	// An answer that is not the declared reply does not prove it.
	task.Ladder.Request.Reply = &driver.Reply{Prefix: "PONG"}
	dps, _ = collectProbe(t.Context(), &collection.Runner{Line: &lineRecorder{}}, task, nil)
	for _, d := range dps {
		if d.Name == "endpoint-responsive" && d.Text != "down" {
			t.Fatalf("an unexpected answer proved the rung: %+v", d)
		}
	}
}

// Review of the fingerprint slice, finding 1: the gate is republished until
// the worklist reflects it (a lost publish must not leave a restarted node
// without it), under a task from the current worklist (a re-derived driver
// deletes the old task ids, and a sample under one is an orphan).
func TestTheRejectionIsRepublishedUntilReflectedUnderACurrentTask(t *testing.T) {
	rej := &rejectionLog{}
	var mu sync.Mutex
	var under []string
	m := newSessionManager(nil, sessionHooks{rejection: func(taskID, _ string, v string) {
		mu.Lock()
		under = append(under, taskID)
		mu.Unlock()
		rej.record("", "", v)
	}})
	task := func(id, rejection string) collection.TaskSpec {
		return withFingerprint(collection.TaskSpec{ID: id, Transport: "ssh", EndpointID: "ep-1", EndpointName: "ssh",
			EndpointParams: json.RawMessage(`{"target":"10.0.0.5:22"}`), Spec: json.RawMessage(`{}`)}, "fp-1", rejection)
	}
	m.reconcile([]collection.TaskSpec{task("a-old", "")})
	m.refuse("ep-1", "fp-1", &collection.TerminalError{Class: "auth", Reason: "refused"})
	// The publish was lost: the server still reflects nothing. Each tick
	// publishes it again, now under the current task.
	m.reconcile([]collection.TaskSpec{task("b-new", "")})
	m.reconcile([]collection.TaskSpec{task("b-new", "")})
	mu.Lock()
	got := append([]string(nil), under...)
	mu.Unlock()
	if len(got) < 3 || got[len(got)-1] != "b-new" {
		t.Fatalf("published under %v, want a republish per tick under the current task", got)
	}
	// Reflected: no more publishes.
	n := len(got)
	m.reconcile([]collection.TaskSpec{task("b-new", "authenticated auth fp-1")})
	mu.Lock()
	defer mu.Unlock()
	if len(under) != n {
		t.Fatalf("published again after the worklist reflected the gate: %v", under)
	}
}

// Finding 3: a refusal reported for an attempt made under an old
// fingerprint never gates the new one.
func TestARefusalUnderAnOldFingerprintIsDropped(t *testing.T) {
	m := newSessionManager(nil, sessionHooks{})
	task := withFingerprint(collection.TaskSpec{ID: "r1", Transport: "telnet", EndpointID: "ep-1", EndpointName: "telnet",
		EndpointParams: json.RawMessage(`{"target":"10.0.0.5:23"}`), Spec: json.RawMessage(`{}`)}, "fp-new", "")
	m.reconcile([]collection.TaskSpec{task})
	m.refuse("ep-1", "fp-old", &collection.TerminalError{Class: "auth", Reason: "late"})
	if _, ok := m.blocked("ep-1", rungAuthenticated); ok {
		t.Fatal("a refusal from the old fingerprint's attempt gated the new fingerprint")
	}
}
