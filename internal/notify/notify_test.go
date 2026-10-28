package notify_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/ingress"
	"github.com/hyperscaleav/omniglass/internal/notify"
)

// The send pipeline's order (#618): the body is rendered once, the signature
// covers exactly those bytes, and Omniglass's own inbound hmac scheme
// (ADR-1024) verifies them, so a receiver can verify the sender is this
// install and Omniglass can verify its own notifications.

// omniglassScheme is the inbound hmac webhook declared the way a driver for
// an Omniglass-shaped sender would declare it.
var omniglassScheme = &driver.WebhookSpec{Signature: &driver.Signature{
	Header: notify.HeaderSignature, Prefix: notify.SignaturePrefix, Timestamp: notify.HeaderTimestamp,
}}

func TestTheInboundHMACSchemeVerifiesTheBytesBuilt(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	env := notify.Envelope{ID: "n-1", Type: "alarm-raised", Transition: "raised", Subject: "s", Text: "t\nu",
		AlarmID: "a-1", Facts: map[string]string{"Component": "display-1"}}
	req, err := notify.Build("https://hooks.example.com/x", env, notify.Credential{HMACKey: "k-618"}, now)
	if err != nil {
		t.Fatal(err)
	}
	post := ingress.Post{Header: req.Header, Body: req.Body}
	if err := ingress.Authenticate(driver.WebhookHMAC, omniglassScheme, ingress.Credential{Key: "k-618"}, post, now.Add(time.Minute)); err != nil {
		t.Fatalf("the inbound scheme refused our own signature: %v", err)
	}
	// One byte changed after signing is a mismatch: the signature is over the
	// rendered bytes, not over the envelope's fields.
	tampered := ingress.Post{Header: req.Header, Body: append(append([]byte{}, req.Body...), ' ')}
	if err := ingress.Authenticate(driver.WebhookHMAC, omniglassScheme, ingress.Credential{Key: "k-618"}, tampered, now); err == nil {
		t.Fatal("a body changed after signing verified")
	}
	if err := ingress.Authenticate(driver.WebhookHMAC, omniglassScheme, ingress.Credential{Key: "other"}, post, now); err == nil {
		t.Fatal("another key verified")
	}
	var back notify.Envelope
	if err := json.Unmarshal(req.Body, &back); err != nil || back.Version != notify.EnvelopeVersion || back.Text != "t\nu" || back.Facts["Component"] != "display-1" {
		t.Fatalf("body = %s (%v)", req.Body, err)
	}
}

func TestTheIdempotencyKeyIsTheNotificationOnEveryAttempt(t *testing.T) {
	env := notify.Envelope{ID: "n-7", Type: "t", Transition: "raised"}
	a, _ := notify.Build("https://x.example/h", env, notify.Credential{}, time.Unix(1, 0))
	b, _ := notify.Build("https://x.example/h", env, notify.Credential{}, time.Unix(99, 0))
	if a.Header.Get(notify.HeaderIdempotency) != "n-7" || b.Header.Get(notify.HeaderIdempotency) != "n-7" {
		t.Fatalf("idempotency keys = %q, %q", a.Header.Get(notify.HeaderIdempotency), b.Header.Get(notify.HeaderIdempotency))
	}
	if a.Header.Get(notify.HeaderSignature) != "" || a.Header.Get("Authorization") != "" {
		t.Fatal("an unsigned target carried a signature or a token")
	}
}

func TestABearerTokenRidesTheAuthorizationHeader(t *testing.T) {
	req, _ := notify.Build("https://x.example/h", notify.Envelope{ID: "n"}, notify.Credential{BearerToken: "tok"}, time.Unix(1, 0))
	if req.Header.Get("Authorization") != "Bearer tok" {
		t.Fatalf("authorization = %q", req.Header.Get("Authorization"))
	}
}

func TestTheEnvelopeSchemaMatchesCommitted(t *testing.T) {
	fresh, err := notify.EnvelopeSchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("../../docs/src/generated/notification-envelope.json")
	if err != nil {
		t.Fatalf("read committed notification-envelope.json (run make gen and commit it): %v", err)
	}
	if string(committed) != string(fresh) {
		t.Error("docs/src/generated/notification-envelope.json drifted from notify.Envelope: run make gen and commit the result")
	}
}
