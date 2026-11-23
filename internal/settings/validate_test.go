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

func TestValidateMetricWindowUnderThreeDaysRefused(t *testing.T) {
	for _, n := range []float64{1, 2} {
		var fe *FieldError
		err := Validate("retention", map[string]any{"metric_days": n})
		if !errors.As(err, &fe) || fe.Key != "metric_days" {
			t.Fatalf("a %v-day window: err = %v", n, err)
		}
		for _, want := range []string{"metric", "3-day minimum"} {
			if !strings.Contains(fe.Message, want) {
				t.Errorf("message %q lacks %q", fe.Message, want)
			}
		}
	}
	if err := Validate("retention", map[string]any{"metric_days": 3}); err != nil {
		t.Errorf("3 days refused: %v", err)
	}
}

func TestEnvMetricWindowUnderThreeDaysRefusedWithItsMessage(t *testing.T) {
	err := validateEnvValue("retention", "metric_days", 2, "2")
	if err == nil || !strings.Contains(err.Error(), "3-day minimum") {
		t.Fatalf("err = %v", err)
	}
}
