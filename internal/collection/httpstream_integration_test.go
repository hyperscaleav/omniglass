package collection_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A server-sent events stream held as a session over real sockets (#1038):
// the stream opens through its auth step, its events reach the listeners
// in order, a drop resumes from the last event id after the server's retry
// with nothing lost or landed twice, and a refused credential rejects the
// session after one attempt.

// sseComponent serves a stream of numbered events: a replay from the
// Last-Event-ID a reconnect sends (including that event itself, as some
// servers do, so the client's dedup is exercised on every resume), then
// every event pushed while it is connected.
type sseComponent struct {
	mu       sync.Mutex
	events   []string // event i+1's "event: ...\ndata: ..." lines
	subs     map[chan int]chan struct{}
	retryMS  int
	gate     func(http.ResponseWriter, *http.Request) bool
	other    http.Handler // a login's paths
	connects atomic.Int32
	resumed  []string // the Last-Event-ID each connection sent
}

func newSSEComponent(t *testing.T, retryMS int) (*sseComponent, string) {
	t.Helper()
	c := &sseComponent{subs: map[chan int]chan struct{}{}, retryMS: retryMS}
	srv := httptest.NewServer(c)
	t.Cleanup(func() { c.drop(); srv.Close() })
	return c, srv.Listener.Addr().String()
}

func (c *sseComponent) push(event, data string) {
	c.mu.Lock()
	c.events = append(c.events, fmt.Sprintf("event: %s\ndata: %s\n", event, data))
	id := len(c.events)
	for ch := range c.subs {
		select {
		case ch <- id:
		default:
		}
	}
	c.mu.Unlock()
}

// drop ends every connected stream, as a component restarting would.
func (c *sseComponent) drop() {
	c.mu.Lock()
	for ch, stop := range c.subs {
		close(stop)
		delete(c.subs, ch)
	}
	c.mu.Unlock()
}

func (c *sseComponent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/events" {
		if c.other != nil {
			c.other.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}
	if c.gate != nil && !c.gate(w, r) {
		return
	}
	c.connects.Add(1)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fl := w.(http.Flusher)
	fmt.Fprintf(w, ": hello\nretry: %d\n\n", c.retryMS)
	ch, stop := make(chan int, 64), make(chan struct{})
	c.mu.Lock()
	last := r.Header.Get("Last-Event-ID")
	c.resumed = append(c.resumed, last)
	from := len(c.events) + 1
	if n, err := strconv.Atoi(last); err == nil {
		from = n // an overlap of one: the last event comes again
	}
	replay := []int{}
	for id := from; id <= len(c.events); id++ {
		replay = append(replay, id)
	}
	c.subs[ch] = stop
	c.mu.Unlock()
	write := func(id int) {
		c.mu.Lock()
		body := c.events[id-1]
		c.mu.Unlock()
		fmt.Fprintf(w, "id: %d\n%s\n", id, body)
		fl.Flush()
	}
	for _, id := range replay {
		write(id)
	}
	fl.Flush()
	for {
		select {
		case id := <-ch:
			write(id)
		case <-stop:
			return
		case <-r.Context().Done():
			c.mu.Lock()
			delete(c.subs, ch)
			c.mu.Unlock()
			return
		}
	}
}

// heard collects the messages a stream offers.
type heard struct {
	mu   sync.Mutex
	msgs []driver.Message
	got  chan struct{}
}

func newHeard() *heard { return &heard{got: make(chan struct{}, 256)} }

func (h *heard) Claim(m driver.Message) bool {
	h.mu.Lock()
	h.msgs = append(h.msgs, m)
	h.mu.Unlock()
	h.got <- struct{}{}
	return true
}
func (h *heard) Unclaimed(driver.Message) {}

func (h *heard) waitFor(t *testing.T, n int) []driver.Message {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		h.mu.Lock()
		if len(h.msgs) >= n {
			out := append([]driver.Message(nil), h.msgs...)
			h.mu.Unlock()
			return out
		}
		h.mu.Unlock()
		select {
		case <-h.got:
		case <-deadline:
			h.mu.Lock()
			defer h.mu.Unlock()
			t.Fatalf("heard %d messages, want %d: %+v", len(h.msgs), n, h.msgs)
		}
	}
}

// transitions records a session's states.
type transitions struct {
	mu   sync.Mutex
	seen []collection.SessionTransition
	auth []collection.AuthVerdict
}

func (tr *transitions) on(t collection.SessionTransition) {
	tr.mu.Lock()
	tr.seen = append(tr.seen, t)
	tr.mu.Unlock()
}

func (tr *transitions) onAuth(v collection.AuthVerdict) {
	tr.mu.Lock()
	tr.auth = append(tr.auth, v)
	tr.mu.Unlock()
}

func (tr *transitions) waitState(t *testing.T, want collection.SessionState) collection.SessionTransition {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tr.mu.Lock()
		for i := len(tr.seen) - 1; i >= 0; i-- {
			if tr.seen[i].State == want {
				got := tr.seen[i]
				tr.mu.Unlock()
				return got
			}
		}
		tr.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the session never read %s: %+v", want, tr.seen)
	return collection.SessionTransition{}
}

