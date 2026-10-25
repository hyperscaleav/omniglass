package collection

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/redact"
)

// The http fetcher (#902): one real request a driver's poll or command
// sends, over plain http or over the shared TLS primitive (https), with the
// endpoint's credential applied by its declared scheme, and the answer
// judged by the status table the architect ruled (decision 5 of #1113). A
// credential is written only into the outgoing request: no error, reason or
// log line carries it, a query-parameter key is redacted from every URL the
// node names, and a component's text is scrubbed of the credential before it
// reaches a reason.

// DefaultBodyCap bounds the body a fetch reads: 1 MiB, ample for a status
// document or a status page, and a ceiling on what a component can make a
// node hold.
const DefaultBodyCap = 1 << 20

// HTTPExchange is one http request to send: the endpoint's target
// (host:port), its TLS trust (nil for plain http), the rendered request, the
// credential its session's scheme sends (nil for none), the session whose
// declared terminal rules classify the answer, and the bounds.
type HTTPExchange struct {
	Target     string
	TLS        *TLSTrust
	Request    driver.Request
	Credential *driver.HTTPCredential
	Session    *driver.SessionSpec
	Timeout    time.Duration
	BodyCap    int64
	// State is the endpoint's learned auth (#1114): the cookie jar the
	// component's cookies ride, the digest challenge a digest scheme reuses,
	// the headers a login's captures fill. Nil for none.
	State *HTTPAuthState
	// SessionHeaders are the headers a login's captures fill, sent on this
	// request (never on a login step's own).
	SessionHeaders map[string]string
	// Redact is every further value no error or reason may carry: a login's
	// secret fields, its captures, the component's cookies.
	Redact []string
}

// HTTPAnswer is what a request drew: the status, the body (capped), the
// Retry-After a component asked for, and whether the request carried a
// credential (what makes a 401 the credential's refusal).
type HTTPAnswer struct {
	Status       int
	Body         []byte
	RetryAfter   string
	Credentialed bool
	Location     string
}

// HTTPFetcher sends one http request, faked in unit tests. err is a request
// that drew no answer (a refused connect, a timeout, a certificate the trust
// refused, which is a TerminalError); an answer of any status is data, which
// JudgeHTTPAnswer reads.
type HTTPFetcher interface {
	Exchange(ctx context.Context, x HTTPExchange) (HTTPAnswer, error)
}

// NewHTTPFetcher returns the real http fetcher.
func NewHTTPFetcher() HTTPFetcher { return httpFetcher{} }

type httpFetcher struct{}

func (httpFetcher) Exchange(ctx context.Context, x HTTPExchange) (HTTPAnswer, error) {
	if x.Timeout <= 0 {
		x.Timeout = defaultTCPTimeout
	}
	if x.BodyCap <= 0 {
		x.BodyCap = DefaultBodyCap
	}
	dialer := &net.Dialer{Timeout: x.Timeout}
	tr := &http.Transport{DialContext: dialer.DialContext, DisableKeepAlives: true, ResponseHeaderTimeout: x.Timeout,
		MaxResponseHeaderBytes: maxResponseHeaderBytes}
	if x.TLS != nil {
		trust := *x.TLS
		tr.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			tc, _, err := HandshakeTLS(ctx, c, trust, x.Timeout)
			if err != nil {
				return nil, err
			}
			return tc, nil
		}
	}
	client := &http.Client{Transport: tr, Timeout: x.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	// Only a login session keeps the component's cookies (#1114): every
	// other request is its own, as it was before a login could open one.
	if x.State != nil && x.Session != nil && x.Session.Login != nil {
		client.Jar = x.State.cookieJar()
	}
	digest := x.Credential != nil && x.Credential.Scheme == driver.SchemeDigest
	var ds *DigestState
	if digest {
		if x.State == nil {
			x.State = NewHTTPAuthState()
		}
		ds = &x.State.digest
	}
	// A digest request answers the held challenge preemptively; a 401
	// carrying a challenge the answer did not meet (none held yet, the held
	// nonce called stale, or a nonce the component rotated without saying
	// so) is answered once more, and a 401 to an answer of the nonce it
	// still offers is the credential's refusal, which the status table
	// reads.
	for attempt := 0; ; attempt++ {
		req, err := BuildHTTPRequest(ctx, x)
		if err != nil {
			return HTTPAnswer{}, err
		}
		answered, nonce := false, ""
		if digest {
			if answered, nonce, err = ds.authorize(req, x.Credential.Username, x.Credential.Password); err != nil {
				return HTTPAnswer{}, err
			}
		}
		var wrote atomic.Bool
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
			WroteRequest: func(httptrace.WroteRequestInfo) { wrote.Store(true) },
		}))
		resp, err := client.Do(req)
		if err != nil {
			return HTTPAnswer{}, redactedRequestError(req, x, err, wrote.Load())
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, x.BodyCap+1))
		_ = resp.Body.Close()
		if err != nil {
			return HTTPAnswer{}, &httpTransportError{msg: fmt.Sprintf("collection: %s %s: reading the body: %s", req.Method, x.redactedURL(req.URL),
				Scrub(err.Error(), x.Credential, x.Redact...)), cause: err, sent: true}
		}
		if digest && resp.StatusCode == http.StatusUnauthorized {
			ch, ok := parseDigestChallenges(resp.Header)
			if ok && attempt < 2 && (!answered || ch.stale || ch.nonce != nonce) {
				ds.learn(ch)
				continue
			}
			if !answered {
				// The credential was never sent: the component offers no
				// challenge this node answers, which is no verdict on it.
				return HTTPAnswer{}, &RequestFault{Status: resp.StatusCode, Reason: "the component's 401 offers no digest challenge this node answers (" + offeredSchemes(resp.Header) + ")"}
			}
		}
		if int64(len(body)) > x.BodyCap {
			return HTTPAnswer{}, &RequestFault{Status: resp.StatusCode, Reason: fmt.Sprintf("the response body exceeds the cap of %d bytes", x.BodyCap)}
		}
		credentialed := x.Credential != nil
		if digest {
			credentialed = answered
		}
		return HTTPAnswer{Status: resp.StatusCode, Body: body, RetryAfter: resp.Header.Get("Retry-After"),
			Credentialed: credentialed, Location: resp.Header.Get("Location")}, nil
	}
}

