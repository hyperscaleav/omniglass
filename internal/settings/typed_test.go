package settings

import (
	"context"
	"testing"
)

func TestTypedEffective(t *testing.T) {
	svc := NewService(Doc{}, func(ctx context.Context, scope string) (Doc, map[string][]string, error) {
		return Doc{"ui": {"theme": "omniglass-light"}}, nil, nil
	})
	s, err := svc.EffectiveTyped(context.Background())
	if err != nil {
		t.Fatalf("typed: %v", err)
	}
	if s.UI.Theme != "omniglass-light" {
		t.Fatalf("s.UI.Theme = %q, want omniglass-light (override over default)", s.UI.Theme)
	}
	if s.UI.DefaultLanding != "/" {
		t.Fatalf("s.UI.DefaultLanding = %q, want the / default", s.UI.DefaultLanding)
	}
	if s.Keybindings.OpenDetail != "d" {
		t.Fatalf("s.Keybindings.OpenDetail = %q, want d", s.Keybindings.OpenDetail)
	}
}
