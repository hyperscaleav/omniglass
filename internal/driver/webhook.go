package driver

import (
	"fmt"
	"net/textproto"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/transport"
)

// A webhook driver (#1024, ADR-1024) maps what a sender posts to an
// endpoint's URL on the server. Nothing is dialed, so the spec declares no
// poll and no command: its session block says how a post is authenticated
// (the scheme, its credential, the signature's shape, the delivery id, the
// verification challenge), and its listeners claim each posted body and map
// it onto any lane through the one extraction primitive.

// The webhook schemes (decision 6 of #1146): an hmac signature over the body
// (or a signed timestamp joined to it), a shared token in a declared header,
// basic auth, or none, which checks nothing and reads unsigned. The URL's
// token is a sealed secret in every case, so none is never the only copy of
// a credential.
const (
	WebhookHMAC  = "hmac"
	WebhookToken = "token"
	WebhookBasic = "basic"
	WebhookNone  = "none"
)

// WebhookSchemes is every webhook scheme, in the order refusals name them.
func WebhookSchemes() []string {
	return []string{WebhookHMAC, WebhookToken, WebhookBasic, WebhookNone}
}

// webhookSchemeSecretType is the secret type each credentialed scheme checks.
var webhookSchemeSecretType = map[string]string{
	WebhookHMAC:  "hmac-key",
	WebhookToken: "bearer-token",
	WebhookBasic: "basic-auth",
}

// FormatForm is a form-encoded body (key=value&...), read by key: a webhook
// body format beside json, xml and text.
const FormatForm = "form"

// WebhookFormats is every format a posted body is read in.
func WebhookFormats() []string { return []string{FormatJSON, FormatXML, FormatForm, FormatText} }

// The hmac signature's algorithms and encodings.
const (
	SignatureSHA256 = "sha256"
	SignatureSHA1   = "sha1"
	EncodingHex     = "hex"
	EncodingBase64  = "base64"
)

// WebhookSpec is a webhook driver's session.webhook block.
type WebhookSpec struct {
	// Format is what the posted body is read as (json when absent).
	Format string `json:"format,omitempty"`
	// Signature is the hmac scheme's shape.
	Signature *Signature `json:"signature,omitempty"`
	// Token places the token scheme's shared token.
	Token *TokenPlacement `json:"token,omitempty"`
	// Delivery names the header carrying each delivery's id, remembered per
	// endpoint for the replay window so a sender's redelivery lands once.
	Delivery string `json:"delivery,omitempty"`
	// Challenge names the query parameter a sender's GET carries to verify
	// the URL, answered by echoing its value back.
	Challenge string `json:"challenge,omitempty"`
}

// FormatOf is the body's format, json when the block declares none.
func (w *WebhookSpec) FormatOf() string {
	if w == nil || w.Format == "" {
		return FormatJSON
	}
	return w.Format
}

