package collection_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A websocket held as a session over real sockets (#1034): the upgrade
// rides the session's scheme or login, an arm subscribes on every
// connection, pushes reach the listeners, an exchange is answered by the
// frame carrying back the id minted into it whatever pushes and other
// answers interleave, a drop recovers and arms again, a refused credential
// rejects after one attempt, and the keepalive and the frame bound hold.

// wsComponent speaks a small JSON protocol over a websocket at /ws: a
// subscribe is acknowledged and subscribes the connection to change pushes;
// a get or a set is answered with a result carrying its id (after a push
// and a decoy result for another id, so correlation is exercised on every
// exchange); an unknown path is an error carrying the id.
type wsComponent struct {
	mu         sync.Mutex
	volume     int
	conns      map[*wsPeer]bool
	gate       func(http.ResponseWriter, *http.Request) bool
	other      http.Handler
	upgrades   atomic.Int32
	subscribes atomic.Int32
	// silent upgrades and then never reads, so no ping is answered.
	silent bool
	up     websocket.Upgrader
}

type wsPeer struct {
	mu         sync.Mutex
	c          *websocket.Conn
	subscribed bool
}

func (p *wsPeer) send(kind int, data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.c.WriteMessage(kind, data)
}

func newWSComponent(t *testing.T) (*wsComponent, *httptest.Server) {
	t.Helper()
	c := &wsComponent{volume: 30, conns: map[*wsPeer]bool{}}
	srv := httptest.NewServer(c)
	t.Cleanup(func() { c.drop(); srv.Close() })
	return c, srv
}

func (c *wsComponent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/ws" {
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
	conn, err := c.up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c.upgrades.Add(1)
	p := &wsPeer{c: conn}
	c.mu.Lock()
	c.conns[p] = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.conns, p)
		c.mu.Unlock()
		_ = conn.Close()
	}()
	if c.silent {
		time.Sleep(10 * time.Second)
		return
	}
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		c.answer(p, data)
	}
}

func (c *wsComponent) answer(p *wsPeer, data []byte) {
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		p.send(websocket.TextMessage, []byte(`{"type":"error","error":"not json"}`))
		return
	}
	id, _ := m["id"].(string)
	switch m["type"] {
	case "subscribe":
		c.subscribes.Add(1)
		p.mu.Lock()
		p.subscribed = true
		p.mu.Unlock()
		p.send(websocket.TextMessage, []byte(`{"type":"ack"}`))
	case "get", "set":
		if m["path"] != "status.audio.volume" {
			p.send(websocket.TextMessage, fmt.Appendf(nil, `{"type":"error","id":%q,"error":"no such path"}`, id))
			return
		}
		c.mu.Lock()
		if m["type"] == "set" {
			if v, ok := m["value"].(float64); ok {
				c.volume = int(v)
			}
		}
		v := c.volume
		c.mu.Unlock()
		// A push and another exchange's answer arrive first.
		c.pushTo(p, v)
		p.send(websocket.TextMessage, []byte(`{"type":"result","id":"someone-else","value":-1}`))
		p.send(websocket.TextMessage, fmt.Appendf(nil, `{"type":"result","id":%q,"value":%d}`, id, v))
	}
}

func (c *wsComponent) pushTo(p *wsPeer, v int) {
	p.mu.Lock()
	sub := p.subscribed
	p.mu.Unlock()
	if sub {
		p.send(websocket.TextMessage, fmt.Appendf(nil, `{"type":"change","path":"status.audio.volume","value":%d}`, v))
	}
}

// push sets the volume and pushes the change to every subscriber.
func (c *wsComponent) push(v int) {
	c.mu.Lock()
	c.volume = v
	peers := make([]*wsPeer, 0, len(c.conns))
	for p := range c.conns {
		peers = append(peers, p)
	}
	c.mu.Unlock()
	for _, p := range peers {
		c.pushTo(p, v)
	}
}

// raw sends one frame to every connection.
func (c *wsComponent) raw(kind int, data []byte) {
	c.mu.Lock()
	peers := make([]*wsPeer, 0, len(c.conns))
	for p := range c.conns {
		peers = append(peers, p)
	}
	c.mu.Unlock()
	for _, p := range peers {
		p.send(kind, data)
	}
}

// drop closes every connection, as a component restarting would.
func (c *wsComponent) drop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for p := range c.conns {
		_ = p.c.Close()
	}
}

// changes is the listeners: it claims every change push, and counts what
// nothing claims.
type changes struct {
	heard     *heard
	mu        sync.Mutex
	unclaimed []string
}

func newChanges() *changes { return &changes{heard: newHeard()} }

