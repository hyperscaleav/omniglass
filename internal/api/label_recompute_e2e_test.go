package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The preview-then-apply verb over HTTP (#685): the operator gesture the
// epic's scale argument rests on, driven as an operator drives it.

type labelChangeWire struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

type labelRecomputeWire struct {
	Count   int               `json:"count"`
	Changed []labelChangeWire `json:"changed"`
}

func decodeRecompute(t *testing.T, what string, raw []byte) labelRecomputeWire {
	t.Helper()
	var w labelRecomputeWire
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatalf("decode %s: %v", what, err)
	}
	if w.Count != len(w.Changed) {
		t.Fatalf("%s reports count=%d with %d rows: the two must be the same number or the count is decoration", what, w.Count, len(w.Changed))
	}
	return w
}

func recomputeHarness(t *testing.T) (*apiClient, string, string, func()) {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		gw.Close()
		t.Fatalf("seed: %v", err)
	}
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		gw.Close()
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		gw.Close()
		t.Fatalf("bootstrap: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	return &apiClient{t: t, ctx: ctx, base: srv.URL}, ownerTok, dsn, func() {
		srv.Close()
		gw.Close()
	}
}

// A rule edit, previewed, applied, and applied again: the whole verb, on the
// wire, with the fleet checked between each step.
func TestRuleChangePreviewsThenAppliesOverHTTP(t *testing.T) {
	c, tok, _, stop := recomputeHarness(t)
	defer stop()

	c.do(tok, http.MethodPost, "/locations",
		map[string]any{"name": "hq", "location_type": "building"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/locations",
		map[string]any{"name": "room-a", "location_type": "room", "parent": "hq"}, http.StatusCreated)
	for range 3 {
		c.do(tok, http.MethodPost, "/components",
			map[string]any{"product": "samsung-qm55", "location": "room-a"}, http.StatusCreated)
	}
	// One label the operator typed, which no recompute may touch.
	c.do(tok, http.MethodPost, "/components",
		map[string]any{"name": "hand-named", "product": "samsung-qm55", "location": "room-a", "label": "The Operator's Own"}, http.StatusCreated)

	// The rule edit itself is a component_type edit, the tier an operator
	// actually reaches: it forks the shipped row rather than writing it.
	c.do(tok, http.MethodPatch, "/component-types/display",
		map[string]any{"label_rule": "{{.LocationLabel}} {{.TypeName}} {{.Ordinal}}"}, http.StatusOK)

	// Nothing has moved yet: editing a rule does not silently rewrite the fleet.
	before := decodePen(t, "before", c.do(tok, http.MethodGet, "/components/display-1", nil, http.StatusOK))
	if before.Label != "Display 1" {
		t.Fatalf("a rule edit rewrote a label on its own: %q", before.Label)
	}

	preview := decodeRecompute(t, "preview", c.do(tok, http.MethodPost, "/components:previewLabels", nil, http.StatusOK))
	if preview.Count != 3 {
		t.Fatalf("preview lists %d rows, want the three generated ones: %+v", preview.Count, preview.Changed)
	}
	for _, ch := range preview.Changed {
		if ch.Name == "hand-named" {
			t.Fatalf("the preview lists a row whose label the operator owns")
		}
		if ch.Kind != "component" || ch.From == ch.To {
			t.Fatalf("preview row %+v is not a component change", ch)
		}
	}

	// A preview writes nothing.
	still := decodePen(t, "after preview", c.do(tok, http.MethodGet, "/components/display-1", nil, http.StatusOK))
	if still.Label != before.Label {
		t.Fatalf("the preview changed a label: %q became %q", before.Label, still.Label)
	}

	applied := decodeRecompute(t, "apply", c.do(tok, http.MethodPost, "/components:recomputeLabels", nil, http.StatusOK))
	if applied.Count != preview.Count {
		t.Fatalf("the apply changed %d rows and the preview promised %d", applied.Count, preview.Count)
	}
	got := decodePen(t, "after apply", c.do(tok, http.MethodGet, "/components/display-1", nil, http.StatusOK))
	if got.Label != "Room A Display 1" {
		t.Fatalf("after the apply = %q, want %q", got.Label, "Room A Display 1")
	}
	if !got.LabelGenerated {
		t.Fatalf("the recompute took the pen: %+v", got)
	}
	owned := decodePen(t, "the operator's own", c.do(tok, http.MethodGet, "/components/hand-named", nil, http.StatusOK))
	if owned.Label != "The Operator's Own" || owned.LabelGenerated {
		t.Fatalf("the operator's label was rewritten: %+v", owned)
	}

	// Idempotent.
	again := decodeRecompute(t, "re-apply", c.do(tok, http.MethodPost, "/components:recomputeLabels", nil, http.StatusOK))
	if again.Count != 0 {
		t.Fatalf("a re-apply changed %d rows: %+v", again.Count, again.Changed)
	}
	rePreview := decodeRecompute(t, "re-preview", c.do(tok, http.MethodPost, "/components:previewLabels", nil, http.StatusOK))
	if rePreview.Count != 0 {
		t.Fatalf("a preview after an apply lists %d rows", rePreview.Count)
	}
}

// A location recompute answers with the rows it stales as well as the rows it
// changes, so the operator sees the true blast radius rather than the first hop
// of it.
func TestALocationRecomputeReportsWhatItStales(t *testing.T) {
	c, tok, _, stop := recomputeHarness(t)
	defer stop()

	c.do(tok, http.MethodPost, "/locations",
		map[string]any{"name": "hq", "location_type": "building"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/locations",
		map[string]any{"name": "room-a", "location_type": "room", "parent": "hq"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/components",
		map[string]any{"product": "samsung-qm55", "location": "room-a"}, http.StatusCreated)
	c.do(tok, http.MethodPatch, "/component-types/display",
		map[string]any{"label_rule": "{{.LocationLabel}} {{.TypeName}}"}, http.StatusOK)
	c.do(tok, http.MethodPost, "/components:recomputeLabels", nil, http.StatusOK)
	c.do(tok, http.MethodPatch, "/location-types/room",
		map[string]any{"label_rule": "{{.TypeName}} {{.Name}}"}, http.StatusOK)

	preview := decodeRecompute(t, "location preview", c.do(tok, http.MethodPost, "/locations:previewLabels", nil, http.StatusOK))
	kinds := map[string]int{}
	for _, ch := range preview.Changed {
		kinds[ch.Kind]++
	}
	if kinds["location"] != 1 {
		t.Fatalf("preview changed %d locations, want the room: %+v", kinds["location"], preview.Changed)
	}
	if kinds["component"] != 1 {
		t.Fatalf("preview reported %d components, want the one whose LocationLabel the new room label moves: %+v", kinds["component"], preview.Changed)
	}

	applied := decodeRecompute(t, "location apply", c.do(tok, http.MethodPost, "/locations:recomputeLabels", nil, http.StatusOK))
	if applied.Count != preview.Count {
		t.Fatalf("apply changed %d rows and the preview promised %d", applied.Count, preview.Count)
	}
	got := decodePen(t, "the component below", c.do(tok, http.MethodGet, "/components/display-1", nil, http.StatusOK))
	if got.Label != "Room room-a Display" {
		t.Fatalf("the component reads %q, want %q: its location's new label", got.Label, "Room room-a Display")
	}
}

// A viewer can neither preview nor apply, on any of the three kinds: both
// halves are gated by the entity's own :update, and a viewer holds only :read.
func TestAViewerCannotPreviewOrRecomputeLabels(t *testing.T) {
	c, tok, dsn, stop := recomputeHarness(t)
	defer stop()
	c.do(tok, http.MethodPost, "/locations",
		map[string]any{"name": "hq", "location_type": "building"}, http.StatusCreated)

	viewerTok := principalWithGrants(t, context.Background(), dsn, "label-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	for _, collection := range []string{"/components", "/systems", "/locations"} {
		c.do(viewerTok, http.MethodPost, collection+":previewLabels", nil, http.StatusForbidden)
		c.do(viewerTok, http.MethodPost, collection+":recomputeLabels", nil, http.StatusForbidden)
	}
	// And the fleet is untouched, which is the half a 403 alone does not say.
	loc := decodePen(t, "hq", c.do(tok, http.MethodGet, "/locations/hq", nil, http.StatusOK))
	if loc.Name != "hq" {
		t.Fatalf("hq = %+v", loc)
	}
}

// A preview lists exactly what the apply then changes, for a caller whose read
// scope is WIDER than their update scope: both halves are bounded by the update
// scope, so a preview never promises a row the apply will refuse to touch.
//
// It has its own test because the owner every other case here uses holds both
// at "all", so a preview wired to the read scope alone, or a handler that
// passed the read scope twice, would be indistinguishable there. Neither is
// hypothetical: passing the read scope twice is the mutation that survived the
// first pass of this file, and previewing on the read scope alone is what the
// verb did until ADR-0100's scoping was applied to both halves.
func TestAPreviewIsBoundedByTheUpdateScopeJustAsTheApplyIs(t *testing.T) {
	c, tok, dsn, stop := recomputeHarness(t)
	defer stop()
	ctx := context.Background()

	c.do(tok, http.MethodPost, "/locations",
		map[string]any{"name": "hq", "location_type": "building"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/locations",
		map[string]any{"name": "room-a", "location_type": "room", "parent": "hq"}, http.StatusCreated)
	c.do(tok, http.MethodPost, "/locations",
		map[string]any{"name": "room-b", "location_type": "room", "parent": "hq"}, http.StatusCreated)
	mine := decodePen(t, "mine", c.do(tok, http.MethodPost, "/components",
		map[string]any{"name": "mine", "product": "samsung-qm55", "location": "room-a"}, http.StatusCreated))
	theirs := decodePen(t, "theirs", c.do(tok, http.MethodPost, "/components",
		map[string]any{"name": "theirs", "product": "samsung-qm55", "location": "room-b"}, http.StatusCreated))
	_ = theirs

	// A rule everything drifts against.
	c.do(tok, http.MethodPatch, "/component-types/display",
		map[string]any{"label_rule": "{{.LocationLabel}} {{.TypeName}}"}, http.StatusOK)

	// Reads the whole fleet, may update exactly one component.
	mineID := decodeID(t, c.do(tok, http.MethodGet, "/components/mine", nil, http.StatusOK))
	narrowTok := principalWithGrants(t, ctx, dsn, "narrow-writer", []grant{
		{role: "viewer", scopeKind: "all"},
		{role: "operator", scopeKind: "component", scopeID: mineID, scopeOp: "self"},
	})

	// The preview is bounded by the UPDATE scope too, so it lists the one row
	// the apply can reach and not the one it cannot.
	preview := decodeRecompute(t, "narrow preview", c.do(narrowTok, http.MethodPost, "/components:previewLabels", nil, http.StatusOK))
	if preview.Count != 1 || preview.Changed[0].Name != "mine" {
		t.Fatalf("the preview lists %+v, want only the one component in the caller's update scope", preview.Changed)
	}

	// The apply is the UPDATE scope, so it changes only one.
	applied := decodeRecompute(t, "narrow apply", c.do(narrowTok, http.MethodPost, "/components:recomputeLabels", nil, http.StatusOK))
	if applied.Count != 1 || applied.Changed[0].Name != "mine" {
		t.Fatalf("the apply changed %+v, want only the one component in the caller's update scope", applied.Changed)
	}
	// The invariant this test exists for: the two agree, row for row. A preview
	// that listed the row outside the update scope would be promising an edit
	// the apply then silently declines, with nothing on the wire explaining the
	// gap.
	if preview.Count != applied.Count || preview.Changed[0].ID != applied.Changed[0].ID {
		t.Fatalf("the preview promised %+v and the apply changed %+v", preview.Changed, applied.Changed)
	}
	// Both were hand-named, so neither carries an ordinal and the shipped rule
	// labelled them "Display". The one outside the update scope still does.
	after := decodePen(t, "theirs after", c.do(tok, http.MethodGet, "/components/theirs", nil, http.StatusOK))
	if after.Label != "Display" {
		t.Fatalf("a component outside the caller's update scope was restamped to %q", after.Label)
	}
	mineAfter := decodePen(t, "mine after", c.do(tok, http.MethodGet, "/components/mine", nil, http.StatusOK))
	if mineAfter.Label != "Room A Display" {
		t.Fatalf("the component inside the update scope reads %q, want %q: the apply must have done something, or the assertion above passes for the wrong reason", mineAfter.Label, "Room A Display")
	}
	_ = mine
}

func decodeID(t *testing.T, raw []byte) string {
	t.Helper()
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode id: %v", err)
	}
	if body.ID == "" {
		t.Fatalf("no id in %s", raw)
	}
	return body.ID
}
