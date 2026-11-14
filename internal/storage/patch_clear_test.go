package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/updatemask"
)

// Every nullable column a PATCH writes can be cleared (#1189), in three states
// each Update* must honor: unmentioned leaves the column, a value sets it, and
// the clear takes it back to unset. A string clears by the sentinel "", a
// non-string by naming the field in the write set (ADR-0091), an object by the
// same rule (ADR-0106). Before this the columns were written with coalesce, so
// the third state did not exist.

func TestUpdateStandardParentThreeStates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := storagetest.NewDB(t)

	for _, n := range []string{"clr-base", "clr-other"} {
		if _, err := gw.CreateStandard(ctx, "", storage.Standard{Name: n, Label: n}); err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
	}
	base := "clr-base"
	if _, err := gw.CreateStandard(ctx, "", storage.Standard{Name: "clr-child", Label: "Child", ParentStandardID: &base}); err != nil {
		t.Fatalf("create child: %v", err)
	}
	parent := func(step string, patch storage.StandardPatch) string {
		t.Helper()
		st, err := gw.UpdateStandard(ctx, "", "clr-child", patch)
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		if st.ParentStandardName == nil {
			return ""
		}
		return *st.ParentStandardName
	}

	if got := parent("unmentioned", storage.StandardPatch{Label: strp("Child 2")}); got != "clr-base" {
		t.Fatalf("unmentioned: parent = %q, want clr-base", got)
	}
	if got := parent("set by name", storage.StandardPatch{ParentStandardID: strp("clr-other")}); got != "clr-other" {
		t.Fatalf("set: parent = %q, want clr-other", got)
	}
	if got := parent("cleared", storage.StandardPatch{ParentStandardID: strp("")}); got != "" {
		t.Fatalf("cleared: parent = %q, want none", got)
	}
	// An unknown parent is refused, not read as the clear the CASE would give a
	// name that resolves to nothing.
	if _, err := gw.UpdateStandard(ctx, "", "clr-child", storage.StandardPatch{ParentStandardID: strp("clr-other")}); err != nil {
		t.Fatalf("re-set: %v", err)
	}
	_, err := gw.UpdateStandard(ctx, "", "clr-child", storage.StandardPatch{ParentStandardID: strp("no-such-standard")})
	if !errors.Is(err, storage.ErrParentStandardNotFound) {
		t.Fatalf("unknown parent: err = %v, want ErrParentStandardNotFound", err)
	}
	if got := parent("after refusal", storage.StandardPatch{Label: strp("Child 3")}); got != "clr-other" {
		t.Fatalf("after refusal: parent = %q, want clr-other (the refusal must not clear it)", got)
	}
}

func TestUpdateMetricTypePrecisionThreeStates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := storagetest.NewDB(t)

	two := 2
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "clr-metric", DataType: "float", Precision: &two}); err != nil {
		t.Fatalf("create: %v", err)
	}
	precision := func(step string, patch storage.MetricTypePatch) *int {
		t.Helper()
		mt, err := gw.UpdateMetricType(ctx, "", "clr-metric", patch)
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		return mt.Precision
	}

	if got := precision("unmentioned", storage.MetricTypePatch{Unit: strp("rpm")}); got == nil || *got != 2 {
		t.Fatalf("unmentioned: precision = %v, want 2", got)
	}
	three := 3
	if got := precision("set", storage.MetricTypePatch{Precision: &three}); got == nil || *got != 3 {
		t.Fatalf("set: precision = %v, want 3", got)
	}
	// A write set naming other fields leaves precision.
	if got := precision("other field named", storage.MetricTypePatch{Write: updatemask.Of(storage.MetricTypeFieldUnit), Unit: strp("rpm")}); got == nil || *got != 3 {
		t.Fatalf("other field named: precision = %v, want 3", got)
	}
	if got := precision("cleared", storage.MetricTypePatch{Write: updatemask.Of(storage.MetricTypeFieldPrecision)}); got != nil {
		t.Fatalf("cleared: precision = %d, want none", *got)
	}
	// A named field with no value leaves the others as they were, and a named
	// NOT NULL text column takes its empty string.
	got, err := gw.UpdateMetricType(ctx, "", "clr-metric", storage.MetricTypePatch{Write: updatemask.Of(storage.MetricTypeFieldUnit)})
	if err != nil {
		t.Fatalf("clear unit: %v", err)
	}
	if got.Unit != nil && *got.Unit != "" {
		t.Fatalf("cleared unit = %q, want empty", *got.Unit)
	}
}

