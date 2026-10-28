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
	"github.com/hyperscaleav/omniglass/internal/template"
)

// TestTemplatesAPI drives the catalog reads over HTTP as a viewer: the list
// is the binary's catalog by kind, one template reads whole, and an unknown
// name is a 404. Nothing a template describes is a row.
func TestTemplatesAPI(t *testing.T) {
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
	_ = bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	viewer := principalWithGrants(t, ctx, dsn, "tmpl-viewer", []grant{{role: "viewer", scopeKind: "all"}})

	type body struct {
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		Standard *struct {
			Parent string `json:"parent"`
			Roles  []struct {
				Name   string `json:"name"`
				Quorum int    `json:"quorum"`
			} `json:"roles"`
			Metrics []struct {
				Name string `json:"name"`
			} `json:"metrics"`
		} `json:"standard"`
	}
	var all struct {
		Templates []body `json:"templates"`
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/templates", nil, http.StatusOK), &all); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if got, want := len(all.Templates), len(template.Shipped().List("")); got != want {
		t.Fatalf("listed %d templates, want the catalog's %d", got, want)
	}

	var stds struct {
		Templates []body `json:"templates"`
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/templates?kind=standard_template", nil, http.StatusOK), &stds); err != nil {
		t.Fatalf("decode filtered: %v", err)
	}
	if len(stds.Templates) != len(template.Shipped().List(template.KindStandard)) {
		t.Fatalf("kind filter listed %d, want %d", len(stds.Templates), len(template.Shipped().List(template.KindStandard)))
	}
	for _, tm := range stds.Templates {
		if tm.Kind != string(template.KindStandard) || tm.Standard == nil {
			t.Fatalf("kind filter let through %s (%s)", tm.Name, tm.Kind)
		}
	}
	c.do(viewer, http.MethodGet, "/templates?kind=room_template", nil, http.StatusUnprocessableEntity)

	var one body
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/templates/mr55", nil, http.StatusOK), &one); err != nil {
		t.Fatalf("decode one: %v", err)
	}
	if one.Standard == nil || one.Standard.Parent != "meeting-room" || len(one.Standard.Roles) == 0 || len(one.Standard.Metrics) == 0 {
		t.Fatalf("mr55 = %+v, want a variant of meeting-room with roles and a metric contract", one)
	}
	for _, r := range one.Standard.Roles {
		if r.Quorum < 1 {
			t.Errorf("role %s reads quorum %d, want the floor of one", r.Name, r.Quorum)
		}
	}
	c.do(viewer, http.MethodGet, "/templates/no-such-template", nil, http.StatusNotFound)
}
