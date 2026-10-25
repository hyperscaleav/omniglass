package ingress_test

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // sha1 is a sender's declared hmac algorithm here, not a password hash
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"hash"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/ingress"
)

var now = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

const key = "ucc-lab-signing-key"

func sign(h func() hash.Hash, k string, content []byte) []byte {
	m := hmac.New(h, []byte(k))
	m.Write(content)
	return m.Sum(nil)
}

// signedPost is a post signed as the simulated UCC service signs it: an hmac
// sha256 over the unix timestamp, a dot and the body, hex after a prefix.
func signedPost(body string, at time.Time, k string) ingress.Post {
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := sign(sha256.New, k, []byte(ts+"."+body))
	h := http.Header{}
	h.Set("X-UCC-Timestamp", ts)
	h.Set("X-UCC-Signature", "sha256="+hex.EncodeToString(mac))
	return ingress.Post{Header: h, Body: []byte(body)}
}

func hmacSpec() *driver.WebhookSpec {
	return &driver.WebhookSpec{Signature: &driver.Signature{Header: "X-UCC-Signature", Prefix: "sha256=", Timestamp: "X-UCC-Timestamp"}}
}

func reasonOf(t *testing.T, err error) string {
	t.Helper()
	var r *ingress.Refusal
	if !errors.As(err, &r) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	return r.Reason
}

func TestAnHMACOverASignedTimestampAdmitsTheSender(t *testing.T) {
	body := `{"event":"meeting.started","meeting":"m-1"}`
	if err := ingress.Authenticate(driver.WebhookHMAC, hmacSpec(), ingress.Credential{Key: key}, signedPost(body, now, key), now); err != nil {
		t.Fatalf("a correctly signed post = %v, want admitted", err)
	}
	// Inside the window on either side.
	if err := ingress.Authenticate(driver.WebhookHMAC, hmacSpec(), ingress.Credential{Key: key}, signedPost(body, now.Add(-4*time.Minute), key), now); err != nil {
		t.Fatalf("four minutes old = %v, want admitted", err)
	}
	if err := ingress.Authenticate(driver.WebhookHMAC, hmacSpec(), ingress.Credential{Key: key}, signedPost(body, now.Add(4*time.Minute), key), now); err != nil {
		t.Fatalf("four minutes ahead = %v, want admitted", err)
	}
}

func TestAnHMACRefusesWithoutSayingWhichPartFailed(t *testing.T) {
	body := `{"event":"meeting.started"}`
	cred := ingress.Credential{Key: key}
	cases := map[string]struct {
		post ingress.Post
		want string
	}{
		"a wrong key":        {signedPost(body, now, "not-the-key"), ingress.ReasonBadSignature},
		"a stale timestamp":  {signedPost(body, now.Add(-6*time.Minute), key), ingress.ReasonStaleTimestamp},
		"a future timestamp": {signedPost(body, now.Add(6*time.Minute), key), ingress.ReasonStaleTimestamp},
		"a tampered body": {func() ingress.Post {
			p := signedPost(body, now, key)
			p.Body = []byte(`{"event":"meeting.ended"}`)
			return p
		}(), ingress.ReasonBadSignature},
		"no signature": {func() ingress.Post { p := signedPost(body, now, key); p.Header.Del("X-UCC-Signature"); return p }(), ingress.ReasonMissingSignature},
		"no timestamp": {func() ingress.Post { p := signedPost(body, now, key); p.Header.Del("X-UCC-Timestamp"); return p }(), ingress.ReasonMissingTimestamp},
		"a timestamp of text": {func() ingress.Post {
			p := signedPost(body, now, key)
			p.Header.Set("X-UCC-Timestamp", "yesterday")
			return p
		}(), ingress.ReasonMissingTimestamp},
		"a missing prefix": {func() ingress.Post {
			p := signedPost(body, now, key)
			p.Header.Set("X-UCC-Signature", strings.TrimPrefix(p.Header.Get("X-UCC-Signature"), "sha256="))
			return p
		}(), ingress.ReasonBadSignature},
		"a signature that is not hex": {func() ingress.Post {
			p := signedPost(body, now, key)
			p.Header.Set("X-UCC-Signature", "sha256=zz")
			return p
		}(), ingress.ReasonBadSignature},
	}
	for name, c := range cases {
		err := ingress.Authenticate(driver.WebhookHMAC, hmacSpec(), cred, c.post, now)
		if got := reasonOf(t, err); got != c.want {
			t.Errorf("%s: reason %q, want %q", name, got, c.want)
		}
		// The refusal is counted by its reason; what it says never carries
		// the key, the signature or the body.
		for _, secret := range []string{key, c.post.Header.Get("X-UCC-Signature"), body} {
			if secret != "" && strings.Contains(err.Error(), secret) {
				t.Errorf("%s: the refusal %q discloses %q", name, err, secret)
			}
		}
	}
	if err := ingress.Authenticate(driver.WebhookHMAC, hmacSpec(), ingress.Credential{}, signedPost(body, now, key), now); reasonOf(t, err) != ingress.ReasonNoCredential {
		t.Fatalf("an endpoint whose key is missing = %v, want refused (never admitted unchecked)", err)
	}
}

