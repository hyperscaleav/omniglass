package heartbeat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/notify"
	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// fakeStore is the gateway slice the sender reads, over a test receiver's
// address; the receiver is real, the sender and the egress client under test
// are the shipped ones.
type fakeStore struct {
	mu       sync.Mutex
	hb       settings.HeartbeatSettings
	target   *storage.TargetSend
	policy   egress.Policy
	open     int
	audits   []auditRow
	targetEr error
}

type auditRow struct {
	target string
	ok     bool
	status int
	reason string
}

func (f *fakeStore) HeartbeatSettings(context.Context) (settings.HeartbeatSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hb, nil
}
func (f *fakeStore) HeartbeatTarget(context.Context, string) (*storage.TargetSend, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.target, f.targetEr
}
func (f *fakeStore) EgressPolicy(context.Context) (egress.Policy, error) { return f.policy, nil }
func (f *fakeStore) PlatformAlarmCount(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.open, nil
}
func (f *fakeStore) AuditHeartbeat(_ context.Context, targetID string, ok bool, status int, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audits = append(f.audits, auditRow{targetID, ok, status, reason})
	return nil
}

// receiver records each request it gets, verbatim.
type receiver struct {
	srv  *httptest.Server
	mu   sync.Mutex
	got  []received
	code int
}

type received struct {
	header http.Header
	body   []byte
	at     time.Time
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{code: http.StatusOK}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.got = append(r.got, received{req.Header.Clone(), b, time.Now()})
		code := r.code
		r.mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func (r *receiver) requests() []received {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]received(nil), r.got...)
}

