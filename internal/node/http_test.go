package node

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"google.golang.org/protobuf/proto"
)

// The node side of http polls and commands (#902) over fakes: the gate a
// refused credential holds, the rung a credentialed poll sends at, the
// backoff a request's own fault and a Retry-After book, and a command's
// credential read off the worklist rather than the delivery.

type recordingFetcher struct {
	ans  collection.HTTPAnswer
	err  error
	sent []collection.HTTPExchange
}

func (f *recordingFetcher) Exchange(_ context.Context, x collection.HTTPExchange) (collection.HTTPAnswer, error) {
	f.sent = append(f.sent, x)
	a := f.ans
	a.Credentialed = x.Credential != nil
	return a, f.err
}

func httpTasks() []collection.TaskSpec {
	session := &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeBasic}
	return []collection.TaskSpec{
		{ID: "a-probe", Mode: "poll", Transport: "https", EndpointID: "ep-k", EndpointName: "https", Fingerprint: "fp-1",
			EndpointParams: json.RawMessage(`{"target":"10.0.0.7:443","tls-mode":"pin","timeout":"3s"}`), Spec: json.RawMessage(`{}`)},
		{ID: "b-poll", Mode: "poll", Transport: "https", EndpointID: "ep-k", EndpointName: "https", Fingerprint: "fp-1", Session: session,
			EndpointParams: json.RawMessage(`{"target":"10.0.0.7:443","tls-mode":"pin","timeout":"3s"}`),
			Secrets:        map[string]map[string]string{"login": {"username": "admin", "password": "kestrel"}},
			Spec: json.RawMessage(`{"driver":"kestrel-api","function":"status","schedule":{"every":"30s"},"request":{"path":"/api/status"},
				"emits":[{"name":"power-state","lane":"property","extract":{"jsonpath":"device.power"}}]}`)},
	}
}

func TestACredentialedHTTPPollSendsAtTheSessionRung(t *testing.T) {
	tasks := httpTasks()
	if statelessRung(tasks[1]) != rungAuthenticated {
		t.Fatal("a credentialed http poll does not send at the session rung")
	}
	bare := tasks[1]
	bare.Session = nil
	if statelessRung(bare) != rungResponsive {
		t.Fatal("an http poll with no credential sends at the session rung")
	}
	if holdsSession(tasks[1]) {
		t.Fatal("an http poll asks for a held session")
	}
}

func TestAnHTTPCommandTakesItsCredentialFromTheWorklist(t *testing.T) {
	m := newSessionManager(nil, sessionHooks{})
	m.reconcile(httpTasks())
	f := &recordingFetcher{ans: collection.HTTPAnswer{Status: 200, Body: []byte(`{"ok":true,"echo":"kestrel"}`)}}
	runner := &collection.Runner{HTTPFetch: f}
	d := collection.CommandDelivery{ID: 9, CommandType: "set-volume", Transport: "https", Target: "10.0.0.7:443", EndpointID: "ep-k",
		Request: &driver.Request{Method: "POST", Path: "/api/audio/volume", Body: json.RawMessage(`{"volume":40}`)}}
	reply, out := executeDelivery(t.Context(), runner, d, m)
	if out != "" {
		t.Fatalf("actuation failed: %s", out)
	}
	if strings.Contains(reply, "kestrel") {
		t.Fatalf("reply %q carries the credential the component echoed", reply)
	}
	x := f.sent[0]
	if x.Credential == nil || x.Credential.Username != "admin" || x.TLS == nil || x.TLS.Mode != driver.TLSModePin || x.TLS.Pin == nil || x.Timeout != 3*time.Second {
		t.Fatalf("sent %+v", x)
	}

	// A refused credential gates the endpoint at the session rung; the next
	// command fails fast without a request.
	f.ans = collection.HTTPAnswer{Status: 401}
	if _, out := executeDelivery(t.Context(), runner, d, m); !strings.Contains(out, "auth") {
		t.Fatalf("a 401 = %q, want the auth refusal", out)
	}
	if gt, ok := m.blocked("ep-k", rungAuthenticated); !ok || gt.rung != rungAuthenticated || gt.class != driver.ClassAuth {
		t.Fatalf("gate = %+v %v, want auth at the session rung", gt, ok)
	}
	sent := len(f.sent)
	if _, out := executeDelivery(t.Context(), runner, d, m); !strings.Contains(out, "gated") || len(f.sent) != sent {
		t.Fatalf("a command beneath the gate = %q, sent %d more", out, len(f.sent)-sent)
	}

	// An endpoint with no task on this node has no credential to send.
	d.EndpointID = "ep-elsewhere"
	if _, out := executeDelivery(t.Context(), runner, d, m); !strings.Contains(out, "no task") {
		t.Fatalf("a command for an unknown endpoint = %q", out)
	}
}

// A command rides the worklist's address with the worklist's credential:
// one naming an address the worklist does not is never sent.
func TestAnHTTPCommandIsNeverSentWhereTheWorklistDoesNotPoint(t *testing.T) {
	m := newSessionManager(nil, sessionHooks{})
	m.reconcile(httpTasks())
	f := &recordingFetcher{ans: collection.HTTPAnswer{Status: 200, Body: []byte(`{"ok":true}`)}}
	runner := &collection.Runner{HTTPFetch: f}
	d := collection.CommandDelivery{ID: 10, CommandType: "set-volume", Transport: "https", Target: "10.0.0.99:443", EndpointID: "ep-k",
		Request: &driver.Request{Method: "POST", Path: "/api/audio/volume", Body: json.RawMessage(`{"volume":40}`)}}
	if _, out := executeDelivery(t.Context(), runner, d, m); !strings.Contains(out, "moved") || len(f.sent) != 0 {
		t.Fatalf("a delivery to another address = %q, sent %d", out, len(f.sent))
	}
	d.Target = ""
	if _, out := executeDelivery(t.Context(), runner, d, m); out != "" || len(f.sent) != 1 || f.sent[0].Target != "10.0.0.7:443" {
		t.Fatalf("a delivery naming no address = %q, sent %v", out, f.sent)
	}
}

