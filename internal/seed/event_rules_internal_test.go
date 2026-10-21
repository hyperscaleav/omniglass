package seed

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/internal/rules"
)

// TestSeedRulesMeetTheBackfill proves an upgraded fleet's official rules
// meet the seed unchanged (#957): the backfill lowers each stored short form
// to exactly the expression event_rules.yaml now ships, so the first boot
// after the upgrade writes nothing (no version bump, no settle).
func TestSeedRulesMeetTheBackfill(t *testing.T) {
	// The short forms the seed shipped before #957.
	was := map[string][2]rules.Criteria{
		"reachability-lost": {{Op: "eq", Value: []byte(`"down"`)}, {Op: "eq", Value: []byte(`"up"`)}},
	}
	var doc eventRulesDoc
	if err := yaml.Unmarshal(eventRulesYAML, &doc); err != nil {
		t.Fatal(err)
	}
	for _, r := range doc.Rules {
		old, ok := was[r.Name]
		if !ok {
			continue // a rule new since #957 has no short form to meet
		}
		fire, err := rules.Lower(old[0], rules.LaneProperty, "string")
		if err != nil {
			t.Fatal(err)
		}
		clear, err := rules.Lower(old[1], rules.LaneProperty, "string")
		if err != nil {
			t.Fatal(err)
		}
		if r.Fire != fire.Expr || r.Clear != clear.Expr {
			t.Errorf("%s ships fire %q clear %q, want the backfill's %q and %q", r.Name, r.Fire, r.Clear, fire.Expr, clear.Expr)
		}
	}
}
