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

// reconResp mirrors the reconciliation read body: per-property want/told/is and a
// drift flag.
type reconResp struct {
	Component  string `json:"component"`
	Properties []struct {
		Property string          `json:"property"`
		Want     json.RawMessage `json:"want"`
		Told     json.RawMessage `json:"told"`
		Is       json.RawMessage `json:"is"`
		Drift    bool            `json:"drift"`
	} `json:"properties"`
}

// TestReconciliationAPI drives the reconciliation read over HTTP: an owner sees a
// component's declared property pivoted against its observed value with drift
// computed on read; a viewer scoped to a different component gets a non-disclosing
// 404 (permission gate + scope injection). Skipped under -short.
func TestReconciliationAPI(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	// A declared value (want) and an observed value (is) that differ, so the read
	// reports drift.
	if _, err := gw.SetProperty(ctx, "", "component", "disp-1", "firmware-version", "", json.RawMessage(`"1.0.0"`), all); err != nil {
		t.Fatalf("set declared: %v", err)
	}
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{
		OwnerKind: "component", OwnerID: "disp-1", Key: "firmware-version",
		Instance: "", Value: "2.0.0", TS: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("insert observed: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	out := c.do(ownerTok, http.MethodGet, "/components/disp-1/reconciliation", nil, http.StatusOK)
	var r reconResp
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("decode reconciliation: %v", err)
	}
	if r.Component != "disp-1" {
		t.Fatalf("reconciliation: want disp-1, got %q", r.Component)
	}
	var found bool
	for _, p := range r.Properties {
		if p.Property != "firmware-version" {
			continue
		}
		found = true
		if string(p.Want) != `"1.0.0"` || string(p.Is) != `"2.0.0"` || !p.Drift {
			t.Fatalf("firmware-version pivot: want want=1.0.0 is=2.0.0 drift=true, got %+v", p)
		}
	}
	if !found {
		t.Fatalf("firmware-version not in reconciliation: %+v", r.Properties)
	}

	// A viewer scoped to a different component gets a non-disclosing 404 on disp-1,
	// and sees its own component's reconciliation.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-1"}, all, all, all); err != nil {
		t.Fatalf("create other component: %v", err)
	}
	other, err := gw.GetComponent(ctx, "other-1", all)
	if err != nil {
		t.Fatalf("get other: %v", err)
	}
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-other", "viewer", "component", other.ID)
	c.do(viewerTok, http.MethodGet, "/components/disp-1/reconciliation", nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet, "/components/other-1/reconciliation", nil, http.StatusOK)
}
