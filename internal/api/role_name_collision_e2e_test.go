package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestARoleNameAddressesOneRolePerSystem is #503: a role is addressed by name
// within a system, so the name must mean one role there. Every write that could
// give a system two roles of one name is refused, naming both owners: a system
// declaring a name its standard already declares, a standard declaring a name a
// conforming system already declares itself, and a system moving onto a
// standard that declares a name the system already does.
func TestARoleNameAddressesOneRolePerSystem(t *testing.T) {
	f := newStaffingFixture(t)
	c, owner := f.c, f.owner

	c.do(owner, http.MethodPost, "/standards", map[string]any{"name": "shadow-std", "label": "Shadow"}, http.StatusCreated)
	c.do(owner, http.MethodPatch, "/standards/shadow-std/roles/table-mic", map[string]any{
		"label": "Table mic", "accepted_types": []string{"video-bar"},
	}, http.StatusOK)
	c.do(owner, http.MethodPost, "/systems", map[string]any{"name": "shadow-sys", "standard_id": "shadow-std"}, http.StatusCreated)

	// The system declaring the inherited name is refused, naming the standard.
	st, raw := c.send(owner, http.MethodPatch, "/systems/shadow-sys/roles/table-mic", map[string]any{
		"label": "A second mic", "accepted_types": []string{"display"},
	})
	if st != http.StatusConflict || !strings.Contains(string(raw), "shadow-std") {
		t.Fatalf("a system role shadowing an inherited one = %d %s, want 409 naming the standard", st, raw)
	}
	if got := f.roles(owner, "shadow-sys"); len(got.Roles) != 1 || !got.Roles[0].FromStandard {
		t.Fatalf("roles after the refused shadow = %+v, want only the inherited table-mic", got.Roles)
	}
	// So staffing binds the one role the name addresses: the inherited one.
	f.component("shadow-bar", f.bar)
	c.do(owner, http.MethodPut, "/systems/shadow-sys/roles/table-mic/assignments/shadow-bar", nil, http.StatusNoContent)
	if mic := f.roles(owner, "shadow-sys").find(t, "table-mic"); len(mic.AssignedTo) != 1 || mic.AssignedTo[0] != "shadow-bar" {
		t.Fatalf("table-mic after staffing = %+v, want shadow-bar", mic)
	}

	// The standard declaring a name a conforming system already declares
	// itself is refused, naming that system.
	c.do(owner, http.MethodPatch, "/systems/shadow-sys/roles/ceiling-mic", map[string]any{"label": "Ceiling mic"}, http.StatusOK)
	st, raw = c.send(owner, http.MethodPatch, "/standards/shadow-std/roles/ceiling-mic", map[string]any{"label": "Ceiling mic"})
	if st != http.StatusConflict || !strings.Contains(string(raw), "shadow-sys") {
		t.Fatalf("a standard role shadowed by a conforming system's own = %d %s, want 409 naming the system", st, raw)
	}
	if n := f.count(`select count(*) from system_role where name = 'ceiling-mic' and standard_id = (select id from standard where name = 'shadow-std')`); n != 0 {
		t.Fatalf("the refused standard role was written anyway (%d rows)", n)
	}

	// A one-off system that declares table-mic itself cannot move onto the
	// standard that declares it too: the move is refused and nothing moves.
	c.do(owner, http.MethodPost, "/systems", map[string]any{"name": "lone-sys"}, http.StatusCreated)
	c.do(owner, http.MethodPatch, "/systems/lone-sys/roles/table-mic", map[string]any{"label": "Own mic"}, http.StatusOK)
	st, raw = c.send(owner, http.MethodPatch, "/systems/lone-sys", map[string]any{"standard_id": "shadow-std"})
	if st != http.StatusConflict || !strings.Contains(string(raw), "table-mic") {
		t.Fatalf("moving onto a standard that declares the system's own role name = %d %s, want 409 naming the role", st, raw)
	}
	var sys struct {
		Standard string `json:"standard"`
	}
	if err := json.Unmarshal(c.do(owner, http.MethodGet, "/systems/lone-sys", nil, http.StatusOK), &sys); err != nil {
		t.Fatalf("decode system: %v", err)
	}
	if sys.Standard != "" {
		t.Fatalf("lone-sys standard after the refused move = %q, want none", sys.Standard)
	}
}

// TestARoleCollisionNamesOnlyAReadableSystem: the 409 for a standard role a
// conforming system already declares names that system to a caller who may
// read it, and only to one. standard:update can be held beside a narrow grant
// that reads no system at all, and the refusal must not tell that caller which
// system exists or what roles it declares.
func TestARoleCollisionNamesOnlyAReadableSystem(t *testing.T) {
	f := newStaffingFixture(t)
	c, owner := f.c, f.owner
	c.do(owner, http.MethodPost, "/standards", map[string]any{"name": "redact-std", "label": "Redact"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/systems", map[string]any{"name": "secret-boardroom", "standard_id": "redact-std"}, http.StatusCreated)
	c.do(owner, http.MethodPatch, "/systems/secret-boardroom/roles/lectern-mic", map[string]any{"label": "Lectern mic"}, http.StatusOK)
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "redact-site", "location_type": "campus"}, http.StatusCreated)
	narrow := principalWithGrants(t, f.ctx, f.dsn, "catalog-admin", []grant{
		{role: "admin", scopeKind: "location", scopeID: entityID(t, c, owner, "/locations", "redact-site")},
	})
	c.do(narrow, http.MethodGet, "/systems/secret-boardroom", nil, http.StatusNotFound)

	st, raw := c.send(narrow, http.MethodPatch, "/standards/redact-std/roles/lectern-mic", map[string]any{"label": "Lectern mic"})
	if st != http.StatusConflict {
		t.Fatalf("a colliding standard role by a caller who reads no system = %d %s, want 409", st, raw)
	}
	if strings.Contains(string(raw), "secret-boardroom") {
		t.Fatalf("the refusal names a system the caller cannot read: %s", raw)
	}
	st, raw = c.send(owner, http.MethodPatch, "/standards/redact-std/roles/lectern-mic", map[string]any{"label": "Lectern mic"})
	if st != http.StatusConflict || !strings.Contains(string(raw), "secret-boardroom") {
		t.Fatalf("the same refusal to a caller who reads the system = %d %s, want 409 naming it", st, raw)
	}
}
