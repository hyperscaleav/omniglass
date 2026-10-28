package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type notificationTypeWire struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Label    string `json:"label"`
	Subject  string `json:"subject"`
	Text     string `json:"text"`
	Official bool   `json:"official"`
}

type notificationPreviewWire struct {
	Subject    string                         `json:"subject"`
	Text       string                         `json:"text"`
	Transition string                         `json:"transition"`
	Facts      []struct{ Name, Value string } `json:"facts"`
}

func (p notificationPreviewWire) fact(name string) (string, bool) {
	for _, f := range p.Facts {
		if f.Name == name {
			return f.Value, true
		}
	}
	return "", false
}

// TestNotificationTypeAPI drives the notification_type registry (#618, #1319)
// over HTTP: the official types are read-only to everyone, an operator
// writes its own and a viewer reads both and writes neither; a template the
// sandbox refuses is a 422 naming the problem and is never stored; a fact
// outside the map renders empty; and a preview against a real alarm renders
// exactly what a delivery would carry, writing nothing.
func TestNotificationTypeAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "elsewhere"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	viewer := setupAllViewer(t, ctx, dsn, "nt-viewer")
	operator := principalWithGrants(t, ctx, dsn, "nt-operator", []grant{{role: "operator", scopeKind: "all"}})

	var list struct {
		NotificationTypes []notificationTypeWire `json:"notification_types"`
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/notification-types", nil, http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.NotificationTypes) != 2 || list.NotificationTypes[0].Name != "alarm-cleared" || list.NotificationTypes[1].Name != "alarm-raised" ||
		!list.NotificationTypes[0].Official || !list.NotificationTypes[1].Official {
		t.Fatalf("types = %+v, want the two official types", list.NotificationTypes)
	}

	// The official types are read-only, to an admin as much as anyone.
	c.do(ownerTok, http.MethodPatch, "/notification-types/alarm-raised", map[string]any{"subject": "mine"}, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/notification-types/alarm-raised", nil, http.StatusConflict)
	c.do(ownerTok, http.MethodPost, "/notification-types/alarm-raised:rename", map[string]any{"name": "raised"}, http.StatusUnprocessableEntity)

	// A viewer writes nothing; an operator creates and patches its own.
	draft := map[string]any{"name": "page-oncall", "label": "Page on-call", "subject": "{{.SeverityLabel | upper}} {{.Component}}", "text": "{{.Message}}{{.NotAFact}}"}
	c.do(viewer, http.MethodPost, "/notification-types", draft, http.StatusForbidden)
	var mine notificationTypeWire
	if err := json.Unmarshal(c.do(operator, http.MethodPost, "/notification-types", draft, http.StatusCreated), &mine); err != nil {
		t.Fatal(err)
	}
	if mine.Official || mine.Name != "page-oncall" {
		t.Fatalf("created = %+v", mine)
	}
	c.do(viewer, http.MethodPatch, "/notification-types/page-oncall", map[string]any{"label": "x"}, http.StatusForbidden)
	c.do(operator, http.MethodPatch, "/notification-types/page-oncall", map[string]any{"label": "Page the on-call"}, http.StatusOK)

	// A refused construct is a 422 that names it, and nothing is stored.
	for src, want := range map[string]string{
		`{{printf "%s%s" .Message .Message}}`: `printf`,
		`{{call .Message}}`:                   `call`,
		`{{index .Message 0}}`:                `index`,
	} {
		body := c.do(operator, http.MethodPatch, "/notification-types/page-oncall", map[string]any{"text": src}, http.StatusUnprocessableEntity)
		if !strings.Contains(string(body), want) || !strings.Contains(string(body), "text") {
			t.Errorf("refusal of %q = %s, want it to name text and %s", src, body, want)
		}
	}
	c.do(operator, http.MethodPost, "/notification-types", map[string]any{"name": "bad", "subject": "{{.X", "text": ""}, http.StatusUnprocessableEntity)
	c.do(viewer, http.MethodGet, "/notification-types/bad", nil, http.StatusNotFound)
	var got notificationTypeWire
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/notification-types/page-oncall", nil, http.StatusOK), &got); err != nil {
		t.Fatal(err)
	}
	if got.Text != "{{.Message}}{{.NotAFact}}" || got.Label != "Page the on-call" {
		t.Fatalf("stored = %+v, want the refused patches never stored", got)
	}

	// Preview renders against a real alarm, the fact outside the map empty.
	c.do(ownerTok, http.MethodPost, "/components/display-1/alarms", map[string]any{"severity": "high", "message": "no video"}, http.StatusCreated)
	before := auditCount(t, ctx, dsn)
	var p notificationPreviewWire
	if err := json.Unmarshal(c.do(viewer, http.MethodPost, "/notification-types/page-oncall:preview", map[string]any{"component": "display-1"}, http.StatusOK), &p); err != nil {
		t.Fatal(err)
	}
	if p.Subject != "HIGH display-1" || p.Text != "no video" || p.Transition != "raised" {
		t.Fatalf("preview = %+v, want the rendered templates for the raise", p)
	}
	if sev, _ := p.fact("Severity"); sev != "high" {
		t.Fatalf("facts = %+v", p.Facts)
	}
	if comp, _ := p.fact("Component"); comp != "display-1" || p.Facts[0].Name != "Component" {
		t.Fatalf("facts = %+v, want Component first, in declared order", p.Facts)
	}
	if _, ok := p.fact("NotAFact"); ok {
		t.Fatalf("facts carry a key outside the map: %+v", p.Facts)
	}
	// The official raised type, for the cleared edge, and a draft that is never stored.
	if err := json.Unmarshal(c.do(viewer, http.MethodPost, "/notification-types/alarm-raised:preview",
		map[string]any{"component": "display-1", "transition": "cleared", "subject": "{{.Transition}}: {{.Component}}"}, http.StatusOK), &p); err != nil {
		t.Fatal(err)
	}
	if p.Subject != "cleared: display-1" || !strings.Contains(p.Text, " raised a High alarm: no video") {
		t.Fatalf("draft preview = %+v", p)
	}
	if after := auditCount(t, ctx, dsn); after != before {
		t.Fatalf("preview wrote %d audit rows, want none", after-before)
	}
	// A component with no alarm, and an unknown type, are 404s.
	c.do(viewer, http.MethodPost, "/notification-types/page-oncall:preview", map[string]any{"component": "elsewhere"}, http.StatusNotFound)
	c.do(viewer, http.MethodPost, "/notification-types/nope:preview", map[string]any{"component": "display-1"}, http.StatusNotFound)
	// An output past the subject cap is a render failure, a 422.
	c.do(viewer, http.MethodPost, "/notification-types/page-oncall:preview",
		map[string]any{"component": "display-1", "subject": strings.Repeat("x", 201)}, http.StatusUnprocessableEntity)

	// Rename moves the handle; delete removes an operator type.
	var renamed notificationTypeWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/notification-types/page-oncall:rename", map[string]any{"name": "page-on-call"}, http.StatusOK), &renamed); err != nil {
		t.Fatal(err)
	}
	if renamed.ID != mine.ID || renamed.Name != "page-on-call" {
		t.Fatalf("renamed = %+v", renamed)
	}
	c.do(operator, http.MethodDelete, "/notification-types/page-on-call", nil, http.StatusForbidden)
	c.do(ownerTok, http.MethodDelete, "/notification-types/page-on-call", nil, http.StatusNoContent)
}

func auditCount(t *testing.T, ctx context.Context, dsn string) int {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
