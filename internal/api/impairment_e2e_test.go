package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type impairmentBody struct {
	Unplugged bool    `json:"unplugged"`
	LatencyMs int     `json:"latency_ms"`
	JitterMs  int     `json:"jitter_ms"`
	LossPct   float64 `json:"loss_pct"`
	RateKbit  int     `json:"rate_kbit"`
}

// TestComponentImpairmentAPI drives GET/PUT /components/{name}/impairment:
// an operator breaks and repairs a simulated device, a viewer can look but
// not break, a value the network cannot apply is a 422, and the node's
// simulation view shows the device impaired.
func TestComponentImpairmentAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge"}, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dsp"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	comp, node := "dsp", "edge"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: &comp, Node: &node,
		Params: []byte(`{"target":"127.60.0.5:1710"}`)}, all); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	var got impairmentBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/dsp/impairment", nil, http.StatusOK), &got); err != nil {
		t.Fatal(err)
	}
	if got != (impairmentBody{}) {
		t.Fatalf("a fresh device reads %+v, want healthy", got)
	}
	c.do(ownerTok, http.MethodPut, "/components/dsp/impairment", map[string]any{"unplugged": true}, http.StatusOK)
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/dsp/impairment", nil, http.StatusOK), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Unplugged {
		t.Fatalf("after unplugging: %+v", got)
	}
	var view struct {
		Devices []struct {
			Impairment *impairmentBody `json:"impairment"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/nodes/edge/simulation", nil, http.StatusOK), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Devices) != 1 || view.Devices[0].Impairment == nil || !view.Devices[0].Impairment.Unplugged {
		t.Fatalf("simulation view = %+v, want the device shown unplugged", view)
	}

	c.do(ownerTok, http.MethodPut, "/components/dsp/impairment", map[string]any{"latency_ms": 10, "jitter_ms": 50}, http.StatusUnprocessableEntity)

	viewer := setupAllViewer(t, ctx, dsn, "impair-viewer")
	c.do(viewer, http.MethodGet, "/components/dsp/impairment", nil, http.StatusOK)
	c.do(viewer, http.MethodPut, "/components/dsp/impairment", map[string]any{}, http.StatusForbidden)
	c.do(ownerTok, http.MethodGet, "/components/ghost/impairment", nil, http.StatusNotFound)

	// An all-zero body is a healthy device: replugged.
	c.do(ownerTok, http.MethodPut, "/components/dsp/impairment", map[string]any{}, http.StatusOK)
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/dsp/impairment", nil, http.StatusOK), &got); err != nil {
		t.Fatal(err)
	}
	if got != (impairmentBody{}) {
		t.Fatalf("after replugging: %+v", got)
	}
}
