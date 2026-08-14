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
)

// #716 at the wire. The gateway honouring the sentinel is not enough on its own:
// `stem` carried minLength 1 and the name pattern on the PATCH body, so `""` was
// a 422 in Huma's validator BEFORE the handler ran, and the capability was
// unreachable from any client. These tests drive the empty box over HTTP, which
// is the only tier that can prove the validation change, and they pin the two
// things the relaxation must NOT cost: a malformed stem is still refused, and a
// root still cannot lose the stem its names are minted from.

// TestClearingAnInheritedComponentFactOverTheWireAPI is the acceptance an
// operator's emptied box travels: PATCH {"stem": ""} is a 200 and the row reads
// back declaring nothing, resolving its parent's glyph again on the listing.
func TestClearingAnInheritedComponentFactOverTheWireAPI(t *testing.T) {
	ctx := context.Background()
	gw := storagetest.NewDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	var child componentTypeWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/component-types", map[string]any{
		"name": "ifc-lapel-mic", "display_name": "Lapel Mic", "parent_id": "mic",
		"stem": "lapel", "icon": "radio", "abbrev": "lp",
	}, http.StatusCreated), &child); err != nil {
		t.Fatalf("decode create: %v", err)
	}

	var cleared componentTypeWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/component-types/ifc-lapel-mic",
		map[string]any{"stem": "", "icon": "", "abbrev": ""}, http.StatusOK), &cleared); err != nil {
		t.Fatalf("decode clear: %v", err)
	}
	if cleared.Stem != "" || cleared.Icon != "" || cleared.Abbrev != "" {
		t.Fatalf("cleared row = %+v, want all three facts back to inheriting", cleared)
	}

	// resolved_icon is the listing's inherited answer, so it is what proves the
	// walk resumed rather than the row simply going blank.
	out := c.do(ownerTok, http.MethodGet, "/component-types", nil, http.StatusOK)
	var body struct {
		ComponentTypes []componentTypeWire `json:"component_types"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	var row componentTypeWire
	for _, r := range body.ComponentTypes {
		if r.Name == "ifc-lapel-mic" {
			row = r
		}
	}
	if row.ResolvedIcon != "mic" {
		t.Fatalf("resolved_icon = %q, want the parent mic type's %q", row.ResolvedIcon, "mic")
	}
}

// TestClearingAStemDoesNotWeakenTheStemRuleAPI: the clearing spelling is an
// EMPTY alternation in the pattern, not the absence of one, so every malformed
// stem the rule refused before is still a 422 and only "" is admitted.
func TestClearingAStemDoesNotWeakenTheStemRuleAPI(t *testing.T) {
	ctx := context.Background()
	gw := storagetest.NewDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/component-types", map[string]any{
		"name": "ifc-stem-rule", "display_name": "Stem Rule", "parent_id": "mic", "stem": "good",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/system-types", map[string]any{
		"name": "ifc-sys-stem-rule", "display_name": "Sys Stem Rule", "parent_id": "room", "stem": "good",
	}, http.StatusCreated)

	for _, bad := range []string{"Bad Stem", "-leading-hyphen", "has space", "UPPER", "trailing space "} {
		c.do(ownerTok, http.MethodPatch, "/component-types/ifc-stem-rule",
			map[string]any{"stem": bad}, http.StatusUnprocessableEntity)
		c.do(ownerTok, http.MethodPatch, "/system-types/ifc-sys-stem-rule",
			map[string]any{"stem": bad}, http.StatusUnprocessableEntity)
	}

	// The one string the relaxation admits, on both registries.
	c.do(ownerTok, http.MethodPatch, "/component-types/ifc-stem-rule", map[string]any{"stem": ""}, http.StatusOK)
	c.do(ownerTok, http.MethodPatch, "/system-types/ifc-sys-stem-rule", map[string]any{"stem": ""}, http.StatusOK)
}

// TestClearingARootTypesStemIsA422API: a root has no ancestor to inherit from,
// so the refusal that guards create guards the clear too, on both registries,
// and it names the reason rather than returning a 500 from a later mint.
func TestClearingARootTypesStemIsA422API(t *testing.T) {
	ctx := context.Background()
	gw := storagetest.NewDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/component-types", map[string]any{
		"name": "ifc-root", "display_name": "Root", "stem": "rootstem",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/system-types", map[string]any{
		"name": "ifc-sys-root", "display_name": "Sys Root", "stem": "sysrootstem",
	}, http.StatusCreated)

	for path := range map[string]struct{}{"/component-types/ifc-root": {}, "/system-types/ifc-sys-root": {}} {
		out := c.do(ownerTok, http.MethodPatch, path, map[string]any{"stem": ""}, http.StatusUnprocessableEntity)
		if !strings.Contains(string(out), "must have a stem") {
			t.Fatalf("refusal on %s = %s, want it to name the missing stem", path, out)
		}
	}
}

// TestClearingAStemOnAShippedTypeForksAndTheWalkResumesAPI drives the FORK leg
// (#703, ADR-0106) through a consequence rather than through the column, because
// the column is the one thing the wire cannot see: `abbrev` is `omitempty`, so a
// shadow holding "" and a shadow holding no value read back identically. The
// name generator can tell them apart, since a resolved "" STOPS the inheritance
// walk and refuses.
//
// ceiling-mic is the fixture: a shipped child that declares no stem and inherits
// mic's, with a shipped product classified under it. Fork a stem onto it, clear
// it, and the drafted name has to go back to the parent's stem. A fork leg that
// stored the sentinel verbatim would refuse the draft instead.
func TestClearingAStemOnAShippedTypeForksAndTheWalkResumesAPI(t *testing.T) {
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	owner := principalWithGrants(t, ctx, dsn, "ifc-owner", []grant{{role: "owner", scopeKind: "all"}})

	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "ifc-hq", "location_type": "building"}, http.StatusCreated)
	draft := map[string]any{"product": "shure-mxa920", "location": "ifc-hq"}

	if got := renderLabelAt(t, c, owner, "/components:renderLabel", draft, http.StatusOK); got.Name != "mic-1" {
		t.Fatalf("drafted name = %q before any edit, want the inherited mic-1", got.Name)
	}

	var forked componentTypeWire
	if err := json.Unmarshal(c.do(owner, http.MethodPatch, "/component-types/ceiling-mic",
		map[string]any{"stem": "ceilmic"}, http.StatusOK), &forked); err != nil {
		t.Fatalf("decode fork: %v", err)
	}
	if !forked.Official || !forked.Forked || forked.Stem != "ceilmic" {
		t.Fatalf("forked = %+v, want the shipped row overridden with the operator's stem", forked)
	}
	if got := renderLabelAt(t, c, owner, "/components:renderLabel", draft, http.StatusOK); got.Name != "ceilmic-1" {
		t.Fatalf("drafted name = %q after the fork, want ceilmic-1", got.Name)
	}

	var cleared componentTypeWire
	if err := json.Unmarshal(c.do(owner, http.MethodPatch, "/component-types/ceiling-mic",
		map[string]any{"stem": ""}, http.StatusOK), &cleared); err != nil {
		t.Fatalf("decode clear: %v", err)
	}
	if cleared.Stem != "" {
		t.Fatalf("cleared = %+v, want the fork to declare no stem", cleared)
	}
	if got := renderLabelAt(t, c, owner, "/components:renderLabel", draft, http.StatusOK); got.Name != "mic-1" {
		t.Fatalf("drafted name = %q after the clear, want the walk to resume at mic-1", got.Name)
	}

	// Restore is still the way back out of a fork, cleared facts included.
	var restored componentTypeWire
	if err := json.Unmarshal(c.do(owner, http.MethodPost, "/component-types/ceiling-mic:restore", nil, http.StatusOK), &restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if restored.Forked {
		t.Fatalf("restored = %+v, want the fork discarded", restored)
	}
	if got := renderLabelAt(t, c, owner, "/components:renderLabel", draft, http.StatusOK); got.Name != "mic-1" {
		t.Fatalf("drafted name = %q after the restore, want the shipped mic-1", got.Name)
	}
}
