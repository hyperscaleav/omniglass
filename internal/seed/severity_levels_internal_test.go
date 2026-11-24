package seed

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/internal/health"
)

// Every shipped level declares its health effect (#2558), and the declaration
// is one the health rule knows: a level with none would take its default, and
// the shipped set is explicit about which levels badge a component.
func TestSeededLevelsDeclareTheirHealthEffect(t *testing.T) {
	var doc struct {
		Levels []struct {
			Name         string `yaml:"name"`
			HealthEffect string `yaml:"health_effect"`
		} `yaml:"levels"`
	}
	if err := yaml.Unmarshal(severityLevelsYAML, &doc); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"info": "none", "warning": "degraded", "average": "degraded", "high": "outage", "disaster": "outage"}
	if len(doc.Levels) != len(want) {
		t.Fatalf("seed has %d levels, want %d", len(doc.Levels), len(want))
	}
	for _, l := range doc.Levels {
		if !health.ValidEffect(l.HealthEffect) || l.HealthEffect != want[l.Name] {
			t.Errorf("level %s declares health_effect %q, want %q", l.Name, l.HealthEffect, want[l.Name])
		}
	}
}
