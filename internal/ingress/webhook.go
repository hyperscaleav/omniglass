// Package ingress is the listener primitive's placement-agnostic core
// (#1024, ADR-1024): what a host (the server today, a node in a later
// slice) does to one pushed message between receiving it and landing it.
// It bounds (token buckets, a delivery's replay window), authenticates (the
// scheme the endpoint's driver declares), routes (listeners first, through
// the one extraction primitive) and tallies what it refuses and what no
// endpoint claims. Pure, no I/O: the host owns the socket or the route, the
// store and the bus, and injects the clock.
package ingress

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // a sender's declared hmac algorithm, not a password hash
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"hash"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// ReplayWindow is how far a signed timestamp may sit from its arrival, on
// either side, and how long a delivery id is remembered (#1146 slice 1).
const ReplayWindow = 5 * time.Minute

// Credential is what a scheme checks a post against: an hmac key, a shared
// token, or a basic login, unsealed from the endpoint's secret. It is never
// printed.
type Credential struct {
	Key                string
	Token              string
	Username, Password string
}

// String never renders a credential.
func (Credential) String() string { return "webhook credential" }

// Post is one received post: its headers and its body.
type Post struct {
	Header http.Header
	Body   []byte
}

// The refusal reasons: counted per source and logged once per tick, never
// sent to the sender, whose 401 says nothing about which part failed.
const (
	ReasonMissingSignature = "missing signature"
	ReasonBadSignature     = "signature mismatch"
	ReasonMissingTimestamp = "missing or unreadable timestamp"
	ReasonStaleTimestamp   = "timestamp outside the window"
	ReasonBadToken         = "token mismatch"
	ReasonBadBasic         = "basic credential mismatch"
	ReasonNoCredential     = "the endpoint's credential is missing"
	ReasonUnknownScheme    = "unknown scheme"
)

// Refusal is a post a scheme refused, with the reason it is counted under.
// Its text names the reason alone: never a key, a token, a signature or a
// body.
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return "webhook refused: " + r.Reason }

func refuse(reason string) error { return &Refusal{Reason: reason} }

// Authenticate checks one post against the endpoint's declared scheme at
// now. A nil error admits it; otherwise the error is a *Refusal. Every
// comparison of a secret runs in constant time over a fixed-length digest,
// so neither a near miss nor a length leaks through timing.
func Authenticate(scheme string, w *driver.WebhookSpec, cred Credential, p Post, now time.Time) error {
	switch scheme {
	case driver.WebhookNone:
		return nil
	case driver.WebhookHMAC:
		if cred.Key == "" {
			return refuse(ReasonNoCredential)
		}
		if w == nil || w.Signature == nil {
			return refuse(ReasonMissingSignature)
		}
		return checkHMAC(*w.Signature, cred.Key, p, now)
	case driver.WebhookToken:
		if cred.Token == "" {
			return refuse(ReasonNoCredential)
		}
		if w == nil || w.Token == nil {
			return refuse(ReasonBadToken)
		}
		v := p.Header.Get(w.Token.Header)
		presented, ok := strings.CutPrefix(v, w.Token.Prefix)
		if !equalDigest(presented, cred.Token) || !ok || v == "" {
			return refuse(ReasonBadToken)
		}
		return nil
	case driver.WebhookBasic:
		if cred.Username == "" && cred.Password == "" {
			return refuse(ReasonNoCredential)
		}
		r := http.Request{Header: p.Header}
		user, pass, ok := r.BasicAuth()
		// Both halves compare, whichever fails first: no short circuit.
		u, pw := equalDigest(user, cred.Username), equalDigest(pass, cred.Password)
		if !ok || !(u && pw) {
			return refuse(ReasonBadBasic)
		}
		return nil
	}
	return refuse(ReasonUnknownScheme)
}

// checkHMAC verifies a signature over the body, or over the signed
// timestamp, the separator and the body, refusing a timestamp outside the
// replay window on either side.
func checkHMAC(sg driver.Signature, key string, p Post, now time.Time) error {
	raw := strings.TrimSpace(p.Header.Get(sg.Header))
	if raw == "" {
		return refuse(ReasonMissingSignature)
	}
	encoded, ok := strings.CutPrefix(raw, sg.Prefix)
	if !ok {
		return refuse(ReasonBadSignature)
	}
	presented, err := decodeSignature(encoded, sg.EncodingOf())
	if err != nil {
		return refuse(ReasonBadSignature)
	}
	content := p.Body
	if sg.Timestamp != "" {
		ts := strings.TrimSpace(p.Header.Get(sg.Timestamp))
		secs, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return refuse(ReasonMissingTimestamp)
		}
		at := time.Unix(secs, 0)
		if d := now.Sub(at); d > ReplayWindow || d < -ReplayWindow {
			return refuse(ReasonStaleTimestamp)
		}
		content = append(append([]byte(ts), sg.SeparatorOf()...), p.Body...)
	}
	var h func() hash.Hash = sha256.New
	if sg.AlgorithmOf() == driver.SignatureSHA1 {
		h = sha1.New
	}
	m := hmac.New(h, []byte(key))
	m.Write(content)
	if !hmac.Equal(m.Sum(nil), presented) {
		return refuse(ReasonBadSignature)
	}
	return nil
}

func decodeSignature(s, encoding string) ([]byte, error) {
	if encoding == driver.EncodingBase64 {
		if b, err := base64.StdEncoding.DecodeString(s); err == nil {
			return b, nil
		}
		return base64.RawStdEncoding.DecodeString(s)
	}
	return hex.DecodeString(s)
}

// equalDigest compares two secrets in constant time over their sha256
// digests, so the comparison takes the same time whatever their lengths.
func equalDigest(a, b string) bool {
	da, db := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(da[:], db[:]) == 1
}

// maxDeliveryID bounds a delivery id held in memory: a longer one is held
// by its digest.
const maxDeliveryID = 128

// DeliveryID is the sender's delivery id off the header the driver declares,
// empty when it declares none or the post carries none.
func DeliveryID(w *driver.WebhookSpec, h http.Header) string {
	if w == nil || w.Delivery == "" {
		return ""
	}
	id := strings.TrimSpace(h.Get(w.Delivery))
	if len(id) > maxDeliveryID {
		sum := sha256.Sum256([]byte(id))
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	return id
}
