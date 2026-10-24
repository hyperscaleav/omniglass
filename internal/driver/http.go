package driver

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/transport"
)

// The http transport as a full collection transport (#902, epic #1113): a
// driver's polls and commands send a real request (method, path, query,
// headers, body) and read the answer in the format the request declares,
// through the one extraction primitive. Credentials never ride a request's
// fields: they enter only through the session block's declared scheme
// (basic, bearer, an API key), so every credential path is one fingerprint
// input and one scrub.

// The response formats an http request declares. A request's format is
// declared rather than sniffed from the Content-Type, since gear mislabels
// its content; json is the default.
const (
	FormatJSON = "json"
	FormatXML  = "xml"
	FormatHTML = "html"
	FormatText = "text"
)

// Formats is every response format, in the order refusals name them.
func Formats() []string { return []string{FormatJSON, FormatXML, FormatHTML, FormatText} }

// The request body formats: how a declared body is rendered and encoded.
const (
	BodyFormatJSON = "json"
	BodyFormatForm = "form"
	BodyFormatXML  = "xml"
	BodyFormatText = "text"
)

// BodyFormats is every body format, in the order refusals name them.
func BodyFormats() []string {
	return []string{BodyFormatJSON, BodyFormatForm, BodyFormatXML, BodyFormatText}
}

// The http methods a request may declare; GET is the default.
var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// reservedHeaders are the headers a request may not set itself: the
// credential headers (a credential enters only through the declared scheme),
// and the ones the client owns (the host, the framing, the connection).
var reservedHeaders = []string{"authorization", "proxy-authorization", "cookie", "host", "content-length", "transfer-encoding", "connection"}

// The auth schemes an http session declares (#902): how the credential its
// session.credential input names rides every request.
const (
	// SchemeBasic sends a basic-auth secret's username and password as
	// RFC 7617 basic authorization.
	SchemeBasic = "basic"
	// SchemeBearer sends a bearer-token secret's token as a bearer
	// authorization.
	SchemeBearer = "bearer"
	// SchemeAPIKey sends an api-key secret's key in a header or a query
	// parameter, placed and named by the driver (session.api_key).
	SchemeAPIKey = "api-key"
)

// schemeSecretType is the secret type each scheme reads.
var schemeSecretType = map[string]string{SchemeBasic: "basic-auth", SchemeBearer: "bearer-token", SchemeAPIKey: "api-key"}

// Schemes is every http auth scheme, in the order refusals name them.
func Schemes() []string { return []string{SchemeBasic, SchemeBearer, SchemeAPIKey} }

// APIKeyPlacement is where an api-key scheme's key rides and under what
// name: a header (X-API-Key) or a query parameter (apikey). The driver
// declares it (decision 6 of #1113), so a secret type stays a pure shape.
type APIKeyPlacement struct {
	In   string `json:"in"`
	Name string `json:"name"`
}

// The API key placements.
const (
	APIKeyInHeader = "header"
	APIKeyInQuery  = "query"
)

// IsHTTP reports a transport the http request shape rides: http and https.
func IsHTTP(name string) bool { return name == "http" || name == "https" }

// FormatOf is the request's response format, json when it declares none.
func (r Request) FormatOf() string {
	if r.Format == "" {
		return FormatJSON
	}
	return r.Format
}

// MethodOf is the request's method, GET when it declares none.
func (r Request) MethodOf() string {
	if r.Method == "" {
		return "GET"
	}
	return r.Method
}

// BodyFormatOf is the body's encoding, json when it declares none.
func (r Request) BodyFormatOf() string {
	if r.BodyFormat == "" {
		return BodyFormatJSON
	}
	return r.BodyFormat
}

// hasHTTPFields reports a request carrying any field only http reads.
func (r Request) hasHTTPFields() bool {
	return r.Method != "" || r.Path != "" || len(r.Query) > 0 || len(r.Headers) > 0 || len(r.Body) > 0 ||
		r.BodyFormat != "" || r.Format != "" || len(r.Namespaces) > 0
}

