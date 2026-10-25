package driver

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The websocket transport (#1034, slice 4 of #1113): a held session whose
// messages are text frames, routed by the router a line session uses
// (ADR-0151). A request is a frame: a line sent verbatim, or a JSON body
// rendered injection-safe as an http body is. Its reply is picked out of the
// frames by a shape, as a line's is, or by correlation: the node mints an id
// into a declared field of the frame, and the answer is the frame carrying
// that id back, whatever pushes interleave. The upgrade rides the session's
// scheme or its http login, and its path is the session's upgrade request.

// IsWebSocket reports the websocket transports: websocket, and wss over the
// shared TLS primitive.
func IsWebSocket(name string) bool { return name == "websocket" || name == "wss" }

// upgradeReserved are the headers the upgrade sets itself, or the client
// owns.
var upgradeReserved = []string{"upgrade", "connection", "sec-websocket-key", "sec-websocket-version",
	"sec-websocket-extensions", "sec-websocket-accept"}

// correlatePath is a correlate field: dotted object keys.
var correlatePath = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

// Frame is the text frame a websocket request sends: its line verbatim, or
// its JSON body, compacted.
func (r Request) Frame() (string, error) {
	if r.Line != "" {
		return r.Line, nil
	}
	var b bytes.Buffer
	if err := json.Compact(&b, r.Body); err != nil {
		return "", fmt.Errorf("driver: the frame's body is not JSON: %v", err)
	}
	return b.String(), nil
}

// MintCorrelation is a fresh id for one correlated exchange: random, so a
// late reply to an exchange that gave up never answers another.
func MintCorrelation() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Correlated binds a frame to its reply: with a correlation declared, the
// minted id is written into the declared field of the JSON frame, and the
// reply returned accepts only the frame carrying it back (the error shape
// kept); with none, the frame and the reply are returned as they are.
func (r *Reply) Correlated(frame, id string) (string, *Reply, error) {
	if r == nil || r.Correlate == "" {
		return frame, r, nil
	}
	dec := json.NewDecoder(strings.NewReader(frame))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil || doc == nil {
		return "", nil, fmt.Errorf("driver: a correlated frame is a JSON object, and this one is not")
	}
	keys := strings.Split(r.Correlate, ".")
	node := doc
	for _, k := range keys[:len(keys)-1] {
		next, ok := node[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			node[k] = next
		}
		node = next
	}
	node[keys[len(keys)-1]] = id
	out, err := json.Marshal(doc)
	if err != nil {
		return "", nil, fmt.Errorf("driver: bind the correlated frame: %v", err)
	}
	return string(out), &Reply{Field: &FieldMatch{Path: r.Correlate, Equals: id}, Error: r.Error, bound: true}, nil
}

// Bind is a websocket request as one exchange sends it: its frame, a fresh
// id minted into it when the reply correlates, and the reply that picks its
// answer out of the frames.
func (r Request) Bind() (string, *Reply, error) {
	frame, err := r.Frame()
	if err != nil {
		return "", nil, err
	}
	return r.Reply.Correlated(frame, MintCorrelation())
}

// validateFrameRequest refuses a websocket request no node could send: a
// line or a JSON body, never both and never neither, and none of http's
// other fields; a reply shape of one kind; a correlation only on a JSON
// object frame.
func validateFrameRequest(what string, r Request) error {
	if len(r.Get) > 0 {
		return fmt.Errorf("driver: %s over a websocket sends a frame, not a get", what)
	}
	if (r.Line == "") == (len(r.Body) == 0) {
		return fmt.Errorf("driver: %s over a websocket sends a line or a body (a JSON frame), one of them", what)
	}
	if r.Method != "" || r.Path != "" || len(r.Query) > 0 || len(r.Headers) > 0 || r.Format != "" || len(r.Namespaces) > 0 {
		return fmt.Errorf("driver: %s over a websocket is a frame: it carries no method, path, query, headers, format or namespaces", what)
	}
	if r.BodyFormat != "" && r.BodyFormat != BodyFormatJSON {
		return fmt.Errorf("driver: %s over a websocket sends its body as a json frame, not %s", what, r.BodyFormat)
	}
	var doc any
	if len(r.Body) > 0 {
		if err := json.Unmarshal(r.Body, &doc); err != nil {
			return fmt.Errorf("driver: %s body is not JSON: %v", what, err)
		}
	}
	if rep := r.Reply; rep != nil && rep.Correlate != "" {
		if !correlatePath.MatchString(rep.Correlate) {
			return fmt.Errorf("driver: %s reply correlate %q is a dotted path of object keys", what, rep.Correlate)
		}
		if _, ok := doc.(map[string]any); !ok {
			return fmt.Errorf("driver: %s reply correlates by %s, which needs a body that is a JSON object", what, rep.Correlate)
		}
	}
	return nil
}

// validateWebSocketSession refuses a websocket session block no node could
// run: the upgrade a GET with a path, setting none of the headers the
// upgrade owns; a scheme (basic, bearer, api-key) or an http login, not
// both and never digest, which answers a challenge per request; no stream,
// pty or exec.
func (s *Spec) validateWebSocketSession() error {
	ss := s.Session
	if ss.PTY || ss.Exec != "" {
		return fmt.Errorf("driver: a websocket session declares no pty or exec, which shape an ssh channel")
	}
	if ss.Scheme == SchemeDigest {
		return fmt.Errorf("driver: a websocket upgrade is one request; digest answers a challenge per request, so declare basic, bearer or api-key")
	}
	if ss.Login != nil {
		if ss.Scheme != "" || ss.Credential != "" || ss.APIKey != nil {
			return fmt.Errorf("driver: a websocket session declares a scheme or a login, not both")
		}
		if err := s.validateHTTPLogin(); err != nil {
			return err
		}
	} else if err := s.validateHTTPScheme(); err != nil {
		return err
	}
	if u := ss.Upgrade; u != nil {
		if m := u.MethodOf(); m != "GET" {
			return fmt.Errorf("driver: session.upgrade is a GET, not %s", m)
		}
		if len(u.Body) > 0 {
			return fmt.Errorf("driver: session.upgrade sends no body")
		}
		for h := range u.Headers {
			if slices.Contains(upgradeReserved, strings.ToLower(h)) {
				return fmt.Errorf("driver: session.upgrade may not set the %s header, which the upgrade sets itself", h)
			}
		}
		if err := validateHTTPRequest("session.upgrade", *u, false, ss); err != nil {
			return err
		}
	}
	return nil
}

// validateFrameSettle refuses a websocket binding's settle extract: its
// reply is a frame, read by a jsonpath (a JSON frame) or a regex.
func validateFrameSettle(c CommandBinding) error {
	if st := c.Settle; st != nil && st.Extract != nil && (st.Extract.OID != "" || st.Extract.Key != "" || st.Extract.XPath != "") {
		return fmt.Errorf("driver: command binding %q extracts its reply frame's value with a jsonpath or a regex", c.CommandType)
	}
	// A frame reads as a json answer does: a jsonpath or a regex.
	return validateSettle(CommandBinding{CommandType: c.CommandType, Settle: c.Settle}, true)
}
