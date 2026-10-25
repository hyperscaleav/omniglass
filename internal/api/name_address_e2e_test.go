package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// nameAddressStack is the live HTTP stack #1133's tests drive: a campus with two
// rooms, a display-1 in each (names are unique only within a placement), an https
// endpoint on each, and an owner token.
type nameAddressStack struct {
	t              *testing.T
	ctx            context.Context
	dsn            string
	c              *apiClient
	owner          string
	dispA, dispB   string // component uuids
	httpsA, httpsB string // endpoint uuids
	addrA, addrB   string // component dotted addresses
}

func newNameAddressStack(t *testing.T) *nameAddressStack {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	prov := secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(prov))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	s := &nameAddressStack{t: t, ctx: ctx, dsn: dsn, c: &apiClient{t: t, ctx: ctx, base: srv.URL}}
	s.owner = principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	s.c.do(s.owner, http.MethodPost, "/locations", map[string]any{"name": "campus", "location_type": "campus"}, http.StatusCreated)
	s.c.do(s.owner, http.MethodPost, "/locations", map[string]any{"name": "room-a", "location_type": "room", "parent": "campus"}, http.StatusCreated)
	s.c.do(s.owner, http.MethodPost, "/locations", map[string]any{"name": "room-b", "location_type": "room", "parent": "campus"}, http.StatusCreated)
	s.addrA, s.addrB = "campus.room-a.$comp.display-1", "campus.room-b.$comp.display-1"
	s.dispA = s.idOf(s.c.do(s.owner, http.MethodPost, "/components", map[string]any{"name": "display-1", "location": "campus.room-a", "product": "generic-device"}, http.StatusCreated))
	s.dispB = s.idOf(s.c.do(s.owner, http.MethodPost, "/components", map[string]any{"name": "display-1", "location": "campus.room-b", "product": "generic-device"}, http.StatusCreated))
	s.httpsA = s.idOf(s.c.do(s.owner, http.MethodPost, "/endpoints", map[string]any{"transport": "https", "component": s.addrA}, http.StatusCreated))
	s.httpsB = s.idOf(s.c.do(s.owner, http.MethodPost, "/endpoints", map[string]any{"transport": "https", "component": s.addrB}, http.StatusCreated))
	return s
}

func (s *nameAddressStack) idOf(body []byte) string {
	s.t.Helper()
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.ID == "" {
		s.t.Fatalf("no id in %s (%v)", body, err)
	}
	return out.ID
}

// detail is a problem body's detail.
func detail(t *testing.T, body []byte) string {
	t.Helper()
	var p struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("not a problem body: %s", body)
	}
	return p.Detail
}

// refPath joins a collection and a reference, percent-encoding the reference the
// way the generated CLI does (a slash in it is one path segment, not two).
func refPath(collection, ref, verb string) string {
	return collection + "/" + url.PathEscape(ref) + verb
}

// TestEndpointAddressAPI drives #1133's endpoint acceptance through HTTP: an
// endpoint answers to `<component address>/<name>` on every per-endpoint route
// (the slash percent-encoded), its uuid keeps working, and every response
// carries the uuid. A name two visible endpoints share is a 404 naming the
// reference and each full address.
func TestEndpointAddressAPI(t *testing.T) {
	s := newNameAddressStack(t)
	refA := s.addrA + "/https"

	for _, ref := range []string{refA, s.dispA + "/https", s.httpsA} {
		if got := s.idOf(s.c.do(s.owner, http.MethodGet, refPath("/endpoints", ref, ""), nil, http.StatusOK)); got != s.httpsA {
			t.Errorf("GET %q = endpoint %s, want %s", ref, got, s.httpsA)
		}
	}
	if got := s.idOf(s.c.do(s.owner, http.MethodPost, refPath("/endpoints", refA, ":retry"), map[string]any{}, http.StatusOK)); got != s.httpsA {
		t.Errorf("retry by address answered endpoint %s, want %s", got, s.httpsA)
	}
	if got := s.idOf(s.c.do(s.owner, http.MethodPatch, refPath("/endpoints", refA, ""), map[string]any{"label": "Wall display"}, http.StatusOK)); got != s.httpsA {
		t.Errorf("patch by address answered endpoint %s, want %s", got, s.httpsA)
	}

	msg := detail(t, s.c.do(s.owner, http.MethodGet, refPath("/endpoints", "display-1/https", ""), nil, http.StatusNotFound))
	for _, want := range []string{`endpoint "display-1/https" not found`, refA, s.addrB + "/https"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ambiguous refusal %q does not name %q", msg, want)
		}
	}
	msg = detail(t, s.c.do(s.owner, http.MethodGet, refPath("/endpoints", "https", ""), nil, http.StatusNotFound))
	if !strings.Contains(msg, "<component>/<endpoint>") {
		t.Errorf("a malformed endpoint ref refuses %q, want it to say how an endpoint is addressed", msg)
	}
}

