package driver_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A webhook driver (#1024, ADR-1024): the server hosts the listener, so the
// spec declares no poll and no command, only listeners over the posted body
// and the session block's scheme (hmac, token, basic, or none, which reads
// unsigned) with the signature's shape, the delivery id and the challenge.

func webhookCatalog() fakeCatalog {
	c := catalog()
	c.lanes["call-started"] = "event"
	c.lanes["call-ended"] = "event"
	c.lanes["call-state"] = "property"
	c.secrets["hmac-key"] = true
	c.secrets["bearer-token"] = true
	return c
}

// webhookSpec is a signed webhook driver: an hmac over a signed timestamp
// joined to the body, a delivery id, and listeners claiming the meeting
// events by a JSON field and by the sender's event header.
func webhookSpec() *driver.Spec {
	return &driver.Spec{
		Version:   1,
		Transport: "webhook",
		Inputs:    []driver.Input{{Name: "signing-key", Kind: "secret", SecretType: "hmac-key", Required: true}},
		Session: &driver.SessionSpec{
			Scheme:     driver.WebhookHMAC,
			Credential: "signing-key",
			Webhook: &driver.WebhookSpec{
				Signature: &driver.Signature{Header: "X-UCC-Signature", Prefix: "sha256=", Timestamp: "X-UCC-Timestamp"},
				Delivery:  "X-UCC-Delivery",
			},
		},
		Listeners: []driver.Listener{
			{
				Name:  "meeting-started",
				Match: driver.Match{Field: &driver.FieldMatch{Path: "event", Equals: "meeting.started"}},
				Emits: []driver.Emit{
					{Name: "call-started", Message: &driver.Field{Value: "Meeting started"}, Attributes: map[string]driver.Field{"meeting": {Extract: &driver.Extract{JSONPath: "meeting"}}}},
					{Name: "call-state", Extract: driver.Extract{JSONPath: "event"}, Transform: &driver.Transform{Map: map[string]string{"meeting.started": "active"}}},
				},
			},
			{
				Name:  "meeting-ended",
				Match: driver.Match{Header: &driver.HeaderMatch{Name: "X-UCC-Event", Equals: "meeting.ended"}},
				Emits: []driver.Emit{{Name: "call-ended", Message: &driver.Field{Value: "Meeting ended"}}},
			},
		},
	}
}

func TestAWebhookDriverValidates(t *testing.T) {
	if err := webhookSpec().Validate(webhookCatalog()); err != nil {
		t.Fatalf("validate = %v, want clean", err)
	}
	token := webhookSpec()
	token.Inputs = []driver.Input{{Name: "shared", Kind: "secret", SecretType: "bearer-token", Required: true}}
	token.Session = &driver.SessionSpec{Scheme: driver.WebhookToken, Credential: "shared", Webhook: &driver.WebhookSpec{Token: &driver.TokenPlacement{Header: "Authorization", Prefix: "Bearer "}}}
	if err := token.Validate(webhookCatalog()); err != nil {
		t.Fatalf("a token scheme = %v, want clean", err)
	}
	basic := webhookSpec()
	basic.Inputs = []driver.Input{{Name: "login", Kind: "secret", SecretType: "basic-auth", Required: true}}
	basic.Session = &driver.SessionSpec{Scheme: driver.WebhookBasic, Credential: "login"}
	if err := basic.Validate(webhookCatalog()); err != nil {
		t.Fatalf("a basic scheme = %v, want clean", err)
	}
	unsigned := webhookSpec()
	unsigned.Inputs = nil
	unsigned.Session = &driver.SessionSpec{Scheme: driver.WebhookNone, Webhook: &driver.WebhookSpec{Challenge: "hub.challenge"}}
	if err := unsigned.Validate(webhookCatalog()); err != nil {
		t.Fatalf("an unsigned webhook = %v, want clean", err)
	}
	form := webhookSpec()
	form.Session.Webhook.Format = driver.FormatForm
	form.Listeners = []driver.Listener{{
		Name: "status", Match: driver.Match{Regex: `(^|&)event=room\.status`},
		Emits: []driver.Emit{{Name: "call-state", Extract: driver.Extract{Key: "state"}}},
		Log:   new(bool),
	}}
	if err := form.Validate(webhookCatalog()); err != nil {
		t.Fatalf("a form body read by key, keeping no raw line = %v, want clean", err)
	}
}

func TestWebhookRefusals(t *testing.T) {
	yes := true
	cases := []struct {
		name   string
		mutate func(*driver.Spec)
		want   string
	}{
		{"no session block", func(s *driver.Spec) { s.Session = nil }, "session.scheme"},
		{"no scheme", func(s *driver.Spec) { s.Session.Scheme = "" }, "session.scheme"},
		{"an http scheme", func(s *driver.Spec) { s.Session.Scheme = driver.SchemeBearer }, "hmac, token, basic or none"},
		{"hmac with no signature", func(s *driver.Spec) { s.Session.Webhook.Signature = nil }, "signature"},
		{"hmac with no credential", func(s *driver.Spec) { s.Session.Credential = "" }, "credential"},
		{"hmac over a bearer token", func(s *driver.Spec) { s.Inputs[0].SecretType = "bearer-token" }, "hmac-key"},
		{"a signature header that is no token", func(s *driver.Spec) { s.Session.Webhook.Signature.Header = "X Sig" }, "header"},
		{"an unknown algorithm", func(s *driver.Spec) { s.Session.Webhook.Signature.Algorithm = "md5" }, "sha256 or sha1"},
		{"an unknown encoding", func(s *driver.Spec) { s.Session.Webhook.Signature.Encoding = "base32" }, "hex or base64"},
		{"a separator with no timestamp", func(s *driver.Spec) {
			s.Session.Webhook.Signature.Timestamp, s.Session.Webhook.Signature.Separator = "", ":"
		}, "separator"},
		{"none with a credential", func(s *driver.Spec) { s.Session.Scheme = driver.WebhookNone }, "none"},
		{"token with no header", func(s *driver.Spec) {
			s.Inputs[0].SecretType = "bearer-token"
			s.Session.Scheme, s.Session.Webhook.Signature = driver.WebhookToken, nil
		}, "token"},
		{"a signature on basic", func(s *driver.Spec) {
			s.Inputs[0].SecretType = "basic-auth"
			s.Session.Scheme = driver.WebhookBasic
		}, "signature"},
		{"an unknown body format", func(s *driver.Spec) { s.Session.Webhook.Format = "html" }, "json, xml, form or text"},
		{"a delivery header that is no token", func(s *driver.Spec) { s.Session.Webhook.Delivery = "a b" }, "delivery"},
		{"a poll", func(s *driver.Spec) {
			s.Polls = []driver.Poll{{Name: "p", Schedule: driver.Schedule{Every: "1m"}, Request: driver.Request{Path: "/x"}, Emits: []driver.Emit{{Name: "call-state", Extract: driver.Extract{JSONPath: "a"}}}}}
		}, "nothing is dialed"},
		{"a listener that arms", func(s *driver.Spec) { s.Listeners[0].Arm = []string{"SUB"} }, "arm"},
		{"a listener naming an event", func(s *driver.Spec) { s.Listeners[0].Match.Event = "x" }, "event"},
		{"a header match with no name", func(s *driver.Spec) { s.Listeners[1].Match.Header.Name = "" }, "header"},
		{"a header match both equal and matching", func(s *driver.Spec) { s.Listeners[1].Match.Header.Regex = "x" }, "one of"},
		{"a field match on a text body", func(s *driver.Spec) { s.Session.Webhook.Format = driver.FormatText }, "json"},
		{"a jsonpath on a form body", func(s *driver.Spec) {
			s.Session.Webhook.Format = driver.FormatForm
			s.Listeners[0].Match = driver.Match{Prefix: "event="}
		}, "key"},
		{"discarding what nothing claims", func(s *driver.Spec) { s.Unmatched = driver.UnmatchedDiscard }, "log"},
		{"a ladder", func(s *driver.Spec) { s.Ladder = &driver.Ladder{Ping: &driver.PingRung{Enabled: &yes}} }, "ladder"},
		{"a login", func(s *driver.Spec) { s.Session.Login = &driver.Login{} }, "login"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := webhookSpec()
			c.mutate(s)
			err := s.Validate(webhookCatalog())
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("validate = %v, want an error naming %q", err, c.want)
			}
		})
	}
}