// maxResponseHeaderBytes bounds the headers a component's answer may carry:
// a status document's headers are small, and a hostile component cannot make
// a node hold megabytes of cookies or challenges.
const maxResponseHeaderBytes = 64 << 10

// offeredSchemes names the auth schemes a 401's challenges offer, never
// their parameters.
func offeredSchemes(h http.Header) string {
	var out []string
	for _, v := range h.Values("WWW-Authenticate") {
		for _, ch := range splitChallenges(v) {
			scheme := strings.Fields(ch)
			if len(scheme) > 0 && len(scheme[0]) <= 32 && !slices.Contains(out, scheme[0]) {
				out = append(out, scheme[0])
			}
		}
	}
	if len(out) == 0 {
		return "none offered"
	}
	return "it offers " + strings.Join(out, ", ")
}

// String names an exchange by its method and path, never a value it
// carries: the credential, the session headers and the redactions are
// secrets.
func (x HTTPExchange) String() string {
	return fmt.Sprintf("http exchange %s %s to %s", x.Request.MethodOf(), x.Request.Path, x.Target)
}

// GoString is String: a %#v names no secret either.
func (x HTTPExchange) GoString() string { return x.String() }

// LogValue is String, for a log line.
func (x HTTPExchange) LogValue() slog.Value { return slog.StringValue(x.String()) }

// BuildHTTPRequest builds the request an exchange sends: the URL from the
// target, the path and the query (the api key added where it rides), the
// declared headers, the body with its media type, and the credential. Pure
// but for the context it carries.
func BuildHTTPRequest(ctx context.Context, x HTTPExchange) (*http.Request, error) {
	scheme := "http"
	if x.TLS != nil {
		scheme = "https"
	}
	r := x.Request
	path := r.Path
	if path == "" {
		path = "/"
	}
	u, err := url.Parse(scheme + "://" + x.Target + path)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("collection: http request to %s: the target and path do not form a url", x.Target)
	}
	q := url.Values{}
	for k, v := range r.Query {
		q.Set(k, v)
	}
	c := x.Credential
	if c != nil && c.Scheme == driver.SchemeAPIKey && c.In == driver.APIKeyInQuery {
		q.Set(c.Name, c.Key)
	}
	u.RawQuery = q.Encode()
	var body io.Reader
	encoded, err := r.BodyBytes()
	if err != nil {
		return nil, fmt.Errorf("collection: http request %s: %v", x.redactedURL(u), Scrub(err.Error(), c, x.Redact...))
	}
	if len(encoded) > 0 {
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, r.MethodOf(), u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("collection: http request %s: %v", x.redactedURL(u), Scrub(fmt.Sprint(errors.Unwrap(err)), c, x.Redact...))
	}
	req.Header.Set("User-Agent", "omniglass-node")
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}
	for k, v := range x.SessionHeaders {
		req.Header.Set(k, v)
	}
	if len(r.Body) > 0 {
		req.Header.Set("Content-Type", r.ContentTypeOf())
	}
	if c != nil {
		switch c.Scheme {
		case driver.SchemeBasic:
			req.SetBasicAuth(c.Username, c.Password)
		case driver.SchemeBearer:
			req.Header.Set("Authorization", "Bearer "+c.Token)
		case driver.SchemeAPIKey:
			if c.In == driver.APIKeyInHeader {
				req.Header.Set(c.Name, c.Key)
			}
		}
	}
	return req, nil
}

