package mail_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/mail"
	"github.com/hyperscaleav/omniglass/internal/mailsim"
)

// The SMTP session against the mail sim (#1317): the relay is judged by the
// egress contract before a byte is sent, TLS is required unless the allow
// list names the relay, and the relay's own refusal is the send's reason.

func message(t *testing.T) []byte {
	t.Helper()
	raw, err := mail.Build(mail.Message{ID: "n-1", From: "omniglass@example.com", To: []string{"alice@example.com"},
		Subject: "[High] display-1: no video", Text: "The display lost its signal.", Date: time.Unix(0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func loopback(t *testing.T) egress.Policy {
	t.Helper()
	p, err := egress.ParsePolicy([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAMessageReachesTheSimThroughAnAllowedRelay(t *testing.T) {
	sim, err := mailsim.Start("127.0.0.1:0", mailsim.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer sim.Close()
	res := mail.New(loopback(t), egress.Options{}).Send(context.Background(), mail.Relay{Addr: sim.Addr(), Security: mail.SecurityNone},
		"omniglass@example.com", []string{"alice@example.com"}, message(t))
	if !res.OK {
		t.Fatalf("send = %+v", res)
	}
	got := sim.Messages()
	if len(got) != 1 || got[0].Subject != "[High] display-1: no video" || got[0].To[0] != "alice@example.com" || got[0].Text != "The display lost its signal." {
		t.Fatalf("the sim got %+v", got)
	}
}

func TestTheRelayIsJudgedByTheEgressContract(t *testing.T) {
	sim, err := mailsim.Start("127.0.0.1:0", mailsim.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer sim.Close()
	ctx := context.Background()
	// An internal relay the allow list does not name is refused at the dial,
	// whatever its security mode: nothing reaches it.
	for _, sec := range []string{mail.SecurityStartTLS, mail.SecurityTLS} {
		res := mail.New(egress.Policy{}, egress.Options{}).Send(ctx, mail.Relay{Addr: sim.Addr(), Security: sec}, "a@example.com", []string{"b@example.com"}, message(t))
		if res.OK || !strings.Contains(res.Reason, "egress allow list does not name") {
			t.Fatalf("%s to an unnamed internal relay = %+v, want refused by the egress contract", sec, res)
		}
	}
	// Plain SMTP only to a relay the allow list names.
	res := mail.New(egress.Policy{}, egress.Options{}).Send(ctx, mail.Relay{Addr: sim.Addr(), Security: mail.SecurityNone}, "a@example.com", []string{"b@example.com"}, message(t))
	if res.OK || !strings.Contains(res.Reason, "allow list names") {
		t.Fatalf("plain SMTP to an unnamed relay = %+v", res)
	}
	if n := len(sim.Messages()); n != 0 {
		t.Fatalf("a refused relay received %d messages", n)
	}
	// A metadata address is refused even when the allow list names it.
	meta, _ := egress.ParsePolicy([]string{"169.254.169.254"})
	res = mail.New(meta, egress.Options{}).Send(ctx, mail.Relay{Addr: "169.254.169.254:25", Security: mail.SecurityStartTLS}, "a@example.com", []string{"b@example.com"}, message(t))
	if res.OK || !strings.Contains(res.Reason, "metadata") {
		t.Fatalf("a metadata relay = %+v", res)
	}
}

func TestStartTLSIsRequiredAndVerified(t *testing.T) {
	cfg, roots, err := mailsim.SelfSigned("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	withTLS, err := mailsim.Start("127.0.0.1:0", mailsim.Options{TLS: cfg})
	if err != nil {
		t.Fatal(err)
	}
	defer withTLS.Close()
	ctx := context.Background()
	res := mail.New(loopback(t), egress.Options{RootCAs: roots}).Send(ctx, mail.Relay{Addr: withTLS.Addr(), Security: mail.SecurityStartTLS, Username: "relay-user", Password: "relay-pass"},
		"omniglass@example.com", []string{"alice@example.com"}, message(t))
	if !res.OK {
		t.Fatalf("starttls send = %+v", res)
	}
	if got := withTLS.Messages(); len(got) != 1 || !got[0].TLS || got[0].User != "relay-user" {
		t.Fatalf("the sim got %+v, want one message over TLS from relay-user", got)
	}
	// An untrusted certificate is a failed send, never a downgrade.
	res = mail.New(loopback(t), egress.Options{}).Send(ctx, mail.Relay{Addr: withTLS.Addr(), Security: mail.SecurityStartTLS},
		"omniglass@example.com", []string{"alice@example.com"}, message(t))
	if res.OK || !strings.Contains(res.Reason, "TLS") {
		t.Fatalf("an untrusted relay = %+v", res)
	}
	plain, err := mailsim.Start("127.0.0.1:0", mailsim.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	res = mail.New(loopback(t), egress.Options{}).Send(ctx, mail.Relay{Addr: plain.Addr(), Security: mail.SecurityStartTLS},
		"omniglass@example.com", []string{"alice@example.com"}, message(t))
	if res.OK || !strings.Contains(res.Reason, "does not offer STARTTLS") {
		t.Fatalf("a relay without STARTTLS = %+v", res)
	}
}

func TestARelayFailureIsTheSendsReason(t *testing.T) {
	sim, err := mailsim.Start("127.0.0.1:0", mailsim.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer sim.Close()
	sim.SetFailing(true)
	res := mail.New(loopback(t), egress.Options{}).Send(context.Background(), mail.Relay{Addr: sim.Addr(), Security: mail.SecurityNone},
		"omniglass@example.com", []string{"alice@example.com"}, message(t))
	if res.OK || res.Status != 451 || !strings.Contains(res.Reason, "451") {
		t.Fatalf("a failing relay = %+v", res)
	}
}
