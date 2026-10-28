package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestCreateStandardFromTemplateAPI drives POST /standards:createFromTemplate: a viewer is refused and
// nothing is written, an admin forks a template into a standard of their own
// with one audit row naming the template, the same name again is a 409, a
// second name forks an independent standard, a variant whose parent is not
// held is a 422 naming the template to fork first, and an unknown template is
// a 404. A fresh install holds no standard until one is forked.
func TestCreateStandardFromTemplateAPI(t *testing.T) {
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
	owner := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	viewer := principalWithGrants(t, ctx, dsn, "fork-viewer", []grant{{role: "viewer", scopeKind: "all"}})

	type std struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Label    string `json:"label"`
		Official bool   `json:"official"`
		Parent   string `json:"parent_standard"`
	}
	var list struct {
		Standards []std `json:"standards"`
	}
	count := func() int {
		t.Helper()
		if err := json.Unmarshal(c.do(owner, http.MethodGet, "/standards", nil, http.StatusOK), &list); err != nil {
			t.Fatalf("decode standards: %v", err)
		}
		return len(list.Standards)
	}
	if n := count(); n != 0 {
		t.Fatalf("a fresh install holds %d standards, want none", n)
	}

	c.do(viewer, http.MethodPost, "/standards:createFromTemplate", map[string]any{"template": "meeting-room"}, http.StatusForbidden)
	if n := count(); n != 0 {
		t.Fatalf("a refused fork wrote %d standards", n)
	}

	var forked std
	if err := json.Unmarshal(c.do(owner, http.MethodPost, "/standards:createFromTemplate", map[string]any{"template": "meeting-room"}, http.StatusCreated), &forked); err != nil {
		t.Fatalf("decode fork: %v", err)
	}
	if forked.Name != "meeting-room" || forked.Official {
		t.Fatalf("forked = %+v, want an operator-owned meeting-room", forked)
	}
	var audit struct {
		Events []struct {
			ResourceID string          `json:"resource_id"`
			New        json.RawMessage `json:"new"`
		} `json:"events"`
	}
	if err := json.Unmarshal(c.do(owner, http.MethodGet, "/audit-log?resource=standard&verb=create", nil, http.StatusOK), &audit); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if len(audit.Events) != 1 || audit.Events[0].ResourceID != forked.ID || !strings.Contains(string(audit.Events[0].New), `"from_template":"meeting-room"`) {
		t.Fatalf("fork audit = %+v, want one row naming the template", audit.Events)
	}

	c.do(owner, http.MethodPost, "/standards:createFromTemplate", map[string]any{"template": "meeting-room"}, http.StatusConflict)
	var second std
	if err := json.Unmarshal(c.do(owner, http.MethodPost, "/standards:createFromTemplate",
		map[string]any{"template": "meeting-room", "name": "hq-meeting", "label": "HQ Meeting"}, http.StatusCreated), &second); err != nil {
		t.Fatalf("decode second fork: %v", err)
	}
	if second.ID == forked.ID || second.Label != "HQ Meeting" {
		t.Fatalf("second fork = %+v, want an independent standard", second)
	}

	detail := string(c.do(owner, http.MethodPost, "/standards:createFromTemplate", map[string]any{"template": "ds55"}, http.StatusUnprocessableEntity))
	if !strings.Contains(detail, "create a standard from digital-sign first") {
		t.Fatalf("variant refusal = %s, want it to name the template to fork first", detail)
	}
	var variant std
	if err := json.Unmarshal(c.do(owner, http.MethodPost, "/standards:createFromTemplate",
		map[string]any{"template": "mr55", "parent": "hq-meeting"}, http.StatusCreated), &variant); err != nil {
		t.Fatalf("decode variant: %v", err)
	}
	if variant.Parent != "hq-meeting" {
		t.Fatalf("variant parent = %q, want the one named", variant.Parent)
	}
	c.do(owner, http.MethodPost, "/standards:createFromTemplate", map[string]any{"template": "mr55-system"}, http.StatusNotFound)
	c.do(owner, http.MethodPost, "/standards:createFromTemplate", map[string]any{"template": "boardroom"}, http.StatusNotFound)
	if n := count(); n != 3 {
		t.Fatalf("after the forks: %d standards, want 3", n)
	}
}
