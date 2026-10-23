package node

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The node runs a session's auth step and honours rejected (#1049): the
// driver's session block arrives on the worklist, the login renders against
// the delivered secrets, the auth rung and the reason publish, a rejected
// session dials no more until its credential changes or its endpoint's
// revision moves (an operator's retry), and nothing falls back to a
// throwaway connection that would retry the bad credential.

// authFakeDialer is a staged dialer whose auth step accepts exactly the
// password "newtron", answering any other with the device's refusal.
type authFakeDialer struct {
	heldFakeDialer
	amu      sync.Mutex
	connects int
	sends    [][]string
}

func (d *authFakeDialer) Connect(_ context.Context, target string, _ time.Duration) (collection.PendingConn, error) {
	d.amu.Lock()
	d.connects++
	d.amu.Unlock()
	return &authFakePending{d: d, target: target}, nil
}

func (d *authFakeDialer) connectCount() int {
	d.amu.Lock()
	defer d.amu.Unlock()
	return d.connects
}

type authFakePending struct {
	d      *authFakeDialer
	target string
}

func (p *authFakePending) Authenticate(_ context.Context, auth *collection.SessionAuth, terminal collection.Classifier, _ time.Duration) error {
	if auth == nil || auth.Login == nil {
		return errors.New("no login declared")
	}
	var sent []string
	for _, st := range auth.Login.Steps {
		sent = append(sent, st.Send)
	}
	p.d.amu.Lock()
	p.d.sends = append(p.d.sends, sent)
	p.d.amu.Unlock()
	if len(sent) == 2 && sent[0] == "admin" && sent[1] == "newtron" {
		return nil
	}
	if class, ok := terminal("auth rejected"); ok {
		return &collection.TerminalError{Class: class, Reason: "auth rejected"}
	}
	return errors.New("auth rejected, unclassified")
}

func (p *authFakePending) Open(in collection.Inbound, _ collection.ChannelOptions) (collection.HeldConn, error) {
	return p.d.heldFakeDialer.Dial(context.Background(), p.target, 0, in)
}

func (p *authFakePending) Close() error { return nil }

// loginSession is the session block the worklist delivers for a driver that
// declares a login.
func loginSession() *driver.SessionSpec {
	return &driver.SessionSpec{
		Login: &driver.Login{
			Steps: []driver.LoginStep{
				{Expect: driver.Match{Regex: `login: ?$`}, Send: "${secret.login.username}"},
				{Expect: driver.Match{Regex: `Password: ?$`}, Send: "${secret.login.password}"},
			},
			Ready: driver.Match{Prefix: "READY"},
		},
		Terminal: []driver.TerminalRule{{Class: driver.ClassAuth, Match: driver.Match{Prefix: "auth rejected"}}},
	}
}

func pollTaskWithLogin(id, endpointID, password string, revision int64) collection.TaskSpec {
	return collection.TaskSpec{
		ID: id, Mode: "poll", Transport: "tcp", EndpointID: endpointID, EndpointName: "tcp",
		EndpointParams: json.RawMessage(`{"target":"10.0.0.5:23"}`),
		Spec: json.RawMessage(`{"driver":"newtron-nvp","function":"status","schedule":{"every":"30s"},
			"request":{"line":"GET INPUT"},
			"emits":[{"name":"video-input","lane":"property","extract":{"regex":"^INPUT (\\S+)$"}}]}`),
		Secrets:          map[string]map[string]string{"login": {"username": "admin", "password": password}},
		Session:          loginSession(),
		EndpointRevision: revision,
	}
}

type authLog struct {
	mu  sync.Mutex
	got []collection.AuthVerdict
}

func (l *authLog) record(_, _ string, v collection.AuthVerdict) {
	l.mu.Lock()
	l.got = append(l.got, v)
	l.mu.Unlock()
}

func (l *authLog) all() []collection.AuthVerdict {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]collection.AuthVerdict(nil), l.got...)
}

// A driver that declares a session block holds its session for a poll, with
// no listener: its login cannot run on a throwaway connection.
func TestDesiredSessionsHoldsForAPollWhoseDriverDeclaresASession(t *testing.T) {
	want, refused := desiredSessions([]collection.TaskSpec{pollTaskWithLogin("p1", "ep-1", "newtron", 0)},
		map[string]collection.SessionDialer{"tcp": &authFakeDialer{}})
	if len(refused) != 0 {
		t.Fatalf("refused = %v", refused)
	}
	w, ok := want["ep-1"]
	if !ok || w.taskID != "p1" {
		t.Fatalf("desired = %+v, want a session for ep-1 publishing under its poll", want)
	}
	if w.auth == nil || w.auth.Login == nil || w.auth.Login.Steps[1].Send != "newtron" {
		t.Fatalf("the session's login is not rendered from the delivered secret: %+v", w.auth)
	}
}