var changeMatch = driver.Match{Field: &driver.FieldMatch{Path: "type", Equals: "change"}}

func (c *changes) Claim(m driver.Message) bool {
	if !changeMatch.MatchesMessage(m) {
		return false
	}
	return c.heard.Claim(m)
}

func (c *changes) Unclaimed(m driver.Message) {
	c.mu.Lock()
	c.unclaimed = append(c.unclaimed, m.Data)
	c.mu.Unlock()
}

func (c *changes) unclaimedLines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.unclaimed...)
}

func wsTarget(srv *httptest.Server) string { return srv.Listener.Addr().String() }

var subscribe = `{"type":"subscribe","paths":["status.audio"]}`

func startWS(target string, in collection.Inbound, a *collection.HTTPStreamAuth, tr *transitions, arms *[]string) *collection.Session {
	var mu sync.Mutex
	return collection.StartSession(collection.SessionConfig{
		Target: target, Timeout: 2 * time.Second, Dialer: collection.WebSocketDialer{Ping: 200 * time.Millisecond}, Inbound: in,
		Auth: &collection.SessionAuth{HTTP: a}, OnTransition: tr.on, OnAuth: tr.onAuth, Arm: []string{subscribe},
		OnArm: func(line, reply string, err error) {
			mu.Lock()
			defer mu.Unlock()
			if arms != nil {
				*arms = append(*arms, reply)
			}
		},
	})
}

func basicWS(pass string) *collection.HTTPStreamAuth {
	return &collection.HTTPStreamAuth{
		Request:    driver.Request{Path: "/ws"},
		Session:    &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeBasic},
		Credential: &driver.HTTPCredential{Scheme: driver.SchemeBasic, Username: "admin", Password: pass},
	}
}

func getVolume(t *testing.T, sess *collection.Session) (string, error) {
	t.Helper()
	req := driver.Request{Body: json.RawMessage(`{"type":"get","path":"status.audio.volume"}`), Reply: &driver.Reply{Correlate: "id"}}
	frame, reply, err := req.Bind()
	if err != nil {
		t.Fatal(err)
	}
	return sess.Exchange(context.Background(), frame, reply, 2*time.Second)
}

func TestWebSocketArmsPushesAndAnswersByItsID(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	comp, srv := newWSComponent(t)
	comp.gate = basicGate("admin", "kestrel-pass")
	in, tr := newChanges(), &transitions{}
	var arms []string
	sess := startWS(wsTarget(srv), in, basicWS("kestrel-pass"), tr, &arms)
	defer sess.Close()
	tr.waitState(t, collection.SessionUp)
	if comp.subscribes.Load() != 1 {
		t.Fatalf("subscribes = %d, want the arm sent on connect", comp.subscribes.Load())
	}

	comp.push(41)
	if got := in.heard.waitFor(t, 1); got[0].Data != `{"type":"change","path":"status.audio.volume","value":41}` {
		t.Fatalf("push = %+v", got[0])
	}
	// The answer is the frame carrying the exchange's own id: the push
	// before it went to the listener, and the result for another id went
	// nowhere.
	answer, err := getVolume(t, sess)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	var res struct {
		Type  string `json:"type"`
		ID    string `json:"id"`
		Value int    `json:"value"`
	}
	if err := json.Unmarshal([]byte(answer), &res); err != nil || res.Type != "result" || res.Value != 41 || res.ID == "someone-else" {
		t.Fatalf("answer = %s, %v", answer, err)
	}
	in.heard.waitFor(t, 2)
	if un := in.unclaimedLines(); len(un) != 1 || !strings.Contains(un[0], "someone-else") {
		t.Fatalf("unclaimed = %q, want only the other exchange's answer", un)
	}
	// A refusal carrying the id answers the exchange as the device's
	// refusal.
	req := driver.Request{Body: json.RawMessage(`{"type":"get","path":"status.nope"}`), Reply: &driver.Reply{Correlate: "id", Error: &driver.Match{Regex: `"type":"error"`}}}
	frame, reply, _ := req.Bind()
	_, err = sess.Exchange(context.Background(), frame, reply, 2*time.Second)
	var refused *collection.RefusedError
	if !errors.As(err, &refused) || !strings.Contains(refused.Line, "no such path") {
		t.Fatalf("a refused get = %v, want the device's refusal", err)
	}
	tr.mu.Lock()
	auth := append([]collection.AuthVerdict(nil), tr.auth...)
	tr.mu.Unlock()
	if len(auth) != 1 || !auth[0].Authenticated {
		t.Fatalf("auth verdicts = %+v, want one yes", auth)
	}
	if len(arms) != 1 || arms[0] != `{"type":"ack"}` {
		t.Fatalf("arm replies = %q, want the ack", arms)
	}

	// The component drops the socket: the session recovers, arms again,
	// and pushes land once more.
	comp.drop()
	tr.waitState(t, collection.SessionRecovering)
	deadline := time.Now().Add(10 * time.Second)
	for comp.subscribes.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if comp.subscribes.Load() != 2 {
		t.Fatalf("subscribes after the drop = %d, want the arm sent again", comp.subscribes.Load())
	}
	comp.push(42)
	if got := in.heard.waitFor(t, 3); !strings.Contains(got[2].Data, `"value":42`) {
		t.Fatalf("a push after the recover = %+v", got[2])
	}
}