// A listener's raw-log choice (decision 5) and a header match belong to a
// push transport's listeners alone: a held line session lands only its emits.
func TestPushOnlyListenerFieldsRefuseElsewhere(t *testing.T) {
	keep := false
	s := validSpec()
	s.Listeners[0].Log = &keep
	if err := s.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "push") {
		t.Fatalf("log on a tcp listener = %v, want a refusal naming push", err)
	}
	s = validSpec()
	s.Listeners[0].Match.Header = &driver.HeaderMatch{Name: "X-Event", Equals: "a"}
	if err := s.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "header") {
		t.Fatalf("a header match on a tcp listener = %v, want a refusal naming the header", err)
	}
}

// The posted body is read in its declared format through the one primitive:
// json by jsonpath, form by key, xml by xpath, text by regex; a header match
// reads the sender's header case-insensitively.
func TestAWebhookMessageReadsInItsFormat(t *testing.T) {
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		format, body string
		extract      driver.Extract
		want         string
	}{
		{driver.FormatJSON, `{"room":{"state":"active"}}`, driver.Extract{JSONPath: "room.state"}, "active"},
		{driver.FormatForm, `event=room.status&state=idle`, driver.Extract{Key: "state"}, "idle"},
		{driver.FormatXML, `<room><state>active</state></room>`, driver.Extract{XPath: "/room/state"}, "active"},
		{driver.FormatText, `ROOM STATE idle`, driver.Extract{Regex: `STATE (\w+)`}, "idle"},
	}
	for _, c := range cases {
		msg := driver.WebhookMessage([]byte(c.body), c.format, map[string][]string{"X-Ucc-Event": {"room.status"}})
		fn := &driver.BakedFunction{Driver: "d", Function: "f", Emits: []driver.BakedEmit{{Name: "call-state", Lane: "property", Extract: c.extract}}}
		got, faults := fn.Interpret(msg.Payload(), at)
		if len(faults) != 0 || len(got) != 1 || got[0].Text != c.want {
			t.Errorf("%s: interpret = %+v %v, want %q", c.format, got, faults, c.want)
		}
		if !(driver.Match{Header: &driver.HeaderMatch{Name: "x-ucc-event", Equals: "room.status"}}).MatchesMessage(msg) {
			t.Errorf("%s: the header match did not read the sender's header", c.format)
		}
		if (driver.Match{Header: &driver.HeaderMatch{Name: "X-UCC-Event", Regex: `^meeting\.`}}).MatchesMessage(msg) {
			t.Errorf("%s: a header regex claimed a header it does not match", c.format)
		}
	}
	// A header rule and a data rule both have to hold.
	msg := driver.WebhookMessage([]byte(`{"event":"meeting.started"}`), driver.FormatJSON, map[string][]string{"X-Ucc-Event": {"meeting.started"}})
	both := driver.Match{Header: &driver.HeaderMatch{Name: "X-UCC-Event", Equals: "meeting.started"}, Field: &driver.FieldMatch{Path: "event", Equals: "meeting.ended"}}
	if both.MatchesMessage(msg) {
		t.Fatal("a match whose data rule fails claimed the message on its header alone")
	}
}