// A login whose credential was not delivered refuses the session by
// reference, never by value.
func TestDesiredSessionsRefusesALoginWithNoCredential(t *testing.T) {
	task := pollTaskWithLogin("p1", "ep-1", "newtron", 0)
	task.Secrets = map[string]map[string]string{"login": {"username": "admin"}}
	_, refused := desiredSessions([]collection.TaskSpec{task}, map[string]collection.SessionDialer{"tcp": &authFakeDialer{}})
	reason := refused["ep-1"]
	if !strings.Contains(reason, "login.password") {
		t.Fatalf("refused = %v, want the missing reference named", refused)
	}
	if strings.Contains(reason, "admin") {
		t.Fatalf("the refusal leaks a credential: %s", reason)
	}
}

// A wrong credential: one attempt, rejected with the device's text and the
// auth rung no; ticks with the same worklist dial nothing; a revision move
// (an operator's retry) dials exactly once more; the fixed credential
// reconnects at the next reconcile.
func TestSessionManagerRejectsThenRetriesThenRecovers(t *testing.T) {
	dialer := &authFakeDialer{}
	log, auth := &transitionLog{}, &authLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record, auth: auth.record})
	defer m.closeAll()

	wrong := []collection.TaskSpec{pollTaskWithLogin("p1", "ep-1", "wrong", 1)}
	m.reconcile(wrong)
	log.waitFor(t, "p1", collection.SessionRejected)
	if v := auth.all(); len(v) != 1 || v[0].Authenticated || v[0].Reason != "auth: auth rejected" {
		t.Fatalf("auth verdicts = %+v, want one refusal", v)
	}
	for i := 0; i < 5; i++ {
		m.reconcile(wrong)
	}
	time.Sleep(20 * time.Millisecond)
	if n := dialer.connectCount(); n != 1 {
		t.Fatalf("connected %d times across five ticks of a rejected session, want 1", n)
	}

	// The operator's retry advances the endpoint's revision: one more attempt.
	m.reconcile([]collection.TaskSpec{pollTaskWithLogin("p1", "ep-1", "wrong", 2)})
	deadline := time.Now().Add(3 * time.Second)
	for dialer.connectCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	m.reconcile([]collection.TaskSpec{pollTaskWithLogin("p1", "ep-1", "wrong", 2)})
	time.Sleep(20 * time.Millisecond)
	if n := dialer.connectCount(); n != 2 {
		t.Fatalf("connected %d times after one retry, want 2", n)
	}

	// The fixed credential recycles the session and reads up.
	m.reconcile([]collection.TaskSpec{pollTaskWithLogin("p1", "ep-1", "newtron", 2)})
	log.waitFor(t, "p1", collection.SessionUp)
	v := auth.all()
	if !v[len(v)-1].Authenticated {
		t.Fatalf("auth verdicts = %+v, want the fix to read authenticated", v)
	}
}

// With a session block the poll never falls back to a throwaway connection:
// with none held it faults; over a rejected one it fails fast, terminal.
func TestDriverPollNeverFallsBackWhenItsDriverDeclaresASession(t *testing.T) {
	throwaway := &lineRecorder{}
	runner := &collection.Runner{Line: throwaway}
	task := pollTaskWithLogin("p1", "ep-1", "wrong", 0)
	_, faults := collectDriverTask(t.Context(), runner, task, nil)
	if len(faults) != 1 || !strings.Contains(faults[0].Error(), "no session") {
		t.Fatalf("faults = %v, want the missing session named", faults)
	}
	if len(throwaway.sent) != 0 {
		t.Fatalf("a login driver's poll dialed a throwaway connection: %v", throwaway.sent)
	}

	dialer := &authFakeDialer{}
	log := &transitionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record})
	defer m.closeAll()
	m.reconcile([]collection.TaskSpec{task})
	log.waitFor(t, "p1", collection.SessionRejected)
	_, faults = collectDriverTask(t.Context(), runner, task, m)
	if len(faults) != 1 {
		t.Fatalf("faults = %v", faults)
	}
	if _, ok := collection.AsTerminal(faults[0]); !ok {
		t.Fatalf("a poll over a rejected session faulted %v, want the terminal refusal", faults[0])
	}
	if len(throwaway.sent) != 0 {
		t.Fatalf("a rejected session's poll dialed a throwaway connection: %v", throwaway.sent)
	}
}

