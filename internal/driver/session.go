package driver

import (
	"fmt"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/transport"
)

// SessionSpec is what a driver declares about the held session its functions
// ride (#1049): the auth step the session runs before anything borrows the
// connection, the replies that are terminal, and how slowly a rejected
// session may re-probe on its own. Auth is a layer that gates the session: a
// session runs no poll, command or listener arm until its auth step passes.
type SessionSpec struct {
	// Login is the dialog the session runs as its auth step: the prompts to
	// expect, what to send at each, and the match that means ready. One
	// primitive serves a raw tcp password prompt, a telnet login and ssh
	// keyboard-interactive.
	Login *Login `json:"login,omitempty"`
	// Terminal lists the replies retrying cannot fix, each with its class: a
	// login's failure line, a reply that means "account locked". A line
	// matching one (during the login, or answering any exchange) moves the
	// session to rejected rather than charging its retry budget.
	Terminal []TerminalRule `json:"terminal,omitempty"`
	// Reprobe is how often a rejected session tries once more on its own (a
	// duration, at least MinReprobe); empty is never, and the session waits
	// for its endpoint to change or an operator's retry.
	Reprobe string `json:"reprobe,omitempty"`
	// Credential names the secret input a transport authenticates with on
	// its own (ssh userauth, #1032); the login dialog names its secrets in
	// its sends instead.
	Credential string `json:"credential,omitempty"`
	// PTY asks the ssh shell channel for a terminal; Exec runs a command as
	// the channel instead of a shell (#1032). Both are ssh's alone.
	PTY  bool   `json:"pty,omitempty"`
	Exec string `json:"exec,omitempty"`
	// Scheme is how an http session's credential rides every request
	// (#902): basic, bearer or api-key, sending the secret input Credential
	// names; APIKey places an api key in a header or a query parameter.
	Scheme string           `json:"scheme,omitempty"`
	APIKey *APIKeyPlacement `json:"api_key,omitempty"`
}

// Login is a declared login dialog: each step waits for its prompt and then
// sends its line, in order, and the dialog passes when Ready matches. A
// prompt need not end a line ("login: " waits for the reply on the same
// line). A step's Send may reference a secret input's field as
// ${secret.<input>.<field>}; nothing else renders.
type Login struct {
	Steps []LoginStep `json:"steps"`
	Ready Match       `json:"ready"`
}

// LoginStep is one prompt and the answer to it.
type LoginStep struct {
	Expect Match  `json:"expect"`
	Send   string `json:"send"`
}

// TerminalRule classifies a reply retrying cannot fix. Over http a rule may
// match a response's Status (a gear's 403 for a wrong credential), its body
// by Match, or both (#902).
type TerminalRule struct {
	Class  string `json:"class"`
	Match  Match  `json:"match"`
	Status int    `json:"status,omitempty"`
}

// The terminal classes. A transport classifies its own protocol refusals
// into these too (an ssh auth failure is auth, a changed host key host-key).
const (
	ClassAuth        = "auth"
	ClassHostKey     = "host-key"
	ClassCertificate = "certificate"
	ClassProtocol    = "protocol"
)

// TerminalClasses is every class a spec may declare, in render order.
func TerminalClasses() []string {
	return []string{ClassAuth, ClassHostKey, ClassCertificate, ClassProtocol}
}

// MinReprobe is the floor on a declared re-probe: a rejected session's own
// retry exists to notice a fixed device eventually, never to hammer a locked
// account (5m is 288 attempts a day at most).
const MinReprobe = 5 * time.Minute

