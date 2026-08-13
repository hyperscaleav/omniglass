package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A create that writes a membership is gated as a membership write (#707).
//
// POST /components accepts a system reference and inserts the component's
// PRIMARY MEMBERSHIP from it. That is the same row PUT
// /systems/{name}/members/{component} writes, and that route requires the
// system:update permission plus the system:update scope, while the create asked
// for neither: a principal holding component:create and no system permission at
// all could write a membership through the create that the membership route
// refused it. The architect's ruling (2026-08-12) shifts the create to
// system:update and accepts the consequence, which is that an operator-tier
// principal can no longer create a component INTO a system: an operator
// maintains components, a deploy tech builds out systems and their membership.

// TestCreatingAComponentIntoASystemNeedsTheMembershipPermission drives the
// permission half, with the two principals side by side so the refusal is a
// permission boundary and not a broken route.
func TestCreatingAComponentIntoASystemNeedsTheMembershipPermission(t *testing.T) {
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
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	rack := createdID(t, c.do(ownerTok, http.MethodPost, "/components", map[string]any{
		"name": "rack", "product": "generic-device",
	}, http.StatusCreated))
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{
		"name": "av", "system_type_id": "board",
	}, http.StatusCreated)

	// The operator tier: component:create,update,rename,move and no system
	// permission of any kind, beside the all-scoped viewer floor a real
	// principal carries, so system:READ is held and only system:update is not.
	// That is what makes this test about the permission that moved rather than
	// about visibility.
	operator := principalWithGrants(t, ctx, dsn, "rack-operator", []grant{
		{role: "operator", scopeKind: "component", scopeID: rack},
		{role: "viewer", scopeKind: "all"},
	})
	// The deploy tier holds both halves: it builds systems and their membership.
	deploy := principalWithGrants(t, ctx, dsn, "rack-deploy", []grant{
		{role: "deploy", scopeKind: "component", scopeID: rack},
		{role: "deploy", scopeKind: "system", scopeID: entityID(t, c, ownerTok, "/systems", "av")},
		{role: "viewer", scopeKind: "all"},
	})

	// The operator still creates components: only the membership is refused.
	c.do(operator, http.MethodPost, "/components", map[string]any{
		"name": "panel-a", "product": "generic-device", "parent": rack,
	}, http.StatusCreated)

	// The same body naming a system is refused, and the refusal names the
	// permission to ask for rather than a bare "forbidden": this message is what
	// the CLI prints, and an operator who cannot see why cannot ask for the fix.
	status, body := c.send(operator, http.MethodPost, "/components", map[string]any{
		"name": "panel-b", "product": "generic-device", "parent": rack, "system": "av",
	})
	if status != http.StatusForbidden {
		t.Fatalf("operator create naming a system = %d, want 403\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "system:update") {
		t.Errorf("403 body = %s, want it to name the system:update permission", body)
	}
	// Nothing was written: the refusal is not a half-create.
	c.do(operator, http.MethodGet, "/components/panel-b", nil, http.StatusNotFound)

	// The membership ROUTE refuses the same principal the same way, which is the
	// agreement the ruling is about: one permission, one answer, whichever path
	// writes the row.
	panelA := entityID(t, c, operator, "/components", "panel-a")
	if st, b := c.send(operator, http.MethodPut, "/systems/av/members/"+panelA, nil); st != http.StatusForbidden {
		t.Fatalf("operator membership write = %d, want 403 (the two paths must agree)\nbody: %s", st, b)
	}

	// The deploy principal runs the create the operator was refused, and the
	// membership it wrote is real.
	created := createdID(t, c.do(deploy, http.MethodPost, "/components", map[string]any{
		"name": "panel-c", "product": "generic-device", "parent": rack, "system": "av",
	}, http.StatusCreated))
	var members struct {
		Members []struct {
			Component string `json:"component"`
			Primary   bool   `json:"primary"`
		} `json:"members"`
	}
	if err := json.Unmarshal(c.do(deploy, http.MethodGet, "/systems/av/members", nil, http.StatusOK), &members); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(members.Members) != 1 || members.Members[0].Component != "panel-c" || !members.Members[0].Primary {
		t.Fatalf("members after the deploy create = %+v, want panel-c as the primary member", members.Members)
	}
	_ = created
}

// TestTheCreatesSystemBindResolvesInTheUpdateScope is the SCOPE half, which the
// permission test above cannot see: a principal holding system:update somewhere
// must not bind a system it may only READ. The narrow principal here holds the
// all-scoped viewer floor, so before this slice the bind resolved in a
// system:read scope covering the whole estate and the create was served.
func TestTheCreatesSystemBindResolvesInTheUpdateScope(t *testing.T) {
	f := newPlacementFixture(t)

	// The fixture's narrow principal, plus the read floor: system:read now
	// covers av-b, system:update still stops at av-a.
	floored := principalWithGrants(t, f.c.ctx, f.dsn, "wing-a-deploy-floored", []grant{
		{role: "deploy", scopeKind: "component", scopeID: f.rackID},
		{role: "deploy", scopeKind: "system", scopeID: f.sysAID},
		{role: "viewer", scopeKind: "all"},
	})

	// It reads the sibling system perfectly well.
	f.c.do(floored, http.MethodGet, "/systems/"+f.wingBSys, nil, http.StatusOK)

	// Its own system is bindable.
	f.c.do(floored, http.MethodPost, "/components", map[string]any{
		"name": "panel-in", "product": "samsung-qm55", "parent": f.rackID, "system": "av-a",
	}, http.StatusCreated)

	// The one it may only read is not. The refusal names the missing authority
	// rather than answering the non-disclosing not-found this expected before the
	// #707 review: that answer denied the existence of the very row the GET above
	// just returned, which an operator can only read as a broken platform.
	status, body := f.c.send(floored, http.MethodPost, "/components", map[string]any{
		"name": "panel-out", "product": "samsung-qm55", "parent": f.rackID, "system": f.wingBSys,
	})
	if status != http.StatusForbidden {
		t.Fatalf("bind of a readable system outside the update scope = %d, want 403\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "system:update") {
		t.Errorf("refusal = %s, want it to name the scope the bind resolves in", body)
	}

	// The owner runs the refused body and is served, so the refusal is a scope
	// boundary rather than a broken route.
	f.c.do(f.owner, http.MethodPost, "/components", map[string]any{
		"name": "panel-out", "product": "samsung-qm55", "parent": f.rackID, "system": f.wingBSys,
	}, http.StatusCreated)
}

// TestTheDraftRefusesTheSystemBindTheCreateRefuses is the PREVIEW half of the
// same gate (#713). The draft route resolved its system reference in
// system:read while the create beside it moved to system:update, so a caller
// could be served a preview naming a system it may read and may not bind, and
// the create it previewed then refused. Nothing hit it in practice because the
// console does not offer the picker to a principal that cannot use it, which
// restores the agreement by the caller's good behaviour rather than by the
// platform.
//
// Both halves of the create's gate are driven here, against the same principals
// the create's own tests use, and each refusal is asserted to be the SAME
// refusal on both routes rather than merely a refusal.
func TestTheDraftRefusesTheSystemBindTheCreateRefuses(t *testing.T) {
	f := newPlacementFixture(t)

	// The permission half: component:create and no system:update at all, beside
	// the all-scoped viewer floor, so system:read is held and only the write
	// permission is missing.
	operator := principalWithGrants(t, f.c.ctx, f.dsn, "rack-operator-draft", []grant{
		{role: "operator", scopeKind: "component", scopeID: f.rackID},
		{role: "viewer", scopeKind: "all"},
	})
	// The scope half: system:update held over av-a and not over av-b, which the
	// viewer floor still lets it read.
	floored := principalWithGrants(t, f.c.ctx, f.dsn, "wing-a-deploy-draft", []grant{
		{role: "deploy", scopeKind: "component", scopeID: f.rackID},
		{role: "deploy", scopeKind: "system", scopeID: f.sysAID},
		{role: "viewer", scopeKind: "all"},
	})

	// The same body drafts and creates, which is the property under test: a form
	// posts what it drafted with, plus the precondition.
	body := func(name, system string) map[string]any {
		return map[string]any{"name": name, "product": "samsung-qm55", "parent": f.rackID, "system": system}
	}

	// A draft naming no system is served to the operator, so what follows is
	// about the bind rather than about its right to draft at all.
	f.c.do(operator, http.MethodPost, "/components:renderLabel",
		map[string]any{"name": "panel-free", "product": "samsung-qm55", "parent": f.rackID}, http.StatusOK)

	// The permission half, on both routes, with the same status and the same
	// missing authority named.
	draftStatus, draftBody := f.c.send(operator, http.MethodPost, "/components:renderLabel", body("panel-p", "av-a"))
	createStatus, createBody := f.c.send(operator, http.MethodPost, "/components", body("panel-p", "av-a"))
	if draftStatus != http.StatusForbidden || draftStatus != createStatus {
		t.Fatalf("draft = %d, create = %d, want both 403: a preview must not be served for a create the platform refuses\ndraft body: %s", draftStatus, createStatus, draftBody)
	}
	if !strings.Contains(string(draftBody), "system:update") {
		t.Errorf("draft refusal = %s, want it to name the permission the create names", draftBody)
	}
	if !strings.Contains(string(createBody), "system:update") {
		t.Errorf("create refusal = %s, want it to name system:update", createBody)
	}

	// The scope half: the bindable system drafts, so the refusal below is a
	// scope boundary and not a broken route.
	f.c.do(floored, http.MethodPost, "/components:renderLabel", body("panel-in", "av-a"), http.StatusOK)

	draftStatus, draftBody = f.c.send(floored, http.MethodPost, "/components:renderLabel", body("panel-out", f.wingBSys))
	createStatus, createBody = f.c.send(floored, http.MethodPost, "/components", body("panel-out", f.wingBSys))
	if draftStatus != http.StatusForbidden || draftStatus != createStatus {
		t.Fatalf("draft = %d, create = %d, want both 403 for a readable system outside the update scope\ndraft body: %s", draftStatus, createStatus, draftBody)
	}
	if !strings.Contains(string(draftBody), "system:update") {
		t.Errorf("draft refusal = %s, want it to name the scope the bind resolves in", draftBody)
	}
	if strings.Contains(string(draftBody), "not found") {
		t.Errorf("draft refusal = %s, want it not to deny the existence of a system the caller just read", draftBody)
	}
	_ = createBody

	// A system the caller cannot read at all stays the non-disclosing refusal on
	// both routes: naming the missing authority is only safe once the caller has
	// proved it may see the row.
	narrowDraft, _ := f.c.send(f.narrow, http.MethodPost, "/components:renderLabel", body("panel-hidden", f.wingBSys))
	narrowCreate, _ := f.c.send(f.narrow, http.MethodPost, "/components", body("panel-hidden", f.wingBSys))
	if narrowDraft != http.StatusUnprocessableEntity || narrowDraft != narrowCreate {
		t.Errorf("unreadable system: draft = %d, create = %d, want both 422", narrowDraft, narrowCreate)
	}

	// The owner runs every refused body and is served, so none of the above is a
	// broken route.
	f.c.do(f.owner, http.MethodPost, "/components:renderLabel", body("panel-owner", f.wingBSys), http.StatusOK)
}
