package collection

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The server-sent events stream as a held session (#1038, slice 3 of
// #1113). Its staged dialer is the session machine's, unchanged: Connect is
// the TCP connect (and the TLS handshake for https), Authenticate runs the
// endpoint's auth (a login first when it declares one, then the stream's
// GET carrying the scheme's credential, the login's session and the
// component's cookies, and the Last-Event-ID it resumes from) and reads the
// answer's head, and Open starts the reader that feeds the event-stream
// interpretation and offers every event to the listeners. A refused stream
// is the terminal auth the status table and the login judge every request
// by; a drop resumes from the last event ID, after the reconnection time
// the server asked for, and an event whose id the stream already landed is
// dropped.

// HTTPStreamAuth is what an http stream connection authenticates and opens
// with: the stream request, the session block whose scheme or login it
// rides, the credential and the secrets, the endpoint's learned auth (its
// cookies, a login's session and a digest challenge, shared with its polls
// and commands), the fetcher a login's steps ride, the trust those steps
// run over https, and the resume the stream carries across its reconnects.
// Its fields carry credentials: it is never printed.
type HTTPStreamAuth struct {
	Request    driver.Request
	Session    *driver.SessionSpec
	Credential *driver.HTTPCredential
	Secrets    map[string]map[string]string
	State      *HTTPAuthState
	Fetcher    HTTPFetcher
	TLS        *TLSTrust
	Resume     *StreamResume
}

// credentialed reports a stream whose every connection carries a
// credential: a scheme's, or a login's session.
func (a *HTTPStreamAuth) credentialed() bool {
	return a.Credential != nil || (a.Session != nil && a.Session.Login != nil)
}

// errStreamCarriesNoRequests answers an exchange over a stream: its polls
// and commands are requests of their own.
var errStreamCarriesNoRequests = errors.New("collection: a server-sent events stream carries no requests; the endpoint's polls and commands send their own")

// NewHTTPStreamDialer returns the held http stream dialer: http, and https
// through ConnectTLS.
func NewHTTPStreamDialer() TLSDialer { return httpStreamDialer{} }

type httpStreamDialer struct{}

// Dial is refused: a stream opens only through its auth step, which sends
// its request.
func (httpStreamDialer) Dial(context.Context, string, time.Duration, Inbound) (HeldConn, error) {
	return nil, &TerminalError{Class: driver.ClassProtocol, Reason: "an http stream opens through its auth step"}
}

func (d httpStreamDialer) Connect(ctx context.Context, target string, timeout time.Duration) (PendingConn, error) {
	return d.connect(ctx, target, timeout, nil)
}

func (d httpStreamDialer) ConnectTLS(ctx context.Context, target string, timeout time.Duration, trust TLSTrust) (PendingConn, error) {
	return d.connect(ctx, target, timeout, &trust)
}

func (httpStreamDialer) connect(ctx context.Context, target string, timeout time.Duration, trust *TLSTrust) (PendingConn, error) {
	if timeout <= 0 {
		timeout = defaultSessionTimeout
	}
	p := &pendingStream{target: target, timeout: timeout, trust: trust}
	if err := p.dial(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// pendingStream is a stream connection made and not yet routing: the auth
// step sends the stream request over it (reconnecting for a digest
// challenge or a fresh login), and Open hands the answer's body to the
// reader.
type pendingStream struct {
	target  string
	timeout time.Duration
	trust   *TLSTrust
	conn    net.Conn
	br      *bufio.Reader
	resp    *http.Response
	format  string
	resume  *StreamResume
}

// dial makes the connection: the TCP connect, then the handshake for https.
func (p *pendingStream) dial(ctx context.Context) error {
	d := net.Dialer{Timeout: p.timeout, KeepAlive: SessionKeepAlive}
	c, err := d.DialContext(ctx, "tcp", p.target)
	if err != nil {
		return fmt.Errorf("collection: http stream dial %s: %w", p.target, err)
	}
	if p.trust != nil {
		tc, _, err := HandshakeTLS(ctx, c, *p.trust, p.timeout)
		if err != nil {
			_ = c.Close()
			if _, ok := AsTerminal(err); ok {
				return err
			}
			return fmt.Errorf("collection: http stream handshake %s: %w", p.target, err)
		}
		c = tc
	}
	p.conn, p.br = c, bufio.NewReader(c)
	return nil
}

// redial replaces the connection: a component may close one after a 401.
func (p *pendingStream) redial(ctx context.Context) error {
	if p.conn != nil {
		_ = p.conn.Close()
	}
	return p.dial(ctx)
}

// refusedStreamBodyCap bounds what a refused stream's answer is read for:
// enough for its reason.
const refusedStreamBodyCap = 64 << 10

// streamAttempts bounds the requests one auth step sends: the first, one
// answer to a digest challenge or one after a fresh login, and one more for
// a stale nonce.
const streamAttempts = 3

// Authenticate logs in first when the session declares a login, then sends
// the stream request and judges its answer's head: a 200 carrying
// text/event-stream opens; a 401 to a fresh digest challenge is answered
// once more; a lapsed login session logs in once more and sends again; and
// anything else is judged by the status table, a refusal terminal.
func (p *pendingStream) Authenticate(ctx context.Context, auth *SessionAuth, _ Classifier, timeout time.Duration) error {
	if auth == nil || auth.HTTP == nil {
		return &TerminalError{Class: driver.ClassProtocol, Reason: "an http stream has no request to send"}
	}
	a := auth.HTTP
	if a.State == nil {
		a.State = NewHTTPAuthState()
	}
	if a.Resume == nil {
		a.Resume = NewStreamResume()
	}
	p.format, p.resume = a.Request.FormatOf(), a.Resume
	if timeout <= 0 {
		timeout = p.timeout
	}
	r := &Runner{HTTPFetch: a.Fetcher}
	x := HTTPExchange{Target: p.target, TLS: a.TLS, Request: a.Request, Credential: a.Credential, Session: a.Session, Timeout: timeout, State: a.State}
	login := a.Session != nil && a.Session.Login != nil
	var gen uint64
	var headers map[string]string
	opened := false
	if login {
		s := a.State
		var ok bool
		if gen, headers, ok = s.current(s.clock()); !ok {
			why := "opening"
			if gen > 0 {
				why = "renewing before expiry"
			}
			var err error
			if gen, headers, err = r.ensureLogin(ctx, x, a.Secrets, gen, why); err != nil {
				return err
			}
			opened = true
		}
	}
	digest := a.Credential != nil && a.Credential.Scheme == driver.SchemeDigest
	for attempt := 0; attempt < streamAttempts; attempt++ {
		if attempt > 0 {
			if err := p.redial(ctx); err != nil {
				return err
			}
		}
		sx := x
		sx.SessionHeaders = headers
		sx.Redact = a.State.redactions()
		resp, answered, nonce, err := p.send(ctx, sx, a, login, digest)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusOK && isEventStream(resp.Header) {
			p.resp = resp
			return nil
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, refusedStreamBodyCap))
		_ = resp.Body.Close()
		if login {
			a.State.cookieJar().SetCookies(resp.Request.URL, resp.Cookies())
		}
		sx.Redact = append(sx.Redact, a.State.redactions()...)
		ans := HTTPAnswer{Status: resp.StatusCode, Body: body, RetryAfter: resp.Header.Get("Retry-After"),
			Credentialed: a.credentialed(), Location: resp.Header.Get("Location")}
		if digest {
			ans.Credentialed = answered
			if resp.StatusCode == http.StatusUnauthorized {
				ch, ok := parseDigestChallenges(resp.Header)
				if ok && attempt < streamAttempts-1 && (!answered || ch.stale || ch.nonce != nonce) {
					a.State.digest.learn(ch)
					continue
				}
				if !answered {
					return &RequestFault{Status: resp.StatusCode, Reason: "the component's 401 offers no digest challenge this node answers (" + offeredSchemes(resp.Header) + ")"}
				}
			}
		}
		if login {
			if _, lapsed := SessionLapse(ans, sx); lapsed && !opened {
				a.State.lapse(gen)
				if gen, headers, err = r.ensureLogin(ctx, x, a.Secrets, gen, "the session lapsed"); err != nil {
					return err
				}
				opened = true
				continue
			}
		}
		if judged := JudgeHTTPAnswer(ans, sx); judged != nil {
			if terr, ok := AsTerminal(judged); ok {
				return terr
			}
			return fmt.Errorf("collection: the stream at %s answered %w", a.Request.Path, judged)
		}
		return fmt.Errorf("collection: the stream at %s answered %d with %q, not text/event-stream", a.Request.Path, resp.StatusCode, snippet([]byte(resp.Header.Get("Content-Type")), nil))
	}
	return fmt.Errorf("collection: the stream at %s did not open in %d requests", a.Request.Path, streamAttempts)
}

// send writes the stream request over the connection and reads its
// answer's head, the body left unread. answered and nonce say whether (and
// under which nonce) a digest challenge was answered.
func (p *pendingStream) send(ctx context.Context, x HTTPExchange, a *HTTPStreamAuth, login, digest bool) (resp *http.Response, answered bool, nonce string, err error) {
	if p.trust != nil {
		// BuildHTTPRequest names the scheme by the trust it is handed.
		x.TLS = p.trust
	}
	req, err := BuildHTTPRequest(ctx, x)
	if err != nil {
		return nil, false, "", err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if id := a.Resume.LastID(); id != "" {
		req.Header.Set("Last-Event-ID", id)
	}
	if login {
		for _, c := range a.State.cookieJar().Cookies(req.URL) {
			req.AddCookie(c)
		}
	}
	if digest {
		if answered, nonce, err = a.State.digest.authorize(req, a.Credential.Username, a.Credential.Password); err != nil {
			return nil, false, "", err
		}
	}
	_ = p.conn.SetDeadline(time.Now().Add(x.Timeout))
	wrote := false
	if err = req.Write(p.conn); err == nil {
		wrote = true
		resp, err = http.ReadResponse(p.br, req)
	}
	_ = p.conn.SetDeadline(time.Time{})
	if err != nil {
		return nil, false, "", redactedRequestError(req, x, err, wrote)
	}
	if resp.StatusCode == http.StatusOK && login {
		a.State.cookieJar().SetCookies(req.URL, resp.Cookies())
	}
	return resp, answered, nonce, nil
}

// isEventStream reports an answer whose media type is text/event-stream.
func isEventStream(h http.Header) bool {
	mt, _, err := mime.ParseMediaType(h.Get("Content-Type"))
	return err == nil && mt == "text/event-stream"
}

// Open starts the reader: from here the stream offers every event.
func (p *pendingStream) Open(in Inbound, _ ChannelOptions) (HeldConn, error) {
	if p.resp == nil {
		return nil, errors.New("collection: the stream opened with no answer")
	}
	c := &streamConn{conn: p.conn, body: p.resp.Body, in: in, format: p.format, resume: p.resume, done: make(chan struct{})}
	go c.read()
	return c, nil
}

func (p *pendingStream) Close() error {
	if p.conn == nil {
		return nil
	}
	return p.conn.Close()
}

// streamConn is a held stream: its reader feeds the event-stream
// interpretation and offers each event, in order, to the listeners.
type streamConn struct {
	conn   net.Conn
	body   io.ReadCloser
	in     Inbound
	format string
	resume *StreamResume

	once sync.Once
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func (c *streamConn) Exchange(context.Context, string, *driver.Reply, time.Duration) (string, error) {
	return "", errStreamCarriesNoRequests
}

func (c *streamConn) Done() <-chan struct{} { return c.done }

func (c *streamConn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *streamConn) Close() error {
	err := c.conn.Close()
	c.die(errors.New("the stream was closed"))
	return err
}

// RetryHint is the reconnection time the server asked for, which the
// session waits before its next attempt, within its backoff's bounds.
func (c *streamConn) RetryHint() time.Duration { return c.resume.Retry() }

func (c *streamConn) die(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		close(c.done)
	})
}

// read is the reader goroutine: lines in, events out. The last event ID
// moves at every dispatch, an event whose own id was already landed is
// dropped, and anything the stream ends with is why the connection died.
func (c *streamConn) read() {
	defer func() { _ = c.body.Close() }()
	br := bufio.NewReader(c.body)
	p := &sseParser{}
	for {
		line, err := readSSELine(br, maxSSELine)
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = errors.New("the component ended the stream")
			}
			c.die(err)
			return
		}
		ev, ok, err := p.line(line)
		if err != nil {
			c.die(err)
			_ = c.conn.Close()
			return
		}
		if p.retrySet {
			c.resume.setRetry(p.retry)
		}
		if line == "" {
			c.resume.setLastID(p.lastEventID)
		}
		if !ok {
			continue
		}
		if ev.hasID && ev.id != "" && !c.resume.land(ev.id) {
			continue // a replay of an event this stream already landed
		}
		msg := driver.Message{Data: ev.data, Event: ev.event, Format: c.format}
		if ev.hasID {
			msg.ID = ev.id
		}
		if c.in != nil && !c.in.Claim(msg) {
			c.in.Unclaimed(msg)
		}
	}
}
