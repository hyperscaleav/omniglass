package lab

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/world"
)

func TestWriteWorldsWritesOneLoadableFilePerOutpost(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "world")
	ws := map[string]*world.World{
		"edge-hq":   {Version: world.Version, Scenario: "t", Outpost: "edge-hq", Sites: []world.Site{{Name: "hq"}}},
		"edge-b-01": {Version: world.Version, Scenario: "t", Outpost: "edge-b-01", Sites: []world.Site{{Name: "b-01"}}},
	}
	names, err := WriteWorlds(dir, ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "edge-b-01" || names[1] != "edge-hq" {
		t.Fatalf("wrote %v", names)
	}
	for outpost, w := range ws {
		got, err := world.Load(filepath.Join(dir, outpost+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if got.Outpost != w.Outpost || got.Sites[0].Name != w.Sites[0].Name {
			t.Errorf("%s reads back as %+v", outpost, got)
		}
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(left) != 0 {
		t.Errorf("temp files left: %v", left)
	}
	if got := WorldFile("edge-hq"); got != "/var/lib/omniglass/world/edge-hq.json" {
		t.Errorf("WorldFile = %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "edge-hq.json")); err != nil {
		t.Fatal(err)
	}
}