func basicGate(user, pass string) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		u, p, ok := r.BasicAuth()
		if ok && u == user && p == pass {
			return true
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="bar"`)
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return false
	}
}

func startStream(target string, in collection.Inbound, a *collection.HTTPStreamAuth, tr *transitions) *collection.Session {
	return collection.StartSession(collection.SessionConfig{
		Target: target, Timeout: 2 * time.Second, Dialer: collection.NewHTTPStreamDialer(), Inbound: in,
		Auth: &collection.SessionAuth{HTTP: a}, OnTransition: tr.on, OnAuth: tr.onAuth,
	})
}

func TestHTTPStreamLandsEventsAndResumesWithNoGapOrDuplicate(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	comp, target := newSSEComponent(t, 400)
	comp.gate = basicGate("admin", "kestrel-pass")
	in, tr := newHeard(), &transitions{}
	resume := collection.NewStreamResume()
	sess := startStream(target, in, &collection.HTTPStreamAuth{
		Request:    driver.Request{Path: "/events"},
		Session:    &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeBasic},
		Credential: &driver.HTTPCredential{Scheme: driver.SchemeBasic, Username: "admin", Password: "kestrel-pass"},
		Resume:     resume,
	}, tr)
	defer sess.Close()
	tr.waitState(t, collection.SessionUp)
	for i := 1; i <= 3; i++ {
		comp.push("change", fmt.Sprintf(`{"path":"status.audio.volume","value":%d}`, i))
	}
	got := in.waitFor(t, 3)
	if got[0].Event != "change" || got[0].ID != "1" || got[0].Format != driver.FormatJSON || got[2].Data != `{"path":"status.audio.volume","value":3}` {
		t.Fatalf("messages = %+v", got)
	}
	if resume.LastID() != "3" || resume.Retry() != 400*time.Millisecond {
		t.Fatalf("resume = %q after %v, want 3 after 400ms", resume.LastID(), resume.Retry())
	}

	// The component drops the stream, and two events happen while it is
	// gone. The session waits the retry it was asked for, resumes from id 3,
	// and the component's replay (3 again, then 4 and 5) lands 4 and 5 once.
	comp.drop()
	dropped := time.Now()
	comp.push("change", `{"path":"status.audio.volume","value":4}`)
	comp.push("change", `{"path":"status.audio.volume","value":5}`)
	tr.waitState(t, collection.SessionRecovering)
	got = in.waitFor(t, 5)
	if waited := time.Since(dropped); waited < 400*time.Millisecond {
		t.Fatalf("the stream resumed %v after the drop, sooner than the server's retry", waited)
	}
	time.Sleep(200 * time.Millisecond)
	in.mu.Lock()
	all := append([]driver.Message(nil), in.msgs...)
	in.mu.Unlock()
	if len(all) != 5 {
		t.Fatalf("messages after the resume = %d, want 5 with nothing twice: %+v", len(all), all)
	}
	for i, m := range all {
		if m.ID != strconv.Itoa(i+1) {
			t.Fatalf("message %d carries id %q, want %d: %+v", i, m.ID, i+1, all)
		}
	}
	comp.mu.Lock()
	resumed := append([]string(nil), comp.resumed...)
	comp.mu.Unlock()
	if len(resumed) != 2 || resumed[0] != "" || resumed[1] != "3" {
		t.Fatalf("the connections sent Last-Event-ID %q, want none then 3", resumed)
	}
	tr.mu.Lock()
	auth := append([]collection.AuthVerdict(nil), tr.auth...)
	tr.mu.Unlock()
	if len(auth) != 1 || !auth[0].Authenticated {
		t.Fatalf("auth verdicts = %+v, want one yes", auth)
	}
}

func TestHTTPStreamRefusedCredentialIsOneAttempt(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	comp, target := newSSEComponent(t, 100)
	comp.gate = basicGate("admin", "kestrel-pass")
	var attempts atomic.Int32
	inner := comp.gate
	comp.gate = func(w http.ResponseWriter, r *http.Request) bool { attempts.Add(1); return inner(w, r) }
	tr := &transitions{}
	sess := startStream(target, newHeard(), &collection.HTTPStreamAuth{
		Request:    driver.Request{Path: "/events"},
		Session:    &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeBasic},
		Credential: &driver.HTTPCredential{Scheme: driver.SchemeBasic, Username: "admin", Password: "wrong-pass"},
	}, tr)
	defer sess.Close()
	rejected := tr.waitState(t, collection.SessionRejected)
	if rejected.Refusal == nil || rejected.Refusal.Class != driver.ClassAuth || !strings.Contains(rejected.Reason, "401") {
		t.Fatalf("rejected = %+v, want the credential's auth refusal", rejected)
	}
	if strings.Contains(rejected.Reason, "wrong-pass") {
		t.Fatalf("the reason carries the password: %s", rejected.Reason)
	}
	time.Sleep(500 * time.Millisecond)
	if attempts.Load() != 1 {
		t.Fatalf("the stream was attempted %d times, want one", attempts.Load())
	}
	tr.mu.Lock()
	auth := append([]collection.AuthVerdict(nil), tr.auth...)
	tr.mu.Unlock()
	if len(auth) != 1 || auth[0].Authenticated {
		t.Fatalf("auth verdicts = %+v, want one no", auth)
	}
}

func TestHTTPStreamAnswersDigestAndOpensBehindALogin(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	// Digest: the first connection draws the challenge; the stream reconnects
	// and answers it.
	dc := &digestComponent{algorithm: "SHA-256", password: "d1gest-pass"}
	dc.rotate()
	comp, target := newSSEComponent(t, 100)
	comp.gate = dc.gate
	in, tr := newHeard(), &transitions{}
	sess := startStream(target, in, &collection.HTTPStreamAuth{
		Request:    driver.Request{Path: "/events", Format: driver.FormatText},
		Session:    &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeDigest},
		Credential: &driver.HTTPCredential{Scheme: driver.SchemeDigest, Username: "admin", Password: "d1gest-pass"},
	}, tr)
	tr.waitState(t, collection.SessionUp)
	comp.push("tick", "volume 7")
	if got := in.waitFor(t, 1); got[0].Data != "volume 7" || got[0].Format != driver.FormatText || got[0].Event != "tick" {
		t.Fatalf("a text event = %+v", got[0])
	}
	sess.Close()

	// A form login: the stream logs in first, and rides the session cookie.
	login, loginTarget := newLoginComponent(t)
	lcomp, _ := newSSEComponent(t, 100)
	lcomp.other = login
	lcomp.gate = func(w http.ResponseWriter, r *http.Request) bool {
		if c, err := r.Cookie("sid"); err == nil && login.live(c.Value) {
			return true
		}
		http.Redirect(w, r, "/login", http.StatusFound)
		return false
	}
	srv := httptest.NewServer(lcomp)
	defer srv.Close()
	_ = loginTarget
	state := collection.NewHTTPAuthState()
	lin, ltr := newHeard(), &transitions{}
	lsess := startStream(srv.Listener.Addr().String(), lin, &collection.HTTPStreamAuth{
		Request: driver.Request{Path: "/events"},
		Session: formLoginSession(),
		Secrets: loginSecrets("s3cret-pass"),
		State:   state,
		Fetcher: collection.NewHTTPFetcher(),
	}, ltr)
	defer lsess.Close()
	ltr.waitState(t, collection.SessionUp)
	lcomp.push("change", `{"path":"status.audio.volume","value":9}`)
	lin.waitFor(t, 1)
	if login.loginHits.Load() != 1 || state.Logins() != 1 {
		t.Fatalf("logins = %d, want the one the stream opened with", login.loginHits.Load())
	}
	// The component forgets the session and drops the stream: the
	// reconnect is sent to the login page, which logs in once more.
	login.expire()
	lcomp.drop()
	deadline := time.Now().Add(10 * time.Second)
	for login.loginHits.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	ltr.waitState(t, collection.SessionUp)
	lcomp.push("change", `{"path":"status.audio.volume","value":10}`)
	lin.waitFor(t, 2)
	if login.loginHits.Load() != 2 {
		t.Fatalf("logins after the lapse = %d, want exactly one more", login.loginHits.Load())
	}
}

func TestHTTPStreamWithNoCredentialProvesNone(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	comp, target := newSSEComponent(t, 100)
	in, tr := newHeard(), &transitions{}
	sess := startStream(target, in, &collection.HTTPStreamAuth{Request: driver.Request{Path: "/events"}}, tr)
	defer sess.Close()
	tr.waitState(t, collection.SessionUp)
	comp.push("message", `{"a":1}`)
	in.waitFor(t, 1)
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.auth) != 0 {
		t.Fatalf("an uncredentialed stream reported auth verdicts %+v", tr.auth)
	}
	// Its polls are requests of their own: the stream answers none.
	if _, err := sess.Exchange(t.Context(), "GET", nil, time.Second); err == nil || !strings.Contains(err.Error(), "no requests") {
		t.Fatalf("an exchange over the stream = %v", err)
	}
}

func TestHTTPStreamThatIsNotAStreamIsNotARefusal(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"not":"a stream"}`))
	}))
	defer srv.Close()
	tr := &transitions{}
	sess := collection.StartSession(collection.SessionConfig{
		Target: srv.Listener.Addr().String(), Timeout: time.Second, Dialer: collection.NewHTTPStreamDialer(), Inbound: newHeard(),
		Auth: &collection.SessionAuth{HTTP: &collection.HTTPStreamAuth{Request: driver.Request{Path: "/events"}}}, OnTransition: tr.on,
		Backoff: func(int) time.Duration { return 10 * time.Millisecond },
	})
	defer sess.Close()
	down := tr.waitState(t, collection.SessionDown)
	if !strings.Contains(down.Reason, "not text/event-stream") {
		t.Fatalf("down = %+v, want the media type named", down)
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	for _, s := range tr.seen {
		if s.State == collection.SessionRejected {
			t.Fatalf("a json answer rejected the session: %+v", s)
		}
	}
}