// validateHTTPFieldsOff refuses http's request fields on another transport:
// they would be silently ignored there.
func validateHTTPFieldsOff(transportName, what string, r Request) error {
	if IsHTTP(transportName) || !r.hasHTTPFields() {
		return nil
	}
	return fmt.Errorf("driver: %s carries http request fields (method, path, query, headers, body, format, namespaces), which %q does not read", what, transportName)
}

// validateHTTPRequest refuses an http request no node could send: no path, a
// field another transport reads, an unknown method or format, a body where
// none belongs, a header the client owns or a credential would ride, or a
// template reference the family cannot render (a poll renders none; a
// command renders ${value} and ${arg.key}; neither ever names a secret).
func validateHTTPRequest(what string, r Request, command bool, ss *SessionSpec) error {
	if r.Line != "" || len(r.Get) > 0 {
		return fmt.Errorf("driver: %s over http sends a request with a path, not a line or a get", what)
	}
	if r.Reply != nil {
		return fmt.Errorf("driver: %s over http declares a reply shape, which only a line request has", what)
	}
	if r.Path == "" {
		return fmt.Errorf("driver: %s over http needs a path", what)
	}
	if !strings.HasPrefix(r.Path, "/") {
		return fmt.Errorf("driver: %s path %q must start with /", what, r.Path)
	}
	if strings.ContainsAny(r.Path, "?#") {
		return fmt.Errorf("driver: %s path %q carries a query or a fragment; declare query parameters in query", what, r.Path)
	}
	if !slices.Contains(httpMethods, r.MethodOf()) {
		return fmt.Errorf("driver: %s method %q is not one of %s", what, r.Method, strings.Join(httpMethods, ", "))
	}
	if !slices.Contains(Formats(), r.FormatOf()) {
		return fmt.Errorf("driver: %s format %q is not one of %s", what, r.Format, strings.Join(Formats(), ", "))
	}
	if len(r.Namespaces) > 0 && r.FormatOf() != FormatXML {
		return fmt.Errorf("driver: %s declares namespaces, which only an xml response has", what)
	}
	for prefix, uri := range r.Namespaces {
		if prefix == "" || uri == "" || strings.ContainsAny(prefix, ": \t") {
			return fmt.Errorf("driver: %s namespace %q = %q: a prefix is a name and its uri is not empty", what, prefix, uri)
		}
	}
	for name := range r.Query {
		if name == "" {
			return fmt.Errorf("driver: %s declares a query parameter with no name", what)
		}
		if ss != nil && ss.APIKey != nil && ss.APIKey.In == APIKeyInQuery && name == ss.APIKey.Name {
			return fmt.Errorf("driver: %s query parameter %q is where the session's api key rides", what, name)
		}
	}
	for name := range r.Headers {
		if !isToken(name) {
			return fmt.Errorf("driver: %s header name %q is not an http token", what, name)
		}
		if slices.Contains(reservedHeaders, strings.ToLower(name)) {
			return fmt.Errorf("driver: %s may not set the %s header: a credential enters only through the session's scheme, and the client owns the rest", what, name)
		}
		if ss != nil && ss.APIKey != nil && ss.APIKey.In == APIKeyInHeader && strings.EqualFold(name, ss.APIKey.Name) {
			return fmt.Errorf("driver: %s header %s is where the session's api key rides", what, name)
		}
	}
	if !slices.Contains(BodyFormats(), r.BodyFormatOf()) {
		return fmt.Errorf("driver: %s body_format %q is not one of %s", what, r.BodyFormat, strings.Join(BodyFormats(), ", "))
	}
	if r.BodyFormat != "" && len(r.Body) == 0 {
		return fmt.Errorf("driver: %s declares a body_format and no body", what)
	}
	if len(r.Body) > 0 {
		if r.MethodOf() == "GET" {
			return fmt.Errorf("driver: %s sends a body on a GET; declare the method that carries one", what)
		}
		if err := validateBody(what, r); err != nil {
			return err
		}
	}
	refs, err := httpTemplateRefs(r)
	if err != nil {
		return fmt.Errorf("driver: %s %v", what, err)
	}
	for _, ref := range refs {
		switch {
		case strings.HasPrefix(ref, "secret."):
			return fmt.Errorf("driver: %s references %q: a request never names a secret; the session's scheme carries the credential", what, ref)
		case !command:
			return fmt.Errorf("driver: %s references the template field %q, and a poll carries no value to render", what, ref)
		case ref != "value" && !strings.HasPrefix(ref, "arg."):
			return fmt.Errorf("driver: %s references unknown template field %q", what, ref)
		}
	}
	return nil
}

