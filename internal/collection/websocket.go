package collection

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The websocket transport as a held session (#1034, slice 4 of #1113). Its
// staged dialer is the session machine's, unchanged: Connect is the TCP
// connect (and the TLS handshake for wss), Authenticate runs the endpoint's
// auth (a login first when it declares one, then the upgrade carrying the
// scheme's credential, the login's session and its cookies) and reads the
// answer, and Open starts the connection's three goroutines. The reader owns
// every read: it bounds each frame, answers the component's pings, and dies
// when neither a frame nor a pong arrives in time. The router owns every
// data frame written: it routes what the reader hands it by the precedence
// a line session's reader applies (ADR-0151), so an exchange's answer is
// never a frame received before it was sent, and a correlated exchange
// takes only the frame carrying its id back, whatever pushes interleave.
// The pinger keeps the connection proven between exchanges. A refused
// upgrade is the terminal auth the status table and the login judge every
// http request by.

// WebSocketPingInterval is how often a held websocket pings its component
// when a dialer names no interval; a connection that hears neither a frame
// nor a pong for two intervals is dead.
const WebSocketPingInterval = 30 * time.Second

// maxWebSocketFrame bounds one inbound frame: a control interface's message
// is a small document, and a hostile component cannot make the node hold
// more. A larger frame is read off the wire, dropped, and named unclaimed;
// the connection is kept.
const maxWebSocketFrame = 1 << 20

// maxUpgradeHead bounds the upgrade answer's head, as a stream's is: a few
// headers, never more than the node will hold.
const maxUpgradeHead = maxStreamHead

// upgradeAttempts bounds the upgrades one auth step sends: the first, and
// one after a fresh login when the session lapsed.
const upgradeAttempts = 2

// WebSocketDialer is the held websocket dialer: websocket, and wss through
// ConnectTLS. Ping is the keepalive interval, zero being
// WebSocketPingInterval; tests shorten it.
type WebSocketDialer struct {
	Ping time.Duration
}

// NewWebSocketDialer returns the held websocket dialer.
func NewWebSocketDialer() TLSDialer { return WebSocketDialer{} }

// Dial is refused: a websocket opens only through its auth step, which
// sends its upgrade.
func (WebSocketDialer) Dial(context.Context, string, time.Duration, Inbound) (HeldConn, error) {
	return nil, &TerminalError{Class: driver.ClassProtocol, Reason: "a websocket opens through its auth step"}
}

func (d WebSocketDialer) Connect(ctx context.Context, target string, timeout time.Duration) (PendingConn, error) {
	return d.connect(ctx, target, timeout, nil)
}

func (d WebSocketDialer) ConnectTLS(ctx context.Context, target string, timeout time.Duration, trust TLSTrust) (PendingConn, error) {
	return d.connect(ctx, target, timeout, &trust)
}

func (d WebSocketDialer) connect(ctx context.Context, target string, timeout time.Duration, trust *TLSTrust) (PendingConn, error) {
	if timeout <= 0 {
		timeout = defaultSessionTimeout
	}
	ping := d.Ping
	if ping <= 0 {
		ping = WebSocketPingInterval
	}
	p := &pendingWS{target: target, timeout: timeout, trust: trust, ping: ping}
	if err := p.dial(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// pendingWS is a websocket connection made and not yet routing: the auth
// step sends the upgrade over it (reconnecting after a fresh login), and
// Open starts routing the frames.
type pendingWS struct {
	target  string
	timeout time.Duration
	trust   *TLSTrust
	ping    time.Duration
	conn    *headConn
	ws      *websocket.Conn
}

// headConn bounds the bytes read off a connection until it is lifted: the
// upgrade answer's head is read through it, the frames after it with no
// bound but the reader's own per frame.
type headConn struct {
	net.Conn
	head *headLimit
}

func (c *headConn) Read(b []byte) (int, error) { return c.head.Read(b) }

// dial makes the connection: the TCP connect, then the handshake for wss.
func (p *pendingWS) dial(ctx context.Context) error {
	d := net.Dialer{Timeout: p.timeout, KeepAlive: SessionKeepAlive}
	c, err := d.DialContext(ctx, "tcp", p.target)
	if err != nil {
		return fmt.Errorf("collection: websocket dial %s: %w", p.target, err)
	}
	if p.trust != nil {
		tc, _, err := HandshakeTLS(ctx, c, *p.trust, p.timeout)
		if err != nil {
			_ = c.Close()
			if _, ok := AsTerminal(err); ok {
				return err
			}
			return fmt.Errorf("collection: websocket handshake %s: %w", p.target, err)
		}
		c = tc
	}
	p.conn = &headConn{Conn: c, head: &headLimit{r: c, n: maxUpgradeHead, limited: true}}
	return nil
}

// redial replaces the connection: a refused upgrade closes the one it rode.
func (p *pendingWS) redial(ctx context.Context) error {
	if p.conn != nil {
		_ = p.conn.Close()
	}
	return p.dial(ctx)
}

// Authenticate logs in first when the session declares a login, then sends
// the upgrade and judges its answer: a 101 that completes the opening
// handshake opens; a lapsed login session logs in once more and upgrades
// again; and anything else is judged by the status table, a refusal of a
// credentialed upgrade terminal.
func (p *pendingWS) Authenticate(ctx context.Context, auth *SessionAuth, _ Classifier, timeout time.Duration) error {
	if auth == nil || auth.HTTP == nil {
		return &TerminalError{Class: driver.ClassProtocol, Reason: "a websocket has no upgrade to send"}
	}
	a := auth.HTTP
	if a.State == nil {
		a.State = NewHTTPAuthState()
	}
	if timeout <= 0 {
		timeout = p.timeout
	}
	if a.Gate != nil {
		if terr := a.Gate(); terr != nil {
			return terr
		}
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
	for attempt := 0; attempt < upgradeAttempts; attempt++ {
		if attempt > 0 {
			if err := p.redial(ctx); err != nil {
				return err
			}
		}
		sx := x
		sx.SessionHeaders = headers
		sx.Redact = a.State.redactions()
		ws, resp, err := p.upgrade(ctx, sx, a, login, timeout)
		if err == nil {
			p.ws = ws
			p.conn.head.limited = false
			return nil
		}
		if resp == nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body) // the client kept the first KiB, enough for a reason
		sx.Redact = append(sx.Redact, a.State.redactions()...)
		ans := HTTPAnswer{Status: resp.StatusCode, Body: body, RetryAfter: resp.Header.Get("Retry-After"),
			Credentialed: a.credentialed(), Location: resp.Header.Get("Location")}
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
		path := a.Request.Path
		if path == "" {
			path = "/"
		}
		if judged := JudgeUpgrade(ans, sx); judged != nil {
			if terr, ok := AsTerminal(judged); ok {
				return terr
			}
			return fmt.Errorf("collection: the websocket upgrade at %s answered %w", path, judged)
		}
		return fmt.Errorf("collection: the websocket upgrade at %s answered %d, which does not open a websocket", path, resp.StatusCode)
	}
	return fmt.Errorf("collection: the websocket upgrade at %s did not open in %d requests", a.Request.Path, upgradeAttempts)
}

// JudgeUpgrade reads a refused upgrade's answer by the status table, with
// one difference: a websocket opens by this one request, so a 401 or a 403
// to it refuses the session, terminal whether or not it carried a
// credential (a credential refused, or one the component wants and the
// driver does not send, or an origin it will not admit), where the table
// lets a request's 403 fault that request alone. Pure.
func JudgeUpgrade(a HTTPAnswer, x HTTPExchange) error {
	judged := JudgeHTTPAnswer(a, x)
	if _, ok := AsTerminal(judged); ok {
		return judged
	}
	if a.Status != http.StatusUnauthorized && a.Status != http.StatusForbidden {
		return judged
	}
	reason := fmt.Sprintf("the component refused the upgrade (%d), and the driver declares no credential", a.Status)
	if a.Credentialed {
		reason = fmt.Sprintf("the component refused the credential on the upgrade (%d)", a.Status)
	}
	if text := snippet(a.Body, x.Credential, x.Redact...); text != "" {
		reason += ": " + text
	}
	return &TerminalError{Class: driver.ClassAuth, Reason: reason}
}

// upgrade sends the opening handshake over the connection: the upgrade
// request built as every http request is (its path, query and headers, the
// scheme's credential, a login's session headers and its cookies) and the
// answer read through the head bound. A refused upgrade returns the answer
// with ErrBadHandshake; anything that drew no answer returns no answer and
// an error free of the credential.
func (p *pendingWS) upgrade(ctx context.Context, x HTTPExchange, a *HTTPStreamAuth, login bool, timeout time.Duration) (*websocket.Conn, *http.Response, error) {
	if p.trust != nil {
		x.TLS = p.trust // the scheme follows the trust the connection runs
	}
	req, err := BuildHTTPRequest(ctx, x)
	if err != nil {
		return nil, nil, err
	}
	u := *req.URL
	u.Scheme = "ws"
	if p.trust != nil {
		u.Scheme = "wss"
	}
	conn := p.conn
	given := func(context.Context, string, string) (net.Conn, error) { return conn, nil }
	d := websocket.Dialer{
		NetDialContext:    given,
		NetDialTLSContext: given, // the handshake already ran
		HandshakeTimeout:  timeout,
	}
	if login {
		d.Jar = a.State.cookieJar()
	}
	// The session's end cuts the handshake short; the dialer's own deadline
	// bounds it otherwise.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	ws, resp, err := d.DialContext(ctx, u.String(), req.Header)
	if err == nil {
		return ws, resp, nil
	}
	if errors.Is(err, websocket.ErrBadHandshake) && resp != nil {
		return nil, resp, err
	}
	return nil, nil, redactedRequestError(req, x, err, true)
}

// Open starts routing: from here the connection's frames reach the
// listeners and its exchanges.
func (p *pendingWS) Open(in Inbound, _ ChannelOptions) (HeldConn, error) {
	if p.ws == nil {
		return nil, errors.New("collection: the websocket opened with no connection")
	}
	c := &wsConn{
		ws: p.ws, target: p.target, in: in, ping: p.ping,
		asks: make(chan lineAsk, 1), frames: make(chan wsFrame, 16), done: make(chan struct{}),
	}
	go c.read()
	go c.route()
	go c.keepalive()
	return c, nil
}

func (p *pendingWS) Close() error {
	if p.conn == nil {
		return nil
	}
	return p.conn.Close()
}

// wsFrame is one inbound frame as the reader hands it to the router: its
// text, or a note of what was dropped (a binary frame, one past the bound).
type wsFrame struct {
	text    string
	dropped bool
}

// wsConn is one held websocket. xmu serializes exchanges, so at most one
// ask is pending; the router writes every data frame, the reader reads
// every frame, and the pinger writes only control frames, which the
// library lets any goroutine send.
type wsConn struct {
	ws     *websocket.Conn
	target string
	in     Inbound
	ping   time.Duration
	xmu    sync.Mutex
	asks   chan lineAsk
	frames chan wsFrame

	mu  sync.Mutex
	err error

	done      chan struct{}
	closeOnce sync.Once
}

// silence is how long the connection may hear nothing, neither a frame nor
// a pong, before it is dead: two pings unanswered.
func (c *wsConn) silence() time.Duration { return 2 * c.ping }

// read is the reader goroutine: frames in, bounded, to the router. Every
// frame and every pong extends the deadline a silent component dies at.
func (c *wsConn) read() {
	alive := func() { _ = c.ws.SetReadDeadline(time.Now().Add(c.silence())) }
	alive()
	c.ws.SetPongHandler(func(string) error { alive(); return nil })
	for {
		kind, r, err := c.ws.NextReader()
		if err != nil {
			c.fail(c.readCause(err))
			return
		}
		data, err := io.ReadAll(io.LimitReader(r, maxWebSocketFrame+1))
		if err == nil && len(data) > maxWebSocketFrame {
			var rest int64
			rest, err = io.Copy(io.Discard, r)
			data = fmt.Appendf(nil, "(a frame of %d bytes, past the %d byte bound, dropped)", int64(len(data))+rest, maxWebSocketFrame)
			kind = -1
		}
		if err != nil {
			c.fail(c.readCause(err))
			return
		}
		alive()
		f := wsFrame{text: trimLine(string(data))}
		switch kind {
		case websocket.TextMessage:
		case websocket.BinaryMessage:
			f = wsFrame{text: fmt.Sprintf("(a binary frame of %d bytes, which no listener reads)", len(data)), dropped: true}
		default:
			f.dropped = true
		}
		select {
		case c.frames <- f:
		case <-c.done:
			return
		}
	}
}

// readCause names why the reader stopped: the component's close, its
// silence, or the wire.
func (c *wsConn) readCause(err error) error {
	var ce *websocket.CloseError
	switch {
	case errors.As(err, &ce):
		return fmt.Errorf("the component closed the websocket (%d %s)", ce.Code, ce.Text)
	case isTimeout(err):
		return fmt.Errorf("the component sent nothing, not even a pong, within %s", c.silence())
	}
	return err
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// route is the router goroutine. It holds the armed ask, the exchange
// awaiting its answer, and hands every frame on by the line session's
// precedence.
func (c *wsConn) route() {
	var armed *lineAsk
	for {
		select {
		case <-c.done:
			return
		case f := <-c.frames:
			armed = c.deliver(f, armed, false)
		case ask := <-c.asks:
			// One ask at a time: an armed one still here gave up.
			armed = nil
			if !c.boundary(ask) {
				return
			}
			armed = &ask
		}
	}
}

// boundary draws an ask's boundary and sends its frame. Every frame already
// received is routed (to a listener, or unclaimed) as received before the
// ask, so a waiting push is delivered rather than lost and is never the
// answer; a short drain window catches frames already in flight. Then the
// frame goes out. A write failure answers the ask with the error and kills
// the connection.
func (c *wsConn) boundary(ask lineAsk) bool {
	t := time.NewTimer(drainWindow)
	defer t.Stop()
drain:
	for {
		select {
		case f := <-c.frames:
			c.deliver(f, nil, true)
		case <-t.C:
			break drain
		case <-c.done:
			ask.answer <- lineAnswer{err: fmt.Errorf("collection: websocket %s: %w", c.target, c.Err())}
			return false
		}
	}
	_ = c.ws.SetWriteDeadline(time.Now().Add(ask.timeout))
	if err := c.ws.WriteMessage(websocket.TextMessage, []byte(ask.line)); err != nil {
		ask.answer <- lineAnswer{err: fmt.Errorf("collection: websocket send %s: %w", c.target, err)}
		c.fail(err)
		return false
	}
	return true
}

// deliver routes one frame and returns the ask still armed. The precedence
// is a line session's: a frame a listener claims is the listener's, and an
// exchange is never handed it unless its reply shape matches it (then both
// have it); an armed exchange otherwise takes the first frame its answer
// shape matches (its own id, when correlated), or, with none declared, the
// first frame no listener claims; an unclaimed frame its error shape
// matches answers it as a refusal; a frame received before the ask is never
// its answer; whatever is left is unclaimed.
func (c *wsConn) deliver(f wsFrame, armed *lineAsk, pre bool) *lineAsk {
	msg := driver.LineMessage(f.text)
	if f.dropped {
		if c.in != nil {
			c.in.Unclaimed(msg)
		}
		return armed
	}
	claimed := c.in != nil && c.in.Claim(msg)
	if armed != nil && armed.abandoned() {
		armed = nil
	}
	if a := armed; a != nil && !pre {
		switch {
		case (a.reply != nil && a.reply.Accepts(f.text)) || (a.reply == nil && !claimed):
			a.answer <- lineAnswer{line: f.text} // buffered: never blocks
			return nil
		case !claimed && a.reply.Fails(f.text):
			a.answer <- lineAnswer{line: f.text, refused: true}
			return nil
		}
	}
	if !claimed && c.in != nil {
		c.in.Unclaimed(msg)
	}
	return armed
}

// keepalive is the pinger goroutine: a ping every interval, the reader's
// deadline catching a component that stopped answering.
func (c *wsConn) keepalive() {
	t := time.NewTicker(c.ping)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			if err := c.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(c.ping)); err != nil {
				c.fail(fmt.Errorf("ping: %w", err))
				return
			}
		}
	}
}

// fail records the first cause, says goodbye with a close frame, and closes
// the connection and Done.
func (c *wsConn) fail(err error) {
	c.closeOnce.Do(func() {
		if err == nil {
			err = io.EOF
		}
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		_ = c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		_ = c.ws.Close()
		close(c.done)
	})
}

func (c *wsConn) Done() <-chan struct{} { return c.done }

func (c *wsConn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *wsConn) Close() error {
	c.fail(net.ErrClosed)
	return nil
}

// Exchange hands one frame to the router and waits for its answer, as a
// line session's Exchange does: a shaped exchange (a correlated one
// included) that gets no answer in time fails alone, since its late answer
// can never be taken for another's; an unshaped one recycles the connection,
// since its late answer would answer the next.
func (c *wsConn) Exchange(ctx context.Context, frame string, reply *driver.Reply, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = defaultSessionTimeout
	}
	c.xmu.Lock()
	defer c.xmu.Unlock()
	select {
	case <-c.done:
		return "", fmt.Errorf("collection: websocket %s: %w", c.target, c.Err())
	default:
	}
	ask := lineAsk{line: frame, reply: reply, timeout: timeout, answer: make(chan lineAnswer, 1), gone: make(chan struct{})}
	select {
	case c.asks <- ask: // capacity 1, and xmu admits one ask at a time
	case <-c.done:
		return "", fmt.Errorf("collection: websocket %s: %w", c.target, c.Err())
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case a := <-ask.answer:
		if a.err != nil {
			return "", a.err
		}
		if a.refused {
			return "", &RefusedError{Request: frame, Line: a.line}
		}
		return a.line, nil
	case <-c.done:
		return "", fmt.Errorf("collection: websocket %s: %w", c.target, c.Err())
	case <-ctx.Done():
		if reply != nil {
			close(ask.gone)
			return "", fmt.Errorf("collection: websocket %s: %w", c.target, ctx.Err())
		}
		c.fail(fmt.Errorf("exchange abandoned (%v); connection recycled", ctx.Err()))
		return "", fmt.Errorf("collection: websocket %s: %w", c.target, ctx.Err())
	case <-t.C:
		if reply != nil {
			close(ask.gone)
			return "", fmt.Errorf("collection: websocket %s: %s got no answer within %s: %w", c.target, snippet([]byte(frame), nil), timeout, ErrNoAnswer)
		}
		c.fail(ErrExchangeTimeout)
		return "", fmt.Errorf("collection: websocket %s: no answer within %s: %w", c.target, timeout, ErrExchangeTimeout)
	}
}
