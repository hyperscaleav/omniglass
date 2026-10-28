package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/mailsim"
	"github.com/hyperscaleav/omniglass/internal/notifier"
)

// TestAnEmailReachesTheMailSimAndARetrySendsTheSameBytes drives the email
// channel over HTTP and a real SMTP session against the mail sim (#1317): a
// relay configured in settings, a shared-mailbox target tested and notified,
// a group's members emailed only about what each can read, a relay failure
// retried with the existing backoff to a final failure, and :retry sending
// the very bytes the failed attempts sent.
func TestAnEmailReachesTheMailSimAndARetrySendsTheSameBytes(t *testing.T) {
	p := newPeople(t)
	sim, err := mailsim.Start("127.0.0.1:0", mailsim.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer sim.Close()
	p.human("alice", "viewer", "all", "")
	p.human("bob", "viewer", "component", p.comps["display-2"])
	tick := notifier.New(p.gw, egress.Options{})
	pass := func() {
		t.Helper()
		if _, err := tick.Pass(p.ctx, 100); err != nil {
			t.Fatal(err)
		}
	}

	p.c.do(p.owner, http.MethodPatch, "/settings/notification", map[string]any{"egress_allow": []string{"127.0.0.1"},
		"mail_relay": sim.Addr(), "mail_security": "none", "mail_from": "Omniglass <alerts@example.com>"}, http.StatusOK)
	p.c.do(p.owner, http.MethodPost, "/notification-targets", map[string]any{"name": "ops-mailbox", "kind": "email",
		"addresses": []string{"ops@example.com"}}, http.StatusCreated)
	p.c.do(p.owner, http.MethodPost, "/notification-targets", map[string]any{"name": "bad-mailbox", "kind": "email",
		"addresses": []string{"not an address"}}, http.StatusUnprocessableEntity)

	// :test sends one synthetic email through the relay.
	var tr struct {
		OK     bool   `json:"ok"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(p.c.do(p.owner, http.MethodPost, "/notification-targets/ops-mailbox:test", nil, http.StatusOK), &tr)
	if got := sim.Messages(); !tr.OK || len(got) != 1 || got[0].Subject != "Omniglass test notification" {
		t.Fatalf(":test = %+v, the sim got %+v", tr, got)
	}

	p.c.do(p.owner, http.MethodPost, "/principal-groups", map[string]any{"name": "on-call"}, http.StatusCreated)
	for _, u := range []string{"alice", "bob"} {
		p.c.do(p.owner, http.MethodPost, "/principal-groups/on-call/members", map[string]any{"principal_id": u}, http.StatusNoContent)
	}
	p.c.do(p.owner, http.MethodPost, "/notification-targets", map[string]any{"name": "on-call-page", "kind": "principal_group",
		"principal_group": "on-call", "channels": []string{"inbox", "email"}}, http.StatusCreated)
	p.c.do(p.owner, http.MethodPost, "/action-rules", map[string]any{"name": "page-on-call", "on": []string{"raised"},
		"targets": []string{"on-call-page", "ops-mailbox"}}, http.StatusCreated)

	// display-1: the mailbox and alice are emailed; bob cannot read it.
	p.c.do(p.owner, http.MethodPost, "/components/display-1/alarms", map[string]any{"severity": "high", "message": "no video"}, http.StatusCreated)
	pass()
	got := sim.Messages()[1:]
	to := []string{}
	for _, m := range got {
		to = append(to, m.To[0])
		if !strings.Contains(m.Subject, "no video") || !strings.Contains(m.Text, "no video") {
			t.Fatalf("an email = %+v, want the rendered subject and text", m)
		}
	}
	if strings.Join(to, ",") != "alice@example.com,ops@example.com" && strings.Join(to, ",") != "ops@example.com,alice@example.com" {
		t.Fatalf("display-1's raise emailed %v, want the mailbox and alice alone", to)
	}

	// The relay fails at the last step: each email is retried with the
	// existing backoff to its final failure, every attempt the same bytes.
	sim.SetRejectData(true)
	var two struct{ ID string }
	_ = json.Unmarshal(p.c.do(p.owner, http.MethodPost, "/components/display-2/alarms", map[string]any{"severity": "high", "message": "no audio"}, http.StatusCreated), &two)
	pass()
	type row struct {
		ID         string `json:"id"`
		Channel    string `json:"channel"`
		Recipient  string `json:"recipient"`
		Status     string `json:"status"`
		Attempts   int    `json:"attempts"`
		LastStatus *int   `json:"last_status"`
		LastError  string `json:"last_error"`
	}
	list := func() []row {
		var out struct{ Notifications []row }
		_ = json.Unmarshal(p.c.do(p.owner, http.MethodGet, "/notifications?alarm="+two.ID, nil, http.StatusOK), &out)
		return out.Notifications
	}
	var bobs string
	for _, r := range list() {
		if r.Channel != "email" {
			continue
		}
		if r.Status != "queued" || r.Attempts != 1 || r.LastStatus == nil || *r.LastStatus != 451 || !strings.Contains(r.LastError, "451") {
			t.Fatalf("an email after a relay failure = %+v, want queued for a retry with the relay's answer", r)
		}
		if r.Recipient == "bob" {
			bobs = r.ID
		}
	}
	conn, err := pgx.Connect(p.ctx, p.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(p.ctx)
	for range 7 {
		if _, err := conn.Exec(p.ctx, `update notification set next_attempt_at = now() where status = 'queued'`); err != nil {
			t.Fatal(err)
		}
		pass()
	}
	for _, r := range list() {
		if r.Channel == "email" && (r.Status != "failed" || r.Attempts != 8) {
			t.Fatalf("after the eighth failure = %+v, want failed", r)
		}
	}

	// The relay is back; :retry sends bob's email again, byte for byte.
	sim.SetRejectData(false)
	p.c.do(p.owner, http.MethodPost, "/notifications/"+bobs+":retry", nil, http.StatusOK)
	pass()
	var sent []byte
	for _, m := range sim.Messages() {
		if m.To[0] == "bob@example.com" {
			sent = m.Raw
		}
	}
	var failed [][]byte
	for _, m := range sim.Rejected() {
		if m.To[0] == "bob@example.com" {
			failed = append(failed, m.Raw)
		}
	}
	if sent == nil || len(failed) != 8 {
		t.Fatalf("bob's retry reached the sim: %v; failed attempts kept: %d", sent != nil, len(failed))
	}
	for _, f := range failed {
		if !bytes.Equal(f, sent) {
			t.Fatalf("a retry sent different bytes:\n%s\nthen\n%s", f, sent)
		}
	}
}

// TestBindingTheRelaysSecretTakesTheRightToRevealIt: the relay session hands
// its secret to whatever mail_relay names, so binding one (or moving the relay
// while one is bound) is judged as binding a secret to a target is.
func TestBindingTheRelaysSecretTakesTheRightToRevealIt(t *testing.T) {
	p := newPeople(t)
	p.c.do(p.owner, http.MethodPost, "/secrets", map[string]any{"name": "relay-login", "secret_type": "basic-auth", "owner_kind": "platform",
		"fields": map[string]string{"username": "omniglass", "password": "relay-pass-1317"}}, http.StatusCreated)
	p.c.do(p.owner, http.MethodPost, "/secrets", map[string]any{"name": "a-signing-key", "secret_type": "hmac-key", "owner_kind": "platform",
		"fields": map[string]string{"key": "k"}}, http.StatusCreated)
	p.human("sam", "settings-admin", "all", "")
	// sam may write settings and read secrets, but not reveal one.
	p.c.do(p.toks["sam"], http.MethodPatch, "/settings/notification", map[string]any{"mail_secret": "relay-login"}, http.StatusForbidden)
	p.c.do(p.toks["sam"], http.MethodPatch, "/settings/notification", map[string]any{"mail_relay": "smtp.example.com:587"}, http.StatusOK)
	p.c.do(p.owner, http.MethodPatch, "/settings/notification", map[string]any{"mail_secret": "no-such-secret"}, http.StatusNotFound)
	p.c.do(p.owner, http.MethodPatch, "/settings/notification", map[string]any{"mail_secret": "a-signing-key"}, http.StatusUnprocessableEntity)
	p.c.do(p.owner, http.MethodPatch, "/settings/notification", map[string]any{"mail_secret": "relay-login"}, http.StatusOK)
	// With the secret bound, moving the relay takes the same right.
	p.c.do(p.toks["sam"], http.MethodPatch, "/settings/notification", map[string]any{"mail_relay": "elsewhere.example.com:587"}, http.StatusForbidden)
	p.c.do(p.owner, http.MethodPatch, "/settings/notification", map[string]any{"mail_relay": "elsewhere.example.com:587"}, http.StatusOK)
}
