package tag

import (
	"strings"
	"testing"
)

func TestValidateKey(t *testing.T) {
	cases := []struct {
		name string
		key  string
		ok   bool
	}{
		{"simple", "category", true},
		{"underscore", "cost_center", true},
		{"digits", "tier2", true},
		{"single letter", "x", true},
		{"empty", "", false},
		{"uppercase", "Environment", false},
		{"mixed case", "costCenter", false},
		{"leading digit", "2tier", false},
		{"leading underscore", "_hidden", false},
		{"space", "cost center", false},
		{"hyphen", "cost-center", false},
		{"dot", "a.b", false},
		{"too long", strings.Repeat("a", MaxKeyLen+1), false},
		{"at limit", strings.Repeat("a", MaxKeyLen), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateKey(c.key)
			if c.ok && err != nil {
				t.Fatalf("ValidateKey(%q) = %v, want ok", c.key, err)
			}
			if !c.ok && err == nil {
				t.Fatalf("ValidateKey(%q) = nil, want error", c.key)
			}
		})
	}
}

func TestValidateValue(t *testing.T) {
	cases := []struct {
		name string
		val  string
		ok   bool
	}{
		{"simple", "prod", true},
		{"spaces allowed inside", "audio dsp", true},
		{"mixed case allowed", "Prod", true},
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"too long", strings.Repeat("x", MaxValueLen+1), false},
		{"at limit", strings.Repeat("x", MaxValueLen), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateValue(c.val)
			if c.ok && err != nil {
				t.Fatalf("ValidateValue(%q) = %v, want ok", c.val, err)
			}
			if !c.ok && err == nil {
				t.Fatalf("ValidateValue(%q) = nil, want error", c.val)
			}
		})
	}
}

func TestValidateAppliesTo(t *testing.T) {
	cases := []struct {
		name  string
		kinds []string
		ok    bool
	}{
		{"empty is universal", nil, true},
		{"single", []string{"component"}, true},
		{"all three", []string{"component", "system", "location"}, true},
		{"unknown kind", []string{"file"}, false},
		{"duplicate", []string{"component", "component"}, false},
		{"one bad among good", []string{"system", "bogus"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateAppliesTo(c.kinds)
			if c.ok && err != nil {
				t.Fatalf("ValidateAppliesTo(%v) = %v, want ok", c.kinds, err)
			}
			if !c.ok && err == nil {
				t.Fatalf("ValidateAppliesTo(%v) = nil, want error", c.kinds)
			}
		})
	}
}

func TestAppliesToKind(t *testing.T) {
	cases := []struct {
		name      string
		appliesTo []string
		kind      EntityKind
		want      bool
	}{
		{"empty applies to component", nil, KindComponent, true},
		{"empty applies to system", nil, KindSystem, true},
		{"narrowed and matching", []string{"component"}, KindComponent, true},
		{"narrowed and not matching", []string{"component"}, KindSystem, false},
		{"multi and matching", []string{"system", "location"}, KindLocation, true},
		{"unknown kind never applies", nil, EntityKind("file"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := AppliesToKind(c.appliesTo, c.kind); got != c.want {
				t.Fatalf("AppliesToKind(%v, %q) = %v, want %v", c.appliesTo, c.kind, got, c.want)
			}
		})
	}
}

func TestValidateAllowedValues(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		ok     bool
	}{
		{"empty is free text", nil, true},
		{"a valid enum", []string{"prod", "staging", "dev"}, true},
		{"duplicate", []string{"prod", "prod"}, false},
		{"blank member", []string{"prod", "  "}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateAllowedValues(c.values)
			if c.ok != (err == nil) {
				t.Fatalf("ValidateAllowedValues(%v) = %v, want ok=%v", c.values, err, c.ok)
			}
		})
	}
}

func TestValueAllowed(t *testing.T) {
	enum := []string{"prod", "staging", "dev"}
	if !ValueAllowed(nil, "anything") {
		t.Error("empty allowed set should admit any value")
	}
	if !ValueAllowed(enum, "prod") {
		t.Error("a member should be allowed")
	}
	if ValueAllowed(enum, "qa") {
		t.Error("a non-member should be rejected")
	}
}
