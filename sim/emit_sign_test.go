package sim

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/ingress"
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// The simulated UCC service signs its webhooks as a real sender does
// (#1024): an hmac sha256 over its unix timestamp, a dot and the body, sent
// as hex after a prefix, beside a delivery id and its event header. Each
// post is checked here by the very primitive the server checks it with, so
// the face and the receiver cannot drift. Its faults post a wrong
// signature, a stale timestamp, and the same delivery twice.

type caught struct {
	mu    sync.Mutex
	posts []ingress.Post
}

func (c *caught) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.posts = append(c.posts, ingress.Post{Header: r.Header.Clone(), Body: b})
		c.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (c *caught) wait(t *testing.T, n int) []ingress.Post {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		got := len(c.posts)
		c.mu.Unlock()
		if got >= n {
			time.Sleep(50 * time.Millisecond) // a redelivery's second post
			c.mu.Lock()
			defer c.mu.Unlock()
			return append([]ingress.Post(nil), c.posts...)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("caught %d posts, want %d", len(c.posts), n)
	return nil
}

func uccSpec() *driver.WebhookSpec {
	return &driver.WebhookSpec{Signature: &driver.Signature{Header: "X-UCC-Signature", Prefix: "sha256=", Timestamp: "X-UCC-Timestamp"}, Delivery: "X-UCC-Delivery"}
}

// uccDevice is a device whose family signs the meeting it posts as a UCC
// service does, with the given fault.
func uccDevice(t *testing.T, url, fault string) *Device {
	d := newTestDevice(t, "ucc-1", map[string]any{
		"configuration": map[string]any{"webhook": map[string]any{
			"target_url": url, "scheme": "hmac", "secret": "ucc-lab-signing-key", "fault": fault,
		}},
	})
	return d
}

// startMeeting posts the meeting-started webhook the way a family's Go route
// does (spec.Send): signed as the device's subscription says.
func startMeeting(t *testing.T, d *Device) {
	t.Helper()
	cfg := d.State()["configuration"].(map[string]any)["webhook"].(map[string]any)
	goDevice{d}.Post(spec.Post{
		URL:     cfg["target_url"].(string),
		Body:    []byte(`{"event":"meeting.started","meeting":"m-1"}`),
		Headers: map[string]string{"X-UCC-Event": "meeting.started"},
		Sign: &spec.Signing{
			Scheme: cfg["scheme"].(string), Secret: cfg["secret"].(string), Fault: cfg["fault"].(string),
			Header: "X-UCC-Signature", Prefix: "sha256=", Timestamp: "X-UCC-Timestamp", Delivery: "X-UCC-Delivery",
		},
	})
}

func TestTheUCCServiceSignsWhatItPosts(t *testing.T) {
	var c caught
	d := uccDevice(t, c.server(t).URL, "")
	startMeeting(t, d)
	posts := c.wait(t, 1)
	p := posts[0]
	if err := ingress.Authenticate(driver.WebhookHMAC, uccSpec(), ingress.Credential{Key: "ucc-lab-signing-key"}, p, time.Now()); err != nil {
		t.Fatalf("the server's check refused the face's signed post: %v", err)
	}
	if string(p.Body) != `{"event":"meeting.started","meeting":"m-1"}` || p.Header.Get("X-UCC-Event") != "meeting.started" {
		t.Fatalf("post = %s %v, want the rendered body and its event header", p.Body, p.Header)
	}
	if ingress.DeliveryID(uccSpec(), p.Header) == "" {
		t.Fatal("the post carries no delivery id")
	}
}

func TestTheUCCServiceFaultsAsASenderGoneWrong(t *testing.T) {
	cases := map[string]string{"bad-signature": ingress.ReasonBadSignature, "stale": ingress.ReasonStaleTimestamp}
	for fault, want := range cases {
		var c caught
		d := uccDevice(t, c.server(t).URL, fault)
		startMeeting(t, d)
		p := c.wait(t, 1)[0]
		err := ingress.Authenticate(driver.WebhookHMAC, uccSpec(), ingress.Credential{Key: "ucc-lab-signing-key"}, p, time.Now())
		var r *ingress.Refusal
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: check = %v (%v), want refused for %q", fault, err, r, want)
		}
	}
	var c caught
	d := uccDevice(t, c.server(t).URL, "redeliver")
	startMeeting(t, d)
	posts := c.wait(t, 2)
	if len(posts) != 2 || ingress.DeliveryID(uccSpec(), posts[0].Header) != ingress.DeliveryID(uccSpec(), posts[1].Header) {
		t.Fatalf("redeliver = %d posts, want the same delivery twice", len(posts))
	}
	for _, p := range posts {
		if err := ingress.Authenticate(driver.WebhookHMAC, uccSpec(), ingress.Credential{Key: "ucc-lab-signing-key"}, p, time.Now()); err != nil {
			t.Fatalf("a redelivered post is not validly signed: %v", err)
		}
	}
}
