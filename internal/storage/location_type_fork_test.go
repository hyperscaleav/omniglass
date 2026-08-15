package storage

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/updatemask"
)

func nameRulePtr(stem string, bareFirst bool) *NameRule {
	return &NameRule{Stem: stem, BareFirst: bareFirst}
}

// TestApplyLocationTypePatchWriteSet is the whole meaning of a location_type
// PATCH, tested where it lives: a pure function over the row, with no database
// in the way. The three states it has to keep apart are the reason #692 exists.
//
// An omitted field is unchanged (the implied mask), which is what every caller
// that predates the mask sends. A field NAMED in the mask is written even when
// the body carries nothing for it, which is the only way to clear a nullable
// object: an omitted key and an explicit null both decode to the same nil
// pointer in Go, so the body alone can never spell the difference.
func TestApplyLocationTypePatchWriteSet(t *testing.T) {
	base := LocationType{
		Name: "floor", Official: true, Label: "Floor", Icon: "layers",
		AllowedParentTypes: []string{"building", "campus"},
		LabelRule:          strPtr("{{.TypeName}} {{.Name}}"),
		NameRule:           nameRulePtr("", false),
	}
	label := "Level"
	empty := ""

	cases := []struct {
		name  string
		patch LocationTypePatch
		want  func(LocationType) LocationType
	}{
		{
			name:  "an empty patch changes nothing",
			patch: LocationTypePatch{},
			want:  func(lt LocationType) LocationType { return lt },
		},
		{
			name:  "a populated field is written under the implied mask",
			patch: LocationTypePatch{Label: &label},
			want: func(lt LocationType) LocationType {
				lt.Label = "Level"
				return lt
			},
		},
		{
			name:  "an omitted name rule is left alone under the implied mask",
			patch: LocationTypePatch{Label: &label},
			want: func(lt LocationType) LocationType {
				lt.Label = "Level"
				return lt
			},
		},
		{
			name:  "the mask plus no rule CLEARS the rule",
			patch: LocationTypePatch{Write: updatemask.Of(LocationTypeFieldNameRule)},
			want: func(lt LocationType) LocationType {
				lt.NameRule = nil
				return lt
			},
		},
		{
			name: "a mask naming only the rule leaves every other field alone",
			patch: LocationTypePatch{
				Label: &label,
				Write: updatemask.Of(LocationTypeFieldNameRule),
			},
			want: func(lt LocationType) LocationType {
				lt.NameRule = nil
				return lt
			},
		},
		{
			name: "a mask can still SET a rule",
			patch: LocationTypePatch{
				NameRule: nameRulePtr("wing", true),
				Write:    updatemask.Of(LocationTypeFieldNameRule),
			},
			want: func(lt LocationType) LocationType {
				lt.NameRule = nameRulePtr("wing", true)
				return lt
			},
		},
		{
			name:  "a non-nil EMPTY mask writes nothing, even a populated field",
			patch: LocationTypePatch{Label: &label, Write: updatemask.Fields{}},
			want:  func(lt LocationType) LocationType { return lt },
		},
		{
			name:  "the string sentinel still clears a label rule",
			patch: LocationTypePatch{LabelRule: &empty},
			want: func(lt LocationType) LocationType {
				lt.LabelRule = nil
				return lt
			},
		},
		{
			name:  "the mask clears a label rule too, with no value",
			patch: LocationTypePatch{Write: updatemask.Of(LocationTypeFieldLabelRule)},
			want: func(lt LocationType) LocationType {
				lt.LabelRule = nil
				return lt
			},
		},
		{
			name:  "a masked allowed-parent set with no value clears to unconstrained",
			patch: LocationTypePatch{Write: updatemask.Of(LocationTypeFieldAllowedParentTypes)},
			want: func(lt LocationType) LocationType {
				lt.AllowedParentTypes = []string{}
				return lt
			},
		},
		{
			name: "full replacement writes every field, clearing the ones the patch omits",
			patch: LocationTypePatch{
				Label: &label,
				Write: updatemask.Of(LocationTypePatchFields...),
			},
			want: func(lt LocationType) LocationType {
				lt.Label = "Level"
				lt.Icon = ""
				lt.AllowedParentTypes = []string{}
				lt.LabelRule = nil
				lt.NameRule = nil
				return lt
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.want(base)
			got := applyLocationTypePatch(base, tc.patch)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("applyLocationTypePatch = %+v, want %+v", got, want)
			}
			// The row the caller handed in is never mutated: both write legs
			// keep reading it after the patch is applied (the fork records it
			// as the audit's before-image), so an in-place edit here would make
			// the trail claim nothing changed.
			if base.Label != "Floor" || base.NameRule == nil {
				t.Errorf("applyLocationTypePatch mutated its input: %+v", base)
			}
		})
	}
}

