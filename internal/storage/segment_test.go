package storage

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSegment(t *testing.T) {
	valid := []string{"a", "av-rack-3", "boardroom-a", "meeting-room", "x0", strings.Repeat("a", 100)}
	for _, n := range valid {
		if err := ValidateSegment(n); err != nil {
			t.Errorf("ValidateSegment(%q) = %v, want nil", n, err)
		}
	}
	invalid := []string{"", "-lead", "Uppercase", "has space", "under_score", "tab\t", "dot.name", strings.Repeat("a", 101)}
	for _, n := range invalid {
		if err := ValidateSegment(n); !errors.Is(err, ErrInvalidSegment) {
			t.Errorf("ValidateSegment(%q) = %v, want ErrInvalidSegment", n, err)
		}
	}
}