// RedactedURL renders a request's URL with a query-parameter api key masked:
// the form every error and log line names it in.
func RedactedURL(u *url.URL, c *driver.HTTPCredential) string {
	if u == nil {
		return ""
	}
	cp := *u
	if c != nil && c.Scheme == driver.SchemeAPIKey && c.In == driver.APIKeyInQuery {
		q := cp.Query()
		if q.Has(c.Name) {
			q.Set(c.Name, "[redacted]")
			cp.RawQuery = q.Encode()
		}
	}
	cp.User = nil
	return cp.String()
}

// redactedRequestError renders a request that drew no answer without the
// URL the client's error would carry (a query key rides it): the method,
// the redacted URL, and the cause. A TerminalError (a certificate the trust
// refused) is kept whole, so the gate reads its class and evidence.
func redactedRequestError(req *http.Request, x HTTPExchange, err error, sent bool) error {
	if terr, ok := AsTerminal(err); ok {
		return terr
	}
	cause := err
	var uerr *url.Error
	if errors.As(err, &uerr) {
		cause = uerr.Err
	}
	return &httpTransportError{msg: fmt.Sprintf("collection: %s %s: %s", req.Method, x.redactedURL(req.URL), Scrub(strings.ToValidUTF8(cause.Error(), "\uFFFD"), x.Credential, x.Redact...)), cause: cause, sent: sent}
}

// redactedURL is RedactedURL scrubbed of every further secret the exchange
// carries: a login step may render a secret field into its path or query.
func (x HTTPExchange) redactedURL(u *url.URL) string {
	return Scrub(RedactedURL(u, x.Credential), x.Credential, x.Redact...)
}

// httpTransportError is a request that drew no answer, its message free of
// the URL's credential, its cause kept for classification.
type httpTransportError struct {
	msg   string
	cause error
	// sent is whether the request was written before the connection
	// failed: a login's secrets reached the component.
	sent bool
}

func (e *httpTransportError) Error() string { return e.msg }
func (e *httpTransportError) Unwrap() error { return e.cause }

// Scrub masks every form of the credential a component could echo back in
// its text (the password, the token, the key, the basic authorization's
// base64, in every form redact.Secrets knows), so it never reaches an error
// or a reason. It runs on the whole text before anything collapses or cuts
// it, so no transform can hide a form from it. extra are further secrets the
// request carried (a login's secret fields, its captures, the component's
// cookies), masked in the same forms.
func Scrub(text string, c *driver.HTTPCredential, extra ...string) string {
	if c == nil && len(extra) == 0 {
		return text
	}
	var secrets []string
	if c != nil {
		secrets = append(secrets, c.Password, c.Token, c.Key)
		if c.Scheme == driver.SchemeBasic {
			secrets = append(secrets, redact.BasicAuth(c.Username, c.Password))
		}
	}
	return redact.Secrets(text, append(secrets, extra...)...)
}

// RequestFault is an answer that faults the request itself (decision 5 of
// #1113): a 403, a 400 or 404 the spec or the firmware must fix, a redirect
// not followed, a body over the cap. Retrying cannot fix it, so a poll waits
// its interval rather than backing off; but the connection is fine, so it
// gates nothing.
type RequestFault struct {
	Status int
	Reason string
}

func (e *RequestFault) Error() string {
	if e.Status == 0 {
		return "http: " + e.Reason
	}
	return fmt.Sprintf("http %d: %s", e.Status, e.Reason)
}

// AsRequestFault reports whether err is, or wraps, a RequestFault.
func AsRequestFault(err error) (*RequestFault, bool) {
	var f *RequestFault
	if errors.As(err, &f) {
		return f, true
	}
	return nil, false
}