// A terminal fault waits the poll's interval, never the failure backoff.
func TestTerminalFaultBooksTheFullInterval(t *testing.T) {
	sched := newPollSchedule(5 * time.Second)
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	every := time.Hour
	if !sched.due("p1", every, now) {
		t.Fatal("a new poll was not due")
	}
	terminal := &collection.TerminalError{Class: driver.ClassAuth, Reason: "auth rejected"}
	bookPollRun(sched, "p1", every, now, nil, []error{terminal})
	if next, _ := sched.nextOf("p1"); !next.Equal(now.Add(every)) {
		t.Fatalf("next run = %s after a terminal fault, want the full interval", next.Sub(now))
	}
	if !sched.due("p2", every, now) {
		t.Fatal("a new poll was not due")
	}
	bookPollRun(sched, "p2", every, now, nil, []error{errors.New("connection refused")})
	if next, _ := sched.nextOf("p2"); !next.Equal(now.Add(5 * time.Second)) {
		t.Fatalf("next run = %s after a transient fault, want the heartbeat backoff", next.Sub(now))
	}
}

// A poll its session refused waits its interval while the session stays
// rejected, but runs at the first tick the session is up again, so a fixed
// credential lands data within a tick, not an interval later.
func TestARefusedPollRunsOnceItsSessionIsUpAgain(t *testing.T) {
	sched := newPollSchedule(5 * time.Second)
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	every := time.Hour
	sched.due("p1", every, now)
	bookPollRun(sched, "p1", every, now, nil, []error{&collection.TerminalError{Class: driver.ClassAuth, Reason: "auth rejected"}})
	later := now.Add(time.Minute)
	sched.releaseRefused("p1", false)
	if sched.due("p1", every, later) {
		t.Fatal("a refused poll ran while its session was still rejected")
	}
	sched.releaseRefused("p1", true)
	if !sched.due("p1", every, later) {
		t.Fatal("a refused poll did not run once its session was up")
	}
	sched.releaseRefused("p1", true)
	if sched.due("p1", every, later.Add(time.Second)) {
		t.Fatal("the release ran the poll twice")
	}
}

// A command on an endpoint whose driver declares a session rides it or
// fails: never a throwaway connection.
func TestExecuteDeliveryNeverFallsBackForASessionOnlyEndpoint(t *testing.T) {
	throwaway := &lineRecorder{}
	d := collection.CommandDelivery{ID: 11, CommandType: "set-input", Transport: "tcp", Target: "10.0.0.5:23",
		EndpointID: "ep-1", Line: "SET INPUT hdmi2", SessionOnly: true}
	_, out := executeDelivery(t.Context(), &collection.Runner{Line: throwaway}, d, nil)
	if !strings.Contains(out, "no session") {
		t.Fatalf("outcome = %q, want the missing session named", out)
	}
	if len(throwaway.sent) != 0 {
		t.Fatalf("a session-only command dialed a throwaway connection: %v", throwaway.sent)
	}
}

// A transition renders as session-state and session-reason: the reason is a
// property an event rule can read, not only an unpersisted label.
func TestSessionSamplesCarryTheReason(t *testing.T) {
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	dps := sessionSamples(collection.SessionTransition{State: collection.SessionRejected, Reason: "auth: auth rejected", At: at})
	ev := buildBatch("p1", "edge-1", dps)
	byName := map[string]string{}
	for _, p := range ev.Properties {
		byName[p.Name] = p.ValueJson
	}
	if byName["session-state"] != `"rejected"` || byName["session-reason"] != `"auth: auth rejected"` {
		t.Fatalf("properties = %v", byName)
	}
}

// The auth verdict renders as the endpoint-authenticated rung.
func TestAuthSample(t *testing.T) {
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	ev := buildBatch("p1", "edge-1", []collection.Sample{authSample(collection.AuthVerdict{Authenticated: false, Reason: "auth: auth rejected", At: at})})
	if len(ev.Properties) != 1 || ev.Properties[0].Name != "endpoint-authenticated" || ev.Properties[0].ValueJson != `"no"` {
		t.Fatalf("properties = %+v", ev.Properties)
	}
}