func TestBookPollRunForHTTPAnswers(t *testing.T) {
	now := time.Now()
	s := newPollSchedule(time.Second)
	every := time.Hour
	auth := collection.Sample{Name: collection.SignalEndpointAuthenticated, Text: "yes", IsText: true}

	// A request's own fault (a 404) waits its interval, even with the
	// session rung it proved.
	s.due("p", every, now)
	bookPollRun(s, "p", every, now, []collection.Sample{auth}, []error{&collection.RequestFault{Status: 404, Reason: "x"}})
	if next, _ := s.nextOf("p"); next.Before(now.Add(every - time.Second)) {
		t.Fatalf("a 404 rebooked at %v, want its interval", next.Sub(now))
	}

	// A 503 asking for two minutes is not asked sooner.
	s.due("q", every, now)
	bookPollRun(s, "q", every, now, nil, []error{&collection.RetryableAnswer{Status: 503, Reason: "busy", RetryAfter: 2 * time.Minute}})
	if next, _ := s.nextOf("q"); next.Before(now.Add(2*time.Minute)) || next.After(now.Add(3*time.Minute)) {
		t.Fatalf("a 503 with Retry-After rebooked at %v, want two minutes", next.Sub(now))
	}

	// A plain failure backs off from the heartbeat.
	s.due("r", every, now)
	bookPollRun(s, "r", every, now, nil, []error{errors.New("refused")})
	if next, _ := s.nextOf("r"); next.After(now.Add(2 * time.Second)) {
		t.Fatalf("a failure rebooked at %v, want the heartbeat", next.Sub(now))
	}
}

// A reason carrying a component's own text (an error page in Latin-1) still
// marshals: a protobuf string refuses invalid UTF-8, which would fail the
// whole batch (and, for a collection-failed, the tick).
func TestABatchCarryingAComponentsBytesStillMarshals(t *testing.T) {
	bad := string([]byte{0xe9, 'x', 0xff})
	ev := buildBatch("t1", "n1", []collection.Sample{
		{Name: collection.SignalEndpointAuthenticated, Text: "no", IsText: true, Labels: map[string]string{collection.ReasonLabel: bad}},
		{Name: "volume", Value: 1, Labels: map[string]string{collection.ReasonLabel: bad}},
	})
	if _, err := proto.Marshal(ev); err != nil {
		t.Fatalf("a batch with a component's bytes in a label does not marshal: %v", err)
	}
}

// blockingDialer holds every attempt until released: a stream making its
// first attempt.
type blockingDialer struct{ release chan struct{} }

func (d blockingDialer) wait(ctx context.Context) error {
	select {
	case <-d.release:
	case <-ctx.Done():
	}
	return errors.New("refused")
}

func (d blockingDialer) Dial(ctx context.Context, _ string, _ time.Duration, _ collection.Inbound) (collection.HeldConn, error) {
	return nil, d.wait(ctx)
}

func (d blockingDialer) Connect(ctx context.Context, _ string, _ time.Duration) (collection.PendingConn, error) {
	return nil, d.wait(ctx)
}

func (d blockingDialer) ConnectTLS(ctx context.Context, _ string, _ time.Duration, _ collection.TLSTrust) (collection.PendingConn, error) {
	return nil, d.wait(ctx)
}

// An http endpoint's polls and commands wait while its stream makes its
// first attempt under the fingerprint, so a credential the stream is about
// to have refused is not sent beside it (#1038).
func TestHTTPPollsAndCommandsWaitForTheStreamsFirstVerdict(t *testing.T) {
	d := blockingDialer{release: make(chan struct{})}
	m := newSessionManager(map[string]collection.SessionDialer{"https": d}, sessionHooks{})
	defer m.closeAll()
	tasks := httpTasks()
	session := *tasks[1].Session
	session.Stream = &driver.Request{Path: "/api/events"}
	listen := tasks[1]
	listen.ID, listen.Mode, listen.Session = "c-listen", "listen", &session
	listen.Spec = json.RawMessage(`{"driver":"kestrel-api","function":"volume-change","match":{"event":"change"},"emits":[{"name":"volume","lane":"metric","extract":{"jsonpath":"value"}}]}`)
	tasks[1].Session = &session
	m.reconcile(append(tasks, listen))
	time.Sleep(100 * time.Millisecond)
	if !m.streamPending("ep-k") {
		st, why := m.held["ep-k"].sess.State()
		t.Fatalf("a stream making its first attempt is not pending: %s (%s)", st, why)
	}
	if !commandWaits(collection.CommandDelivery{Transport: "https", EndpointID: "ep-k"}, m) {
		t.Fatal("an http command did not wait for the stream's verdict")
	}
	if commandWaits(collection.CommandDelivery{Transport: "tcp", EndpointID: "ep-k"}, m) {
		t.Fatal("a line command waited on an http stream")
	}
	close(d.release)
}
