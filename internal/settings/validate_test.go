package settings

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateUnknownNamespace(t *testing.T) {
	if err := Validate("nope", map[string]any{"x": 1}); !errors.Is(err, ErrUnknownNamespace) {
		t.Fatalf("unknown namespace err = %v, want ErrUnknownNamespace", err)
	}
}

func TestValidateUnknownKey(t *testing.T) {
	var fe *FieldError
	if err := Validate("ui", map[string]any{"bogus": "x"}); !errors.As(err, &fe) {
		t.Fatalf("unknown key err = %v, want *FieldError", err)
	}
}

func TestValidateEnumViolation(t *testing.T) {
	var fe *FieldError
	if err := Validate("ui", map[string]any{"theme": "purple"}); !errors.As(err, &fe) || fe.Key != "theme" {
		t.Fatalf("enum violation err = %v, want *FieldError on theme", err)
	}
}

func TestValidateValidPatchAndNullDelete(t *testing.T) {
	if err := Validate("ui", map[string]any{"theme": "omniglass-light", "default_landing": nil}); err != nil {
		t.Fatalf("valid patch (with a null delete) errored: %v", err)
	}
}

func TestValidatePatternViolation(t *testing.T) {
	var fe *FieldError
	if err := Validate("ui", map[string]any{"default_landing": "home"}); !errors.As(err, &fe) || fe.Key != "default_landing" {
		t.Fatalf("pattern violation err = %v, want *FieldError on default_landing", err)
	}
	if err := Validate("ui", map[string]any{"default_landing": "/alarms"}); err != nil {
		t.Fatalf("valid absolute path errored: %v", err)
	}
}

// TestLearnDoneIsBounded (#2450): any signed-in person writes their own
// learn.done, so it is held to a short list of slug-shaped entries rather than
// stored at any size and read back on every console boot.
func TestLearnDoneIsBounded(t *testing.T) {
	ok := []any{"device-to-samples", "rules-and-alarms"}
	if err := Validate("learn", map[string]any{"done": ok}); err != nil {
		t.Fatalf("two slugs: %v", err)
	}
	long := make([]any, 33)
	for i := range long {
		long[i] = "device-to-samples"
	}
	for name, v := range map[string][]any{
		"too many":     long,
		"not a slug":   {"Device To Samples!"},
		"too long":     {strings.Repeat("a", 65)},
		"empty string": {""},
	} {
		if err := Validate("learn", map[string]any{"done": v}); err == nil {
			t.Errorf("%s: accepted, want a field error", name)
		}
	}
}