// validateBody refuses a body its declared format cannot render: a form body
// is an object of strings, an xml or text body is a string, a json body is
// any JSON value.
func validateBody(what string, r Request) error {
	var v any
	if err := json.Unmarshal(r.Body, &v); err != nil {
		return fmt.Errorf("driver: %s body is not JSON: %v", what, err)
	}
	switch r.BodyFormatOf() {
	case BodyFormatForm:
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("driver: %s form body is an object of strings", what)
		}
		for k, val := range obj {
			if _, ok := val.(string); !ok {
				return fmt.Errorf("driver: %s form body field %q is not a string", what, k)
			}
		}
	case BodyFormatXML, BodyFormatText:
		if _, ok := v.(string); !ok {
			return fmt.Errorf("driver: %s %s body is a string (the document, its values templated)", what, r.BodyFormatOf())
		}
	}
	return nil
}

// httpTemplateRefs lists every template reference in a request's path,
// query values, header values and body.
func httpTemplateRefs(r Request) ([]string, error) {
	var refs []string
	add := func(s string) {
		for _, m := range argPattern.FindAllStringSubmatch(s, -1) {
			refs = append(refs, m[1])
		}
	}
	add(r.Path)
	for _, v := range r.Query {
		add(v)
	}
	for _, v := range r.Headers {
		add(v)
	}
	if len(r.Body) > 0 {
		var v any
		if err := json.Unmarshal(r.Body, &v); err != nil {
			return nil, fmt.Errorf("body is not JSON: %v", err)
		}
		walkStrings(v, add)
	}
	return refs, nil
}

// walkStrings calls f with every string in a decoded JSON value.
func walkStrings(v any, f func(string)) {
	switch t := v.(type) {
	case string:
		f(t)
	case []any:
		for _, e := range t {
			walkStrings(e, f)
		}
	case map[string]any:
		for _, e := range t {
			walkStrings(e, f)
		}
	}
}

// isToken reports an RFC 9110 token: a header name.
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r > 0x7e || r <= 0x20 || strings.ContainsRune(`"(),/:;<=>?@[\]{}`, r) {
			return false
		}
	}
	return true
}

// checkHTTPEmit binds an emit's extractor to the response format: a JSON
// path reads a json response, an XPath an xml or html one, a regex any body;
// an oid or a key reads nothing an http response carries.
func checkHTTPEmit(what string, ex Extract, r Request) error {
	switch {
	case ex.OID != "" || ex.Key != "":
		return fmt.Errorf("driver: %s locates by oid or key, which an http response does not carry; use jsonpath, xpath or regex", what)
	case ex.JSONPath != "" && r.FormatOf() != FormatJSON:
		return fmt.Errorf("driver: %s locates by jsonpath on a %s response; declare format json", what, r.FormatOf())
	case ex.XPath != "" && r.FormatOf() != FormatXML && r.FormatOf() != FormatHTML:
		return fmt.Errorf("driver: %s locates by xpath on a %s response; declare format xml or html", what, r.FormatOf())
	case ex.XPath != "":
		if _, err := compiledXPath(ex.XPath, r.Namespaces); err != nil {
			return fmt.Errorf("driver: %s has an xpath that does not compile: %v", what, err)
		}
	}
	return nil
}

