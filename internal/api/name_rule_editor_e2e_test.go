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

// #710 at the wire. The console can turn a name rule OFF but has no surface to
// turn one ON, and after #657's floor reversal no shipped location type carries
// one, so generated location names are reachable only by opting a type in. The
// console's half of that is an editor; the SERVER's half is answering what a
// rule produces, so the editor never restates the mint shape in TypeScript
// (#695 tracked exactly that duplication, #702 closed its naming half by having
// the server return the drafted name).

type nameRuleWire struct {
	Stem      string   `json:"stem"`
	BareFirst bool     `json:"bare_first"`
	Examples  []string `json:"examples"`
}

type locationTypeWire struct {
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	DisplayName        string        `json:"display_name"`
	Icon               string        `json:"icon"`
	AllowedParentTypes []string      `json:"allowed_parent_types"`
	NameRule           *nameRuleWire `json:"name_rule"`
	Official           bool          `json:"official"`
	Forked             bool          `json:"forked"`
}

func locationTypeByName(t *testing.T, c *apiClient, tok, name string) locationTypeWire {
	t.Helper()
	out := c.do(tok, http.MethodGet, "/location-types", nil, http.StatusOK)
	var body struct {
		LocationTypes []locationTypeWire `json:"location_types"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("decode location-types: %v", err)
	}
	for _, r := range body.LocationTypes {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("location type %q not in the listing", name)
	return locationTypeWire{}
}

// TestANameRuleReadsBackWithTheNamesItMintsAPI is the acceptance for the served
// answer, and it is a CONSEQUENCE test rather than a shape test: the examples a
// surface would display are compared with the names two nameless creates
// actually get. A restatement that drifted from the mint would fail here, which
// is the whole reason the field exists rather than a TypeScript function.
func TestANameRuleReadsBackWithTheNamesItMintsAPI(t *testing.T) {
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
	owner := principalWithGrants(t, ctx, dsn, "nre-owner", []grant{{role: "owner", scopeKind: "all"}})

	for _, tc := range []struct {
		typeName string
		rule     map[string]any
		want     []string
	}{
		{"nre-deck", map[string]any{"stem": ""}, []string{"1", "2"}},
		{"nre-wing", map[string]any{"stem": "wing"}, []string{"wing-1", "wing-2"}},
		{"nre-berth", map[string]any{"stem": "berth", "bare_first": true}, []string{"berth", "berth-2"}},
	} {
		t.Run(tc.typeName, func(t *testing.T) {
			var created locationTypeWire
			if err := json.Unmarshal(c.do(owner, http.MethodPost, "/location-types", map[string]any{
				"name": tc.typeName, "display_name": tc.typeName, "name_rule": tc.rule,
			}, http.StatusCreated), &created); err != nil {
				t.Fatalf("decode create: %v", err)
			}
			if created.NameRule == nil {
				t.Fatalf("created %+v, want the rule back", created)
			}
			if len(created.NameRule.Examples) != 2 ||
				created.NameRule.Examples[0] != tc.want[0] || created.NameRule.Examples[1] != tc.want[1] {
				t.Fatalf("examples = %v, want %v", created.NameRule.Examples, tc.want)
			}
			if listed := locationTypeByName(t, c, owner, tc.typeName); listed.NameRule == nil ||
				len(listed.NameRule.Examples) != 2 || listed.NameRule.Examples[0] != tc.want[0] {
				t.Fatalf("listed %+v, want the same examples the create returned", listed.NameRule)
			}

			// The parent is the bucket a location's ordinal is counted in, so
			// each case gets its own and the numbers start from 1 again.
			parent := tc.typeName + "-parent"
			c.do(owner, http.MethodPost, "/locations", map[string]any{"name": parent, "location_type": "building"}, http.StatusCreated)
			for i, want := range tc.want {
				var row struct {
					Name string `json:"name"`
				}
				if err := json.Unmarshal(c.do(owner, http.MethodPost, "/locations", map[string]any{
					"location_type": tc.typeName, "parent": parent,
				}, http.StatusCreated), &row); err != nil {
					t.Fatalf("decode create %d: %v", i, err)
				}
				if row.Name != want {
					t.Fatalf("the create stamped %q where the rule's examples showed %q", row.Name, want)
				}
			}
		})
	}
}

// TestSettingANameRuleOnAShippedTypeForksAndRestoresAPI is #703's fork through
// the surface #710 adds. No shipped location type carries a rule, so opting one
// IN is the ordinary way an operator reaches generated names, and it must fork
// the platform's row rather than write it, with :restore taking the rule away
// again.
func TestSettingANameRuleOnAShippedTypeForksAndRestoresAPI(t *testing.T) {
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
	owner := principalWithGrants(t, ctx, dsn, "nre-fork-owner", []grant{{role: "owner", scopeKind: "all"}})

	shipped := locationTypeByName(t, c, owner, "floor")
	if !shipped.Official || shipped.Forked || shipped.NameRule != nil {
		t.Fatalf("floor = %+v, want a shipped row carrying no rule (ADR-0103's reversal)", shipped)
	}

	var forked locationTypeWire
	if err := json.Unmarshal(c.do(owner, http.MethodPatch, "/location-types/floor",
		map[string]any{"name_rule": map[string]any{"stem": "level", "bare_first": true}}, http.StatusOK), &forked); err != nil {
		t.Fatalf("decode fork: %v", err)
	}
	if forked.ID != shipped.ID || !forked.Official || !forked.Forked {
		t.Fatalf("forked = %+v, want the same row overridden rather than a second one", forked)
	}
	if forked.NameRule == nil || len(forked.NameRule.Examples) != 2 ||
		forked.NameRule.Examples[0] != "level" || forked.NameRule.Examples[1] != "level-2" {
		t.Fatalf("forked rule = %+v, want the examples the suppressed mint produces", forked.NameRule)
	}

	// The rule reaches the generator through the fork, which is what makes the
	// opt-in real rather than decorative.
	c.do(owner, http.MethodPost, "/locations", map[string]any{"name": "nre-hq", "location_type": "building"}, http.StatusCreated)
	var minted struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(c.do(owner, http.MethodPost, "/locations",
		map[string]any{"location_type": "floor", "parent": "nre-hq"}, http.StatusCreated), &minted); err != nil {
		t.Fatalf("decode nameless floor create: %v", err)
	}
	if minted.Name != "level" {
		t.Fatalf("the nameless create stamped %q, want the forked rule's first example %q", minted.Name, "level")
	}

	var restored locationTypeWire
	if err := json.Unmarshal(c.do(owner, http.MethodPost, "/location-types/floor:restore", nil, http.StatusOK), &restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if restored.Forked || restored.NameRule != nil {
		t.Fatalf("restored = %+v, want the shipped ruleless row back", restored)
	}
	// And a nameless create refuses again, which is the shipped behaviour.
	c.do(owner, http.MethodPost, "/locations",
		map[string]any{"location_type": "floor", "parent": "nre-hq"}, http.StatusUnprocessableEntity)
}

// TestTheRefusalANameRuleEditorHasToSurfaceAPI pins WHICH 422 an operator can
// actually reach, because the console's job is to show that message and a
// screenshot of the wrong one proves nothing.
//
// The mint's own refusal (ErrInvalidNameRule, a rule that mints legally at
// ordinal 1 and illegally at the ceiling) is real and unit-tested, but it is
// UNREACHABLE through this body: `stem` carries maxLength 90, and 90 plus the
// widest ordinal is exactly the 100-character name cap, so every stem the
// schema admits mints legally at both ends. The refusals a surface has to
// render are therefore the schema's, and they are the ones asserted here.
func TestTheRefusalANameRuleEditorHasToSurfaceAPI(t *testing.T) {
	ctx := context.Background()
	gw := storagetest.NewDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	c.do(ownerTok, http.MethodPost, "/location-types",
		map[string]any{"name": "nre-refusal", "display_name": "Refusal"}, http.StatusCreated)

	stem := func(n int) string {
		s := make([]byte, n)
		for i := range s {
			s[i] = 'w'
		}
		return string(s)
	}
	// The longest stem the mint proves legal is also the longest the schema
	// admits, which is the coincidence that makes ErrInvalidNameRule
	// unreachable from here. Both halves are asserted so the day one moves the
	// other has to move with it.
	c.do(ownerTok, http.MethodPatch, "/location-types/nre-refusal",
		map[string]any{"name_rule": map[string]any{"stem": stem(90)}}, http.StatusOK)
	c.do(ownerTok, http.MethodPatch, "/location-types/nre-refusal",
		map[string]any{"name_rule": map[string]any{"stem": stem(91)}}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPatch, "/location-types/nre-refusal",
		map[string]any{"name_rule": map[string]any{"stem": "Bad Stem"}}, http.StatusUnprocessableEntity)
}