// RetryableAnswer is an answer retrying may fix (a 408, a 429, a 5xx): the
// poll backs off, never sooner than the Retry-After the component asked for.
type RetryableAnswer struct {
	Status     int
	Reason     string
	RetryAfter time.Duration
}

func (e *RetryableAnswer) Error() string { return fmt.Sprintf("http %d: %s", e.Status, e.Reason) }

// RetryAfterOf is the wait a retryable answer asked for, if any.
func RetryAfterOf(err error) (time.Duration, bool) {
	var r *RetryableAnswer
	if errors.As(err, &r) && r.RetryAfter > 0 {
		return r.RetryAfter, true
	}
	return 0, false
}

// snippetLimit bounds the component's text a reason carries.
const snippetLimit = 200

// snippet is the start of a body as one line of valid UTF-8, scrubbed of
// the credential first (on the whole text, before whitespace collapses and
// the cut), then cut on a character boundary, so a reason is always a string
// a protobuf message carries and never carries a credential.
func snippet(body []byte, c *driver.HTTPCredential, extra ...string) string {
	s := Scrub(strings.ToValidUTF8(string(body), "\uFFFD"), c, extra...)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > snippetLimit {
		s = string(r[:snippetLimit]) + "..."
	}
	return s
}

// JudgeHTTPAnswer reads an answer by the status table (decision 5 of #1113):
// a rule the driver declares terminal decides first; then 2xx is success; a
// 401 to a credentialed request is the credential refused (a terminal auth);
// 408, 429 and 5xx are retryable, honouring Retry-After; a redirect is not
// followed; and every other 4xx (a 403 among them, unless declared) faults
// the request alone. Pure.
func JudgeHTTPAnswer(a HTTPAnswer, x HTTPExchange) error {
	text := snippet(a.Body, x.Credential, x.Redact...)
	if class, ok := x.Session.TerminalForResponse(a.Status, Scrub(strings.ToValidUTF8(string(a.Body), "\uFFFD"), x.Credential, x.Redact...)); ok {
		reason := fmt.Sprintf("the component answered %d", a.Status)
		if text != "" {
			reason += ": " + text
		}
		return &TerminalError{Class: class, Reason: reason}
	}
	// DoHTTP has logged in once more and replayed by the time an answer is
	// judged, so a lapsed session here followed a fresh login (#1114).
	if why, lapsed := SessionLapse(a, x); lapsed {
		return &TerminalError{Class: driver.ClassAuth, Reason: "the component refused the session a fresh login opened (" + why + ")"}
	}
	switch {
	case a.Status >= 200 && a.Status < 300:
		return nil
	case a.Status == http.StatusUnauthorized && a.Credentialed:
		return &TerminalError{Class: driver.ClassAuth, Reason: "the component refused the credential (401)"}
	case a.Status == http.StatusUnauthorized:
		return &RequestFault{Status: a.Status, Reason: "the component wants a credential, and the driver declares none"}
	case a.Status == http.StatusRequestTimeout || a.Status == http.StatusTooManyRequests || a.Status >= 500:
		return &RetryableAnswer{Status: a.Status, Reason: nonEmpty(text, http.StatusText(a.Status)), RetryAfter: parseRetryAfter(a.RetryAfter, time.Now())}
	case a.Status >= 300 && a.Status < 400:
		where := "elsewhere"
		if a.Location != "" {
			where = "to " + snippet([]byte(a.Location), x.Credential, x.Redact...)
		}
		return &RequestFault{Status: a.Status, Reason: "the component redirected " + where + ", and a request does not follow a redirect"}
	default:
		return &RequestFault{Status: a.Status, Reason: nonEmpty(text, http.StatusText(a.Status))}
	}
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// parseRetryAfter reads a Retry-After as seconds or an http date; zero when
// absent or unreadable.
func parseRetryAfter(v string, now time.Time) time.Duration {
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

// Authenticated is the session rung an answer proves (decision 7 of #1113):
// a credentialed 2xx says yes; a credentialed refusal (a 401, or a rule the
// driver declares auth) says no; anything else (a redirect, a 403, another
// 4xx, a 5xx) never judged the credential and proves nothing. ok is false
// when it proves nothing.
func Authenticated(a HTTPAnswer, judged error) (verdict string, ok bool) {
	if !a.Credentialed {
		return "", false
	}
	if terr, isTerm := AsTerminal(judged); isTerm && terr.Class == driver.ClassAuth {
		return "no", true
	}
	if a.Status >= 200 && a.Status < 300 {
		return "yes", true
	}
	return "", false
}