func TestWebSocketRefusedCredentialIsOneAttempt(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			comp, srv := newWSComponent(t)
			comp.gate = func(w http.ResponseWriter, r *http.Request) bool {
				if u, p, ok := r.BasicAuth(); ok && u == "admin" && p == "kestrel-pass" {
					return true
				}
				http.Error(w, "no", status)
				return false
			}
			var attempts atomic.Int32
			inner := comp.gate
			comp.gate = func(w http.ResponseWriter, r *http.Request) bool { attempts.Add(1); return inner(w, r) }
			tr := &transitions{}
			sess := startWS(wsTarget(srv), newChanges(), basicWS("wrong-pass"), tr, nil)
			defer sess.Close()
			rejected := tr.waitState(t, collection.SessionRejected)
			if rejected.Refusal == nil || rejected.Refusal.Class != driver.ClassAuth || !strings.Contains(rejected.Reason, fmt.Sprint(status)) {
				t.Fatalf("rejected = %+v, want the credential's auth refusal", rejected)
			}
			if strings.Contains(rejected.Reason, "wrong-pass") {
				t.Fatalf("the reason carries the password: %s", rejected.Reason)
			}
			time.Sleep(300 * time.Millisecond)
			if n := attempts.Load(); n != 1 {
				t.Fatalf("upgrades sent = %d, want one", n)
			}
		})
	}
}

func TestWebSocketWithNoCredentialProvesNoneAndAnOpenOneOpens(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	comp, srv := newWSComponent(t)
	in, tr := newChanges(), &transitions{}
	sess := startWS(wsTarget(srv), in, &collection.HTTPStreamAuth{Request: driver.Request{Path: "/ws"}}, tr, nil)
	defer sess.Close()
	tr.waitState(t, collection.SessionUp)
	comp.push(5)
	in.heard.waitFor(t, 1)
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.auth) != 0 {
		t.Fatalf("an open websocket reported auth verdicts %+v, want none", tr.auth)
	}
}

