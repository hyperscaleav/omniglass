package driver

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// http logins (#1114, slice 2 of epic #1113): the login dialog #1061
// shipped, carried over to http. Each step is a request naming the secret
// fields it sends (${secret.<input>.<field>}); the last answer passes the
// login unless it refuses (a 401, a redirect back to the login page, a rule
// the driver declares terminal) or misses the declared ready match. What the
// answer carries is captured through the one extraction primitive and rides
// every later request under the declared headers (${session.<capture>}),
// beside the cookies the component set, kept in a jar per endpoint in the
// node's memory alone. Expiry reads how long the session lasts, so a token
// is renewed before it lapses: the shape an OAuth2 client-credentials grant
// takes too (#1119), a request step, a captured token, an expiry.

// Capture is one value a login's last answer carries, named for the
// headers that send it.
type Capture struct {
	Name    string  `json:"name"`
	Extract Extract `json:"extract"`
}

// captureName is a capture's name: a lowercase word, as a template names it.
var captureName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// loginHeaderReserved are the headers a login may not set: the cookie (the
// jar sends the component's own) and the ones the client owns.
var loginHeaderReserved = []string{"cookie", "host", "content-length", "content-type", "transfer-encoding", "connection",
	"proxy-authorization", "user-agent", "expect", "te", "upgrade"}

// validateHTTPLogin refuses an http login no node could run.
func (s *Spec) validateHTTPLogin() error {
	l := s.Session.Login
	if len(l.Steps) == 0 {
		return fmt.Errorf("driver: the http login declares no step")
	}
	var last Request
	for i, st := range l.Steps {
		what := fmt.Sprintf("login step %d", i+1)
		if st.Request == nil {
			return fmt.Errorf("driver: %s declares no request; an http login step is a request", what)
		}
		if st.Send != "" || st.Expect.Prefix != "" || st.Expect.Regex != "" {
			return fmt.Errorf("driver: %s declares a prompt and a send, which only a line login has", what)
		}
		if err := s.validateLoginRequest(what, *st.Request); err != nil {
			return err
		}
		last = *st.Request
	}
	if l.Ready != (Match{}) {
		if err := validateMatch("login ready", l.Ready); err != nil {
			return err
		}
	}
	if l.Path != "" && (!strings.HasPrefix(l.Path, "/") || strings.ContainsAny(l.Path, "?#")) {
		return fmt.Errorf("driver: login path %q is a path that starts with / and carries no query", l.Path)
	}
	if l.Expired != nil {
		if err := validateMatch("login expired", *l.Expired); err != nil {
			return err
		}
	}
	captured := map[string]bool{}
	for _, c := range l.Capture {
		if !captureName.MatchString(c.Name) {
			return fmt.Errorf("driver: login capture name %q is a lowercase word", c.Name)
		}
		if captured[c.Name] {
			return fmt.Errorf("driver: login captures %q twice", c.Name)
		}
		captured[c.Name] = true
		if err := validateExtract("login", Emit{Name: c.Name, Extract: c.Extract}); err != nil {
			return err
		}
		if err := checkHTTPEmit(fmt.Sprintf("login capture %q", c.Name), c.Extract, last); err != nil {
			return err
		}
	}
	if l.Expiry != nil {
		if err := validateExtract("login", Emit{Name: "expiry", Extract: *l.Expiry}); err != nil {
			return err
		}
		if err := checkHTTPEmit("login expiry", *l.Expiry, last); err != nil {
			return err
		}
	}
	for name, v := range l.Headers {
		if !isToken(name) {
			return fmt.Errorf("driver: login header name %q is not an http token", name)
		}
		if slices.Contains(loginHeaderReserved, strings.ToLower(name)) {
			return fmt.Errorf("driver: a login may not set the %s header (the jar sends the component's cookies, and the client owns the rest)", name)
		}
		if _, err := noControl(stripRefs(v)); err != nil {
			return fmt.Errorf("driver: login header %s %v", name, err)
		}
		for _, m := range argPattern.FindAllStringSubmatch(v, -1) {
			ref, ok := strings.CutPrefix(m[1], "session.")
			if !ok {
				return fmt.Errorf("driver: login header %s references %q; a login header sends only a captured value, ${session.<capture>}, never a secret", name, m[1])
			}
			if !captured[ref] {
				return fmt.Errorf("driver: login header %s references the capture %q, which the login does not declare", name, ref)
			}
		}
		for _, p := range s.Polls {
			for h := range p.Request.Headers {
				if strings.EqualFold(h, name) {
					return fmt.Errorf("driver: poll %q header %s is the one the login sends", p.Name, h)
				}
			}
		}
		for _, c := range s.Commands {
			for h := range c.Request.Headers {
				if strings.EqualFold(h, name) {
					return fmt.Errorf("driver: command %q header %s is the one the login sends", c.CommandType, h)
				}
			}
		}
	}
	return nil
}

// stripRefs drops a template's references, leaving its literal text.
func stripRefs(v string) string { return argPattern.ReplaceAllString(v, "") }