// validateHTTPSession refuses an http session block no node could run: the
// scheme and the credential come together, the credential names a secret
// input of the scheme's type, an api key is placed where a node can put it,
// and nothing a held line session declares (a login, a re-probe, a channel)
// rides along. The login dialog over http requests is #1114.
func (s *Spec) validateHTTPSession() error {
	ss := s.Session
	if ss.Login != nil {
		return fmt.Errorf("driver: an http session declares no login yet (#1114); declare a scheme and its credential")
	}
	if ss.Reprobe != "" || ss.PTY || ss.Exec != "" {
		return fmt.Errorf("driver: an http session holds no connection, so it declares no reprobe, pty or exec")
	}
	switch {
	case ss.Scheme == "" && ss.Credential != "":
		return fmt.Errorf("driver: the session credential %q needs a scheme (one of %s)", ss.Credential, strings.Join(Schemes(), ", "))
	case ss.Scheme != "" && ss.Credential == "":
		return fmt.Errorf("driver: the session scheme %q needs a credential, the secret input it sends", ss.Scheme)
	}
	if ss.Scheme != "" {
		want, ok := schemeSecretType[ss.Scheme]
		if !ok {
			return fmt.Errorf("driver: session scheme %q is not one of %s", ss.Scheme, strings.Join(Schemes(), ", "))
		}
		in, ok := s.inputNamed(ss.Credential)
		if !ok || in.Kind != "secret" {
			return fmt.Errorf("driver: session credential %q is not a declared secret input", ss.Credential)
		}
		if in.SecretType != want {
			return fmt.Errorf("driver: session scheme %s sends a %s secret, and the credential %q is a %s", ss.Scheme, want, ss.Credential, in.SecretType)
		}
	}
	if (ss.Scheme == SchemeAPIKey) != (ss.APIKey != nil) {
		return fmt.Errorf("driver: session api_key places the key of the api-key scheme, and only it")
	}
	if p := ss.APIKey; p != nil {
		switch p.In {
		case APIKeyInHeader:
			if !isToken(p.Name) {
				return fmt.Errorf("driver: session api_key header name %q is not an http token", p.Name)
			}
			if slices.Contains(reservedHeaders, strings.ToLower(p.Name)) {
				return fmt.Errorf("driver: session api_key may not ride the %s header", p.Name)
			}
		case APIKeyInQuery:
			if p.Name == "" {
				return fmt.Errorf("driver: session api_key query parameter needs a name")
			}
		default:
			return fmt.Errorf("driver: session api_key rides a header or a query parameter, not %q", p.In)
		}
	}
	return nil
}

// inputNamed is the declared input of that name.
func (s *Spec) inputNamed(name string) (Input, bool) {
	for _, in := range s.Inputs {
		if in.Name == name {
			return in, true
		}
	}
	return Input{}, false
}

// HTTPCredential is a credential resolved for an http request: the scheme
// and the fields it sends. It carries secrets: it is never printed.
type HTTPCredential struct {
	Scheme             string
	Username, Password string
	Token              string
	Key                string
	// In and Name place an api key (a header or a query parameter).
	In, Name string
}

// String never renders a credential.
func (c *HTTPCredential) String() string { return "http credential" }