func TestWebSocketUpgradeCarriesEachScheme(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	cases := []struct {
		name string
		cred *driver.HTTPCredential
		ok   func(*http.Request) bool
	}{
		{"bearer", &driver.HTTPCredential{Scheme: driver.SchemeBearer, Token: "t0ken"},
			func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer t0ken" }},
		{"an api key header", &driver.HTTPCredential{Scheme: driver.SchemeAPIKey, In: driver.APIKeyInHeader, Name: "X-Key", Key: "k3y"},
			func(r *http.Request) bool { return r.Header.Get("X-Key") == "k3y" }},
		{"an api key query", &driver.HTTPCredential{Scheme: driver.SchemeAPIKey, In: driver.APIKeyInQuery, Name: "key", Key: "k3y"},
			func(r *http.Request) bool {
				return r.URL.Query().Get("key") == "k3y" && r.URL.Query().Get("room") == "huddle"
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comp, srv := newWSComponent(t)
			comp.gate = func(w http.ResponseWriter, r *http.Request) bool {
				if tc.ok(r) && r.Header.Get("X-Room") == "huddle" {
					return true
				}
				http.Error(w, "no", http.StatusUnauthorized)
				return false
			}
			tr := &transitions{}
			sess := startWS(wsTarget(srv), newChanges(), &collection.HTTPStreamAuth{
				Request:    driver.Request{Path: "/ws", Query: map[string]string{"room": "huddle"}, Headers: map[string]string{"X-Room": "huddle"}},
				Session:    &driver.SessionSpec{Credential: "login", Scheme: tc.cred.Scheme},
				Credential: tc.cred,
			}, tr, nil)
			defer sess.Close()
			tr.waitState(t, collection.SessionUp)
		})
	}
}

func TestWebSocketOpensBehindALoginAndLogsInOnceOnLapse(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	login, _ := newLoginComponent(t)
	comp, srv := newWSComponent(t)
	comp.other = login
	comp.gate = func(w http.ResponseWriter, r *http.Request) bool {
		if c, err := r.Cookie("sid"); err == nil && login.live(c.Value) {
			return true
		}
		http.Redirect(w, r, "/login", http.StatusFound)
		return false
	}
	state := collection.NewHTTPAuthState()
	in, tr := newChanges(), &transitions{}
	sess := startWS(wsTarget(srv), in, &collection.HTTPStreamAuth{
		Request: driver.Request{Path: "/ws"},
		Session: formLoginSession(),
		Secrets: loginSecrets("s3cret-pass"),
		State:   state,
		Fetcher: collection.NewHTTPFetcher(),
	}, tr, nil)
	defer sess.Close()
	tr.waitState(t, collection.SessionUp)
	if login.loginHits.Load() != 1 {
		t.Fatalf("logins = %d, want the one the upgrade opened with", login.loginHits.Load())
	}
	login.expire()
	comp.drop()
	deadline := time.Now().Add(10 * time.Second)
	for comp.upgrades.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	comp.push(12)
	in.heard.waitFor(t, 1)
	if login.loginHits.Load() != 2 {
		t.Fatalf("logins after the lapse = %d, want exactly one more", login.loginHits.Load())
	}
}

func TestWebSocketKeepaliveCatchesASilentComponent(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	comp, srv := newWSComponent(t)
	comp.silent = true
	conn := openWS(t, wsTarget(srv), nil, newChanges())
	defer conn.Close()
	select {
	case <-conn.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a component that answers no ping kept the connection alive")
	}
	if err := conn.Err(); err == nil || !strings.Contains(err.Error(), "not even a pong") {
		t.Fatalf("cause = %v, want the silence named", err)
	}
}

func TestWebSocketAnsweredPingsKeepTheConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	_, srv := newWSComponent(t)
	conn := openWS(t, wsTarget(srv), nil, newChanges())
	defer conn.Close()
	select {
	case <-conn.Done():
		t.Fatalf("a component that answers pings lost the connection: %v", conn.Err())
	case <-time.After(1200 * time.Millisecond): // six pings, three silences
	}
}

func TestWebSocketDropsAnOversizeOrBinaryFrameAndReadsOn(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	comp, srv := newWSComponent(t)
	in := newChanges()
	conn := openWS(t, wsTarget(srv), nil, in)
	defer conn.Close()
	if _, err := conn.Exchange(context.Background(), subscribe, driver.AnyLine(), 2*time.Second); err != nil {
		t.Fatal(err)
	}
	big := `{"type":"change","value":"` + strings.Repeat("x", 1<<20) + `"}`
	comp.raw(websocket.TextMessage, []byte(big))
	comp.raw(websocket.BinaryMessage, []byte{1, 2, 3})
	comp.push(7)
	got := in.heard.waitFor(t, 1)
	if !strings.Contains(got[0].Data, `"value":7`) {
		t.Fatalf("after the dropped frames = %+v", got)
	}
	un := in.unclaimedLines()
	if len(un) != 2 || !strings.Contains(un[0], "past the") || !strings.Contains(un[1], "binary frame of 3 bytes") {
		t.Fatalf("unclaimed = %q, want the two dropped frames named", un)
	}
	select {
	case <-conn.Done():
		t.Fatalf("the connection died: %v", conn.Err())
	default:
	}
}

func TestWebSocketUnshapedExchangeTakesTheFirstUnclaimedFrame(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	_, srv := newWSComponent(t)
	conn := openWS(t, wsTarget(srv), nil, newChanges())
	defer conn.Close()
	if _, err := conn.Exchange(context.Background(), subscribe, driver.AnyLine(), 2*time.Second); err != nil {
		t.Fatal(err)
	}
	// Unshaped: the push is the listener's, so the first unclaimed frame,
	// the decoy, is the answer, which is why a websocket driver correlates.
	ans, err := conn.Exchange(context.Background(), `{"type":"get","path":"status.audio.volume","id":"x"}`, nil, 2*time.Second)
	if err != nil || !strings.Contains(ans, "someone-else") {
		t.Fatalf("an unshaped exchange = %q, %v", ans, err)
	}
	// Shaped, no answer in time: the exchange fails alone and the
	// connection is kept.
	_, err = conn.Exchange(context.Background(), `{"type":"get","path":"status.audio.volume","id":"y"}`, &driver.Reply{Field: &driver.FieldMatch{Path: "id", Equals: "never"}}, 200*time.Millisecond)
	if !errors.Is(err, collection.ErrNoAnswer) {
		t.Fatalf("a shaped timeout = %v, want no answer", err)
	}
	select {
	case <-conn.Done():
		t.Fatalf("a shaped timeout recycled the connection: %v", conn.Err())
	default:
	}
}

