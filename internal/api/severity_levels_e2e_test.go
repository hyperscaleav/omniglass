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

type severityLevelBody struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Color    string `json:"color"`
	Rank     int    `json:"rank"`
	Official bool   `json:"official"`
	Outage   bool   `json:"outage"`
}

// TestSeverityLevelAPI drives the severity registry over HTTP: every reader
// lists the levels, least to most severe, each saying whether it puts a
// component in outage; an admin adds, renames and deletes operator levels;
// official levels are read-only; and raising an alarm names a level that
// exists.
func TestSeverityLevelAPI(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "sev-bar"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	viewer := setupAllViewer(t, ctx, dsn, "sev-viewer")

	var list struct {
		SeverityLevels []severityLevelBody `json:"severity_levels"`
	}
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/severity-levels", nil, http.StatusOK), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.SeverityLevels) != 5 || list.SeverityLevels[0].Name != "info" || list.SeverityLevels[4].Name != "disaster" {
		t.Fatalf("levels = %+v, want the five seeded, least severe first", list.SeverityLevels)
	}
	for _, l := range list.SeverityLevels {
		if l.Outage != (l.Rank >= 40) || !l.Official {
			t.Errorf("level %+v: want official, outage exactly at high and above", l)
		}
	}

	c.do(viewer, http.MethodPost, "/severity-levels", map[string]any{"name": "major", "label": "Major", "color": "orange", "rank": 35}, http.StatusForbidden)
	var major severityLevelBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/severity-levels",
		map[string]any{"name": "major", "label": "Major", "color": "orange", "rank": 35}, http.StatusCreated), &major); err != nil {
		t.Fatal(err)
	}
	if major.Official || major.Outage || major.Rank != 35 {
		t.Fatalf("major = %+v, want an operator level below the outage floor", major)
	}
	c.do(ownerTok, http.MethodPost, "/severity-levels", map[string]any{"name": "other", "color": "red", "rank": 35}, http.StatusConflict)
	c.do(ownerTok, http.MethodPost, "/severity-levels", map[string]any{"name": "pink", "color": "pink", "rank": 36}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPatch, "/severity-levels/high", map[string]any{"label": "Mine now"}, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/severity-levels/info", nil, http.StatusConflict)
	c.do(ownerTok, http.MethodGet, "/severity-levels/nope", nil, http.StatusNotFound)

	// Raising names a level that exists.
	c.do(ownerTok, http.MethodPost, "/components/sev-bar/alarms", map[string]any{"severity": "critical", "message": "old word"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/components/sev-bar/alarms", map[string]any{"severity": "major", "message": "fan noise"}, http.StatusCreated)

	// A rename carries the alarm; a level in use cannot be deleted.
	var renamed severityLevelBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/severity-levels/major", map[string]any{"name": "serious"}, http.StatusOK), &renamed); err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "serious" {
		t.Fatalf("renamed = %+v", renamed)
	}
	var alarms struct {
		Alarms []struct {
			Severity string `json:"severity"`
		} `json:"alarms"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/sev-bar/alarms", nil, http.StatusOK), &alarms); err != nil {
		t.Fatal(err)
	}
	if len(alarms.Alarms) != 1 || alarms.Alarms[0].Severity != "serious" {
		t.Fatalf("alarms = %+v, want the renamed level", alarms.Alarms)
	}
	c.do(ownerTok, http.MethodDelete, "/severity-levels/serious", nil, http.StatusConflict)
}
