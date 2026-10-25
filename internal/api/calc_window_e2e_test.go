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

// TestCalcRuleWindowAPI drives a windowed input over HTTP (#1142): a rule
// written with an input's window_seconds reads it back on the input, the
// validate route binds the window as a list of records, and a window over an
// hour or on a system rule is a 422 placed on the input's window_seconds.
func TestCalcRuleWindowAPI(t *testing.T) {
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
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	c.do(tok, http.MethodPost, "/metric-types", map[string]any{"name": "temp-5m", "data_type": "float"}, http.StatusCreated)

	rule := map[string]any{"name": "temp-mean-5m", "owner_kind": "component",
		"inputs": []map[string]any{{"alias": "t", "key": "room-temperature", "window_seconds": 300}},
		"reduce": map[string]any{"expr": "avg(t)"}, "output_key": "temp-5m"}
	var made struct {
		Inputs []struct {
			Alias         string `json:"alias"`
			WindowSeconds int    `json:"window_seconds"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal(c.do(tok, http.MethodPost, "/calc-rules", rule, http.StatusCreated), &made); err != nil {
		t.Fatal(err)
	}
	if len(made.Inputs) != 1 || made.Inputs[0].WindowSeconds != 300 {
		t.Fatalf("made inputs %+v, want the 300s window", made.Inputs)
	}
	if got := string(c.do(tok, http.MethodGet, "/calc-rules/temp-mean-5m", nil, http.StatusOK)); !strings.Contains(got, `"window_seconds":300`) {
		t.Fatalf("read back %s, want window_seconds 300", got)
	}

	var v struct {
		OK       bool `json:"ok"`
		Bindings []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"bindings"`
	}
	if err := json.Unmarshal(c.do(tok, http.MethodPost, "/expressions:validate", map[string]any{"site": "calc", "source": "avg(t)",
		"key": "temp-5m", "inputs": []map[string]any{{"alias": "t", "key": "room-temperature", "window_seconds": 300}}}, http.StatusOK), &v); err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	for _, b := range v.Bindings {
		types[b.Name] = b.Type
	}
	if !v.OK || types["t"] != "[]record" || types["t[].value"] != "float64" || types["t[].at"] != "float64" {
		t.Fatalf("validate %+v, want avg over a window to check, t bound as records with value and at", v)
	}

	for name, in := range map[string]map[string]any{
		"over an hour": {"name": "too-long", "owner_kind": "component",
			"inputs": []map[string]any{{"alias": "t", "key": "room-temperature", "window_seconds": 7200}},
			"reduce": map[string]any{"expr": "avg(t)"}, "output_key": "temp-5m"},
		"system rule": {"name": "sys-window", "owner_kind": "system",
			"inputs": []map[string]any{{"alias": "t", "key": "room-temperature", "window_seconds": 60}},
			"reduce": map[string]any{"expr": "avg(t)"}, "output_key": "temp-5m"},
	} {
		p := problem{}
		if err := json.Unmarshal(c.do(tok, http.MethodPost, "/calc-rules", in, http.StatusUnprocessableEntity), &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Errors) != 1 || p.Errors[0].Location != "body.inputs[0].window_seconds" {
			t.Errorf("%s: %+v, want a 422 at body.inputs[0].window_seconds", name, p)
		}
	}
}