func TestWebSocketOverTLSPinsTheKey(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	comp := &wsComponent{volume: 30, conns: map[*wsPeer]bool{}}
	srv := httptest.NewTLSServer(comp)
	t.Cleanup(func() { comp.drop(); srv.Close() })
	pin := collection.NewHostKeyPin("")
	trust := collection.TLSTrust{Mode: driver.TLSModePin, Pin: pin, ServerName: "127.0.0.1"}
	in, tr := newChanges(), &transitions{}
	sess := collection.StartSession(collection.SessionConfig{
		Target: wsTarget(srv), Timeout: 2 * time.Second, Dialer: collection.NewWebSocketDialer(), Inbound: in, TLS: &trust,
		Auth: &collection.SessionAuth{HTTP: &collection.HTTPStreamAuth{Request: driver.Request{Path: "/ws"}, TLS: &trust}}, OnTransition: tr.on,
		Arm: []string{subscribe},
	})
	tr.waitState(t, collection.SessionUp)
	comp.push(3)
	in.heard.waitFor(t, 1)
	sess.Close()
	if pin.Get() == "" {
		t.Fatal("the first connection trusted no key")
	}
	// Another key pinned: the handshake refuses the certificate, and no
	// upgrade is sent.
	other := collection.TLSTrust{Mode: driver.TLSModePin, Pin: collection.NewHostKeyPin("SHA256:not-this-key"), ServerName: "127.0.0.1"}
	tr2 := &transitions{}
	before := comp.upgrades.Load()
	sess2 := collection.StartSession(collection.SessionConfig{
		Target: wsTarget(srv), Timeout: 2 * time.Second, Dialer: collection.NewWebSocketDialer(), TLS: &other,
		Auth: &collection.SessionAuth{HTTP: &collection.HTTPStreamAuth{Request: driver.Request{Path: "/ws"}, TLS: &other}}, OnTransition: tr2.on,
	})
	defer sess2.Close()
	rejected := tr2.waitState(t, collection.SessionRejected)
	if rejected.Refusal == nil || rejected.Refusal.Class != driver.ClassCertificate {
		t.Fatalf("rejected = %+v, want a certificate refusal", rejected)
	}
	if comp.upgrades.Load() != before {
		t.Fatal("an upgrade reached a component whose key the pin refused")
	}
}

func TestWebSocketBoundsTheUpgradeHeadAndRedactsTheKey(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var mode atomic.Int32 // 0: an endless head; 1: hang up
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				for {
					line, err := br.ReadString('\n')
					if err != nil || line == "\r\n" {
						break
					}
				}
				if mode.Load() == 1 {
					return
				}
				_, _ = c.Write([]byte("HTTP/1.1 101 Switching Protocols\r\n"))
				pad := "X-Pad: " + strings.Repeat("a", 1024) + "\r\n"
				for i := 0; i < 200; i++ {
					if _, err := c.Write([]byte(pad)); err != nil {
						return
					}
				}
			}(c)
		}
	}()
	key := &driver.HTTPCredential{Scheme: driver.SchemeAPIKey, In: driver.APIKeyInQuery, Name: "key", Key: "s3cret-key"}
	auth := &collection.SessionAuth{HTTP: &collection.HTTPStreamAuth{Request: driver.Request{Path: "/ws"}, Credential: key,
		Session: &driver.SessionSpec{Credential: "login", Scheme: driver.SchemeAPIKey}}}
	for i, want := range []string{"head", "EOF"} {
		mode.Store(int32(i))
		pc, err := collection.NewWebSocketDialer().Connect(context.Background(), ln.Addr().String(), 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		err = pc.Authenticate(context.Background(), auth, nil, 2*time.Second)
		_ = pc.Close()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("attempt %d = %v, want %q named", i, err, want)
		}
		if strings.Contains(err.Error(), "s3cret-key") {
			t.Fatalf("the error carries the key: %v", err)
		}
	}
}

// openWS opens one websocket connection through the dialer's stages.
func openWS(t *testing.T, target string, a *collection.HTTPStreamAuth, in collection.Inbound) collection.HeldConn {
	t.Helper()
	if a == nil {
		a = &collection.HTTPStreamAuth{Request: driver.Request{Path: "/ws"}}
	}
	pc, err := collection.WebSocketDialer{Ping: 200 * time.Millisecond}.Connect(context.Background(), target, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := pc.Authenticate(context.Background(), &collection.SessionAuth{HTTP: a}, nil, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	conn, err := pc.Open(in, collection.ChannelOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return conn
}
