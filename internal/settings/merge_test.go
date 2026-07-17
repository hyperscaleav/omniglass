package settings

import (
	"reflect"
	"testing"
)

func TestDeepMergeLaterWinsAndPresenceControls(t *testing.T) {
	base := map[string]any{"theme": "dark", "nested": map[string]any{"a": 1, "b": 2}}
	over := map[string]any{"nested": map[string]any{"b": 9}, "extra": true}
	got := DeepMerge(base, over)
	want := map[string]any{
		"theme":  "dark",                         // absent in over: inherited
		"nested": map[string]any{"a": 1, "b": 9}, // merged, b overridden
		"extra":  true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DeepMerge = %v, want %v", got, want)
	}
	if base["theme"] != "dark" || over["extra"] != true {
		t.Fatalf("DeepMerge mutated an input")
	}
}

func TestApplyMergePatchNullDeletes(t *testing.T) {
	target := map[string]any{"theme": "light", "landing": "/home"}
	patch := map[string]any{"theme": nil, "landing": "/alarms"}
	got := ApplyMergePatch(target, patch)
	want := map[string]any{"landing": "/alarms"} // theme deleted, restored to lower layer
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ApplyMergePatch = %v, want %v", got, want)
	}
}

func TestDeepMergeCopiesSlices(t *testing.T) {
	base := map[string]any{"nested": map[string]any{"list": []any{"a", "b"}}}
	got := DeepMerge(base)
	got["nested"].(map[string]any)["list"].([]any)[0] = "MUT"
	if base["nested"].(map[string]any)["list"].([]any)[0] != "a" {
		t.Fatalf("DeepMerge aliased a nested slice: mutating the result mutated the input")
	}
}

func TestApplyMergePatchCopiesSlices(t *testing.T) {
	target := map[string]any{"keep": []any{"x"}}
	patch := map[string]any{"add": []any{"y"}}
	got := ApplyMergePatch(target, patch)
	got["keep"].([]any)[0] = "MUT"
	got["add"].([]any)[0] = "MUT"
	if target["keep"].([]any)[0] != "x" {
		t.Fatalf("ApplyMergePatch aliased the target slice")
	}
	if patch["add"].([]any)[0] != "y" {
		t.Fatalf("ApplyMergePatch aliased the patch slice")
	}
}
