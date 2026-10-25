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

	// loginMu serializes logins, so callers that find the session lapsed
	// together open one session between them.
	loginMu sync.Mutex

	// OnLogin is told of each login the state starts and why ("opening",
	// "renewing before expiry", "the session lapsed"), never a value: the
	// node's log line. Set before the state is shared.
	OnLogin func(why string)
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

// maxRecordedCookies bounds what a component can make the jar remember for
// the scrub: past it, the oldest names are not tracked (the jar itself still
// holds them).
const maxRecordedCookies = 64

func newRecordingJar() *recordingJar {
	// A jar with no public suffix list: it only ever holds one endpoint's
	// cookies, for one host.
	j, _ := cookiejar.New(nil)
	return &recordingJar{CookieJar: j, seen: map[string]string{}}
}

func (j *recordingJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	for _, c := range cookies {
		if _, ok := j.seen[c.Name]; ok || len(j.seen) < maxRecordedCookies {
			j.seen[c.Name] = c.Value
		}
	}
	j.mu.Unlock()
	j.CookieJar.SetCookies(u, cookies)
}

func (j *recordingJar) values() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]string, 0, len(j.seen))
	for _, v := range j.seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

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
	gen, headers, ok := s.current(time.Now())
	if !ok {
		why := "opening"
		if gen > 0 {
			why = "renewing before expiry"
		}
		if err := r.ensureLogin(ctx, x, secrets, gen, why); err != nil {
			return HTTPAnswer{}, x, err
		}
		opened = true
		if gen, headers, ok = s.current(time.Now()); !ok {
			return HTTPAnswer{}, x, errors.New("collection: the http login opened no session")
		}
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
		if err := r.ensureLogin(ctx, x, secrets, gen, "the session lapsed"); err != nil {
			return HTTPAnswer{}, sx, err
		}
		opened = true
		if gen, headers, ok = s.current(time.Now()); !ok {
			return HTTPAnswer{}, sx, errors.New("collection: the http login opened no session")
		}
	}
}

// ensureLogin opens a session unless another caller opened one since gen;
// a refusal a login met under this state is returned again, never a second
// attempt (the login budget: one try per fingerprint).
func (r *Runner) ensureLogin(ctx context.Context, x HTTPExchange, secrets map[string]map[string]string, gen uint64, why string) error {
	s := x.State
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	s.mu.Lock()
	refused := s.refused
	s.mu.Unlock()
	if refused != nil {
		return refused
	}
	if g, _, ok := s.current(time.Now()); ok && g != gen {
		return nil
	}
	err := r.httpLogin(ctx, x, secrets, why)
	if _, terminal := AsTerminal(err); terminal {
		s.mu.Lock()
		s.refused = err
		s.mu.Unlock()
	}
	return err
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
		if err := judgeLoginAnswer(ans, lx, i == len(l.Steps)-1); err != nil {
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
		values[c.Name] = v
		redact = append(redact, v)
	}
	headers, err := l.SessionHeaders(values)
	if err != nil {
		return &TerminalError{Class: driver.ClassAuth, Reason: "the login's captured value cannot ride a header: " + Scrub(err.Error(), nil, redact...)}
	}
	var renewAt time.Time
	if l.Expiry != nil {
		v, err := driver.ExtractValue(*l.Expiry, p)
		if err != nil {
			return &TerminalError{Class: driver.ClassAuth, Reason: "the login answered without the session's expiry it declares"}
		}
		d, err := driver.ParseExpiry(v)
		if err != nil {
			return &TerminalError{Class: driver.ClassAuth, Reason: Scrub(err.Error(), nil, redact...)}
		}
		renewAt = time.Now().Add(time.Duration(float64(d) * renewFraction))
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
