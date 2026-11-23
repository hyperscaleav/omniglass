package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestHealthAPIOutsideSlotMember drives #2459 over HTTP as an operator would: a
// hand-built system (no standard, so no slots) with one member, a high alarm on
// that member, and the system's health read answering degraded with the member,
// its alarm, the outside-a-slot fact and the reason, then healthy on the clear.
// The place above rolls up the same verdict. Skipped under -short.
func TestHealthAPIOutsideSlotMember(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	tok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(tok, http.MethodPost, "/locations", map[string]any{"name": "hq", "location_type": "campus"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/systems", map[string]any{"name": "hand", "location": "hq"}, http.StatusCreated)
	p := storagetest.MintProduct(t, ctx, gw, "video-bar")
	c.do(tok, http.MethodPost, "/components", map[string]any{"name": "amp-1", "product": p.Name}, http.StatusCreated)
	c.do(tok, http.MethodPut, "/systems/hand/members/amp-1", nil, http.StatusNoContent)

	type wire struct {
		Health  string `json:"health"`
		Reason  string `json:"reason"`
		Members []struct {
			Name        string      `json:"name"`
			ID          string      `json:"id"`
			Health      string      `json:"health"`
			OutsideSlot bool        `json:"outside_slot"`
			ToAttach    bool        `json:"to_attach"`
			Alarms      []alarmWire `json:"alarms"`
		} `json:"members"`
	}
	read := func(path string) wire {
		t.Helper()
		var w wire
		if err := json.Unmarshal(c.do(tok, http.MethodGet, path, nil, http.StatusOK), &w); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return w
	}

	var raised alarmWire
	if err := json.Unmarshal(c.do(tok, http.MethodPost, "/components/amp-1/alarms", map[string]any{
		"severity": "high", "message": "powered off",
	}, http.StatusCreated), &raised); err != nil {
		t.Fatalf("decode raised: %v", err)
	}

	sys := read("/systems/hand/health")
	if sys.Health != "degraded" {
		t.Fatalf("system health = %q, want degraded: a member outside a slot never decides outage", sys.Health)
	}
	if !strings.Contains(sys.Reason, "1 member outside a slot has an open alarm (amp-1)") {
		t.Fatalf("reason = %q, want it to name amp-1", sys.Reason)
	}
	if len(sys.Members) != 1 {
		t.Fatalf("members = %+v, want amp-1 alone", sys.Members)
	}
	m := sys.Members[0]
	if m.Name != "amp-1" || m.ID == "" || !m.OutsideSlot || m.Health != "outage" || len(m.Alarms) != 1 || m.Alarms[0].ID != raised.ID {
		t.Fatalf("member = %+v, want amp-1 outside a slot, outage, with its alarm", m)
	}
	if loc := read("/locations/hq/health"); loc.Health != "degraded" || loc.Reason != "" || len(loc.Members) != 0 {
		t.Fatalf("location = %+v, want degraded, with no reason or members (a system's fields)", loc)
	}

	c.do(tok, http.MethodDelete, "/components/amp-1/alarms/"+raised.ID, nil, http.StatusNoContent)
	sys = read("/systems/hand/health")
	if sys.Health != "healthy" || len(sys.Members[0].Alarms) != 0 {
		t.Fatalf("after clear = %+v, want healthy with no open alarms", sys)
	}
	if sys.Reason != "no slot is impaired and no member has an open alarm" {
		t.Fatalf("reason after clear = %q", sys.Reason)
	}
}
