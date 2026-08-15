package updatemask_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/updatemask"
)

// These tests are the specification of AIP-134's mask rules, one test per rule,
// written against the worked example the primitive was built for: a role
// declaration whose patchable fields are these.
var rolePatchable = []string{"label", "quorum", "capacity", "position_labels", "impact", "alternate"}

// RULE: an absent mask is the implied mask of the populated fields. This is the
// rule that keeps every route and every test that predates the mask valid, so
// it is the one worth breaking first if you doubt the suite has teeth.
func TestAbsentMaskIsTheImpliedMaskOfPopulatedFields(t *testing.T) {
	populated := updatemask.Of("label", "quorum")
	got, err := updatemask.Resolve(nil, rolePatchable, populated)
	if err != nil {
		t.Fatalf("resolve an absent mask: %v, want the implied mask", err)
	}
	if want := []string{"label", "quorum"}; !reflect.DeepEqual(got.Names(), want) {
		t.Fatalf("implied mask = %v, want exactly the populated fields %v", got.Names(), want)
	}
	if got.Has("capacity") {
		t.Fatalf("implied mask = %v, want an unpopulated field left out of it (the whole reason "+
			"an omitted capacity survives an unrelated edit)", got.Names())
	}
}

// RULE: the implied mask is a copy. A consumer that narrows the write set must
// not reach back through it and narrow the body's populated set too.
func TestTheImpliedMaskDoesNotAliasThePopulatedSet(t *testing.T) {
	populated := updatemask.Of("label")
	got, err := updatemask.Resolve(nil, rolePatchable, populated)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	delete(got, "label")
	if !populated.Has("label") {
		t.Fatalf("narrowing the resolved set emptied the caller's populated set too")
	}
}

// RULE: a present mask writes exactly the fields it names, and nothing else,
// however populated the rest of the body is.
func TestAPresentMaskWritesOnlyTheFieldsItNames(t *testing.T) {
	populated := updatemask.Of("label", "quorum", "impact")
	got, err := updatemask.Resolve([]string{"quorum"}, rolePatchable, populated)
	if err != nil {
		t.Fatalf("resolve an explicit mask: %v", err)
	}
	if want := []string{"quorum"}; !reflect.DeepEqual(got.Names(), want) {
		t.Fatalf("explicit mask = %v, want only %v: a populated field the mask does not name is not written",
			got.Names(), want)
	}
}

// RULE: a named field is written whether or not the body populated it. This is
// the clear path, and the reason the mask exists at all (#638): under the
// implied mask alone, "clear my capacity" and "leave my capacity alone" are the
// same request.
func TestAPresentMaskWritesAFieldTheBodyLeftEmpty(t *testing.T) {
	got, err := updatemask.Resolve([]string{"capacity"}, rolePatchable, updatemask.Of("label"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !got.Has("capacity") {
		t.Fatalf("mask [capacity] over an empty capacity = %v, want capacity written (and so cleared)", got.Names())
	}
	if got.Has("label") {
		t.Fatalf("mask [capacity] = %v, want the populated label left out: a present mask "+
			"replaces the implied one, it does not add to it", got.Names())
	}
}

// RULE: a mask that is present and names nothing writes nothing. An empty array
// is a caller stating "change none of it", which is different from omitting the
// field, and Resolve must not collapse the two.
func TestAPresentButEmptyMaskWritesNothing(t *testing.T) {
	got, err := updatemask.Resolve([]string{}, rolePatchable, updatemask.Of("label", "quorum"))
	if err != nil {
		t.Fatalf("resolve an empty mask: %v", err)
	}
	if len(got.Names()) != 0 {
		t.Fatalf("empty mask = %v, want nothing written even though the body populated fields", got.Names())
	}
}

// RULE: "*" is full replacement, the equivalent of a PUT: every patchable field
// is written, including the ones the body never mentioned, which therefore
// clear.
func TestStarWritesEveryPatchableField(t *testing.T) {
	got, err := updatemask.Resolve([]string{updatemask.Star}, rolePatchable, updatemask.Of("label"))
	if err != nil {
		t.Fatalf("resolve a star mask: %v", err)
	}
	if !reflect.DeepEqual(got.Names(), updatemask.Of(rolePatchable...).Names()) {
		t.Fatalf("star mask = %v, want every patchable field %v", got.Names(), rolePatchable)
	}
}

// RULE: "*" cannot be combined with named fields. Full replacement and a named
// subset cannot both be meant, and guessing which one the caller wanted is how
// a mask silently does the opposite of what it says.
func TestStarRefusesToBeCombinedWithNamedFields(t *testing.T) {
	_, err := updatemask.Resolve([]string{updatemask.Star, "quorum"}, rolePatchable, nil)
	var maskErr *updatemask.Error
	if !errors.As(err, &maskErr) {
		t.Fatalf(`resolve ["*","quorum"]: err = %v, want an *updatemask.Error`, err)
	}
	if maskErr.Field != updatemask.Star {
		t.Fatalf("error names %q, want it to name the star", maskErr.Field)
	}
}

// RULE: a mask naming a field the resource does not patch is refused, and the
// refusal names the field and lists what the resource does patch. A silent
// no-op would leave the operator believing a write landed.
func TestAnUnknownFieldIsRefusedByName(t *testing.T) {
	_, err := updatemask.Resolve([]string{"quorum", "colour"}, rolePatchable, nil)
	var maskErr *updatemask.Error
	if !errors.As(err, &maskErr) {
		t.Fatalf("resolve an unknown field: err = %v, want an *updatemask.Error", err)
	}
	if maskErr.Field != "colour" {
		t.Fatalf("error names %q, want the offending field colour", maskErr.Field)
	}
	msg := maskErr.Error()
	for _, want := range []string{`"colour"`, "label", "capacity"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal %q, want it to carry %q: the operator has to be able to act on it", msg, want)
		}
	}
}

// A field name is not a value: the mask names the wire field, so a case or
// spelling variant is a different field and is refused like any other unknown.
func TestFieldNamesAreExactAndNotNormalized(t *testing.T) {
	// "position-labels" is the hyphenated variant of the real "position_labels".
	// It used to be "display-name", the hyphenated variant of the field this
	// slice renamed; `label` is now a patchable field in its own right and
	// resolves, so keeping it here would have asserted the opposite of the rule
	// (#613).
	for _, name := range []string{"Quorum", "quorum ", "position-labels", ""} {
		if _, err := updatemask.Resolve([]string{name}, rolePatchable, nil); err == nil {
			t.Fatalf("resolve %q: err = nil, want it refused rather than silently matched", name)
		}
	}
}

// A repeated path is one field, not two: the mask is a set.
func TestARepeatedPathIsOneField(t *testing.T) {
	got, err := updatemask.Resolve([]string{"quorum", "quorum"}, rolePatchable, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if want := []string{"quorum"}; !reflect.DeepEqual(got.Names(), want) {
		t.Fatalf("mask = %v, want %v", got.Names(), want)
	}
}

// The zero value of the set is usable: a write with no resolved set writes
// nothing rather than panicking on a nil map.
func TestTheZeroSetWritesNothing(t *testing.T) {
	var f updatemask.Fields
	if f.Has("quorum") || len(f.Names()) != 0 {
		t.Fatalf("zero Fields = %v, want an empty write set", f.Names())
	}
}