// ResolveHTTPCredential reads the session's credential out of the secrets a
// task carries; nil for a session with no scheme. A missing field refuses
// naming the reference (input.field), never a value.
func ResolveHTTPCredential(ss *SessionSpec, secrets map[string]map[string]string) (*HTTPCredential, error) {
	if ss == nil || ss.Scheme == "" {
		return nil, nil
	}
	fields := secrets[ss.Credential]
	need := func(field string) (string, error) {
		v := fields[field]
		if v == "" {
			return "", fmt.Errorf("driver: the http credential needs %s.%s, and no such credential was delivered", ss.Credential, field)
		}
		return v, nil
	}
	c := &HTTPCredential{Scheme: ss.Scheme}
	var err error
	switch ss.Scheme {
	case SchemeBasic:
		if c.Username, err = need("username"); err != nil {
			return nil, err
		}
		if c.Password, err = need("password"); err != nil {
			return nil, err
		}
	case SchemeBearer:
		if c.Token, err = need("token"); err != nil {
			return nil, err
		}
	case SchemeAPIKey:
		if c.Key, err = need("key"); err != nil {
			return nil, err
		}
		if ss.APIKey == nil {
			return nil, fmt.Errorf("driver: the api-key scheme declares no placement")
		}
		c.In, c.Name = ss.APIKey.In, ss.APIKey.Name
	default:
		return nil, fmt.Errorf("driver: unknown http scheme %q", ss.Scheme)
	}
	for _, v := range []string{c.Username, c.Password, c.Token, c.Key} {
		for _, r := range v {
			if r < 0x20 || r == 0x7f {
				return nil, fmt.Errorf("driver: the http credential %s carries a control character", ss.Credential)
			}
		}
	}
	return c, nil
}

// renderHTTP renders an http request's path, query, headers and body
// against a command's value (text, and typed: the JSON a whole-leaf body
// reference takes) and params. Every placement is injection-safe: a path
// substitution is escaped as one segment, a query value is encoded by the
// client, a header value with a control character refuses, a JSON body is
// rendered leaf by leaf and then encoded, and an xml body's substitutions
// are escaped as text.
func renderHTTP(req Request, value string, typed json.RawMessage, params map[string]any) (Request, error) {
	out := req
	var err error
	if out.Path, err = renderTemplate(req.Path, value, params, func(s string) (string, error) { return url.PathEscape(s), nil }); err != nil {
		return Request{}, err
	}
	if len(req.Query) > 0 {
		out.Query = make(map[string]string, len(req.Query))
		for k, v := range req.Query {
			if out.Query[k], err = renderTemplate(v, value, params, plain); err != nil {
				return Request{}, err
			}
		}
	}
	if len(req.Headers) > 0 {
		out.Headers = make(map[string]string, len(req.Headers))
		for k, v := range req.Headers {
			if out.Headers[k], err = renderTemplate(v, value, params, noControl); err != nil {
				return Request{}, fmt.Errorf("driver: header %s: %w", k, err)
			}
		}
	}
	if len(req.Body) > 0 {
		if out.Body, err = renderBody(req, value, typed, params); err != nil {
			return Request{}, err
		}
	}
	return out, nil
}

// plain inserts a substitution as is (a query value the client encodes).
func plain(s string) (string, error) { return s, nil }

// noControl refuses a substitution carrying a control character: a header
// value with CR or LF would inject a header of its own.
func noControl(s string) (string, error) {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("carries a control character")
		}
	}
	return s, nil
}

// renderTemplate resolves ${value} and ${arg.key} in s, passing each
// substitution through place. A reference nothing supplies refuses.
func renderTemplate(s, value string, params map[string]any, place func(string) (string, error)) (string, error) {
	var fault error
	out := argPattern.ReplaceAllStringFunc(s, func(m string) string {
		if fault != nil {
			return m
		}
		raw, err := lookupRef(m[2:len(m)-1], value, params)
		if err != nil {
			fault = err
			return m
		}
		placed, err := place(raw)
		if err != nil {
			fault = fmt.Errorf("driver: request's %s %v", m, err)
			return m
		}
		return placed
	})
	return out, fault
}

