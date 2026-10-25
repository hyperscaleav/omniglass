package collection

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// http sessions (#1114, slice 2 of #1113): a driver whose session declares
// an http login runs it before its first request, keeps what it opened (the
// component's cookies, the values it captured, the headers they fill), and
// sends every later request inside it. A request the component answers as
// a lapsed session (a 401, a redirect to the declared login page, the
// declared expired text) logs in once more and is replayed once; a lapse
// right after a fresh login is the credential's terminal refusal, never a
// second login. A login the component refuses is held as refused for the
// life of the state, so no later request logs in again under it: the node
// drops the state when the endpoint's fingerprint moves, and the gate holds
// every request until then.

// HTTPAuthState is one http endpoint's learned auth: the cookies the
// component set, the digest challenge it last issued, and a login's
// session. It lives in the node's memory alone (decision 3 of #1113):
// never logged, published or persisted, dropped whole when the endpoint's
// fingerprint moves, on a terminal refusal, and with the process. Safe for
// concurrent use: a poll and a command share it, and one login serves both.
type HTTPAuthState struct {
	mu       sync.Mutex
	jar      *recordingJar
	digest   DigestState
	headers  map[string]string
	redact   []string
	loggedIn bool
	renewAt  time.Time
	gen      uint64
	refused  error
	logins   int
	// failed counts the logins in a row that drew no verdict after the
	// secrets were sent (a 5xx, a dropped connection): past loginBudget,
	// the refusal the component never said.
	failed int

	// loginMu serializes logins, so callers that find the session lapsed
	// together open one session between them.
	loginMu sync.Mutex

	// OnLogin is told of each login the state starts and why ("opening",
	// "renewing before expiry", "the session lapsed"), never a value: the
	// node's log line. Set before the state is shared.
	OnLogin func(why string)

	// now is the clock a session's lifetime is read against; time.Now
	// unless a test sets one (SetClock).
	now func() time.Time
}

// SetClock sets the clock a session's declared lifetime is read against,
// for a test that renews a session without waiting out its lifetime. Set
// before the state is shared.
func (s *HTTPAuthState) SetClock(now func() time.Time) { s.now = now }

func (s *HTTPAuthState) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

// NewHTTPAuthState is an empty state: no cookies, no challenge, no session.
func NewHTTPAuthState() *HTTPAuthState {
	return &HTTPAuthState{jar: newRecordingJar()}
}

// String never prints a value the state holds.
func (s *HTTPAuthState) String() string {
	return fmt.Sprintf("http auth state (%d logins)", s.Logins())
}

// Logins is how many logins the state has started, passed or refused.
func (s *HTTPAuthState) Logins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins
}

// cookieJar is the jar the next request rides.
func (s *HTTPAuthState) cookieJar() http.CookieJar {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jar
}

// redactions is every value the state holds that no error or reason may
// carry: the secrets the login sent, the values it captured, the cookies
// the component set.
func (s *HTTPAuthState) redactions() []string {
	s.mu.Lock()
	out := append([]string(nil), s.redact...)
	jar := s.jar
	s.mu.Unlock()
	return append(out, jar.values()...)
}

// current is the held session: its generation, and the headers it sends;
// ok is false when none is held (never opened, lapsed, or due for renewal).
func (s *HTTPAuthState) current(now time.Time) (gen uint64, headers map[string]string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loggedIn || (!s.renewAt.IsZero() && !now.Before(s.renewAt)) {
		return s.gen, nil, false
	}
	return s.gen, s.headers, true
}

// held is the session the state holds, due for renewal or not: what the
// request a login just opened rides, however short the lifetime it declared.
func (s *HTTPAuthState) held() (gen uint64, headers map[string]string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen, s.headers, s.loggedIn
}

// lapse drops the session opened at gen; a session another caller opened
// since is kept.
func (s *HTTPAuthState) lapse(gen uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen == gen {
		s.loggedIn, s.headers = false, nil
	}
}

// recordingJar is the endpoint's cookie jar, which remembers the latest
// value of each cookie the component set, so a reason that echoes one is
// scrubbed of it.
type recordingJar struct {
	http.CookieJar
	mu   sync.Mutex
	seen map[string]string
}

// The bounds on what a component can make an endpoint's jar hold: a cookie
// past maxCookieValue (RFC 6265's floor for what a client stores is 4096
// bytes for the whole cookie) or past maxCookies distinct cookies is
// dropped, never stored, sent back or remembered.
const (
	maxCookies     = 64
	maxCookieValue = 4096
)

