package settings

import (
	"errors"
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
