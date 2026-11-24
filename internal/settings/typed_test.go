package settings

import (
	"context"
	"testing"
)

func TestTypedEffective(t *testing.T) {
	svc := NewService(Doc{}, nil, func(ctx context.Context, scope string) (Doc, map[string][]string, error) {
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

// A metric window of 1 or 2 days that is already stored (a settings file, an
// environment value or a binding written before the 3-day minimum, #2144)
// reads as the minimum: it folds nothing below it, so the effective window
// never falls under it.
func TestTypedClampsStoredMetricWindowToTheMinimum(t *testing.T) {
	for _, stored := range []float64{1, 2} {
		s, err := Typed(Doc{"retention": {"metric_days": stored}})
		if err != nil {
			t.Fatal(err)
		}
		if s.Retention.MetricDays != MetricMinDays {
			t.Errorf("stored %v days reads as %d, want %d", stored, s.Retention.MetricDays, MetricMinDays)
		}
	}
	s, err := Typed(Doc{"retention": {"metric_days": float64(30)}})
	if err != nil || s.Retention.MetricDays != 30 {
		t.Errorf("30 days reads as %d (%v), want 30", s.Retention.MetricDays, err)
	}
}