func newRecordingJar() *recordingJar {
	// A jar with no public suffix list: it only ever holds one endpoint's
	// cookies, for one host.
	j, _ := cookiejar.New(nil)
	return &recordingJar{CookieJar: j, seen: map[string]string{}}
}

func (j *recordingJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	kept := cookies[:0:0]
	for _, c := range cookies {
		key := c.Name + "\x00" + c.Domain + "\x00" + c.Path
		if _, ok := j.seen[key]; (!ok && len(j.seen) >= maxCookies) || len(c.Name)+len(c.Value) > maxCookieValue {
			continue
		}
		j.seen[key] = c.Value
		kept = append(kept, c)
	}
	j.mu.Unlock()
	if len(kept) > 0 {
		j.CookieJar.SetCookies(u, kept)
	}
}

// minCookieSecret is the shortest cookie value treated as a secret: a
// session id, even embedded gear's numeric one, is longer, and masking a
// shorter value (a language's "en", a flag's "1") would mangle every reason
// and hide nothing.
const minCookieSecret = 4

func (j *recordingJar) values() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]string, 0, len(j.seen))
	for _, v := range j.seen {
		if len(v) >= minCookieSecret {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// maxCaptureLen bounds a captured value: it rides a header on every later
// request, and a token or session id is far shorter.
const maxCaptureLen = 8 << 10

// loginBudget is how many logins in a row may draw no verdict after the
// secrets were sent (a 5xx, a dropped connection) before the endpoint reads
// rejected, as a line session's login budget counts a component hanging up
// on a wrong password without saying so: a component that fails a wrong
// password that way is not tried on every poll.
const loginBudget = 3

// renewFraction is how much of a declared session lifetime is used before
// the session is renewed: a tenth is left as margin for the clock and the
// round trip.
const renewFraction = 0.9

// DoHTTP sends one driver request with its endpoint's auth (#1114): over a
// login session, opening one first when none is held, and logging in once
// more then replaying once when the answer reads as a lapsed session. It
// returns the answer and the exchange as last sent (its session headers and
// redactions set), which JudgeHTTPAnswer reads; err is a request that drew
// no answer, or a login that failed (a TerminalError when the component
// refused it). Without a login it is one Exchange.
func (r *Runner) DoHTTP(ctx context.Context, x HTTPExchange, secrets map[string]map[string]string) (HTTPAnswer, HTTPExchange, error) {
	if r.HTTPFetch == nil {
		return HTTPAnswer{}, x, errors.New("collection: no http fetcher wired")
	}
	if x.Session == nil || x.Session.Login == nil {
		ans, err := r.HTTPFetch.Exchange(ctx, x)
		return ans, x, err
	}
	if x.State == nil {
		// A caller holding no state still logs in, once for this request.
		x.State = NewHTTPAuthState()
	}
	s := x.State
	opened := false
	gen, headers, ok := s.current(s.clock())
	if !ok {
		why := "opening"
		if gen > 0 {
			why = "renewing before expiry"
		}
		var err error
		if gen, headers, err = r.ensureLogin(ctx, x, secrets, gen, why); err != nil {
			return HTTPAnswer{}, x, err
		}
		opened = true
	}
	for {
		sx := x
		sx.SessionHeaders, sx.Redact = headers, append(append([]string(nil), x.Redact...), s.redactions()...)
		ans, err := r.HTTPFetch.Exchange(ctx, sx)
		if err != nil {
			return ans, sx, err
		}
		ans.Credentialed = true
		sx.Redact = append(sx.Redact, s.redactions()...)
		if _, lapsed := SessionLapse(ans, sx); !lapsed || opened {
			// A lapse right after a login opened the session is left in the
			// answer, which JudgeHTTPAnswer reads as the terminal refusal.
			return ans, sx, nil
		}
		s.lapse(gen)
		if gen, headers, err = r.ensureLogin(ctx, x, secrets, gen, "the session lapsed"); err != nil {
			return HTTPAnswer{}, sx, err
		}
		opened = true
	}
}

// ensureLogin opens a session unless another caller opened one since gen,
// and returns the session held; a refusal a login met under this state is
// returned again, never a second attempt (one try per fingerprint), and
// logins that draw no verdict after the secrets were sent are counted
// against loginBudget.
func (r *Runner) ensureLogin(ctx context.Context, x HTTPExchange, secrets map[string]map[string]string, gen uint64, why string) (uint64, map[string]string, error) {
	s := x.State
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	s.mu.Lock()
	refused := s.refused
	s.mu.Unlock()
	if refused != nil {
		return 0, nil, refused
	}
	if g, h, ok := s.current(s.clock()); ok && g != gen {
		return g, h, nil
	}
	err := r.httpLogin(ctx, x, secrets, why)
	s.mu.Lock()
	defer s.mu.Unlock()
	switch _, terminal := AsTerminal(err); {
	case err == nil:
		s.failed = 0
		return s.gen, s.headers, nil
	case terminal:
		s.refused = err
	case unjudgedLogin(err):
		s.failed++
		if s.failed >= loginBudget {
			s.refused = &TerminalError{Class: driver.ClassAuth, Reason: fmt.Sprintf("the login failed %d times in a row after the secrets were sent, and the component never said why: %s", s.failed, loginCause(err))}
			return 0, nil, s.refused
		}
	}
	return 0, nil, err
}

// unjudgedLogin reports a failed login whose secrets reached the component
// with no verdict back: an answer that backs off (a 5xx, a 408, a 429), or
// a connection that ended after it was made. A connection never made, or a
// credential never delivered, sent nothing and charges nothing.
func unjudgedLogin(err error) bool {
	var retry *RetryableAnswer
	if errors.As(err, &retry) {
		return true
	}
	var transport *httpTransportError
	return errors.As(err, &transport) && transport.sent && !errors.Is(err, context.Canceled)
}

// loginCause is a failed login's cause without its wrapping.
func loginCause(err error) string {
	var retry *RetryableAnswer
	if errors.As(err, &retry) {
		return retry.Error()
	}
	var transport *httpTransportError
	if errors.As(err, &transport) && transport.cause != nil {
		return transport.cause.Error()
	}
	return err.Error()
}

// httpLogin runs the login's steps on a fresh jar, judges each answer,
// captures from the last, and opens the session.
func (r *Runner) httpLogin(ctx context.Context, x HTTPExchange, secrets map[string]map[string]string, why string) error {
	s := x.State
	l := x.Session.Login
	s.mu.Lock()
	s.logins++
	s.loggedIn, s.headers, s.renewAt = false, nil, time.Time{}
	s.jar = newRecordingJar()
	s.mu.Unlock()
	if s.OnLogin != nil {
		s.OnLogin(why)
	}
	var used []string
	var ans HTTPAnswer
	var lx HTTPExchange
	for i, st := range l.Steps {
		req, vals, err := driver.RenderLoginRequest(*st.Request, secrets)
		if err != nil {
			return fmt.Errorf("collection: login step %d: %w", i+1, err)
		}
		used = append(used, vals...)
		lx = HTTPExchange{Target: x.Target, TLS: x.TLS, Request: req, Session: x.Session, Timeout: x.Timeout, BodyCap: x.BodyCap, State: s,
			Redact: append(append([]string(nil), used...), s.redactions()...)}
		if ans, err = r.HTTPFetch.Exchange(ctx, lx); err != nil {
			return fmt.Errorf("collection: login step %d: %w", i+1, err)
		}
		ans.Credentialed = true
		lx.Redact = append(lx.Redact, s.redactions()...)
		last := i == len(l.Steps)-1
		if last {
			// What the login would capture is a secret before it is judged:
			// a refusal's reason quotes the answer that carries it.
			lx.Redact = append(lx.Redact, captured(l, ans, lx.Request)...)
		}
		if err := judgeLoginAnswer(ans, lx, last); err != nil {
			return fmt.Errorf("collection: login step %d: %w", i+1, err)
		}
	}
	values := map[string]string{}
	var redact []string
	p := driver.HTTPPayload(ans.Body, &lx.Request)
	for _, c := range l.Capture {
		v, err := driver.ExtractValue(c.Extract, p)
		if err != nil || v == "" {
			return &TerminalError{Class: driver.ClassAuth, Reason: fmt.Sprintf("the login answered without the %s it captures, so it opened no session", c.Name)}
		}
		if len(v) > maxCaptureLen {
			return &TerminalError{Class: driver.ClassAuth, Reason: fmt.Sprintf("the login's %s is longer than %d bytes, more than any header carries", c.Name, maxCaptureLen)}
		}
		values[c.Name] = v
		redact = append(redact, v)
	}
	headers, err := l.SessionHeaders(values)
	if err != nil {
		return &TerminalError{Class: driver.ClassAuth, Reason: "the login's captured value cannot ride a header: " + Scrub(err.Error(), nil, redact...)}
	}
	// An expiry the answer does not carry, or carries as no lifetime (zero,
	// a word), declares none: the session lasts until a request finds it
	// lapsed. Its value is never quoted.
	var renewAt time.Time
	if l.Expiry != nil {
		if v, err := driver.ExtractValue(*l.Expiry, p); err == nil {
			if d, ok := driver.ParseExpiry(v); ok {
				renewAt = s.clock().Add(time.Duration(float64(d) * renewFraction))
			}
		}
	}
	s.mu.Lock()
	s.loggedIn, s.headers, s.renewAt = true, headers, renewAt
	s.redact = append(used, redact...)
	s.gen++
	s.mu.Unlock()
	return nil
}

// judgeLoginAnswer reads one login step's answer. A rule the driver
// declares terminal, a 401, a redirect back to the login page, any other
// refusal of the request (a 4xx) or, at the last step, a 2xx without the
// declared ready text is the component refusing the login: terminal, so it
// is tried once per fingerprint and never again in a loop that could lock
// the account out. A 5xx, a 408 or a 429 is retryable; a redirect elsewhere
// passes (a form login's way of saying yes).
func judgeLoginAnswer(a HTTPAnswer, x HTTPExchange, last bool) error {
	l := x.Session.Login
	text := snippet(a.Body, nil, x.Redact...)
	if class, ok := x.Session.TerminalForResponse(a.Status, Scrub(strings.ToValidUTF8(string(a.Body), "�"), nil, x.Redact...)); ok {
		return &TerminalError{Class: class, Reason: refusedLogin(a.Status, text)}
	}
	switch {
	case a.Status >= 300 && a.Status < 400:
		if l.Path != "" && locationPath(a.Location) == l.Path {
			return &TerminalError{Class: driver.ClassAuth, Reason: fmt.Sprintf("the login was sent back to the login page (%d to %s)", a.Status, l.Path)}
		}
		return nil
	case a.Status == http.StatusRequestTimeout || a.Status == http.StatusTooManyRequests || a.Status >= 500:
		return &RetryableAnswer{Status: a.Status, Reason: nonEmpty(text, http.StatusText(a.Status)), RetryAfter: parseRetryAfter(a.RetryAfter, time.Now())}
	case a.Status >= 400:
		return &TerminalError{Class: driver.ClassAuth, Reason: refusedLogin(a.Status, text)}
	case a.Status < 200:
		return &RequestFault{Status: a.Status, Reason: http.StatusText(a.Status)}
	}
	if last && (l.Ready.Prefix != "" || l.Ready.Regex != "") && !l.Ready.Matches(string(a.Body)) {
		reason := "the login answered without its ready text"
		if text != "" {
			reason += ": " + text
		}
		return &TerminalError{Class: driver.ClassAuth, Reason: reason}
	}
	return nil
}

// captured is every value a login's captures locate in an answer, read
// best-effort, for the scrub.
func captured(l *driver.Login, a HTTPAnswer, req driver.Request) []string {
	if len(l.Capture) == 0 {
		return nil
	}
	p := driver.HTTPPayload(a.Body, &req)
	var out []string
	for _, c := range l.Capture {
		if v, err := driver.ExtractValue(c.Extract, p); err == nil && v != "" {
			out = append(out, v)
		}
	}
	return out
}

func refusedLogin(status int, text string) string {
	reason := fmt.Sprintf("the component refused the login (%d)", status)
	if text != "" {
		reason += ": " + text
	}
	return reason
}

// locationPath is a Location's path, absolute or relative; empty when it
// does not parse.
func locationPath(loc string) string {
	u, err := url.Parse(strings.TrimSpace(loc))
	if err != nil {
		return ""
	}
	return u.Path
}

// SessionLapse reads a credentialed answer over a login session as the
// session having lapsed (#1114): a 401, a redirect to the declared login
// page, or the declared expired text. why names which, never a value.
func SessionLapse(a HTTPAnswer, x HTTPExchange) (why string, lapsed bool) {
	if x.Session == nil || x.Session.Login == nil || !a.Credentialed {
		return "", false
	}
	l := x.Session.Login
	switch {
	case a.Status == http.StatusUnauthorized:
		return "401", true
	case a.Status >= 300 && a.Status < 400 && l.Path != "" && locationPath(a.Location) == l.Path:
		return fmt.Sprintf("%d to the login page %s", a.Status, l.Path), true
	case l.Expired != nil && l.Expired.Matches(strings.ToValidUTF8(string(a.Body), "�")):
		return fmt.Sprintf("%d with the declared expired text", a.Status), true
	}
	return "", false
}