// ReprobeInterval parses the declared re-probe; zero means never (and a
// validated spec never carries a malformed one).
func (s *SessionSpec) ReprobeInterval() time.Duration {
	if s == nil || s.Reprobe == "" {
		return 0
	}
	d, err := time.ParseDuration(s.Reprobe)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// TerminalFor classifies a line against the declared terminal rules: the
// first rule whose match picks it decides the class.
func (s *SessionSpec) TerminalFor(line string) (class string, ok bool) {
	if s == nil {
		return "", false
	}
	for _, r := range s.Terminal {
		if r.Match.Matches(line) {
			return r.Class, true
		}
	}
	return "", false
}

// TerminalForResponse classifies an http response against the declared
// terminal rules: the first whose status (when it names one) and body match
// (when it declares one) both fit decides the class.
func (s *SessionSpec) TerminalForResponse(status int, body string) (class string, ok bool) {
	if s == nil {
		return "", false
	}
	for _, r := range s.Terminal {
		hasMatch := r.Match.Prefix != "" || r.Match.Regex != ""
		if r.Status == 0 && !hasMatch {
			continue
		}
		if r.Status != 0 && r.Status != status {
			continue
		}
		if hasMatch && !r.Match.Matches(body) {
			continue
		}
		return r.Class, true
	}
	return "", false
}

// secretRef is one ${secret.<input>.<field>} reference in a login send.
type secretRef struct{ input, field string }

// secretRefs lists a send's template references, refusing any that is not a
// secret reference.
func secretRefs(send string) ([]secretRef, error) {
	var out []secretRef
	for _, m := range argPattern.FindAllStringSubmatch(send, -1) {
		parts := strings.Split(m[1], ".")
		if len(parts) != 3 || parts[0] != "secret" || parts[1] == "" || parts[2] == "" {
			return nil, fmt.Errorf("references %q; a login send renders only ${secret.<input>.<field>}", m[1])
		}
		out = append(out, secretRef{input: parts[1], field: parts[2]})
	}
	return out, nil
}

// Render resolves each step's send against the delivered secrets (input name
// to fields). A missing field or a value carrying a control character
// refuses, naming the reference and never the value: a credential never
// reaches an error, a log line or a reason.
func (l *Login) Render(secrets map[string]map[string]string) ([]string, error) {
	out := make([]string, 0, len(l.Steps))
	for i, st := range l.Steps {
		var fault error
		rendered := argPattern.ReplaceAllStringFunc(st.Send, func(m string) string {
			ref := m[2 : len(m)-1]
			parts := strings.Split(ref, ".")
			if len(parts) != 3 || parts[0] != "secret" {
				fault = fmt.Errorf("driver: login step %d references %q, which is not a secret field", i+1, ref)
				return m
			}
			v, ok := secrets[parts[1]][parts[2]]
			if !ok || v == "" {
				fault = fmt.Errorf("driver: login step %d needs %s.%s, and no such credential was delivered", i+1, parts[1], parts[2])
				return m
			}
			if _, err := substituted(v); err != nil {
				fault = fmt.Errorf("driver: login step %d: %s.%s carries a control character", i+1, parts[1], parts[2])
				return m
			}
			return v
		})
		if fault != nil {
			return nil, fault
		}
		out = append(out, rendered)
	}
	return out, nil
}

// validateSession refuses a session block that cannot be run: one on a
// transport with no held line, a malformed login or terminal rule, a
// re-probe that is not slow, or a credential on a transport that does not
// authenticate on its own.
func (s *Spec) validateSession() error {
	ss := s.Session
	if ss == nil {
		if s.Transport == "ssh" && (len(s.Polls) > 0 || len(s.Listeners) > 0 || len(s.Commands) > 0) {
			return fmt.Errorf("driver: an ssh session authenticates on every connection; declare session.credential")
		}
		return nil
	}
	httpSession := IsHTTP(s.Transport)
	if (ss.Scheme != "" || ss.APIKey != nil) && !httpSession {
		return fmt.Errorf("driver: a session scheme rides every http request; %q is not http", s.Transport)
	}
	switch {
	case s.Transport == "udp":
		// Nothing logs in or is held over udp (#1037): a udp driver's session
		// block declares the replies retrying cannot fix, and nothing else.
		if ss.Login != nil || ss.Credential != "" || ss.Reprobe != "" || ss.PTY || ss.Exec != "" {
			return fmt.Errorf("driver: over udp a session block declares only terminal replies; nothing logs in, is held, or re-probes")
		}
	case httpSession:
		if err := s.validateHTTPSession(); err != nil {
			return err
		}
	case !isLineTransport(s.Transport):
		return fmt.Errorf("driver: a session block rides a held line transport; %q has none", s.Transport)
	}
	if l := ss.Login; l != nil {
		if len(l.Steps) == 0 {
			return fmt.Errorf("driver: the login declares no step")
		}
		for i, st := range l.Steps {
			if err := validateMatch(fmt.Sprintf("login step %d expect", i+1), st.Expect); err != nil {
				return err
			}
			if st.Send == "" {
				return fmt.Errorf("driver: login step %d has nothing to send", i+1)
			}
			refs, err := secretRefs(st.Send)
			if err != nil {
				return fmt.Errorf("driver: login step %d %v", i+1, err)
			}
			for _, r := range refs {
				if err := s.requireSecretInput(r.input); err != nil {
					return fmt.Errorf("driver: login step %d: %v", i+1, err)
				}
			}
		}
		if err := validateMatch("login ready", l.Ready); err != nil {
			return err
		}
	}
	for i, r := range ss.Terminal {
		if r.Class == "" {
			return fmt.Errorf("driver: terminal rule %d declares no class", i+1)
		}
		known := false
		for _, c := range TerminalClasses() {
			known = known || c == r.Class
		}
		if !known {
			return fmt.Errorf("driver: terminal rule %d has unknown class %q; want one of %s", i+1, r.Class, strings.Join(TerminalClasses(), ", "))
		}
		if r.Status != 0 && !httpSession {
			return fmt.Errorf("driver: terminal rule %d matches a status, which only an http response has", i+1)
		}
		if r.Status != 0 && (r.Status < 100 || r.Status > 599) {
			return fmt.Errorf("driver: terminal rule %d status %d is not an http status (100 to 599)", i+1, r.Status)
		}
		if httpSession && r.Status != 0 && r.Match.Prefix == "" && r.Match.Regex == "" {
			continue // a status alone is a whole rule over http
		}
		if err := validateMatch(fmt.Sprintf("terminal rule %d", i+1), r.Match); err != nil {
			return err
		}
	}
	if ss.Reprobe != "" {
		d, err := time.ParseDuration(ss.Reprobe)
		if err != nil || d < MinReprobe {
			return fmt.Errorf("driver: reprobe %q must be a duration of at least %s (a rejected session's own retry is slow by design)", ss.Reprobe, MinReprobe)
		}
	}
	if s.Transport == "ssh" && ss.Credential == "" {
		// The credential names the ssh user too; a login dialog only answers
		// keyboard-interactive questions beside it.
		return fmt.Errorf("driver: an ssh session authenticates on every connection; declare session.credential (the secret input with its username)")
	}
	if (ss.PTY || ss.Exec != "") && s.Transport != "ssh" {
		return fmt.Errorf("driver: session pty and exec shape an ssh channel; %q has none", s.Transport)
	}
	if ss.PTY && ss.Exec != "" {
		return fmt.Errorf("driver: a session runs a shell (with a pty) or an exec, not both")
	}
	if ss.Credential != "" && !httpSession {
		if s.Transport != "ssh" {
			return fmt.Errorf("driver: a session credential is the transport's own auth, which only ssh and http have; %q authenticates through its login dialog", s.Transport)
		}
		if err := s.requireSecretInput(ss.Credential); err != nil {
			return fmt.Errorf("driver: session credential: %v", err)
		}
	}
	return nil
}

// requireSecretInput refuses a reference to an input the spec does not
// declare as a secret.
func (s *Spec) requireSecretInput(name string) error {
	for _, in := range s.Inputs {
		if in.Name == name {
			if in.Kind != "secret" {
				return fmt.Errorf("input %q is not a secret", name)
			}
			return nil
		}
	}
	return fmt.Errorf("input %q is not declared", name)
}

// isLineTransport reports a transport the line protocol holds a session over.
func isLineTransport(name string) bool {
	t, ok := transport.ByName(name)
	return ok && t.Line
}
