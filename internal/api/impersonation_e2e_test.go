package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestImpersonationAPI drives impersonation end to end against the real binary: an
// owner acts as a scoped writer (mutations land in the target's scope and the audit
// row names BOTH the impersonated principal and the real owner), a view-as session
// is read-only, a lesser admin cannot impersonate the owner (the escalation guard),
// self is refused, a non-impersonator is capability-gated, and stopping the session
// kills the token. Skipped under -short.
func TestImpersonationAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A custom write-only location role for the target.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into role (name, official, permissions, inherits) values ('loc-writer', false, $1, '{}')`,
		[]string{"location:create,update,delete"}); err != nil {
		conn.Close(ctx)
		t.Fatalf("insert role: %v", err)
	}
	// user-admin: all-scope principal management (can reach impersonation) with no
	// infra authority. Inserted before the first request builds the lazy role index.
	if _, err := conn.Exec(ctx, `insert into role (name, official, permissions, inherits) values ('user-admin', false, $1, '{}')`,
		[]string{"principal:read,impersonate"}); err != nil {
		conn.Close(ctx)
		t.Fatalf("insert user-admin role: %v", err)
	}
	// grant-admin: authority over grants only (a non-tree resource), for the
	// non-tree scope-escalation case below. Inserted before the lazy role index.
	if _, err := conn.Exec(ctx, `insert into role (name, official, permissions, inherits) values ('grant-admin', false, $1, '{}')`,
		[]string{"principal_grant:create,delete"}); err != nil {
		conn.Close(ctx)
		t.Fatalf("insert grant-admin role: %v", err)
	}
	conn.Close(ctx)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	ownerID := meID(t, c, ownerTok)

	body := func(name, parent string) map[string]any {
		// A child needs a type placement-compatible with a campus parent
		// (allowed_parent_types constrains same-type nesting); campus at root,
		// building under it, since only the tree shape matters here.
		lt := "campus"
		if parent != "" {
			lt = "building"
		}
		b := map[string]any{"name": name, "location_type": lt}
		if parent != "" {
			b["parent"] = parent
		}
		return b
	}
	c.do(ownerTok, http.MethodPost, "/locations", body("root", ""), http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", body("child", "root"), http.StatusCreated)
	rootID := entityID(t, c, ownerTok, "/locations", "root")
	childID := entityID(t, c, ownerTok, "/locations", "child")

	// Target: a writer scoped to root, plus a full admin (for the escalation case).
	targetTok := principalWithGrants(t, ctx, dsn, "scoped-writer", []grant{{role: "loc-writer", scopeKind: "location", scopeID: rootID}})
	targetID := meID(t, c, targetTok)
	adminTok := principalWithGrants(t, ctx, dsn, "an-admin", []grant{{role: "admin", scopeKind: "all"}})
	patch := map[string]any{"display_name": "x"}

	// --- ACT-AS: owner acts as the scoped writer ---
	begin := func(id, mode string, want int) string {
		code, b := c.send(ownerTok, http.MethodPost, "/principals/"+id+":impersonate", map[string]any{"mode": mode})
		if code != want {
			t.Fatalf(":impersonate %s %s = %d, want %d (%s)", id, mode, code, want, b)
		}
		if want != http.StatusCreated {
			return ""
		}
		var r struct {
			Token string `json:"token"`
			Mode  string `json:"mode"`
		}
		if err := json.Unmarshal(b, &r); err != nil || r.Token == "" || r.Mode != mode {
			t.Fatalf("impersonate response: %s (err %v)", b, err)
		}
		return r.Token
	}
	actTok := begin(targetID, "act_as", http.StatusCreated)

	// The impersonation token acts in the TARGET's scope: a write under root works.
	c.do(actTok, http.MethodPatch, "/locations/child", patch, http.StatusOK)

	// The audit row for that write names the impersonated principal AND the real owner.
	var actor, realActor string
	ac, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("audit connect: %v", err)
	}
	defer ac.Close(ctx)
	if err := ac.QueryRow(ctx, `
		select actor_principal_id, real_actor_principal_id from audit_log
		where verb = 'update' and resource = 'location' and resource_id = $1
		order by ts desc limit 1`, childID).Scan(&actor, &realActor); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if actor != targetID || realActor != ownerID {
		t.Fatalf("audit actor=%s real_actor=%s, want target=%s real=owner=%s", actor, realActor, targetID, ownerID)
	}

	// Stopping the session kills the token on its next request.
	if code, _ := c.send(actTok, http.MethodPost, "/auth/me:stopImpersonation", nil); code != http.StatusNoContent {
		t.Fatalf("stop impersonation: want 204, got %d", code)
	}
	if code, _ := c.send(actTok, http.MethodGet, "/locations/child", nil); code != http.StatusUnauthorized {
		t.Fatalf("token after stop: want 401, got %d", code)
	}

	// --- VIEW-AS: read-only ---
	viewTok := begin(targetID, "view_as", http.StatusCreated)
	c.do(viewTok, http.MethodGet, "/locations/child", nil, http.StatusOK) // reads in target scope
	if code, _ := c.send(viewTok, http.MethodPatch, "/locations/child", patch); code != http.StatusForbidden {
		t.Fatalf("view-as write: want 403 read-only, got %d", code)
	}
	// The read-only guarantee holds even on the SELF-SCOPED routes that skip the
	// capability middleware (the bypass a require()-only enforcement would miss):
	// a view-as session cannot edit the target's profile or change its password.
	if code, _ := c.send(viewTok, http.MethodPatch, "/auth/me", map[string]any{"display_name": "hijacked"}); code != http.StatusForbidden {
		t.Fatalf("view-as self-profile update: want 403, got %d", code)
	}
	if code, _ := c.send(viewTok, http.MethodPost, "/auth/me:changePassword", map[string]any{"current_password": "x", "new_password": "yyyyyyyyyyyy"}); code != http.StatusForbidden {
		t.Fatalf("view-as change-password: want 403, got %d", code)
	}
	// But GET /auth/me still reads, and the session may end itself.
	c.do(viewTok, http.MethodGet, "/auth/me", nil, http.StatusOK)
	if code, _ := c.send(viewTok, http.MethodPost, "/auth/me:stopImpersonation", nil); code != http.StatusNoContent {
		t.Fatalf("view-as stop: want 204, got %d", code)
	}

	// --- ESCALATION GUARD: a lesser admin cannot impersonate the owner ---
	if code, b := c.send(adminTok, http.MethodPost, "/principals/"+ownerID+":impersonate", map[string]any{"mode": "act_as"}); code != http.StatusForbidden {
		t.Fatalf("admin impersonating owner: want 403 (escalation), got %d (%s)", code, b)
	}

	// --- OWNER PROTECTION: an owner target is un-impersonatable by ANYONE, including
	// another owner (not just blocked by the capability-cover arithmetic). Both modes. ---
	otherOwnerTok := principalWithGrants(t, ctx, dsn, "other-owner", []grant{{role: "owner", scopeKind: "all"}})
	otherOwnerID := meID(t, c, otherOwnerTok)
	for _, mode := range []string{"view_as", "act_as"} {
		if code, b := c.send(ownerTok, http.MethodPost, "/principals/"+otherOwnerID+":impersonate", map[string]any{"mode": mode}); code != http.StatusForbidden {
			t.Fatalf("owner impersonating another owner (%s): want 403 (owner protection), got %d (%s)", mode, code, b)
		}
	}

	// --- SELF: refused ---
	begin(ownerID, "act_as", http.StatusUnprocessableEntity)

	// --- CAPABILITY GATE: a viewer cannot impersonate at all ---
	viewerTok := principalWithGrants(t, ctx, dsn, "a-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	if code, _ := c.send(viewerTok, http.MethodPost, "/principals/"+targetID+":impersonate", map[string]any{"mode": "view_as"}); code != http.StatusForbidden {
		t.Fatalf("viewer impersonating: want 403 (no capability), got %d", code)
	}

	// --- SCOPE-ESCALATION GUARD: a split-grant admin (all-scope user-admin + scoped
	// infra) cannot ACT-AS into a DISJOINT infra scope (it would gain write there),
	// but MAY VIEW-AS (read-only grants no write authority). ---
	c.do(ownerTok, http.MethodPost, "/locations", body("campus-y", ""), http.StatusCreated)
	yID := entityID(t, c, ownerTok, "/locations", "campus-y")
	// A split-grant admin: all-scope user-admin (can reach impersonation) + deploy
	// scoped to root only; it has NO authority in campus-y.
	splitTok := principalWithGrants(t, ctx, dsn, "split-admin",
		[]grant{{role: "user-admin", scopeKind: "all"}, {role: "deploy", scopeKind: "location", scopeID: rootID}})
	// The target is a deployer scoped to the disjoint campus-y.
	yTargetTok := principalWithGrants(t, ctx, dsn, "y-deployer", []grant{{role: "deploy", scopeKind: "location", scopeID: yID}})
	yTargetID := meID(t, c, yTargetTok)

	if code, b := c.send(splitTok, http.MethodPost, "/principals/"+yTargetID+":impersonate", map[string]any{"mode": "act_as"}); code != http.StatusForbidden {
		t.Fatalf("split admin act-as into disjoint scope: want 403 (scope escalation), got %d (%s)", code, b)
	}
	// View-as of the same disjoint target is allowed (read-only).
	c.do(splitTok, http.MethodPost, "/principals/"+yTargetID+":impersonate", map[string]any{"mode": "view_as"}, http.StatusCreated)

	// --- NON-TREE SCOPE ESCALATION: the guard is resource-agnostic. A split-grant
	// admin who holds grant authority only through a SCOPED grant (which for a
	// non-tree resource resolves to an empty effective scope, so it cannot create a
	// single grant directly) must not gain all-scope grant authority by acting-as a
	// target that holds it. Without the all-scope-cover rule this laundered a full
	// account takeover (mint yourself owner@all); with it, act-as is refused and
	// view-as remains available. ---
	// Attacker: all-scope user-admin (reaches impersonation) + grant-admin scoped to
	// root only (non-tree, so effectively empty: cannot create grants on its own).
	grantSplitTok := principalWithGrants(t, ctx, dsn, "grant-split-admin",
		[]grant{{role: "user-admin", scopeKind: "all"}, {role: "grant-admin", scopeKind: "location", scopeID: rootID}})
	// Target holds grant authority at ALL scope.
	grantTargetTok := principalWithGrants(t, ctx, dsn, "grant-target", []grant{{role: "grant-admin", scopeKind: "all"}})
	grantTargetID := meID(t, c, grantTargetTok)

	if code, b := c.send(grantSplitTok, http.MethodPost, "/principals/"+grantTargetID+":impersonate", map[string]any{"mode": "act_as"}); code != http.StatusForbidden {
		t.Fatalf("act-as to launder all-scope grant authority: want 403 (scope escalation), got %d (%s)", code, b)
	}
	c.do(grantSplitTok, http.MethodPost, "/principals/"+grantTargetID+":impersonate", map[string]any{"mode": "view_as"}, http.StatusCreated)
}