// lookupRef is one reference's text: the value, or a param rendered as text.
func lookupRef(ref, value string, params map[string]any) (string, error) {
	if ref == "value" {
		if value == "" {
			return "", fmt.Errorf("driver: request references ${value}, and the command carries none")
		}
		return value, nil
	}
	key, ok := strings.CutPrefix(ref, "arg.")
	if !ok {
		return "", fmt.Errorf("driver: request references unknown template field %q", ref)
	}
	v, present := params[key]
	if !present || v == nil {
		return "", fmt.Errorf("driver: request references ${arg.%s}, and the command's params carry no %q", key, key)
	}
	return textOf(v), nil
}

// textOf renders a param as text (a JSON number as written, never %g).
func textOf(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

// renderBody renders a request body in its declared format.
func renderBody(req Request, value string, typed json.RawMessage, params map[string]any) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(req.Body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("driver: request body is not JSON: %v", err)
	}
	switch req.BodyFormatOf() {
	case BodyFormatForm:
		obj, _ := v.(map[string]any)
		form := url.Values{}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			s, _ := obj[k].(string)
			r, err := renderTemplate(s, value, params, plain)
			if err != nil {
				return nil, err
			}
			form.Set(k, r)
		}
		return json.RawMessage(form.Encode()), nil
	case BodyFormatXML:
		s, _ := v.(string)
		r, err := renderTemplate(s, value, params, func(s string) (string, error) {
			var b strings.Builder
			if err := xml.EscapeText(&b, []byte(s)); err != nil {
				return "", err
			}
			return b.String(), nil
		})
		return json.RawMessage(r), err
	case BodyFormatText:
		s, _ := v.(string)
		r, err := renderTemplate(s, value, params, plain)
		return json.RawMessage(r), err
	}
	rendered, err := renderJSONValue(v, value, typed, params)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(rendered)
	if err != nil {
		return nil, fmt.Errorf("driver: request body: %v", err)
	}
	return b, nil
}

// renderJSONValue renders every string leaf of a JSON body. A leaf that is
// exactly one reference takes the referenced value's own JSON type (an
// intended metric's number, a param's number or boolean); any other leaf is
// a string with its references rendered as text, which encoding escapes.
func renderJSONValue(v any, value string, typed json.RawMessage, params map[string]any) (any, error) {
	switch t := v.(type) {
	case string:
		if m := argPattern.FindStringSubmatchIndex(t); m != nil && m[0] == 0 && m[1] == len(t) {
			ref := t[m[2]:m[3]]
			if ref == "value" && len(typed) > 0 {
				dec := json.NewDecoder(bytes.NewReader(typed))
				dec.UseNumber()
				var out any
				if err := dec.Decode(&out); err != nil {
					return nil, fmt.Errorf("driver: request's ${value} is not JSON: %v", err)
				}
				return out, nil
			}
			if key, ok := strings.CutPrefix(ref, "arg."); ok {
				p, present := params[key]
				if !present || p == nil {
					return nil, fmt.Errorf("driver: request references ${arg.%s}, and the command's params carry no %q", key, key)
				}
				return p, nil
			}
		}
		return renderTemplate(t, value, params, plain)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			r, err := renderJSONValue(e, value, typed, params)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			r, err := renderJSONValue(e, value, typed, params)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	default:
		return v, nil
	}
}

// ContentTypeOf is the Content-Type a rendered body is sent with, unless the
// request declares one: its body format's media type.
func (r Request) ContentTypeOf() string {
	for k, v := range r.Headers {
		if strings.EqualFold(k, "content-type") {
			return v
		}
	}
	switch r.BodyFormatOf() {
	case BodyFormatForm:
		return "application/x-www-form-urlencoded"
	case BodyFormatXML:
		return "application/xml"
	case BodyFormatText:
		return "text/plain; charset=utf-8"
	default:
		return "application/json"
	}
}

// canListen reports a transport a listener can run over: a held line
// session's reader, or udp's own socket (#1037). Anywhere else nothing would
// run its standing listen task (#1115); SSE on http is #1038.
func canListen(name string) bool {
	t, ok := transport.ByName(name)
	return ok && (t.Line || name == "udp")
}
