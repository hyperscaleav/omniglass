package api_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Identity bindings over HTTP (#1385, ADR-1282): a binding ties a conduit
// component's reported identity to the system or component it names, and it
// costs a write on both of its ends (the ADR-0510 rule). Each test drives the
// routes as an operator or an integrator would, with principals built from
// grants the way the role-staffing tests build theirs.

type bindingWire struct {
	Conduit   string `json:"conduit"`
	ConduitID string `json:"conduit_id"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	OwnerKind string `json:"owner_kind"`
	Owner     string `json:"owner_name"`
	OwnerID   string `json:"owner_id"`
}

type bindingListWire struct {
	Conduit  string        `json:"conduit"`
	Bindings []bindingWire `json:"bindings"`
	Hidden   int           `json:"hidden"`
}

// bindingE2E is the fixture: a conduit (ucc-cloud), two rooms, a panel, and
// the custom roles the principals are built from, inserted before the first
// request builds the role index.
type bindingE2E struct {
	*staffingFixture
	ids map[string]string
}

func newBindingE2E(t *testing.T) *bindingE2E {
	f := &bindingE2E{staffingFixture: newStaffingFixture(t), ids: map[string]string{}}
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	for _, r := range []string{
		`insert into role (name, official, permissions, inherits) values ('conduit-editor', false, '{"component:read,update"}', '{}')`,
		`insert into role (name, official, permissions, inherits) values ('room-editor', false, '{"system:read,update"}', '{}')`,
	} {
		if _, err := conn.Exec(f.ctx, r); err != nil {
			t.Fatalf("insert role: %v", err)
		}
	}
	conn.Close(f.ctx)
	for _, s := range []string{"room-a", "room-b"} {
		f.c.do(f.owner, http.MethodPost, "/systems", map[string]any{"name": s}, http.StatusCreated)
		f.ids[s] = entityID(t, f.c, f.owner, "/systems", s)
	}
	for _, c := range []string{"ucc-cloud", "panel-1"} {
		f.component(c, f.display)
		f.ids[c] = entityID(t, f.c, f.owner, "/components", c)
	}
	return f
}

func (f *bindingE2E) principal(label string, grants ...grant) string {
	f.t.Helper()
	return principalWithGrants(f.t, f.ctx, f.dsn, label, grants)
}

func bindPath(conduit, key, value string) string {
	return "/components/" + conduit + "/identity-bindings/" + key + "/" + value
}

func ownerBody(kind, name string) map[string]any {
	return map[string]any{"owner_kind": kind, "owner": name}
}

func (f *bindingE2E) list(tok, conduit string) bindingListWire {
	f.t.Helper()
	var w bindingListWire
	if err := json.Unmarshal(f.c.do(tok, http.MethodGet, "/components/"+conduit+"/identity-bindings", nil, http.StatusOK), &w); err != nil {
		f.t.Fatalf("decode bindings: %v", err)
	}
	return w
}

// A binding round-trips and names both ends; a re-put is idempotent.
func TestIdentityBindingRoundTripsOverHTTP(t *testing.T) {
	f := newBindingE2E(t)
	var b bindingWire
	if err := json.Unmarshal(f.c.do(f.owner, http.MethodPut, bindPath("ucc-cloud", "ucc-room-id", "R-100"), ownerBody("system", "room-a"), http.StatusOK), &b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b.Conduit != "ucc-cloud" || b.ConduitID != f.ids["ucc-cloud"] || b.Key != "ucc-room-id" || b.Value != "R-100" ||
		b.OwnerKind != "system" || b.Owner != "room-a" || b.OwnerID != f.ids["room-a"] {
		t.Fatalf("put = %+v, want both ends named", b)
	}
	f.c.do(f.owner, http.MethodPut, bindPath("ucc-cloud", "ucc-room-id", "R-100"), ownerBody("system", f.ids["room-a"]), http.StatusOK)
	if l := f.list(f.owner, "ucc-cloud"); len(l.Bindings) != 1 || l.Hidden != 0 {
		t.Fatalf("list = %+v, want the one binding", l)
	}
	f.c.do(f.owner, http.MethodDelete, bindPath("ucc-cloud", "ucc-room-id", "R-100"), nil, http.StatusNoContent)
	f.c.do(f.owner, http.MethodDelete, bindPath("ucc-cloud", "ucc-room-id", "R-100"), nil, http.StatusNotFound)
}

// The conduit end alone cannot bind onto a system: holding no system:update
// at all is a 403 naming it, a system outside the caller's read is the
// non-disclosing 404, and one it reads but cannot write is a 403 naming the
// system:update scope.
func TestIdentityBindingNeedsTheOwnerEnd(t *testing.T) {
	f := newBindingE2E(t)
	conduit := grant{role: "conduit-editor", scopeKind: "component", scopeID: f.ids["ucc-cloud"]}
	conduitOnly := f.principal("conduit-only", conduit)
	otherRoom := f.principal("conduit-other-room", conduit, grant{role: "room-editor", scopeKind: "system", scopeID: f.ids["room-b"]})
	reader := f.principal("conduit-room-reader", conduit,
		grant{role: "room-editor", scopeKind: "system", scopeID: f.ids["room-b"]}, grant{role: "viewer", scopeKind: "all"})
	path := bindPath("ucc-cloud", "ucc-room-id", "R-1")

	if st, raw := f.c.send(conduitOnly, http.MethodPut, path, ownerBody("system", "room-a")); st != http.StatusForbidden || !strings.Contains(string(raw), "system:update") {
		t.Errorf("bind with no system:update = %d %s, want 403 naming system:update", st, raw)
	}
	if st, raw := f.c.send(otherRoom, http.MethodPut, path, ownerBody("system", "room-a")); st != http.StatusNotFound {
		t.Errorf("bind onto a system outside read = %d %s, want 404", st, raw)
	}
	if st, raw := f.c.send(reader, http.MethodPut, path, ownerBody("system", "room-a")); st != http.StatusForbidden || !strings.Contains(string(raw), "system:update scope") {
		t.Errorf("bind onto a readable, unwritable system = %d %s, want 403 naming the system:update scope", st, raw)
	}
	// A component owner the caller reads but cannot write is the owner end's
	// refusal, naming component:update.
	if st, raw := f.c.send(reader, http.MethodPut, path, ownerBody("component", "panel-1")); st != http.StatusForbidden || !strings.Contains(string(raw), "owner") {
		t.Errorf("bind onto a readable, unwritable component = %d %s, want 403 blaming the owner end", st, raw)
	}
	if l := f.list(f.owner, "ucc-cloud"); len(l.Bindings) != 0 {
		t.Fatalf("bindings = %+v, want none after every refusal", l.Bindings)
	}
	// Inside both ends it is served.
	f.c.do(otherRoom, http.MethodPut, path, ownerBody("system", "room-b"), http.StatusOK)
}

// The owner end alone cannot bind from a conduit: system:update on the room
// and no update on the conduit is refused.
func TestIdentityBindingNeedsTheConduitEnd(t *testing.T) {
	f := newBindingE2E(t)
	room := grant{role: "room-editor", scopeKind: "system", scopeID: f.ids["room-a"]}
	roomOnly := f.principal("room-only", room)
	roomPanel := f.principal("room-panel", room, grant{role: "conduit-editor", scopeKind: "component", scopeID: f.ids["panel-1"]})
	roomReader := f.principal("room-reader", room, grant{role: "conduit-editor", scopeKind: "component", scopeID: f.ids["panel-1"]}, grant{role: "viewer", scopeKind: "all"})
	path := bindPath("ucc-cloud", "ucc-room-id", "R-1")

	// component:update is the route's own gate, so this is the middleware's
	// fast-reject.
	if st, raw := f.c.send(roomOnly, http.MethodPut, path, ownerBody("system", "room-a")); st != http.StatusForbidden {
		t.Errorf("bind with no component:update = %d %s, want 403", st, raw)
	}
	if st, raw := f.c.send(roomPanel, http.MethodPut, path, ownerBody("system", "room-a")); st != http.StatusNotFound {
		t.Errorf("bind from a conduit outside read = %d %s, want 404", st, raw)
	}
	if st, raw := f.c.send(roomReader, http.MethodPut, path, ownerBody("system", "room-a")); st != http.StatusForbidden {
		t.Errorf("bind from a readable, unwritable conduit = %d %s, want 403", st, raw)
	}
	if l := f.list(f.owner, "ucc-cloud"); len(l.Bindings) != 0 {
		t.Fatalf("bindings = %+v, want none", l.Bindings)
	}
}

// A place or a node is no owner (422), and the match key must be registered
// (422).
func TestIdentityBindingRefusesPlacesNodesAndUnknownKeys(t *testing.T) {
	f := newBindingE2E(t)
	path := bindPath("ucc-cloud", "ucc-room-id", "R-1")
	for _, kind := range []string{"location", "node"} {
		if st, raw := f.c.send(f.owner, http.MethodPut, path, ownerBody(kind, "anything")); st != http.StatusUnprocessableEntity {
			t.Errorf("owner kind %s = %d %s, want 422", kind, st, raw)
		}
	}
	if st, raw := f.c.send(f.owner, http.MethodPut, bindPath("ucc-cloud", "no-such-key", "R-1"), ownerBody("system", "room-a")); st != http.StatusUnprocessableEntity || !strings.Contains(string(raw), "property type") {
		t.Errorf("unregistered key = %d %s, want 422 naming the property type registry", st, raw)
	}
}

// One identity has one owner, and moving it costs update on both owners.
func TestIdentityBindingMoveNeedsBothOwners(t *testing.T) {
	f := newBindingE2E(t)
	path := bindPath("ucc-cloud", "ucc-room-id", "R-1")
	f.c.do(f.owner, http.MethodPut, path, ownerBody("system", "room-a"), http.StatusOK)
	conduit := grant{role: "conduit-editor", scopeKind: "component", scopeID: f.ids["ucc-cloud"]}
	onlyB := f.principal("only-b", conduit, grant{role: "room-editor", scopeKind: "system", scopeID: f.ids["room-b"]})
	both := f.principal("both", conduit,
		grant{role: "room-editor", scopeKind: "system", scopeID: f.ids["room-a"]},
		grant{role: "room-editor", scopeKind: "system", scopeID: f.ids["room-b"]})

	if st, raw := f.c.send(onlyB, http.MethodPut, path, ownerBody("system", "room-b")); st != http.StatusForbidden {
		t.Errorf("move without update on the owner it leaves = %d %s, want 403", st, raw)
	}
	if l := f.list(f.owner, "ucc-cloud"); len(l.Bindings) != 1 || l.Bindings[0].Owner != "room-a" {
		t.Fatalf("after refused move = %+v, want still room-a", l.Bindings)
	}
	f.c.do(both, http.MethodPut, path, ownerBody("system", "room-b"), http.StatusOK)
	if l := f.list(f.owner, "ucc-cloud"); len(l.Bindings) != 1 || l.Bindings[0].Owner != "room-b" {
		t.Fatalf("after move = %+v, want one binding, to room-b", l.Bindings)
	}
	// Deleting needs update on the owner too: onlyB now holds it.
	f.c.do(onlyB, http.MethodDelete, path, nil, http.StatusNoContent)
}

// Deleting the owner or the conduit deletes the binding.
func TestIdentityBindingGoesWithEitherEndOverHTTP(t *testing.T) {
	f := newBindingE2E(t)
	f.c.do(f.owner, http.MethodPut, bindPath("ucc-cloud", "ucc-room-id", "R-A"), ownerBody("system", "room-a"), http.StatusOK)
	f.c.do(f.owner, http.MethodPut, bindPath("ucc-cloud", "ucc-room-id", "R-P"), ownerBody("component", "panel-1"), http.StatusOK)
	f.c.do(f.owner, http.MethodDelete, "/systems/room-a", nil, http.StatusNoContent)
	if l := f.list(f.owner, "ucc-cloud"); len(l.Bindings) != 1 || l.Bindings[0].Value != "R-P" {
		t.Fatalf("after deleting room-a = %+v, want only R-P", l.Bindings)
	}
	f.c.do(f.owner, http.MethodDelete, "/components/ucc-cloud", nil, http.StatusNoContent)
	if n := f.count(`select count(*) from identity_binding`); n != 0 {
		t.Fatalf("bindings after deleting the conduit = %d, want 0", n)
	}
}

// The list names only owners the caller can read, and counts the rest.
func TestIdentityBindingListCountsUnreadableOwners(t *testing.T) {
	f := newBindingE2E(t)
	f.c.do(f.owner, http.MethodPut, bindPath("ucc-cloud", "ucc-room-id", "R-A"), ownerBody("system", "room-a"), http.StatusOK)
	f.c.do(f.owner, http.MethodPut, bindPath("ucc-cloud", "ucc-room-id", "R-B"), ownerBody("system", "room-b"), http.StatusOK)
	f.c.do(f.owner, http.MethodPut, bindPath("ucc-cloud", "ucc-room-id", "R-P"), ownerBody("component", "panel-1"), http.StatusOK)
	narrow := f.principal("narrow-reader",
		grant{role: "conduit-editor", scopeKind: "component", scopeID: f.ids["ucc-cloud"]},
		grant{role: "room-editor", scopeKind: "system", scopeID: f.ids["room-a"]})
	l := f.list(narrow, "ucc-cloud")
	if len(l.Bindings) != 1 || l.Bindings[0].Owner != "room-a" || l.Hidden != 2 {
		t.Fatalf("narrow list = %+v, want room-a named and two counted", l)
	}
	if strings.Contains(string(f.c.do(narrow, http.MethodGet, "/components/ucc-cloud/identity-bindings", nil, http.StatusOK)), "room-b") {
		t.Fatal("the list named an owner the caller cannot read")
	}
	// A binding the caller cannot see is not deletable by it either, and the
	// refusal does not disclose it.
	if st, raw := f.c.send(narrow, http.MethodDelete, bindPath("ucc-cloud", "ucc-room-id", "R-B"), nil); st != http.StatusNotFound {
		t.Errorf("delete of a hidden binding = %d %s, want 404", st, raw)
	}
}

// A binding whose owner is its own conduit is a 422.
func TestIdentityBindingRefusesTheConduitAsOwnerOverHTTP(t *testing.T) {
	f := newBindingE2E(t)
	st, raw := f.c.send(f.owner, http.MethodPut, bindPath("ucc-cloud", "ucc-room-id", "R-1"), ownerBody("component", "ucc-cloud"))
	if st != http.StatusUnprocessableEntity || !strings.Contains(string(raw), "conduit") {
		t.Errorf("binding the conduit to itself = %d %s, want 422 naming the conduit", st, raw)
	}
}

// An identity value carrying reserved characters rides the path
// percent-encoded, one segment, and round-trips: the list reads back the
// decoded value, and the delete on the same encoded address removes it.
func TestIdentityBindingValueWithReservedCharactersRoundTrips(t *testing.T) {
	f := newBindingE2E(t)
	for _, v := range []string{"room/1", "a b", "x?y#z%", "R:1"} {
		p := bindPath("ucc-cloud", "ucc-room-id", strings.ReplaceAll(url.PathEscape(v), ":", "%3A"))
		var b bindingWire
		if err := json.Unmarshal(f.c.do(f.owner, http.MethodPut, p, ownerBody("system", "room-a"), http.StatusOK), &b); err != nil {
			t.Fatalf("decode %q: %v", v, err)
		}
		if b.Value != v {
			t.Errorf("put %q came back as %q", v, b.Value)
		}
		l := f.list(f.owner, "ucc-cloud")
		if len(l.Bindings) != 1 || l.Bindings[0].Value != v {
			t.Errorf("list after put %q = %+v, want exactly that value", v, l.Bindings)
		}
		f.c.do(f.owner, http.MethodDelete, p, nil, http.StatusNoContent)
	}
	if n := f.count(`select count(*) from identity_binding`); n != 0 {
		t.Fatalf("bindings = %d, want none after the deletes", n)
	}
}

// Deleting a match key a binding uses is a 409 saying bindings hold it.
func TestIdentityBindingKeyDeleteIsAConflictWhileBound(t *testing.T) {
	f := newBindingE2E(t)
	f.c.do(f.owner, http.MethodPost, "/property-types", map[string]any{"name": "amp-channel", "data_type": "string", "label": "Amp channel"}, http.StatusCreated)
	f.c.do(f.owner, http.MethodPut, bindPath("ucc-cloud", "amp-channel", "1"), ownerBody("system", "room-a"), http.StatusOK)
	st, raw := f.c.send(f.owner, http.MethodDelete, "/property-types/amp-channel", nil)
	if st != http.StatusConflict || !strings.Contains(string(raw), "identity binding") {
		t.Fatalf("delete a bound key = %d %s, want 409 naming identity bindings", st, raw)
	}
	f.c.do(f.owner, http.MethodDelete, bindPath("ucc-cloud", "amp-channel", "1"), nil, http.StatusNoContent)
	f.c.do(f.owner, http.MethodDelete, "/property-types/amp-channel", nil, http.StatusNoContent)
}