func TestUpdateEventTypePayloadSchemaThreeStates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := storagetest.NewDB(t)

	if _, err := gw.CreateEventType(ctx, "", storage.EventTypeSpec{Name: "clr-event", PayloadSchema: []byte(`{"type":"object"}`)}); err != nil {
		t.Fatalf("create: %v", err)
	}
	schema := func(step string, patch storage.EventTypePatch) string {
		t.Helper()
		et, err := gw.UpdateEventType(ctx, "", "clr-event", patch)
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		return string(et.PayloadSchema)
	}

	if got := schema("unmentioned", storage.EventTypePatch{Label: strp("Event")}); got == "" {
		t.Fatalf("unmentioned: schema cleared")
	}
	if got := schema("set", storage.EventTypePatch{PayloadSchema: []byte(`{"type":"string"}`)}); !strings.Contains(got, `"string"`) {
		t.Fatalf("set: schema = %s, want the string schema", got)
	}
	if got := schema("other field named", storage.EventTypePatch{Write: updatemask.Of(storage.EventTypeFieldLabel), Label: strp("Again")}); got == "" {
		t.Fatalf("other field named: schema cleared")
	}
	if got := schema("cleared", storage.EventTypePatch{Write: updatemask.Of(storage.EventTypeFieldPayloadSchema)}); got != "" {
		t.Fatalf("cleared: schema = %s, want none", got)
	}
}

func TestUpdatePropertyTypeValidationThreeStates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := storagetest.NewDB(t)

	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: "clr-prop", DataType: "string", Validation: []byte(`{"type":"string"}`)}); err != nil {
		t.Fatalf("create: %v", err)
	}
	validation := func(step string, patch storage.PropertyTypePatch) string {
		t.Helper()
		pt, err := gw.UpdatePropertyType(ctx, "", "clr-prop", patch)
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		return string(pt.Validation)
	}

	if got := validation("unmentioned", storage.PropertyTypePatch{Label: strp("Prop")}); got == "" {
		t.Fatalf("unmentioned: validation cleared")
	}
	if got := validation("set", storage.PropertyTypePatch{Validation: []byte(`{"minLength":2}`)}); got == "" {
		t.Fatalf("set: validation empty")
	}
	if got := validation("other field named", storage.PropertyTypePatch{Write: updatemask.Of(storage.PropertyTypeFieldLabel), Label: strp("Again")}); got == "" {
		t.Fatalf("other field named: validation cleared")
	}
	if got := validation("cleared", storage.PropertyTypePatch{Write: updatemask.Of(storage.PropertyTypeFieldValidation)}); got != "" {
		t.Fatalf("cleared: validation = %s, want none", got)
	}
}

func TestUpdateCommandTypeParamsSchemaThreeStates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := storagetest.NewDB(t)

	if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{Name: "clr-command", ParamsSchema: []byte(`{"type":"object"}`)}); err != nil {
		t.Fatalf("create: %v", err)
	}
	schema := func(step string, patch storage.CommandTypePatch) string {
		t.Helper()
		ct, err := gw.UpdateCommandType(ctx, "", "clr-command", patch)
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		return string(ct.ParamsSchema)
	}

	if got := schema("unmentioned", storage.CommandTypePatch{Label: strp("Command")}); got == "" {
		t.Fatalf("unmentioned: schema cleared")
	}
	if got := schema("set", storage.CommandTypePatch{ParamsSchema: []byte(`{"type":"string"}`)}); got == "" {
		t.Fatalf("set: schema empty")
	}
	if got := schema("other field named", storage.CommandTypePatch{Write: updatemask.Of(storage.CommandTypeFieldLabel), Label: strp("Again")}); got == "" {
		t.Fatalf("other field named: schema cleared")
	}
	if got := schema("cleared", storage.CommandTypePatch{Write: updatemask.Of(storage.CommandTypeFieldParamsSchema)}); got != "" {
		t.Fatalf("cleared: schema = %s, want none", got)
	}
}
