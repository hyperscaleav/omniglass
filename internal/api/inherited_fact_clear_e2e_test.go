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
		"name": "ifc-lapel-mic", "label": "Lapel Mic", "parent_id": "mic",
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
		"name": "ifc-stem-rule", "label": "Stem Rule", "parent_id": "mic", "stem": "good",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/system-types", map[string]any{
		"name": "ifc-sys-stem-rule", "label": "Sys Stem Rule", "parent_id": "room", "stem": "good",
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
		"name": "ifc-root", "label": "Root", "stem": "rootstem",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/system-types", map[string]any{
		"name": "ifc-sys-root", "label": "Sys Root", "stem": "sysrootstem",
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

// TestTheListingServesWhatAnInheritedFactWouldInheritAPI is #716's console half
// at the wire, and the acceptance the blade's placeholder rests on.
//
// The question a placeholder answers is "clear this box and you get what?", and
// that is NOT the question resolved_icon answers: a row that states its own
// value shows that value, so a console using it as a placeholder would print the
// string the operator had just deleted back at them. So the listing serves the
// nearest ANCESTOR's value beside the row's own, per fact, with the name of the
// ancestor it came from.
//
// The fixture is three deep on purpose. The middle type states an abbrev and
// nothing else, so the leaf takes its stem from the GRANDparent and its abbrev
// from the parent, and a source field that assumed "the parent" would be wrong
// on one of the two.
func TestTheListingServesWhatAnInheritedFactWouldInheritAPI(t *testing.T) {
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
		"name": "ifh-root", "label": "Root", "stem": "rootstem", "icon": "layers", "abbrev": "rt",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/component-types", map[string]any{
		"name": "ifh-mid", "label": "Mid", "parent_id": "ifh-root", "abbrev": "md",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/component-types", map[string]any{
		"name": "ifh-leaf", "label": "Leaf", "parent_id": "ifh-mid", "stem": "ownstem",
	}, http.StatusCreated)

	out := c.do(ownerTok, http.MethodGet, "/component-types", nil, http.StatusOK)
	var body struct {
		ComponentTypes []componentTypeWire `json:"component_types"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	rows := map[string]componentTypeWire{}
	for _, r := range body.ComponentTypes {
		rows[r.Name] = r
	}

	leaf := rows["ifh-leaf"]
	// The leaf states its own stem, so it SHOWS that. What it would inherit is
	// the grandparent's, two levels up, and that is the whole point of the pair.
	if leaf.Stem != "ownstem" {
		t.Fatalf("leaf stem = %q, want its own %q", leaf.Stem, "ownstem")
	}
	if leaf.InheritedStem != "rootstem" || leaf.InheritedStemSource != "ifh-root" {
		t.Fatalf("leaf inherited_stem = %q from %q, want %q from the GRANDparent %q",
			leaf.InheritedStem, leaf.InheritedStemSource, "rootstem", "ifh-root")
	}
	// The abbrev comes from one level up, on the same row, which is why the
	// source is per fact rather than per row.
	if leaf.InheritedAbbrev != "md" || leaf.InheritedAbbrevSource != "ifh-mid" {
		t.Fatalf("leaf inherited_abbrev = %q from %q, want %q from the parent %q",
			leaf.InheritedAbbrev, leaf.InheritedAbbrevSource, "md", "ifh-mid")
	}
	if leaf.InheritedIcon != "layers" || leaf.InheritedIconSource != "ifh-root" {
		t.Fatalf("leaf inherited_icon = %q from %q, want %q from %q",
			leaf.InheritedIcon, leaf.InheritedIconSource, "layers", "ifh-root")
	}

	// A root has nothing above it, so it carries no inherited answer at all and
	// the console has no placeholder to draw. Absent, not empty-string-present:
	// the fields are omitempty for exactly this.
	root := rows["ifh-root"]
	if root.InheritedStem != "" || root.InheritedStemSource != "" || root.InheritedIcon != "" || root.InheritedAbbrev != "" {
		t.Fatalf("root = %+v, want no inherited facts: there is nothing above a root", root)
	}
	if !strings.Contains(string(out), `"inherited_stem"`) {
		t.Fatal("the listing carries no inherited_stem at all, so the omitempty above proved nothing")
	}

	// And the sentinel clear moves the row from stating its own to inheriting,
	// where what it shows and what it inherits become the same string. This is
	// the transition an operator makes by emptying the box.
	c.do(ownerTok, http.MethodPatch, "/component-types/ifh-leaf", map[string]any{"stem": ""}, http.StatusOK)
	// A FRESH decode target, not the one above. encoding/json reuses a slice's
	// existing elements and unmarshals into them field by field, so a key the
	// new payload omits keeps the old decode's value: `stem` is omitempty, so
	// re-decoding into `body` would read back the stem this clear just removed
	// and the assertion would be measuring the decoder.
	var afterClear struct {
		ComponentTypes []componentTypeWire `json:"component_types"`
	}
	out = c.do(ownerTok, http.MethodGet, "/component-types", nil, http.StatusOK)
	if err := json.Unmarshal(out, &afterClear); err != nil {
		t.Fatalf("decode list after clear: %v", err)
	}
	for _, r := range afterClear.ComponentTypes {
		if r.Name != "ifh-leaf" {
			continue
		}
		if r.Stem != "" {
			t.Fatalf("cleared leaf stem = %q, want it back to inheriting", r.Stem)
		}
		if r.InheritedStem != "rootstem" || r.InheritedStemSource != "ifh-root" {
			t.Fatalf("cleared leaf inherited_stem = %q from %q, want %q from %q",
				r.InheritedStem, r.InheritedStemSource, "rootstem", "ifh-root")
		}
	}
}

// TestTheSystemTypeListingServesItsInheritedFactsAPI: the system registry nests
// by the same rule and serves the same pair, so the console has one vocabulary
// across both blades rather than a fact the component page can teach and the
// system page cannot.
func TestTheSystemTypeListingServesItsInheritedFactsAPI(t *testing.T) {
	ctx := context.Background()
	gw := storagetest.NewDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/system-types", map[string]any{
		"name": "ifh-sys-root", "label": "Sys Root", "stem": "sysroot", "icon": "layers", "abbrev": "sr",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/system-types", map[string]any{
		"name": "ifh-sys-mid", "label": "Sys Mid", "parent_id": "ifh-sys-root", "abbrev": "sm",
	}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/system-types", map[string]any{
		"name": "ifh-sys-leaf", "label": "Sys Leaf", "parent_id": "ifh-sys-mid",
	}, http.StatusCreated)

	out := c.do(ownerTok, http.MethodGet, "/system-types", nil, http.StatusOK)
	var body struct {
		SystemTypes []systemTypeWire `json:"system_types"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	var leaf systemTypeWire
	for _, r := range body.SystemTypes {
		if r.Name == "ifh-sys-leaf" {
			leaf = r
		}
	}
	if leaf.InheritedStem != "sysroot" || leaf.InheritedStemSource != "ifh-sys-root" {
		t.Fatalf("sys leaf inherited_stem = %q from %q, want %q from the grandparent %q",
			leaf.InheritedStem, leaf.InheritedStemSource, "sysroot", "ifh-sys-root")
	}
	if leaf.InheritedAbbrev != "sm" || leaf.InheritedAbbrevSource != "ifh-sys-mid" {
		t.Fatalf("sys leaf inherited_abbrev = %q from %q, want %q from the parent %q",
			leaf.InheritedAbbrev, leaf.InheritedAbbrevSource, "sm", "ifh-sys-mid")
	}
	if leaf.InheritedIcon != "layers" || leaf.InheritedIconSource != "ifh-sys-root" {
		t.Fatalf("sys leaf inherited_icon = %q from %q, want %q from %q",
			leaf.InheritedIcon, leaf.InheritedIconSource, "layers", "ifh-sys-root")
	}
}
