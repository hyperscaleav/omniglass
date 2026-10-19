package seed

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/internal/health"
)

// The health rule's outage floor is the seeded high level's rank: the
// constant and the seed move together or not at all.
func TestOutageRankIsTheSeededHighLevel(t *testing.T) {
	var doc struct {
		Levels []struct {
			Name string `yaml:"name"`
			Rank int    `yaml:"rank"`
		} `yaml:"levels"`
	}
	if err := yaml.Unmarshal(severityLevelsYAML, &doc); err != nil {
		t.Fatal(err)
	}
	for _, l := range doc.Levels {
		if l.Name == "high" {
			if l.Rank != health.OutageRank {
				t.Fatalf("the seeded high level ranks %d, but health.OutageRank is %d", l.Rank, health.OutageRank)
			}
			return
		}
	}
	t.Fatal("the seed has no high level, the outage floor")
}
