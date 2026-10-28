package api_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/ingress"
	"github.com/hyperscaleav/omniglass/internal/notify"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type notificationTargetWire struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Secret  string `json:"secret"`
	Auth    string `json:"auth"`
	Enabled bool   `json:"enabled"`
}

type targetTestWire struct {
	OK     bool   `json:"ok"`
	Status int    `json:"status"`
	Reason string `json:"reason"`
}

// receiver is a webhook receiver on a real socket: it keeps every post it
// gets, headers and exact bytes.
type receiver struct {
	mu    sync.Mutex
	posts []ingress.Post
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	b, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.posts = append(r.posts, ingress.Post{Header: req.Header.Clone(), Body: b})
	r.mu.Unlock()
}

func (r *receiver) last(t *testing.T) ingress.Post {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.posts) == 0 {
		t.Fatal("the receiver got nothing")
	}
	return r.posts[len(r.posts)-1]
}

// TestNotificationTargetAPI drives the notification_target surface (#618,
// #1320) against a real receiver: only an admin reads, writes, reveals or
// tests a target; the URL is masked on every read and revealed under an
// audit row; a loopback receiver is refused until the egress allow list names
// it; and a delivery is signed so the inbound hmac scheme verifies the exact
// bytes received.
func TestNotificationTargetAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x6}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	// A role holding every target permission and no secret one, for the
	// confused-deputy case below (inserted before the handler caches roles).
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `insert into role (name, official, permissions, inherits) values ('notifier', false, $1, '{viewer}')`,
		[]string{"notification_target:*"}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `insert into role (name, official, permissions, inherits) values ('notifier-reader', false, $1, '{viewer}')`,
		[]string{"notification_target:*", "secret:read"}); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)
	rcv := &receiver{}
	hook := httptest.NewTLSServer(rcv)
	defer hook.Close()
	roots := x509.NewCertPool()
	roots.AddCert(hook.Certificate())
	srv := httptest.NewServer(api.NewHandler(gw, api.WithEgressOptions(egress.Options{RootCAs: roots})))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	viewer := setupAllViewer(t, ctx, dsn, "nt-viewer")
	operator := principalWithGrants(t, ctx, dsn, "nt-operator", []grant{{role: "operator", scopeKind: "all"}})
	admin := principalWithGrants(t, ctx, dsn, "nt-admin", []grant{{role: "admin", scopeKind: "all"}})

	url := hook.URL + "/services/T0/B0/the-secret-path"
	body := map[string]any{"name": "ops-chat", "kind": "webhook", "url": url}
	// Admin-only: an operator and a viewer are refused every verb.
	for _, tok := range []string{viewer, operator} {
		c.do(tok, http.MethodGet, "/notification-targets", nil, http.StatusForbidden)
		c.do(tok, http.MethodPost, "/notification-targets", body, http.StatusForbidden)
	}
	var tgt notificationTargetWire
	if err := json.Unmarshal(c.do(admin, http.MethodPost, "/notification-targets", body, http.StatusCreated), &tgt); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{viewer, operator} {
		c.do(tok, http.MethodGet, "/notification-targets/ops-chat", nil, http.StatusForbidden)
		c.do(tok, http.MethodPost, "/notification-targets/ops-chat:test", nil, http.StatusForbidden)
		c.do(tok, http.MethodPost, "/notification-targets/ops-chat:reveal", nil, http.StatusForbidden)
	}
	// Masked on read, whole on the audited reveal.
	if strings.Contains(tgt.URL, "the-secret-path") || !strings.HasPrefix(tgt.URL, "https://127.0.0.1:") || tgt.Auth != "none" {
		t.Fatalf("created = %+v, want the URL masked and no auth", tgt)
	}
	var revealed struct{ URL string }
	if err := json.Unmarshal(c.do(admin, http.MethodPost, "/notification-targets/ops-chat:reveal", nil, http.StatusOK), &revealed); err != nil {
		t.Fatal(err)
	}
	if revealed.URL != url {
		t.Fatalf("revealed = %q, want %q", revealed.URL, url)
	}
	audit := auditDump(t, ctx, dsn, "notification_target")
	if !strings.Contains(audit, `"reveal"`) || strings.Contains(audit, "the-secret-path") {
		t.Fatalf("audit = %s, want a reveal row and never the URL's path", audit)
	}

	// The receiver is on loopback: refused until the allow list names it.
	var res targetTestWire
	if err := json.Unmarshal(c.do(admin, http.MethodPost, "/notification-targets/ops-chat:test", nil, http.StatusOK), &res); err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Status != 0 || !strings.Contains(res.Reason, "internal address") || len(rcv.posts) != 0 {
		t.Fatalf("test before the allow list = %+v, want refused before any request", res)
	}
	c.do(ownerTok, http.MethodPatch, "/settings/notification", map[string]any{"egress_allow": []string{"127.0.0.1"}}, http.StatusOK)

	// Sign with a platform hmac key; the inbound scheme verifies the bytes received.
	c.do(admin, http.MethodPost, "/secrets", map[string]any{"name": "ops-chat-signing", "secret_type": "hmac-key", "owner_kind": "platform",
		"fields": map[string]string{"key": "k-1320"}}, http.StatusCreated)
	if err := json.Unmarshal(c.do(admin, http.MethodPatch, "/notification-targets/ops-chat", map[string]any{"secret": "ops-chat-signing"}, http.StatusOK), &tgt); err != nil {
		t.Fatal(err)
	}
	if tgt.Auth != "hmac" || tgt.Secret != "ops-chat-signing" {
		t.Fatalf("bound = %+v", tgt)
	}
	if err := json.Unmarshal(c.do(admin, http.MethodPost, "/notification-targets/ops-chat:test", nil, http.StatusOK), &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Status != http.StatusOK {
		t.Fatalf("test = %+v, want delivered", res)
	}
	got := rcv.last(t)
	scheme := &driver.WebhookSpec{Signature: &driver.Signature{Header: notify.HeaderSignature, Prefix: notify.SignaturePrefix, Timestamp: notify.HeaderTimestamp}}
	if err := ingress.Authenticate(driver.WebhookHMAC, scheme, ingress.Credential{Key: "k-1320"}, got, time.Now()); err != nil {
		t.Fatalf("the inbound hmac scheme refused the delivery: %v", err)
	}
	var env notify.Envelope
	if err := json.Unmarshal(got.Body, &env); err != nil || env.Transition != "test" || env.ID == "" || got.Header.Get(notify.HeaderIdempotency) != env.ID {
		t.Fatalf("envelope = %s (%v), headers %v", got.Body, err, got.Header)
	}

	// Binding a secret takes the right to reveal it (a confused deputy
	// otherwise: a target holder could bind a token it cannot read to a URL it
	// controls and receive it on the next send). A holder of every target
	// permission but no secret:reveal is refused, and binds nothing.
	notifier := principalWithGrants(t, ctx, dsn, "nt-notifier", []grant{{role: "notifier", scopeKind: "all"}})
	c.do(notifier, http.MethodPost, "/notification-targets", map[string]any{"name": "exfil", "kind": "webhook", "url": hook.URL + "/x", "secret": "ops-chat-signing"}, http.StatusNotFound)
	c.do(notifier, http.MethodPatch, "/notification-targets/ops-chat", map[string]any{"secret": "ops-chat-signing"}, http.StatusNotFound)
	c.do(notifier, http.MethodGet, "/notification-targets/exfil", nil, http.StatusNotFound)
	// One that can read the secret but not reveal it is told so, and binds nothing.
	reader := principalWithGrants(t, ctx, dsn, "nt-reader", []grant{{role: "notifier-reader", scopeKind: "all"}})
	c.do(reader, http.MethodPatch, "/notification-targets/ops-chat", map[string]any{"secret": "ops-chat-signing"}, http.StatusForbidden)
	// Unbinding needs no secret right; nor does a target with no secret.
	c.do(notifier, http.MethodPost, "/notification-targets", map[string]any{"name": "plain-hook", "kind": "webhook", "url": hook.URL + "/p"}, http.StatusCreated)

	// A secret a target signs with cannot be deleted from under it.
	c.do(admin, http.MethodDelete, "/secrets/ops-chat-signing", nil, http.StatusConflict)

	// A component's secret, or a secret of another type, is not a target's.
	c.do(admin, http.MethodPost, "/secrets", map[string]any{"name": "a-login", "secret_type": "basic-auth", "owner_kind": "platform",
		"fields": map[string]string{"username": "u", "password": "p"}}, http.StatusCreated)
	c.do(admin, http.MethodPatch, "/notification-targets/ops-chat", map[string]any{"secret": "a-login"}, http.StatusUnprocessableEntity)

	// A bearer token rides the Authorization header.
	c.do(admin, http.MethodPost, "/secrets", map[string]any{"name": "pager-token", "secret_type": "bearer-token", "owner_kind": "platform",
		"fields": map[string]string{"token": "tok-1320"}}, http.StatusCreated)
	c.do(admin, http.MethodPost, "/notification-targets", map[string]any{"name": "pager", "kind": "webhook", "url": hook.URL + "/page", "secret": "pager-token"}, http.StatusCreated)
	c.do(admin, http.MethodPost, "/notification-targets/pager:test", nil, http.StatusOK)
	if got := rcv.last(t); got.Header.Get("Authorization") != "Bearer tok-1320" || got.Header.Get(notify.HeaderSignature) != "" {
		t.Fatalf("bearer delivery headers = %v", got.Header)
	}

	// A redirect is the receiver's answer, a failed attempt, and is not
	// followed to where it points (an untrusted certificate is proved in
	// internal/egress, against a real socket too).
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, hook.URL+"/elsewhere", http.StatusFound)
	}))
	defer redirect.Close()
	before := len(rcv.posts)
	c.do(admin, http.MethodPost, "/notification-targets", map[string]any{"name": "moved", "kind": "webhook", "url": redirect.URL + "/hook"}, http.StatusCreated)
	if err := json.Unmarshal(c.do(admin, http.MethodPost, "/notification-targets/moved:test", nil, http.StatusOK), &res); err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Status != http.StatusFound || !strings.Contains(res.Reason, "not followed") || len(rcv.posts) != before {
		t.Fatalf("redirect = %+v, want the 302 as the answer and nothing delivered where it pointed", res)
	}

	// Plain http only to a destination the allow list names; credentials in a URL never.
	c.do(admin, http.MethodPost, "/notification-targets", map[string]any{"name": "plain", "kind": "webhook", "url": "http://hooks.example.com/x"}, http.StatusUnprocessableEntity)
	c.do(admin, http.MethodPost, "/notification-targets", map[string]any{"name": "creds", "kind": "webhook", "url": "https://u:p@hooks.example.com/x"}, http.StatusUnprocessableEntity)
	c.do(admin, http.MethodPost, "/notification-targets", map[string]any{"name": "lab", "kind": "webhook", "url": "http://127.0.0.1:9/x"}, http.StatusCreated)

	c.do(admin, http.MethodDelete, "/notification-targets/lab", nil, http.StatusNoContent)
	c.do(admin, http.MethodGet, "/notification-targets/lab", nil, http.StatusNotFound)
}

// auditDump renders every audit row about a resource as one string, so a
// test can assert what is and is not in it.
func auditDump(t *testing.T, ctx context.Context, dsn, resource string) string {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var out string
	if err := conn.QueryRow(ctx, `select coalesce(string_agg(row_to_json(l)::text, E'\n'), '') from audit_log l where resource = $1`, resource).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