// TestLocationTypeShadowRoundTrip pins the two halves of the fork's storage
// contract for this registry: what a fork CAPTURES and what a read resolves.
//
// The whole mutable row goes in, every time, each nullable column as an
// explicit null rather than a dropped key (ADR-0095 decision 2). For
// location_type that is not a stylistic choice: name_rule's null is the state
// #692 exists to reach, so a fork that dropped the key would have the row
// re-adopt the shipped rule on the very next read, which is the bug with extra
// steps.
func TestLocationTypeShadowRoundTrip(t *testing.T) {
	official := LocationType{
		Name: "floor", Official: true, Label: "Floor", Icon: "layers",
		AllowedParentTypes: []string{"building", "campus"},
		NameRule:           nameRulePtr("", false),
	}

	cleared := official
	cleared.NameRule = nil
	image, err := locationTypeShadowImage(cleared)
	if err != nil {
		t.Fatalf("image: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(image, &keys); err != nil {
		t.Fatalf("decode image: %v", err)
	}
	for _, k := range []string{"label", "icon", "allowed_parent_types", "label_rule", "name_rule"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("the fork image drops %q; it must carry the whole mutable row", k)
		}
	}
	if string(keys["name_rule"]) != "null" {
		t.Errorf("a cleared rule is stored as %s, want an explicit null", keys["name_rule"])
	}

	resolved, err := applyLocationTypeImage(official, image)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if resolved.NameRule != nil {
		t.Errorf("resolving a cleared rule over the shipped one gives %+v, want nil", resolved.NameRule)
	}
	// Structure is never captured and never overlaid.
	if resolved.Name != "floor" || !resolved.Official {
		t.Errorf("the overlay moved structure: %+v", resolved)
	}

	cases := []struct {
		name  string
		image string
		want  func(LocationType) LocationType
	}{
		{
			name:  "no image leaves the official row alone",
			image: "",
			want:  func(lt LocationType) LocationType { return lt },
		},
		{
			name:  "an absent key falls back to the official value",
			image: `{"label":"Level"}`,
			want: func(lt LocationType) LocationType {
				lt.Label = "Level"
				return lt
			},
		},
		{
			name:  "a valued rule overrides the shipped one",
			image: `{"name_rule":{"stem":"wing","bare_first":true}}`,
			want: func(lt LocationType) LocationType {
				lt.NameRule = nameRulePtr("wing", true)
				return lt
			},
		},
		{
			name:  "a rule is normalized on the way out, as the column's is",
			image: `{"name_rule":{"stem":"","bare_first":true}}`,
			want: func(lt LocationType) LocationType {
				lt.NameRule = nameRulePtr("", false)
				return lt
			},
		},
		{
			name:  "an empty allowed-parent set is a real value, not an absent key",
			image: `{"allowed_parent_types":[]}`,
			want: func(lt LocationType) LocationType {
				lt.AllowedParentTypes = []string{}
				return lt
			},
		},
		{
			name:  "a null label rule clears it rather than falling back",
			image: `{"label_rule":null}`,
			want: func(lt LocationType) LocationType {
				lt.LabelRule = nil
				return lt
			},
		},
	}
	withRule := official
	withRule.LabelRule = strPtr("{{.TypeName}}")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyLocationTypeImage(withRule, []byte(tc.image))
			if err != nil {
				t.Fatalf("apply %s: %v", tc.image, err)
			}
			if want := tc.want(withRule); !reflect.DeepEqual(got, want) {
				t.Errorf("applyLocationTypeImage(%s) = %+v, want %+v", tc.image, got, want)
			}
			// The official row is shared by every read on the pool, so the
			// overlay must never write through a pointer it borrowed from it.
			if withRule.LabelRule == nil || *withRule.LabelRule != "{{.TypeName}}" || withRule.NameRule.Stem != "" {
				t.Errorf("the overlay mutated the official row: %+v", withRule)
			}
		})
	}
}

// TestLocationTypePopulatedIsThePointerSet is the guard on the promise that
// adding a mask changed no existing behavior: the implied mask has to be
// exactly the set of fields a caller set a pointer for, which is what the
// coalesce-per-column statement this replaced wrote.
func TestLocationTypePopulatedIsThePointerSet(t *testing.T) {
	empty := ""
	set := []string{}
	full := LocationTypePatch{
		Label: &empty, Icon: &empty, AllowedParentTypes: &set,
		LabelRule: &empty, NameRule: nameRulePtr("wing", false),
	}
	if got, want := full.Populated().Names(), []string{
		LocationTypeFieldAllowedParentTypes, LocationTypeFieldIcon,
		LocationTypeFieldLabel, LocationTypeFieldLabelRule, LocationTypeFieldNameRule,
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("populated = %v, want %v (a pointer to a zero value is still the caller saying something)", got, want)
	}
	if got := (LocationTypePatch{}).Populated().Names(); len(got) != 0 {
		t.Errorf("an empty patch populates %v, want nothing", got)
	}
}