// validateLoginRequest is validateHTTPRequest for a login step: the same
// shape, its only references secret fields of declared secret inputs.
func (s *Spec) validateLoginRequest(what string, r Request) error {
	// A path that opens with a reference would render a secret into the
	// URL's authority.
	if !strings.HasPrefix(r.Path, "/") {
		return fmt.Errorf("driver: %s path %q must start with a literal /", what, r.Path)
	}
	// The request shape is checked with its secret references set aside,
	// then each reference on its own.
	refs, err := httpTemplateRefs(r)
	if err != nil {
		return fmt.Errorf("driver: %s %v", what, err)
	}
	for _, ref := range refs {
		parts := strings.Split(ref, ".")
		if len(parts) != 3 || parts[0] != "secret" || parts[1] == "" || parts[2] == "" {
			return fmt.Errorf("driver: %s references %q; a login step renders only ${secret.<input>.<field>}", what, ref)
		}
		if err := s.requireSecretInput(parts[1]); err != nil {
			return fmt.Errorf("driver: %s: %v", what, err)
		}
	}
	return validateHTTPRequest(what, withoutRefs(r), false, nil)
}

// withoutRefs is a request with its template references removed, so its
// shape validates as a poll's would.
func withoutRefs(r Request) Request {
	out := r
	out.Path = stripRefs(r.Path)
	if len(r.Query) > 0 {
		out.Query = map[string]string{}
		for k, v := range r.Query {
			out.Query[k] = stripRefs(v)
		}
	}
	if len(r.Headers) > 0 {
		out.Headers = map[string]string{}
		for k, v := range r.Headers {
			out.Headers[k] = stripRefs(v)
		}
	}
	if len(r.Body) > 0 {
		out.Body = json.RawMessage(stripRefs(string(r.Body)))
	}
	return out
}

// RenderLoginRequest renders a login step against the secrets a task
// carries, and returns the secret values it sent, for the scrub. A missing
// field refuses naming the reference, never a value.
func RenderLoginRequest(req Request, secrets map[string]map[string]string) (Request, []string, error) {
	var used []string
	r := refs{text: func(ref string) (string, error) {
		parts := strings.Split(ref, ".")
		if len(parts) != 3 || parts[0] != "secret" {
			return "", fmt.Errorf("driver: a login step references %q, which is not a secret field", ref)
		}
		v := secrets[parts[1]][parts[2]]
		if v == "" {
			return "", fmt.Errorf("driver: the login needs %s.%s, and no such credential was delivered", parts[1], parts[2])
		}
		used = append(used, v)
		return v, nil
	}}
	out, err := renderHTTPWith(req, r)
	if err != nil {
		return Request{}, nil, err
	}
	return out, used, nil
}

// SessionHeaders renders the login's headers against the values it
// captured. A value with a control character refuses: it would inject a
// header of its own.
func (l *Login) SessionHeaders(values map[string]string) (map[string]string, error) {
	if len(l.Headers) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(l.Headers))
	r := refs{text: func(ref string) (string, error) {
		name, _ := strings.CutPrefix(ref, "session.")
		v, ok := values[name]
		if !ok || v == "" {
			return "", fmt.Errorf("driver: the login captured no %q", name)
		}
		return v, nil
	}}
	for k, v := range l.Headers {
		rendered, err := renderTemplate(v, r, noControl)
		if err != nil {
			return nil, fmt.Errorf("driver: login header %s: %w", k, err)
		}
		out[k] = rendered
	}
	return out, nil
}

// The bounds a declared session lifetime is held to: shorter would log in
// on nearly every request, longer is a session that never renews (gear says
// "never" with a huge number), which a lapse renews when it comes.
const (
	MinSessionLifetime = 10 * time.Second
	MaxSessionLifetime = 30 * 24 * time.Hour
)

// ParseExpiry reads a login's expiry, a number of seconds, held to
// [MinSessionLifetime, MaxSessionLifetime]. ok is false for anything that
// is not a finite positive number (zero, a negative, a word), which gear
// uses to mean no expiry: the session lasts until a request finds it lapsed.
func ParseExpiry(v string) (d time.Duration, ok bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
		return 0, false
	}
	switch {
	case f < MinSessionLifetime.Seconds():
		return MinSessionLifetime, true
	case f > MaxSessionLifetime.Seconds():
		return MaxSessionLifetime, true
	}
	return time.Duration(f * float64(time.Second)), true
}

// ExtractValue locates one value in a payload by its selector, the one
// extraction primitive's read outside a baked function (a login's capture).
func ExtractValue(ex Extract, p Payload) (string, error) {
	if p.docs == nil {
		p.docs = &parsedDocs{}
	}
	return locate(ex, p)
}

// IsCredentialed reports an http session whose every request carries a
// credential: a scheme's, or a login's.
func (s *SessionSpec) IsCredentialed() bool {
	return s != nil && (s.Credential != "" || s.Login != nil)
}
