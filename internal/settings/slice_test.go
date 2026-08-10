package settings

import (
	"errors"
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// The settings engine's SLICE support, tested as an engine capability rather
// than through the one namespace that happens to use it (#684).
//
// A list-valued setting is a different shape from the three scalars the engine
// shipped with, and it is different in four places at once: its default arrives
// as a single tag string that has to become a typed slice, it crosses the wire
// as a JSON array the write validator must admit, a write REPLACES it rather
// than merging into it, and the typed decode has to turn generic decoded values
// back into the declared element type. The acronym list is the first list-valued
// setting; these tests are here so the second one does not rediscover the edges.

func TestCoerceDefaultSplitsASliceTagOnCommas(t *testing.T) {
	got, err := fieldDefault("AV,DSP,HDMI", reflect.TypeOf([]string{}))
	if err != nil {
		t.Fatalf("coerce slice default: %v", err)
	}
	want := []string{"AV", "DSP", "HDMI"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fieldDefault = %#v, want %#v", got, want)
	}
}

func TestCoerceDefaultTrimsSpaceAroundSliceElements(t *testing.T) {
	got, err := fieldDefault("AV, DSP ,  HDMI", reflect.TypeOf([]string{}))
	if err != nil {
		t.Fatalf("coerce slice default: %v", err)
	}
	want := []string{"AV", "DSP", "HDMI"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fieldDefault = %#v, want %#v", got, want)
	}
}

// An empty tag is an empty LIST, not a one-element list holding the empty
// string, and not a nil slice: nil marshals as JSON null, and null is the merge
// patch's delete sentinel, so a nil default would read as "restore this key"
// somewhere downstream.
func TestCoerceDefaultEmptySliceTagIsAnEmptyList(t *testing.T) {
	got, err := fieldDefault("", reflect.TypeOf([]string{}))
	if err != nil {
		t.Fatalf("coerce empty slice default: %v", err)
	}
	list, ok := got.([]string)
	if !ok {
		t.Fatalf("fieldDefault = %#v, want a []string", got)
	}
	if list == nil {
		t.Fatalf("fieldDefault returned a nil slice; an empty list must marshal as [], not null")
	}
	if len(list) != 0 {
		t.Fatalf("fieldDefault = %#v, want an empty list", list)
	}
}

// The element type is coerced with the same function the scalars use, so a
// list-valued setting is not silently limited to strings.
func TestCoerceDefaultCoercesSliceElementsToTheElementType(t *testing.T) {
	got, err := fieldDefault("1, 2, 3", reflect.TypeOf([]int{}))
	if err != nil {
		t.Fatalf("coerce int slice default: %v", err)
	}
	want := []int{1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fieldDefault = %#v, want %#v", got, want)
	}
}

func TestCoerceDefaultRefusesAnUncoercibleSliceElement(t *testing.T) {
	if _, err := fieldDefault("1, not-a-number", reflect.TypeOf([]int{})); err == nil {
		t.Fatalf("a bad element must be refused, not skipped or zeroed")
	}
}

// The JSON-array spelling is refused LOUDLY rather than accepted quietly. Huma
// reflects the same tag into the schema and understands both spellings; this
// layer understands one. Admitting the bracket form here without parsing it the
// way huma does would give a setting two defaults that disagree, which is the
// exact drift the single-source struct exists to prevent.
func TestCoerceDefaultRefusesTheJSONArrayTagForm(t *testing.T) {
	if _, err := fieldDefault(`["AV","DSP"]`, reflect.TypeOf([]string{})); err == nil {
		t.Fatalf("the JSON-array default form must be refused so it cannot disagree with the reflected schema")
	}
}

// The parity that matters: the default this layer coerces and the default huma
// reflects into the JSON Schema (and from there into the OpenAPI document, the
// generated client, and the console's field constraints) come from ONE tag, so
// they must be one value.
func TestSliceDefaultAgreesWithTheReflectedSchema(t *testing.T) {
	const tag = "AV, DSP"
	type probe struct {
		Acronyms []string `json:"acronyms" default:"AV, DSP"`
	}
	schema := huma.SchemaFromType(reg, reflect.TypeOf(probe{}))
	want := schema.Properties["acronyms"].Default
	got, err := fieldDefault(tag, reflect.TypeOf([]string{}))
	if err != nil {
		t.Fatalf("coerce slice default: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("coerced default %#v disagrees with the reflected schema default %#v", got, want)
	}
}

// --- the wire and the cascade ------------------------------------------------

func TestValidateAcceptsAListValue(t *testing.T) {
	if err := Validate("label", map[string]any{"acronyms": []any{"AV", "DSP"}}); err != nil {
		t.Fatalf("a list of strings must validate against a list-valued setting: %v", err)
	}
}

// The console edits a list as text, so the one mistake a write path can make is
// sending the joined string back. It must be refused at the surface, not stored
// and then found by a typed decode on a later read.
func TestValidateRefusesAScalarForAListSetting(t *testing.T) {
	err := Validate("label", map[string]any{"acronyms": "AV,DSP"})
	if err == nil {
		t.Fatalf("a scalar written to a list-valued setting must be refused")
	}
	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("want a *FieldError (422), got %T: %v", err, err)
	}
}

func TestValidateRefusesAWrongElementType(t *testing.T) {
	if err := Validate("label", map[string]any{"acronyms": []any{"AV", 7}}); err == nil {
		t.Fatalf("a non-string element must be refused")
	}
}

// A list REPLACES; it never concatenates. An operator's list is the list, so
// removing a shipped entry has to be possible, which a concatenating merge would
// make impossible.
func TestApplyMergePatchReplacesAListWholesale(t *testing.T) {
	target := map[string]any{"acronyms": []any{"AV", "DSP"}}
	out := ApplyMergePatch(target, map[string]any{"acronyms": []any{"QM55"}})
	got, ok := out["acronyms"].([]any)
	if !ok {
		t.Fatalf("acronyms = %#v, want a list", out["acronyms"])
	}
	if len(got) != 1 || got[0] != "QM55" {
		t.Fatalf("acronyms = %#v, want the patch's list wholesale", got)
	}
}

func TestApplyMergePatchCopiesAListRatherThanAliasingIt(t *testing.T) {
	patch := map[string]any{"acronyms": []any{"AV"}}
	out := ApplyMergePatch(map[string]any{}, patch)
	patch["acronyms"].([]any)[0] = "MUTATED"
	if got := out["acronyms"].([]any)[0]; got != "AV" {
		t.Fatalf("merged list aliases the patch: element = %v", got)
	}
}

// Provenance is how the shipped list and an operator's stay distinguishable:
// the value is one key, and the level that supplied it is the answer to "is
// this still what Omniglass ships?".
func TestResolveRecordsProvenanceForAListKey(t *testing.T) {
	r := Resolve(
		Level{Name: "default", Doc: Doc{"label": {"acronyms": []string{"AV"}}}},
		Level{Name: "platform", Doc: Doc{"label": {"acronyms": []any{"AV", "QM55"}}}},
	)
	if got := r.Sources["label.acronyms"]; got != "platform" {
		t.Fatalf("source = %q, want platform", got)
	}
	if got := len(r.Values["label"]["acronyms"].([]any)); got != 2 {
		t.Fatalf("effective list length = %d, want the platform list's 2", got)
	}
}

// The typed decode is the app-wide accessor, so a list stored as generic decoded
// values has to come back as the declared element type rather than as []any.
func TestTypedDecodesAListFromGenericValues(t *testing.T) {
	s, err := Typed(Doc{"label": {"acronyms": []any{"AV", "QM55"}}})
	if err != nil {
		t.Fatalf("typed decode: %v", err)
	}
	if !reflect.DeepEqual(s.Label.Acronyms, []string{"AV", "QM55"}) {
		t.Fatalf("Label.Acronyms = %#v, want the decoded list", s.Label.Acronyms)
	}
}