func storeFor(t *testing.T, r *receiver, interval int) *fakeStore {
	t.Helper()
	pol, err := egress.ParsePolicy([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	return &fakeStore{
		hb:     settings.HeartbeatSettings{Target: "switch", IntervalSeconds: interval},
		target: &storage.TargetSend{TargetID: "t1", Name: "switch", URL: r.srv.URL, Kind: storage.ChannelWebhook, Enabled: true, HMACKey: "shared-key"},
		policy: pol,
	}
}

func TestEnvelopeCarriesTheServerBuildAndOpenAlarms(t *testing.T) {
	t.Parallel()
	env := Envelope("id-1", "omniglass-a", "v1.2.3", 4)
	if env.Type != "heartbeat" || env.Transition != "heartbeat" || env.ID != "id-1" {
		t.Errorf("envelope = %+v, want a heartbeat of id id-1", env)
	}
	want := map[string]string{"server": "omniglass-a", "build": "v1.2.3", "open_platform_alarms": "4"}
	for k, v := range want {
		if env.Facts[k] != v {
			t.Errorf("fact %s = %q, want %q", k, env.Facts[k], v)
		}
	}
	if env.Subject == "" || env.Text == "" {
		t.Errorf("envelope has no subject or text: %+v", env)
	}
}

func TestBeatSendsASignedRequestThroughTheTarget(t *testing.T) {
	t.Parallel()
	r := newReceiver(t)
	st := storeFor(t, r, 60)
	st.open = 2
	s := New(st, egress.Options{}, "omniglass-a", "v1.2.3")
	if err := s.Beat(context.Background()); err != nil {
		t.Fatalf("beat: %v", err)
	}
	reqs := r.requests()
	if len(reqs) != 1 {
		t.Fatalf("receiver got %d requests, want 1", len(reqs))
	}
	got := reqs[0]
	ts := got.header.Get(notify.HeaderTimestamp)
	secs, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		t.Fatalf("timestamp %q: %v", ts, err)
	}
	if want := notify.Sign("shared-key", secs, got.body); got.header.Get(notify.HeaderSignature) != want {
		t.Errorf("signature = %q, want %q (over the body received)", got.header.Get(notify.HeaderSignature), want)
	}
	var env notify.Envelope
	if err := json.Unmarshal(got.body, &env); err != nil {
		t.Fatal(err)
	}
	if env.Transition != "heartbeat" || env.Facts["server"] != "omniglass-a" || env.Facts["build"] != "v1.2.3" || env.Facts["open_platform_alarms"] != "2" {
		t.Errorf("body = %+v, want a heartbeat of omniglass-a v1.2.3 with 2 open alarms", env)
	}
	if got.header.Get(notify.HeaderIdempotency) != env.ID || env.ID == "" {
		t.Errorf("idempotency key %q does not match the envelope id %q", got.header.Get(notify.HeaderIdempotency), env.ID)
	}
}

func TestBeatIsOffWithoutATarget(t *testing.T) {
	t.Parallel()
	r := newReceiver(t)
	st := storeFor(t, r, 60)
	st.hb.Target = ""
	s := New(st, egress.Options{}, "omniglass-a", "v1")
	if err := s.Beat(context.Background()); err != nil || r.count() != 0 {
		t.Errorf("beat with no target = %v, %d requests; want none", err, r.count())
	}
}

func TestBeatRefusesADisabledTargetAndAMissingCredential(t *testing.T) {
	t.Parallel()
	r := newReceiver(t)
	st := storeFor(t, r, 60)
	st.target.Enabled = false
	s := New(st, egress.Options{}, "omniglass-a", "v1")
	if err := s.Beat(context.Background()); err == nil || r.count() != 0 {
		t.Errorf("beat to a disabled target = %v, %d requests; want an error and none sent", err, r.count())
	}
	st.target.Enabled, st.target.CredentialMissing = true, true
	if err := s.Beat(context.Background()); err == nil || r.count() != 0 {
		t.Errorf("beat with a credential that does not unseal = %v, %d requests; want an error and none sent", err, r.count())
	}
	// Each refusal is audited against the target, the two apart by reason.
	if len(st.audits) != 2 || st.audits[0].ok || st.audits[1].ok || st.audits[0].target != "t1" ||
		st.audits[1].target != "t1" || st.audits[0].reason == "" || st.audits[0].reason == st.audits[1].reason {
		t.Errorf("audit rows = %+v, want one refusal for the disabled target and one for the credential", st.audits)
	}
}

// A webhook target with no secret bound would sign with an empty key: the
// heartbeat refuses it rather than send unsigned, and audits the refusal.
func TestBeatRefusesATargetWithNoSigningKey(t *testing.T) {
	t.Parallel()
	r := newReceiver(t)
	st := storeFor(t, r, 60)
	st.target.HMACKey = ""
	s := New(st, egress.Options{}, "omniglass-a", "v1")
	if err := s.Beat(context.Background()); err == nil || r.count() != 0 {
		t.Errorf("beat to a target with no hmac key = %v, %d requests; want an error and none sent", err, r.count())
	}
	if len(st.audits) != 1 || st.audits[0].ok || st.audits[0].target != "t1" || st.audits[0].reason == "" {
		t.Errorf("audit rows = %+v, want one refusal against the target", st.audits)
	}
}

// Pointing the heartbeat at another target is a change of outcome even when
// both succeed, so the move is audited.
func TestBeatAuditsAChangeOfTarget(t *testing.T) {
	t.Parallel()
	r := newReceiver(t)
	st := storeFor(t, r, 60)
	s := New(st, egress.Options{}, "omniglass-a", "v1")
	if err := s.Beat(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Beat(context.Background()); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.target = &storage.TargetSend{TargetID: "t2", Name: "other", URL: r.srv.URL, Kind: storage.ChannelWebhook, Enabled: true, HMACKey: "other-key"}
	st.mu.Unlock()
	if err := s.Beat(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(st.audits) != 2 || st.audits[0].target != "t1" || st.audits[1].target != "t2" || !st.audits[1].ok {
		t.Errorf("audit rows = %+v, want one success on t1 and one on t2", st.audits)
	}
}

func TestBeatHonorsTheEgressAllowList(t *testing.T) {
	t.Parallel()
	r := newReceiver(t)
	st := storeFor(t, r, 60)
	st.policy = egress.Policy{} // loopback is refused unless named
	s := New(st, egress.Options{}, "omniglass-a", "v1")
	if err := s.Beat(context.Background()); err == nil || r.count() != 0 {
		t.Errorf("beat to a destination the allow list does not name = %v, %d requests; want refused", err, r.count())
	}
}

func TestRunBeatsEveryIntervalAndStopsWithItsContext(t *testing.T) {
	t.Parallel()
	r := newReceiver(t)
	st := storeFor(t, r, 1)
	s := New(st, egress.Options{}, "omniglass-a", "v1")
	s.unit = 50 * time.Millisecond // an "interval second" is 50ms, so the test runs in real time
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	deadline := time.After(5 * time.Second)
	for r.count() < 3 {
		select {
		case <-deadline:
			t.Fatalf("receiver got %d heartbeats in 5s, want at least 3", r.count())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the sender did not stop when its context ended")
	}
	after := r.count()
	time.Sleep(300 * time.Millisecond)
	if n := r.count(); n != after {
		t.Errorf("receiver got %d heartbeats after the sender stopped, want none", n-after)
	}
	reqs := r.requests()
	for i := 1; i < len(reqs); i++ {
		if gap := reqs[i].at.Sub(reqs[i-1].at); gap < 25*time.Millisecond {
			t.Errorf("heartbeats %d and %d were %v apart, want about the 50ms interval", i-1, i, gap)
		}
	}
}

func TestRunAuditsAChangeOfOutcomeNotEveryBeat(t *testing.T) {
	t.Parallel()
	r := newReceiver(t)
	st := storeFor(t, r, 1)
	s := New(st, egress.Options{}, "omniglass-a", "v1")
	s.unit = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	for r.count() < 4 {
		time.Sleep(5 * time.Millisecond)
	}
	r.mu.Lock()
	r.code = http.StatusBadGateway
	r.mu.Unlock()
	n := r.count()
	for r.count() < n+4 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.audits) != 2 || !st.audits[0].ok || st.audits[1].ok || st.audits[1].status != 502 {
		t.Errorf("audit rows = %+v, want one for the first success and one for the move to a 502", st.audits)
	}
}