// Signature is how an hmac signature rides a post: the header carrying it,
// its algorithm (sha256 when absent) and encoding (hex when absent), a prefix
// the sender puts before it ("sha256="), and, when the sender signs a
// timestamp, the header carrying it (unix seconds) and the separator joining
// it to the body ("." when absent). The signed content is the body alone, or
// the timestamp, the separator and the body.
type Signature struct {
	Header    string `json:"header"`
	Algorithm string `json:"algorithm,omitempty"`
	Encoding  string `json:"encoding,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Separator string `json:"separator,omitempty"`
}

// AlgorithmOf is the signature's algorithm, sha256 when absent.
func (s Signature) AlgorithmOf() string {
	if s.Algorithm == "" {
		return SignatureSHA256
	}
	return s.Algorithm
}

// EncodingOf is the signature's encoding, hex when absent.
func (s Signature) EncodingOf() string {
	if s.Encoding == "" {
		return EncodingHex
	}
	return s.Encoding
}

// SeparatorOf joins a signed timestamp to the body, "." when absent.
func (s Signature) SeparatorOf() string {
	if s.Separator == "" {
		return "."
	}
	return s.Separator
}

// TokenPlacement is the token scheme's header and the prefix before the
// token in it ("Bearer ").
type TokenPlacement struct {
	Header string `json:"header"`
	Prefix string `json:"prefix,omitempty"`
}

// HeaderMatch claims a posted message by one of its headers (a sender's
// event-type header): the header's value equals a value or matches a regex.
// Header names compare case-insensitively.
type HeaderMatch struct {
	Name   string `json:"name"`
	Equals string `json:"equals,omitempty"`
	Regex  string `json:"regex,omitempty"`
}

// matches reads the header's first value; a message without it never
// matches.
func (h *HeaderMatch) matches(header map[string][]string) bool {
	vs := header[textproto.CanonicalMIMEHeaderKey(h.Name)]
	if len(vs) == 0 {
		return false
	}
	if h.Regex != "" {
		re, err := compiled(h.Regex)
		return err == nil && re.MatchString(vs[0])
	}
	return vs[0] == h.Equals
}

// WebhookMessage is one posted body as a message: its data read in the
// declared format, its headers kept for a header match (canonical keys).
func WebhookMessage(body []byte, format string, header map[string][]string) Message {
	h := make(map[string][]string, len(header))
	for k, v := range header {
		h[textproto.CanonicalMIMEHeaderKey(k)] = v
	}
	if format == "" {
		format = FormatJSON
	}
	return Message{Data: string(body), Format: format, Header: h}
}

// formPayload is a form body in the forms an emit reads: each field's first
// value by key, and the raw text a regex reads.
func formPayload(data string) Payload {
	p := Payload{Text: data, Values: map[string]string{}, docs: &parsedDocs{}}
	if vs, err := url.ParseQuery(data); err == nil {
		for k, v := range vs {
			if len(v) > 0 {
				p.Values[k] = v[0]
			}
		}
	}
	return p
}

// validateWebhookSession refuses a webhook session block the server could
// not check a post against: the scheme is declared (none is a choice, never
// a default), a credentialed scheme names a secret input of its type, the
// hmac scheme declares its signature's shape and only it does, the token
// scheme declares its header and only it does, and nothing a dialed session
// declares rides along.
func (s *Spec) validateWebhookSession() error {
	ss := s.Session
	if ss == nil || ss.Scheme == "" {
		return fmt.Errorf("driver: a webhook driver declares how a post is authenticated: session.scheme is one of %s (none checks nothing and reads unsigned)", schemeList())
	}
	if ss.Login != nil || len(ss.Terminal) > 0 || ss.Reprobe != "" || ss.PTY || ss.Exec != "" || ss.Stream != nil || ss.Upgrade != nil || ss.APIKey != nil {
		return fmt.Errorf("driver: a webhook session declares its scheme, its credential and its webhook block, and no login, terminal rule, reprobe, stream, upgrade or api key: nothing is dialed")
	}
	w := ss.Webhook
	if w == nil {
		w = &WebhookSpec{}
	}
	want, credentialed := webhookSchemeSecretType[ss.Scheme]
	switch {
	case ss.Scheme == WebhookNone:
		if ss.Credential != "" || w.Signature != nil || w.Token != nil {
			return fmt.Errorf("driver: scheme none checks nothing, so it declares no credential, signature or token; its endpoint reads unsigned")
		}
	case !credentialed:
		return fmt.Errorf("driver: webhook scheme %q is not one of %s", ss.Scheme, schemeList())
	default:
		if ss.Credential == "" {
			return fmt.Errorf("driver: webhook scheme %s needs a credential, the %s secret input it checks a post against", ss.Scheme, want)
		}
		in, ok := s.inputNamed(ss.Credential)
		if !ok || in.Kind != "secret" {
			return fmt.Errorf("driver: session credential %q is not a declared secret input", ss.Credential)
		}
		if in.SecretType != want {
			return fmt.Errorf("driver: webhook scheme %s checks a %s secret, and the credential %q is a %s", ss.Scheme, want, ss.Credential, in.SecretType)
		}
	}
	if ss.Scheme == WebhookHMAC {
		if w.Signature == nil {
			return fmt.Errorf("driver: the hmac scheme declares its signature: the header it rides (webhook.signature.header), and its algorithm, encoding, prefix and signed timestamp where the sender has them")
		}
		if err := w.Signature.validate(); err != nil {
			return err
		}
	} else if w.Signature != nil {
		return fmt.Errorf("driver: a signature is the hmac scheme's; scheme %s checks none", ss.Scheme)
	}
	if ss.Scheme == WebhookToken {
		if w.Token == nil || !isToken(w.Token.Header) {
			return fmt.Errorf("driver: the token scheme declares the header its token rides (webhook.token.header)")
		}
	} else if w.Token != nil {
		return fmt.Errorf("driver: a token placement is the token scheme's; scheme %s reads none", ss.Scheme)
	}
	if w.Format != "" && !slices.Contains(WebhookFormats(), w.Format) {
		return fmt.Errorf("driver: webhook format %q is not one of json, xml, form or text", w.Format)
	}
	if w.Delivery != "" && !isToken(w.Delivery) {
		return fmt.Errorf("driver: webhook delivery header %q is not an http token", w.Delivery)
	}
	if w.Challenge != "" && (strings.ContainsAny(w.Challenge, "&=#?/ \t\r\n") || len(w.Challenge) > 64) {
		return fmt.Errorf("driver: webhook challenge %q is not a query parameter name", w.Challenge)
	}
	return nil
}

func schemeList() string {
	s := WebhookSchemes()
	return strings.Join(s[:len(s)-1], ", ") + " or " + s[len(s)-1]
}

// validate refuses a signature no check could read.
func (sg *Signature) validate() error {
	if !isToken(sg.Header) {
		return fmt.Errorf("driver: the signature header %q is not an http token", sg.Header)
	}
	if a := sg.AlgorithmOf(); a != SignatureSHA256 && a != SignatureSHA1 {
		return fmt.Errorf("driver: the signature algorithm %q is not sha256 or sha1", sg.Algorithm)
	}
	if e := sg.EncodingOf(); e != EncodingHex && e != EncodingBase64 {
		return fmt.Errorf("driver: the signature encoding %q is not hex or base64", sg.Encoding)
	}
	if sg.Timestamp != "" && !isToken(sg.Timestamp) {
		return fmt.Errorf("driver: the signature timestamp header %q is not an http token", sg.Timestamp)
	}
	if sg.Separator != "" && sg.Timestamp == "" {
		return fmt.Errorf("driver: a separator joins the signed timestamp to the body; declare signature.timestamp")
	}
	if strings.ContainsAny(sg.Prefix+sg.Separator, "\r\n") {
		return fmt.Errorf("driver: the signature prefix and separator carry no line break")
	}
	return nil
}

// validateHeaderMatch refuses a header rule no router could apply.
func validateHeaderMatch(what string, h *HeaderMatch, transportName string) error {
	if !transport.IsPush(transportName) {
		return fmt.Errorf("driver: %s names a header, which only a push transport's message carries (a webhook's post); a line has none", what)
	}
	if !isToken(h.Name) {
		return fmt.Errorf("driver: %s header match names no header (an http token)", what)
	}
	if (h.Equals == "") == (h.Regex == "") {
		return fmt.Errorf("driver: %s header match declares one of equals or regex", what)
	}
	if h.Regex != "" {
		if _, err := regexp.Compile(h.Regex); err != nil {
			return fmt.Errorf("driver: %s header match has an uncompilable regex: %v", what, err)
		}
	}
	return nil
}

// checkWebhookEmit binds an emit's extractor to the body's format: a json
// body by jsonpath, a form body by key, an xml body by xpath, and any body
// by a regex over its text.
func checkWebhookEmit(what string, ex Extract, format string) error {
	if ex.empty() || ex.Regex != "" {
		return nil
	}
	ok := false
	switch format {
	case FormatJSON:
		ok = ex.JSONPath != ""
	case FormatForm:
		ok = ex.Key != ""
	case FormatXML:
		ok = ex.XPath != ""
		if ok {
			if _, err := compiledXPath(ex.XPath, nil); err != nil {
				return fmt.Errorf("driver: %s has an xpath that does not compile: %v", what, err)
			}
		}
	}
	if !ok {
		return fmt.Errorf("driver: %s reads a %s body, which is read %s", what, format, webhookReaders[format])
	}
	return nil
}

var webhookReaders = map[string]string{
	FormatJSON: "by jsonpath or a regex",
	FormatForm: "by key or a regex",
	FormatXML:  "by xpath or a regex",
	FormatText: "by a regex",
}

// validatePush refuses what a push transport's spec cannot carry: a poll or
// a command (nothing is dialed), an unmatched policy other than log (what no
// listener claims lands as a log line on every push transport, decision 5),
// and a ladder.
func (s *Spec) validatePush() error {
	if len(s.Polls) > 0 || len(s.Commands) > 0 {
		return fmt.Errorf("driver: a %s driver declares listeners only: nothing is dialed, so it has no poll or command", s.Transport)
	}
	if len(s.Listeners) == 0 {
		return fmt.Errorf("driver: a %s driver declares at least one listener; an endpoint with none is attached bare, every post landing as a log line", s.Transport)
	}
	if s.Unmatched != "" && s.Unmatched != UnmatchedLog {
		return fmt.Errorf("driver: what no listener claims on a push transport lands as a log line; unmatched may only be log")
	}
	if s.Ladder != nil {
		return fmt.Errorf("driver: a push transport has no ladder: nothing is dialed, so its endpoint reads last heard")
	}
	return s.validateWebhookSession()
}