// TestEndpointAddressAPIScope: a name never widens what a caller may read or do.
// Scoped to room-a's display, `display-1/https` names room-a's endpoint; room-b's,
// by address or by uuid, refuses exactly as an absent one does; and a viewer who
// may read but not retry gets the same 403 by name as by uuid.
func TestEndpointAddressAPIScope(t *testing.T) {
	s := newNameAddressStack(t)
	op := principalWithGrants(t, s.ctx, s.dsn, "op-a", []grant{{role: "operator", scopeKind: "component", scopeID: s.dispA}})
	viewer := principalWithGrants(t, s.ctx, s.dsn, "viewer-a", []grant{{role: "viewer", scopeKind: "component", scopeID: s.dispA}})

	if got := s.idOf(s.c.do(op, http.MethodGet, refPath("/endpoints", "display-1/https", ""), nil, http.StatusOK)); got != s.httpsA {
		t.Errorf("scoped GET display-1/https = %s, want room-a's %s", got, s.httpsA)
	}
	hiddenRef, absentRef := s.addrB+"/https", "campus.room-b.$comp.display-9/https"
	hidden := s.c.do(op, http.MethodPost, refPath("/endpoints", hiddenRef, ":retry"), map[string]any{}, http.StatusNotFound)
	absent := s.c.do(op, http.MethodPost, refPath("/endpoints", absentRef, ":retry"), map[string]any{}, http.StatusNotFound)
	if h, a := strings.ReplaceAll(detail(t, hidden), hiddenRef, "REF"), strings.ReplaceAll(detail(t, absent), absentRef, "REF"); h != a {
		t.Errorf("out-of-scope retry by address refuses %q, absent refuses %q", h, a)
	}
	if strings.Contains(string(hidden), s.httpsB) || strings.Contains(string(hidden), s.dispB) {
		t.Errorf("the refusal leaked room-b's uuids: %s", hidden)
	}
	s.c.do(op, http.MethodPost, refPath("/endpoints", s.httpsB, ":retry"), map[string]any{}, http.StatusNotFound)

	for _, ref := range []string{s.addrA + "/https", s.httpsA} {
		s.c.do(viewer, http.MethodPost, refPath("/endpoints", ref, ":retry"), map[string]any{}, http.StatusForbidden)
	}
}

