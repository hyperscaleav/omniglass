package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// deleteRefusalBody is the 409 a refused delete answers with (#2463): the
// problem detail every refusal carries, plus the typed list of what is in the
// way and a count of what the caller may not see.
type deleteRefusalBody struct {
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Blockers []struct {
		Kind  string `json:"kind"`
		ID    string `json:"id"`
		Name  string `json:"name"`
		Label string `json:"label"`
	} `json:"blockers"`
	BlockersHidden int `json:"blockers_hidden"`
}

func refusedDelete(t *testing.T, c *apiClient, tok, path string) deleteRefusalBody {
	t.Helper()
	status, body := c.send(tok, http.MethodDelete, path, nil)
	if status != http.StatusConflict {
		t.Fatalf("DELETE %s = %d, want 409\nbody: %s", path, status, body)
	}
	var out deleteRefusalBody
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode refusal: %v\nbody: %s", err, body)
	}
	return out
}

func (b deleteRefusalBody) named() []string {
	var out []string
	for _, x := range b.Blockers {
		out = append(out, x.Kind+":"+x.Name)
	}
	sort.Strings(out)
	return out
}

// TestRefusedDeleteNamesItsBlockers drives the three tree deletes into their
// refusals and reads what the 409 names: a component's child and its
// endpoint, a location's systems and components placed in it, a system's
// child. Each blocker carries its kind, id, address and label, so the console
// can link it and offer its own remove; clearing them lets the delete through.
func TestRefusedDeleteNamesItsBlockers(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "lab", "location_type": "campus"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/systems", map[string]any{"name": "rack", "location": "lab"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/systems", map[string]any{"name": "rack-sub", "parent": "rack"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "codec", "product": "generic-device", "location": "lab"}, http.StatusCreated)
	c.do(owner, http.MethodPost, "/components", map[string]any{"name": "codec-card", "product": "generic-device", "parent": "codec"}, http.StatusCreated)
	var ep struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(c.do(owner, http.MethodPost, "/endpoints", map[string]any{"transport": "icmp", "component": "codec"}, http.StatusCreated), &ep); err != nil {
		t.Fatalf("decode endpoint: %v", err)
	}

	// The component: its child and its endpoint, both named, with ids.
	got := refusedDelete(t, c, owner, "/components/codec")
	if want := []string{"component:codec-card", "endpoint:" + ep.Name}; strings.Join(got.named(), ",") != strings.Join(want, ",") {
		t.Fatalf("component blockers = %v, want %v (body %+v)", got.named(), want, got)
	}
	if got.BlockersHidden != 0 || got.Detail == "" {
		t.Fatalf("component refusal = %+v, want a detail and nothing hidden from the owner", got)
	}
	for _, b := range got.Blockers {
		if b.ID == "" || b.Label == "" {
			t.Fatalf("blocker %+v lacks an id or a label", b)
		}
		if b.Kind == "endpoint" && b.ID != ep.ID {
			t.Fatalf("endpoint blocker id = %s, want %s", b.ID, ep.ID)
		}
	}

	// The system: its child system.
	if got := refusedDelete(t, c, owner, "/systems/rack").named(); strings.Join(got, ",") != "system:rack-sub" {
		t.Fatalf("system blockers = %v, want [system:rack-sub]", got)
	}

	// The location: what is placed in it, both tiers.
	if got := refusedDelete(t, c, owner, "/locations/lab").named(); !containsAll(got, "component:codec", "system:rack") {
		t.Fatalf("location blockers = %v, want codec and rack among them", got)
	}

	// Removing what the refusal named lets the component delete through.
	c.do(owner, http.MethodDelete, "/endpoints/"+ep.ID, nil, http.StatusNoContent)
	c.do(owner, http.MethodDelete, "/components/codec-card", nil, http.StatusNoContent)
	c.do(owner, http.MethodDelete, "/components/codec", nil, http.StatusNoContent)
}

// TestRefusedDeleteCountsWhatTheCallerCannotSee is the non-disclosure case: a
// caller who may delete a component but cannot read the system whose slot it
// staffs is told that something is in the way and how many, never which. The
// owner, who can read it, is told which.
func TestRefusedDeleteCountsWhatTheCallerCannotSee(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := userWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	var cmp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(owner, http.MethodPost, "/components", map[string]any{"name": "dsp", "product": "generic-device"}, http.StatusCreated), &cmp); err != nil {
		t.Fatalf("decode dsp: %v", err)
	}
	var sys struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(owner, http.MethodPost, "/systems", map[string]any{"name": "secret-room"}, http.StatusCreated), &sys); err != nil {
		t.Fatalf("decode system: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.SetSystemSlot(ctx, "", "system", "secret-room", storage.SystemSlotSpec{Name: "mic", Label: "Mic", Quorum: 1, Impact: "degraded"}); err != nil {
		t.Fatalf("declare slot: %v", err)
	}
	if err := gw.AssignSlot(ctx, "", "secret-room", "mic", "dsp", all, all, all, all); err != nil {
		t.Fatalf("assign: %v", err)
	}

	// An admin over the component alone: it may delete dsp, and cannot read
	// the system.
	scoped := userWithGrants(t, ctx, dsn, "admin-dsp", []grant{{role: "admin", scopeKind: "component", scopeID: cmp.ID}})
	c.do(scoped, http.MethodGet, "/systems/secret-room", nil, http.StatusNotFound)

	status, raw := c.send(scoped, http.MethodDelete, "/components/dsp", nil)
	if status != http.StatusConflict {
		t.Fatalf("scoped delete = %d, want 409\nbody: %s", status, raw)
	}
	if strings.Contains(string(raw), sys.ID) || strings.Contains(string(raw), "secret-room") {
		t.Fatalf("refusal names a system the caller cannot read: %s", raw)
	}
	var got deleteRefusalBody
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Blockers) != 0 || got.BlockersHidden != 1 {
		t.Fatalf("scoped refusal = %+v, want no blocker named and one counted", got)
	}

	if got := refusedDelete(t, c, owner, "/components/dsp"); strings.Join(got.named(), ",") != "system:secret-room" || got.BlockersHidden != 0 {
		t.Fatalf("owner refusal = %+v, want the system named", got)
	}
}

func containsAll(have []string, want ...string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
