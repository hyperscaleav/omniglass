package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// propRow mirrors one row of an owner's properties read, the fields this test
// reads: the resolution and the reconciliation block (#1297).
type propRow struct {
	Property       string          `json:"property_type_name"`
	Value          json.RawMessage `json:"value"`
	IsSet          bool            `json:"is_set"`
	FromContract   bool            `json:"from_contract"`
	SourceRule     string          `json:"source_rule"`
	Reconciliation *struct {
		Want       json.RawMessage `json:"want"`
		Told       json.RawMessage `json:"told"`
		Is         json.RawMessage `json:"is"`
		IsInstance string          `json:"is_instance"`
		Drift      bool            `json:"drift"`
	} `json:"reconciliation"`
}

// readProps GETs an owner's properties and indexes the rows by key.
func readProps(t *testing.T, c *apiClient, tok, path string) map[string]propRow {
	t.Helper()
	var body struct {
		Properties []propRow `json:"properties"`
	}
	if err := json.Unmarshal(c.do(tok, http.MethodGet, path, nil, http.StatusOK), &body); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	out := make(map[string]propRow, len(body.Properties))
	for _, p := range body.Properties {
		out[p.Property] = p
	}
	return out
}

// TestPropertiesCarryReconciliationAPI drives the one properties read over HTTP
// (#1297): a declared property a live observer disagrees with reads drift,
// naming the instance; a property only observed appears with want null and no
// drift; a system's declared row carries the same block; an out-of-scope
// component is the non-disclosing 404, a principal without component:read is
// refused, and the retired reconciliation route is gone. Skipped under -short.
func TestPropertiesCarryReconciliationAPI(t *testing.T) {
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

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	// A declared value (want) and an observed value (is) that differ, and a
	// key observed with nothing declared. The observations are the node lane's,
	// stamped with the endpoint whose task collected them.
	if _, err := gw.SetProperty(ctx, "", "component", "disp-1", "firmware-version", "", json.RawMessage(`"1.0.0"`), all, all); err != nil {
		t.Fatalf("set declared: %v", err)
	}
	now := time.Now().UTC()
	if err := gw.InsertPropertySamples(ctx, storage.NodeWriter("disp-1", "panel-ip"), []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "firmware-version", Instance: "panel-ip", Value: "2.0.0", TS: now},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "serial-number", Instance: "panel-ip", Value: "SN-42", TS: now},
	}); err != nil {
		t.Fatalf("insert observed: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	props := readProps(t, c, ownerTok, "/components/disp-1/properties")
	fw := props["firmware-version"]
	if r := fw.Reconciliation; r == nil || string(r.Want) != `"1.0.0"` || string(r.Is) != `"2.0.0"` || r.IsInstance != "panel-ip" || !r.Drift || string(r.Told) != "null" {
		t.Fatalf("firmware-version: want want=1.0.0 is=2.0.0 from panel-ip, drift, told null; got %+v", r)
	}
	sn, ok := props["serial-number"]
	if !ok {
		t.Fatalf("serial-number, observed only, is missing: %+v", props)
	}
	if r := sn.Reconciliation; r == nil || string(r.Want) != "null" || string(r.Is) != `"SN-42"` || r.Drift {
		t.Fatalf("serial-number: want want=null is=SN-42, no drift; got %+v", r)
	}
	if sn.IsSet || sn.FromContract || sn.Value != nil {
		t.Fatalf("serial-number: want no declared value, got %+v", sn)
	}

	// A system's properties carry the same block.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "huddle-1"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	if _, err := gw.SetProperty(ctx, "", "system", "huddle-1", "serial-number", "", json.RawMessage(`"S-1"`), all, all); err != nil {
		t.Fatalf("set on system: %v", err)
	}
	sys := readProps(t, c, ownerTok, "/systems/huddle-1/properties")
	if r := sys["serial-number"].Reconciliation; r == nil || string(r.Want) != `"S-1"` || string(r.Is) != "null" || r.Drift {
		t.Fatalf("system serial-number: want want=S-1, nothing observed, no drift; got %+v", r)
	}

	// A viewer scoped to a different component gets a non-disclosing 404 on
	// disp-1 and reads its own component; a principal holding no component:read
	// is refused outright.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-1"}, all, all, all, all); err != nil {
		t.Fatalf("create other component: %v", err)
	}
	other, err := gw.GetComponent(ctx, "other-1", all)
	if err != nil {
		t.Fatalf("get other: %v", err)
	}
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-other", "viewer", "component", other.ID)
	c.do(viewerTok, http.MethodGet, "/components/disp-1/properties", nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet, "/components/other-1/properties", nil, http.StatusOK)
	limitedTok := setupLimitedPrincipal(t, ctx, dsn)
	c.do(limitedTok, http.MethodGet, "/components/disp-1/properties", nil, http.StatusForbidden)

	// The reconciliation route is retired: nothing answers it.
	c.do(ownerTok, http.MethodGet, "/components/disp-1/reconciliation", nil, http.StatusNotFound)
}
