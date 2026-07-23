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

// locationPropertiesWire is the decoded effective-read body on the location arc.
type locationPropertiesWire struct {
	Location   string                  `json:"location"`
	Properties []effectivePropertyWire `json:"properties"`
}

// find returns the named property from the effective read, failing the test when
// the read does not carry it.
func (w locationPropertiesWire) find(t *testing.T, name string) effectivePropertyWire {
	t.Helper()
	for _, p := range w.Properties {
		if p.PropertyTypeName == name {
			return p
		}
	}
	t.Fatalf("property %q not in the effective read %+v", name, w.Properties)
	return effectivePropertyWire{}
}

// TestLocationPropertiesAPI drives the location effective-property surface over
// HTTP: the read resolves the location type contract's default until the location
// sets an override, an off-contract property lands beside the contract ones marked
// from_contract=false, clearing the override falls back to the default, and a
// clear of an unset property is a 404. An out-of-scope location is a
// non-disclosing 404 (permission passes, scope injection hides it). Skipped under
// -short.
func TestLocationPropertiesAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// The shipped location types are operator-owned, so room can carry the contract
	// under test directly. A room may not sit at the top (its allowed_parent_types
	// exclude root), so a campus goes in first.
	c.do(ownerTok, http.MethodPut, "/location-types/room/properties/serial_number",
		map[string]any{"default_value": "SN-DEFAULT", "required": true}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{
		"name": "hq", "location_type": "campus",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{
		"name": "room-1", "location_type": "room", "parent": "hq",
	}, http.StatusCreated)

	read := func(tok, name string) locationPropertiesWire {
		t.Helper()
		var w locationPropertiesWire
		if err := json.Unmarshal(c.do(tok, http.MethodGet, "/locations/"+name+"/properties", nil, http.StatusOK), &w); err != nil {
			t.Fatalf("decode properties: %v", err)
		}
		return w
	}

	// Unset: the contract's default is the effective value.
	got := read(ownerTok, "room-1")
	if got.Location != "room-1" || len(got.Properties) != 1 {
		t.Fatalf("effective read = %+v, want room-1 with one contract property", got)
	}
	sn := got.find(t, "serial_number")
	if sn.IsSet || !sn.FromContract || !sn.Required || sn.DataType != "string" {
		t.Fatalf("unset serial_number = %+v, want from-contract required string, is_set=false", sn)
	}
	if string(sn.Value) != `"SN-DEFAULT"` || string(sn.DefaultValue) != `"SN-DEFAULT"` || len(sn.SetValue) != 0 || sn.ValueID != "" {
		t.Fatalf("unset serial_number values = %+v, want the contract default with no override", sn)
	}

	// The override wins, and the read reports it as set.
	c.do(ownerTok, http.MethodPut, "/locations/room-1/properties/serial_number",
		map[string]any{"value": "SN-123"}, http.StatusOK)
	sn = read(ownerTok, "room-1").find(t, "serial_number")
	if !sn.IsSet || !sn.FromContract || sn.ValueID == "" {
		t.Fatalf("set serial_number = %+v, want is_set with a value id", sn)
	}
	if string(sn.Value) != `"SN-123"` || string(sn.SetValue) != `"SN-123"` || string(sn.DefaultValue) != `"SN-DEFAULT"` {
		t.Fatalf("set serial_number values = %+v, want SN-123 over the SN-DEFAULT default", sn)
	}

	// A property the contract does not declare is still settable, and reads back
	// beside the contract ones as an off-contract addition.
	c.do(ownerTok, http.MethodPut, "/locations/room-1/properties/model_number",
		map[string]any{"value": "MN-9"}, http.StatusOK)
	got = read(ownerTok, "room-1")
	if len(got.Properties) != 2 {
		t.Fatalf("effective read = %+v, want the contract property plus the off-contract one", got.Properties)
	}
	mn := got.find(t, "model_number")
	if mn.FromContract || mn.Required || !mn.IsSet || string(mn.Value) != `"MN-9"` {
		t.Fatalf("model_number = %+v, want an off-contract set value", mn)
	}
	if len(mn.DefaultValue) != 0 {
		t.Fatalf("model_number default = %s, want none (no contract line)", mn.DefaultValue)
	}

	// A property the catalog does not know, and a location that does not exist,
	// are request faults rather than 500s.
	c.do(ownerTok, http.MethodPut, "/locations/room-1/properties/not_a_property",
		map[string]any{"value": "x"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPut, "/locations/nope-1/properties/serial_number",
		map[string]any{"value": "x"}, http.StatusNotFound)

	// Clearing the override falls back to the contract default; clearing it twice
	// is an explicit miss.
	c.do(ownerTok, http.MethodDelete, "/locations/room-1/properties/serial_number", nil, http.StatusNoContent)
	sn = read(ownerTok, "room-1").find(t, "serial_number")
	if sn.IsSet || string(sn.Value) != `"SN-DEFAULT"` || len(sn.SetValue) != 0 {
		t.Fatalf("cleared serial_number = %+v, want the contract default back", sn)
	}
	c.do(ownerTok, http.MethodDelete, "/locations/room-1/properties/serial_number", nil, http.StatusNotFound)

	// A viewer scoped to another location reads its own but gets a non-disclosing
	// 404 on room-1: the *:read floor passes the gate, scope injection hides it.
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{
		"name": "annex", "location_type": "campus",
	}, http.StatusCreated)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var otherID string
	if err := conn.QueryRow(ctx, `select id from location where name = 'annex'`).Scan(&otherID); err != nil {
		t.Fatalf("other id: %v", err)
	}
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-other-loc", "viewer", "location", otherID)
	c.do(viewerTok, http.MethodGet, "/locations/room-1/properties", nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet, "/locations/annex/properties", nil, http.StatusOK)
}
