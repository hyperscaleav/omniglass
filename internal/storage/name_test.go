package storage

import (
	"errors"
	"strings"
	"testing"
)

func TestTheEntityNameRule(t *testing.T) {
	valid := []string{"a", "av-rack-3", "boardroom-a", "meeting-room", "x0", strings.Repeat("a", 100)}
	for _, n := range valid {
		if err := ValidateName("component", n); err != nil {
			t.Errorf("ValidateName(component, %q) = %v, want nil", n, err)
		}
	}
	invalid := []string{"", "-lead", "Uppercase", "has space", "under_score", "tab\t", "dot.name", strings.Repeat("a", 101)}
	for _, n := range invalid {
		if err := ValidateName("component", n); !errors.Is(err, ErrInvalidEntityName) {
			t.Errorf("ValidateName(component, %q) = %v, want ErrInvalidEntityName", n, err)
		}
	}
}