// The signature's other shapes: the body alone signed, sha1, base64.
func TestAnHMACReadsTheDeclaredShape(t *testing.T) {
	body := []byte(`room=huddle&state=active`)
	sha1Mac := sign(sha1.New, key, body)
	h := http.Header{}
	h.Set("X-Hub-Signature", base64.StdEncoding.EncodeToString(sha1Mac))
	spec := &driver.WebhookSpec{Signature: &driver.Signature{Header: "X-Hub-Signature", Algorithm: driver.SignatureSHA1, Encoding: driver.EncodingBase64}}
	if err := ingress.Authenticate(driver.WebhookHMAC, spec, ingress.Credential{Key: key}, ingress.Post{Header: h, Body: body}, now); err != nil {
		t.Fatalf("a sha1 base64 signature over the body = %v, want admitted", err)
	}
}

func TestATokenAndABasicLoginCompareWhole(t *testing.T) {
	tok := &driver.WebhookSpec{Token: &driver.TokenPlacement{Header: "Authorization", Prefix: "Bearer "}}
	h := http.Header{}
	h.Set("Authorization", "Bearer shared-secret")
	if err := ingress.Authenticate(driver.WebhookToken, tok, ingress.Credential{Token: "shared-secret"}, ingress.Post{Header: h}, now); err != nil {
		t.Fatalf("the shared token = %v, want admitted", err)
	}
	for _, v := range []string{"Bearer shared-secre", "Bearer shared-secretX", "shared-secret", ""} {
		h.Set("Authorization", v)
		if reasonOf(t, ingress.Authenticate(driver.WebhookToken, tok, ingress.Credential{Token: "shared-secret"}, ingress.Post{Header: h}, now)) != ingress.ReasonBadToken {
			t.Errorf("token header %q admitted or misnamed", v)
		}
	}
	req, _ := http.NewRequest(http.MethodPost, "/", nil)
	req.SetBasicAuth("ucc", "lab")
	cred := ingress.Credential{Username: "ucc", Password: "lab"}
	if err := ingress.Authenticate(driver.WebhookBasic, nil, cred, ingress.Post{Header: req.Header}, now); err != nil {
		t.Fatalf("the basic login = %v, want admitted", err)
	}
	for _, login := range [][2]string{{"ucc", "lab2"}, {"uc", "lab"}, {"", ""}} {
		req.SetBasicAuth(login[0], login[1])
		if reasonOf(t, ingress.Authenticate(driver.WebhookBasic, nil, cred, ingress.Post{Header: req.Header}, now)) != ingress.ReasonBadBasic {
			t.Errorf("login %v admitted or misnamed", login)
		}
	}
	// none checks nothing: the endpoint reads unsigned, and the URL's token
	// was the only gate.
	if err := ingress.Authenticate(driver.WebhookNone, nil, ingress.Credential{}, ingress.Post{Header: http.Header{}}, now); err != nil {
		t.Fatalf("scheme none = %v, want admitted", err)
	}
	if reasonOf(t, ingress.Authenticate("digest", nil, ingress.Credential{}, ingress.Post{Header: http.Header{}}, now)) != ingress.ReasonUnknownScheme {
		t.Fatal("an unknown scheme admitted")
	}
}

// A sender's delivery id is read off its declared header, bounded, and empty
// when the driver declares none.
func TestDeliveryIDReadsTheDeclaredHeader(t *testing.T) {
	h := http.Header{}
	h.Set("X-UCC-Delivery", "  d-42 ")
	if got := ingress.DeliveryID(&driver.WebhookSpec{Delivery: "X-UCC-Delivery"}, h); got != "d-42" {
		t.Fatalf("delivery id = %q, want d-42", got)
	}
	if got := ingress.DeliveryID(&driver.WebhookSpec{}, h); got != "" {
		t.Fatalf("an undeclared delivery header read %q", got)
	}
	h.Set("X-UCC-Delivery", strings.Repeat("x", 4096))
	if got := ingress.DeliveryID(&driver.WebhookSpec{Delivery: "X-UCC-Delivery"}, h); len(got) > 128 || got == "" {
		t.Fatalf("a long delivery id = %d bytes, want a bounded digest", len(got))
	}
}
