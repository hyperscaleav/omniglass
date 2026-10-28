package api_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/ingress"
	"github.com/hyperscaleav/omniglass/internal/notifier"
	"github.com/hyperscaleav/omniglass/internal/notify"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type notificationWire struct {
	ID         string `json:"id"`
	Component  string `json:"component"`
	Transition string `json:"transition"`
	ActionRule string `json:"action_rule"`
	Target     string `json:"target"`
	Subject    string `json:"subject"`
	Status     string `json:"status"`
	Attempts   int    `json:"attempts"`
	LastStatus *int   `json:"last_status"`
	LastError  string `json:"last_error"`
}

// TestAnAlarmTransitionIsDeliveredEndToEnd drives the whole lane (#618,
// #1321) over HTTP and a real receiver: an action rule queues a hand-raised
// alarm's notification, the tick delivers it signed so the inbound hmac
// scheme verifies the bytes received, a receiver answering 503 is retried
// and the Idempotency-Key holds across attempts, the cap ends it failed with
// the last status, :retry re-queues it to a success, the clear notifies too,
// and the timeline and the scoped list show it all.
func TestAnAlarmTransitionIsDeliveredEndToEnd(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x5}, 32))))
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
	all := scope.Set{All: true}
	for _, n := range []string{"display-1", "display-2"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: n}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
	}
	var failing atomic.Bool
	rcv := &receiver{}
	hook := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rcv.ServeHTTP(w, r)
		if failing.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer hook.Close()
	roots := x509.NewCertPool()
	roots.AddCert(hook.Certificate())
	eo := egress.Options{RootCAs: roots}
	srv := httptest.NewServer(api.NewHandler(gw, api.WithEgressOptions(eo)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	tick := notifier.New(gw, eo)
	pass := func() {
		t.Helper()
		if _, err := tick.Pass(ctx, 100); err != nil {
			t.Fatal(err)
		}
	}

	c.do(ownerTok, http.MethodPatch, "/settings/notification", map[string]any{"egress_allow": []string{"127.0.0.1"}}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/secrets", map[string]any{"name": "ops-signing", "secret_type": "hmac-key", "owner_kind": "platform",
		"fields": map[string]string{"key": "k-1321"}}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/notification-targets", map[string]any{"name": "ops-chat", "kind": "webhook", "url": hook.URL + "/hook", "secret": "ops-signing"}, http.StatusCreated)
	// A predicate that does not compile is refused with its position.
	if b := c.do(ownerTok, http.MethodPost, "/action-rules", map[string]any{"name": "bad", "on": []string{"raised"}, "when": "severity_rank >= ", "targets": []string{"ops-chat"}}, http.StatusUnprocessableEntity); !strings.Contains(string(b), "when") {
		t.Fatalf("a bad when = %s", b)
	}
	c.do(ownerTok, http.MethodPost, "/action-rules", map[string]any{"name": "page-ops", "on": []string{"raised", "cleared"},
		"when": `severity_rank >= 40 && component == "display-1"`, "targets": []string{"ops-chat"}}, http.StatusCreated)

	// The raise queues one, in its transaction; the tick delivers it signed.
	var alarm struct{ ID string }
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components/display-1/alarms", map[string]any{"severity": "high", "message": "no video"}, http.StatusCreated), &alarm); err != nil {
		t.Fatal(err)
	}
	pass()
	got := rcv.last(t)
	scheme := &driver.WebhookSpec{Signature: &driver.Signature{Header: notify.HeaderSignature, Prefix: notify.SignaturePrefix, Timestamp: notify.HeaderTimestamp}}
	if err := ingress.Authenticate(driver.WebhookHMAC, scheme, ingress.Credential{Key: "k-1321"}, got, time.Now()); err != nil {
		t.Fatalf("the inbound hmac scheme refused the delivery: %v", err)
	}
	var env notify.Envelope
	if err := json.Unmarshal(got.Body, &env); err != nil || env.Transition != "raised" || env.AlarmID != alarm.ID || env.Facts["Severity"] != "high" ||
		!strings.Contains(env.Subject, "no video") || got.Header.Get(notify.HeaderIdempotency) != env.ID {
		t.Fatalf("envelope = %s (%v)", got.Body, err)
	}
	var list struct{ Notifications []notificationWire }
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/notifications?alarm="+alarm.ID, nil, http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Notifications) != 1 || list.Notifications[0].Status != "sent" || list.Notifications[0].ID != env.ID {
		t.Fatalf("after delivery = %+v", list.Notifications)
	}
	// Another component's high alarm does not meet the predicate.
	c.do(ownerTok, http.MethodPost, "/components/display-2/alarms", map[string]any{"severity": "high", "message": "elsewhere"}, http.StatusCreated)

	// The clear notifies, and the receiver is down: retried, the same key
	// on every attempt, failed at the cap with the last status.
	failing.Store(true)
	c.do(ownerTok, http.MethodDelete, "/components/display-1/alarms/"+alarm.ID, nil, http.StatusNoContent)
	before := len(rcv.posts)
	for i := 0; i < storage.NotificationMaxAttempts; i++ {
		if _, err := pgxExec(ctx, dsn, `update notification set next_attempt_at = now() where status = 'queued'`); err != nil {
			t.Fatal(err)
		}
		pass()
	}
	attempts := rcv.posts[before:]
	if len(attempts) != storage.NotificationMaxAttempts {
		t.Fatalf("%d attempts reached the receiver, want %d", len(attempts), storage.NotificationMaxAttempts)
	}
	for _, p := range attempts {
		if p.Header.Get(notify.HeaderIdempotency) != attempts[0].Header.Get(notify.HeaderIdempotency) {
			t.Fatal("the Idempotency-Key changed between attempts")
		}
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/notifications?status=failed", nil, http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Notifications) != 1 || list.Notifications[0].Transition != "cleared" || list.Notifications[0].LastStatus == nil ||
		*list.Notifications[0].LastStatus != 503 || list.Notifications[0].Attempts != storage.NotificationMaxAttempts {
		t.Fatalf("failed = %+v", list.Notifications)
	}
	failedID := list.Notifications[0].ID

	// :retry is an operator's; a viewer reads it and may not retry it.
	viewer := setupAllViewer(t, ctx, dsn, "n-viewer")
	operator := principalWithGrants(t, ctx, dsn, "n-operator", []grant{{role: "operator", scopeKind: "all"}})
	c.do(viewer, http.MethodGet, "/notifications/"+failedID, nil, http.StatusOK)
	c.do(viewer, http.MethodPost, "/notifications/"+failedID+":retry", nil, http.StatusForbidden)
	failing.Store(false)
	c.do(operator, http.MethodPost, "/notifications/"+failedID+":retry", nil, http.StatusOK)
	c.do(operator, http.MethodPost, "/notifications/"+failedID+":retry", nil, http.StatusConflict)
	pass()
	var one notificationWire
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/notifications/"+failedID, nil, http.StatusOK), &one); err != nil {
		t.Fatal(err)
	}
	if one.Status != "sent" || rcv.last(t).Header.Get(notify.HeaderIdempotency) != failedID {
		t.Fatalf("after the retry = %+v", one)
	}

	// The timeline carries both, after their edges.
	var tl struct {
		Entries []struct {
			Kind, What, Target, Transition, ActionRule string
		}
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/components/display-1/alarms/"+alarm.ID+":timeline", nil, http.StatusOK), &tl); err != nil {
		t.Fatal(err)
	}
	var seq []string
	for _, e := range tl.Entries {
		seq = append(seq, e.Kind+":"+e.What)
	}
	if strings.Join(seq, ",") != "audit:raise,notification:sent,audit:clear,notification:sent" {
		t.Fatalf("timeline = %v", seq)
	}

	// A principal scoped to display-2 lists no notification about display-1.
	var d2 struct{ ID string }
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/display-2", nil, http.StatusOK), &d2); err != nil {
		t.Fatal(err)
	}
	scoped := principalWithGrants(t, ctx, dsn, "n-scoped", []grant{{role: "viewer", scopeKind: "component", scopeID: d2.ID}})
	if err := json.Unmarshal(c.do(scoped, http.MethodGet, "/notifications", nil, http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Notifications) != 0 {
		t.Fatalf("a display-2 scope listed %+v", list.Notifications)
	}
	c.do(scoped, http.MethodGet, "/notifications/"+failedID, nil, http.StatusNotFound)
}

// pgxExec runs one statement on the test database (a test reaching past the
// API to make a queued row due without waiting out its backoff).
func pgxExec(ctx context.Context, dsn, sql string) (int64, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return 0, err
	}
	defer conn.Close(ctx)
	tag, err := conn.Exec(ctx, sql)
	return tag.RowsAffected(), err
}
