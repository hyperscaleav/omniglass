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
)

type fleetViewWire struct {
	Locations []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		LocationType string `json:"location_type"`
		Parent       string `json:"parent"`
		Verdict      string `json:"verdict"`
	} `json:"locations"`
	Systems []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Location string `json:"location"`
		Verdict  string `json:"verdict"`
		Dots     []struct {
			Component string `json:"component"`
			Name      string `json:"name"`
			Verdict   string `json:"verdict"`
			Primary   bool   `json:"primary"`
			Shared    bool   `json:"shared"`
		} `json:"dots"`
	} `json:"systems"`
}

// TestFleetViewAPI drives the fleet projection over HTTP as the canvas will.
// Two claims are worth driving end to end rather than at the gateway: that the
// wire carries dots and not component rows (a regression there is invisible on
// screen and costs a fleet-sized payload per paint), and that an alarm moves
// the dot, so the canvas is reading the same rollup the detail page reads
// rather than a second opinion.
func TestFleetViewAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
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

	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "hq", "location_type": "campus"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "hq-r1", "location_type": "room", "parent": "hq"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/standards", map[string]any{"name": "hq-room", "label": "HQ Room"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/standards/hq-room/roles/table-mic", map[string]any{
		"label": "Table Microphone", "quorum": 1,
		"accepted_types": []string{"video-bar"}, "impact": "outage",
	}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "hq-1", "standard_id": "hq-room", "location": "hq-r1"}, http.StatusCreated)
	bar := storagetest.MintProduct(t, ctx, gw, "video-bar")
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "bar-1", "product": bar.Name}, http.StatusCreated)
	c.do(ownerTok, http.MethodPut, "/systems/hq-1/roles/table-mic/assignments/bar-1", nil, http.StatusNoContent)

	view := func(tok string) fleetViewWire {
		t.Helper()
		var w fleetViewWire
		if err := json.Unmarshal(c.do(tok, http.MethodGet, "/views/fleet", nil, http.StatusOK), &w); err != nil {
			t.Fatalf("decode fleet view: %v", err)
		}
		return w
	}

	v := view(ownerTok)

	// The tree arrives flat, each node carrying its parent, because the client
	// assembles it and decides how the fleet is gathered into bands.
	var root, room struct {
		ID, Parent, Verdict string
	}
	for _, l := range v.Locations {
		switch l.Name {
		case "hq":
			root.ID, root.Parent, root.Verdict = l.ID, l.Parent, l.Verdict
			if l.LocationType != "campus" {
				t.Errorf("location type = %q, want campus: the band renders its chip from this", l.LocationType)
			}
		case "hq-r1":
			room.ID, room.Parent, room.Verdict = l.ID, l.Parent, l.Verdict
		}
	}
	if root.ID == "" || room.ID == "" {
		t.Fatalf("both locations must project, got %+v", v.Locations)
	}
	if root.Parent != "" {
		t.Error("a root location must carry no parent")
	}
	if room.Parent != root.ID {
		t.Errorf("room parent = %q, want the root's id %q", room.Parent, root.ID)
	}

	if len(v.Systems) != 1 || v.Systems[0].Name != "hq-1" {
		t.Fatalf("systems = %+v, want just hq-1", v.Systems)
	}
	sys := v.Systems[0]
	if sys.Location != room.ID {
		t.Errorf("system location = %q, want the room's id %q", sys.Location, room.ID)
	}
	if sys.Verdict != "healthy" {
		t.Errorf("staffed and quiet system verdict = %q, want healthy", sys.Verdict)
	}
	if len(sys.Dots) != 1 {
		t.Fatalf("dots = %+v, want one for bar-1", sys.Dots)
	}
	dot := sys.Dots[0]
	if dot.Component == "" || dot.Name != "bar-1" {
		t.Errorf("dot = %+v, want bar-1 with its component id", dot)
	}
	if !dot.Primary {
		t.Error("a component in its only system is primary there")
	}
	if dot.Shared {
		t.Error("a component in one system is not shared")
	}

	// Dots, not rows. The projection must not be quietly widened into a
	// component list: that is the whole reason it exists.
	raw := c.do(ownerTok, http.MethodGet, "/views/fleet", nil, http.StatusOK)
	var loose struct {
		Systems []struct {
			Dots []map[string]any `json:"dots"`
		} `json:"systems"`
	}
	if err := json.Unmarshal(raw, &loose); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	for _, s := range loose.Systems {
		for _, d := range s.Dots {
			for _, banned := range []string{"product", "tags", "properties", "effective_tags", "product_id", "location"} {
				if _, ok := d[banned]; ok {
					t.Errorf("a dot carries %q: the projection is a dot payload, not a component row", banned)
				}
			}
		}
	}

	// An alarm moves the dot and the verdicts above it, so the canvas reads the
	// same rollup the detail page does rather than a second opinion.
	c.do(ownerTok, http.MethodPost, "/components/bar-1/alarms", map[string]any{
		"severity": "critical", "message": "no route to host",
	}, http.StatusCreated)

	v = view(ownerTok)
	if got := v.Systems[0].Dots[0].Verdict; got != "outage" {
		t.Errorf("dot verdict after a critical alarm = %q, want outage", got)
	}
	if got := v.Systems[0].Verdict; got != "outage" {
		t.Errorf("system verdict after a critical alarm = %q, want outage", got)
	}
	for _, l := range v.Locations {
		if l.Name == "hq" && l.Verdict != "outage" {
			t.Errorf("root verdict = %q, want outage: worst-wins climbs the tree", l.Verdict)
		}
	}
}