// TestSecretNameAPI drives #1133's secret acceptance: `PATCH /secrets/<name>`
// rotates a secret by name, a name sealed at two owners refuses with both
// owner-qualified spellings, `$platform/<name>` and `<owner address>/<name>`
// pick one, the uuid keeps working, and a scoped operator resolves the one it
// can see.
func TestSecretNameAPI(t *testing.T) {
	s := newNameAddressStack(t)
	plat := s.idOf(s.c.do(s.owner, http.MethodPost, "/secrets", map[string]any{"name": "demo-panel-login", "secret_type": "basic-auth", "owner_kind": "platform",
		"fields": map[string]string{"username": "admin", "password": "newtron"}}, http.StatusCreated))

	rotate := map[string]any{"fields": map[string]string{"password": "rotated"}}
	if got := s.idOf(s.c.do(s.owner, http.MethodPatch, refPath("/secrets", "demo-panel-login", ""), rotate, http.StatusOK)); got != plat {
		t.Errorf("PATCH by name answered %s, want %s", got, plat)
	}
	room := s.idOf(s.c.do(s.owner, http.MethodPost, "/secrets", map[string]any{"name": "demo-panel-login", "secret_type": "basic-auth", "owner_kind": "location", "owner": "campus.room-a",
		"fields": map[string]string{"username": "admin", "password": "room"}}, http.StatusCreated))

	msg := detail(t, s.c.do(s.owner, http.MethodPatch, refPath("/secrets", "demo-panel-login", ""), rotate, http.StatusNotFound))
	for _, want := range []string{`secret "demo-panel-login" not found`, "$platform/demo-panel-login", "campus.room-a/demo-panel-login"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ambiguous refusal %q does not name %q", msg, want)
		}
	}
	if got := s.idOf(s.c.do(s.owner, http.MethodPatch, refPath("/secrets", "$platform/demo-panel-login", ""), rotate, http.StatusOK)); got != plat {
		t.Errorf("PATCH $platform/demo-panel-login answered %s, want %s", got, plat)
	}
	var revealed struct {
		Fields map[string]string `json:"fields"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodPost, refPath("/secrets", "campus.room-a/demo-panel-login", ":reveal"), nil, http.StatusOK), &revealed); err != nil || revealed.Fields["password"] != "room" {
		t.Errorf("reveal by owner address = %v (err %v), want the room's", revealed.Fields, err)
	}
	if got := s.idOf(s.c.do(s.owner, http.MethodPatch, refPath("/secrets", room, ""), rotate, http.StatusOK)); got != room {
		t.Errorf("PATCH by uuid answered %s, want %s", got, room)
	}

	roomA := entityID(t, s.c, s.owner, "/locations", "room-a")
	op := principalWithGrants(t, s.ctx, s.dsn, "op-room-a", []grant{{role: "operator", scopeKind: "location", scopeID: roomA}})
	if got := s.idOf(s.c.do(op, http.MethodPatch, refPath("/secrets", "demo-panel-login", ""), rotate, http.StatusOK)); got != room {
		t.Errorf("room-a operator's PATCH by name answered %s, want the room's %s", got, room)
	}
	hidden := s.c.do(op, http.MethodPatch, refPath("/secrets", "$platform/demo-panel-login", ""), rotate, http.StatusNotFound)
	if strings.Contains(string(hidden), plat) {
		t.Errorf("a scoped refusal leaked the platform secret's uuid: %s", hidden)
	}
}

// TestGroupFileVariableGrantNamesAPI covers the rest of #1133's surface over
// HTTP: a principal group and its members by name, a file by filename, a
// variable by name, and a grant's scope root by dotted address.
func TestGroupFileVariableGrantNamesAPI(t *testing.T) {
	s := newNameAddressStack(t)

	group := s.idOf(s.c.do(s.owner, http.MethodPost, "/principal-groups", map[string]any{"name": "night-shift"}, http.StatusCreated))
	if got := s.idOf(s.c.do(s.owner, http.MethodGet, "/principal-groups/night-shift", nil, http.StatusOK)); got != group {
		t.Errorf("GET group by name answered %s, want %s", got, group)
	}
	alice := s.idOf(s.c.do(s.owner, http.MethodPost, "/principals", map[string]any{"username": "alice", "password": "orange-boat-42x"}, http.StatusCreated))
	s.c.do(s.owner, http.MethodPost, "/principal-groups/night-shift/members", map[string]any{"principal_id": "alice"}, http.StatusNoContent)
	s.c.do(s.owner, http.MethodPost, "/principal-groups/night-shift/members", map[string]any{"principal_id": "owner-all"}, http.StatusNoContent)
	s.c.do(s.owner, http.MethodDelete, "/principal-groups/night-shift/members/alice", nil, http.StatusNoContent)
	msg := detail(t, s.c.do(s.owner, http.MethodGet, "/principal-groups/day-shift", nil, http.StatusNotFound))
	if !strings.Contains(msg, `principal group "day-shift" not found`) {
		t.Errorf("unknown group refuses %q, want it named", msg)
	}

	content := base64.StdEncoding.EncodeToString([]byte("v2.1.4"))
	file := s.idOf(s.c.do(s.owner, http.MethodPost, "/files", map[string]any{"name": "codec-firmware-2.1.4.txt", "content_type": "text/plain", "content": content}, http.StatusCreated))
	if got := s.idOf(s.c.do(s.owner, http.MethodGet, refPath("/files", "codec-firmware-2.1.4.txt", ""), nil, http.StatusOK)); got != file {
		t.Errorf("GET file by filename answered %s, want %s", got, file)
	}
	s.c.do(s.owner, http.MethodGet, refPath("/files", "codec-firmware-2.1.4.txt", ":download"), nil, http.StatusOK)
	// A filename may carry a colon, which the router would read as a :verb; the
	// CLI percent-encodes it (pathSegment) and the server decodes it back.
	colon := s.idOf(s.c.do(s.owner, http.MethodPost, "/files", map[string]any{"name": "notes:v2.txt", "content_type": "text/plain", "content": content}, http.StatusCreated))
	var dl struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodGet, "/files/notes%3Av2.txt:download", nil, http.StatusOK), &dl); err != nil || dl.Name != "notes:v2.txt" {
		t.Errorf("download a colon-bearing filename = %+v (err %v)", dl, err)
	}
	if got := s.idOf(s.c.do(s.owner, http.MethodGet, "/files/notes%3Av2.txt", nil, http.StatusOK)); got != colon {
		t.Errorf("GET a colon-bearing filename answered %s, want %s", got, colon)
	}

	v := s.idOf(s.c.do(s.owner, http.MethodPost, "/variables", map[string]any{"name": "poll-interval", "value_type": "int", "owner_kind": "platform", "value": 30}, http.StatusCreated))
	if got := s.idOf(s.c.do(s.owner, http.MethodPatch, refPath("/variables", "poll-interval", ""), map[string]any{"value": 60}, http.StatusOK)); got != v {
		t.Errorf("PATCH variable by name answered %s, want %s", got, v)
	}

	var gr struct {
		ScopeID string `json:"scope_id"`
	}
	roomA := entityID(t, s.c, s.owner, "/locations", "room-a")
	if err := json.Unmarshal(s.c.do(s.owner, http.MethodPost, "/principals/alice/grants",
		map[string]any{"role": "viewer", "scope_kind": "location", "scope_id": "campus.room-a"}, http.StatusCreated), &gr); err != nil || gr.ScopeID != roomA {
		t.Errorf("grant scoped to campus.room-a stored %q (err %v), want the room's uuid %s", gr.ScopeID, err, roomA)
	}
	_ = alice
}
