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

// effectivePropertyWire is one decoded effective property: the resolved value
// plus the two flags the surface renders on (is_set, from_contract).
type effectivePropertyWire struct {
	PropertyName string          `json:"property_name"`
	DisplayName  string          `json:"display_name"`
	DataType     string          `json:"data_type"`
	Required     bool            `json:"required"`
	IsSet        bool            `json:"is_set"`
	FromContract bool            `json:"from_contract"`
	DefaultValue json.RawMessage `json:"default_value"`
	SetValue     json.RawMessage `json:"set_value"`
	Value        json.RawMessage `json:"value"`
	ValueID      string          `json:"value_id"`
}

// componentPropertiesWire is the decoded effective-read body.
type componentPropertiesWire struct {
	Component  string                  `json:"component"`
	Properties []effectivePropertyWire `json:"properties"`
}

// find returns the named property from the effective read, failing the test when
// the read does not carry it.
func (w componentPropertiesWire) find(t *testing.T, name string) effectivePropertyWire {
	t.Helper()
	for _, p := range w.Properties {
		if p.PropertyName == name {
			return p
		}
	}
	t.Fatalf("property %q not in the effective read %+v", name, w.Properties)
	return effectivePropertyWire{}
}

// TestComponentPropertiesAPI drives the component effective-property surface over
// HTTP: the read resolves the product contract's default until the component sets
// an override, an off-contract property lands beside the contract ones marked
// from_contract=false, clearing the override falls back to the default, and a
// clear of an unset property is a 404. An out-of-scope component is a
// non-disclosing 404 (permission passes, scope injection hides it). Skipped under
// -short.
func TestComponentPropertiesAPI(t *testing.T) {
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

	// A custom product declaring one property, and a component that is an instance
	// of it: the contract is what the effective read resolves against.
	c.do(ownerTok, http.MethodPost, "/products", map[string]any{
		"id": "acme-display", "display_name": "Acme Display", "kind": "device",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/products/acme-display/properties/serial_number",
		map[string]any{"default_value": "SN-DEFAULT", "required": true}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{
		"name": "disp-1", "product": "acme-display",
	}, http.StatusCreated)

	read := func(tok, name string) componentPropertiesWire {
		t.Helper()
		var w componentPropertiesWire
		if err := json.Unmarshal(c.do(tok, http.MethodGet, "/components/"+name+"/properties", nil, http.StatusOK), &w); err != nil {
			t.Fatalf("decode properties: %v", err)
		}
		return w
	}

	// Unset: the contract's default is the effective value.
	got := read(ownerTok, "disp-1")
	if got.Component != "disp-1" || len(got.Properties) != 1 {
		t.Fatalf("effective read = %+v, want disp-1 with one contract property", got)
	}
	sn := got.find(t, "serial_number")
	if sn.IsSet || !sn.FromContract || !sn.Required || sn.DataType != "string" {
		t.Fatalf("unset serial_number = %+v, want from-contract required string, is_set=false", sn)
	}
	if string(sn.Value) != `"SN-DEFAULT"` || string(sn.DefaultValue) != `"SN-DEFAULT"` || len(sn.SetValue) != 0 || sn.ValueID != "" {
		t.Fatalf("unset serial_number values = %+v, want the contract default with no override", sn)
	}

	// The override wins, and the read reports it as set.
	c.do(ownerTok, http.MethodPut, "/components/disp-1/properties/serial_number",
		map[string]any{"value": "SN-123"}, http.StatusOK)
	sn = read(ownerTok, "disp-1").find(t, "serial_number")
	if !sn.IsSet || !sn.FromContract || sn.ValueID == "" {
		t.Fatalf("set serial_number = %+v, want is_set with a value id", sn)
	}
	if string(sn.Value) != `"SN-123"` || string(sn.SetValue) != `"SN-123"` || string(sn.DefaultValue) != `"SN-DEFAULT"` {
		t.Fatalf("set serial_number values = %+v, want SN-123 over the SN-DEFAULT default", sn)
	}

	// A property the contract does not declare is still settable, and reads back
	// beside the contract ones as an off-contract addition.
	c.do(ownerTok, http.MethodPut, "/components/disp-1/properties/mac_address",
		map[string]any{"value": "aa:bb:cc:dd:ee:ff"}, http.StatusOK)
	got = read(ownerTok, "disp-1")
	if len(got.Properties) != 2 {
		t.Fatalf("effective read = %+v, want the contract property plus the off-contract one", got.Properties)
	}
	mac := got.find(t, "mac_address")
	if mac.FromContract || mac.Required || !mac.IsSet || string(mac.Value) != `"aa:bb:cc:dd:ee:ff"` {
		t.Fatalf("mac_address = %+v, want an off-contract set value", mac)
	}
	if len(mac.DefaultValue) != 0 {
		t.Fatalf("mac_address default = %s, want none (no contract line)", mac.DefaultValue)
	}

	// A property the catalog does not know, and a component that does not exist,
	// are request faults rather than 500s.
	c.do(ownerTok, http.MethodPut, "/components/disp-1/properties/not_a_property",
		map[string]any{"value": "x"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPut, "/components/nope-1/properties/serial_number",
		map[string]any{"value": "x"}, http.StatusNotFound)

	// Clearing the override falls back to the contract default; clearing it twice
	// is an explicit miss.
	c.do(ownerTok, http.MethodDelete, "/components/disp-1/properties/serial_number", nil, http.StatusNoContent)
	sn = read(ownerTok, "disp-1").find(t, "serial_number")
	if sn.IsSet || string(sn.Value) != `"SN-DEFAULT"` || len(sn.SetValue) != 0 {
		t.Fatalf("cleared serial_number = %+v, want the contract default back", sn)
	}
	c.do(ownerTok, http.MethodDelete, "/components/disp-1/properties/serial_number", nil, http.StatusNotFound)

	// A viewer scoped to another component reads its own but gets a non-disclosing
	// 404 on disp-1: the *:read floor passes the gate, scope injection hides the row.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{
		"name": "other-1",
	}, http.StatusCreated)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var otherID string
	if err := conn.QueryRow(ctx, `select id from component where name = 'other-1'`).Scan(&otherID); err != nil {
		t.Fatalf("other id: %v", err)
	}
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-other", "viewer", "component", otherID)
	c.do(viewerTok, http.MethodGet, "/components/disp-1/properties", nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet, "/components/other-1/properties", nil, http.StatusOK)
}
