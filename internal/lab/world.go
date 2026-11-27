package lab

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/hyperscaleav/omniglass/sim/world"
)

// WorldDir is where a scale lab's world files sit in the data volume: the seed
// writes one file per site outpost here (#2773), and each outpost's simulator
// reads its own through a read-only mount of the same volume. The volume is
// the one place the seed and the outposts already share, so no new surface is
// needed to carry the files.
const WorldDir = dataDir + "/world"

// WorldFile is the path, inside a lab container, of the world file the
// simulator beside the named outpost loads.
func WorldFile(outpost string) string { return path.Join(WorldDir, outpost+".json") }

// WriteWorlds writes each outpost's world file under dir, and returns the
// names of the outposts it wrote, sorted. A file is written whole and renamed
// into place, so a simulator never reads half of one.
func WriteWorlds(dir string, worlds map[string]*world.World) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("lab: world dir: %w", err)
	}
	names := make([]string, 0, len(worlds))
	for outpost, w := range worlds {
		b, err := w.Marshal()
		if err != nil {
			return nil, fmt.Errorf("lab: world of %s: %w", outpost, err)
		}
		tmp, err := os.CreateTemp(dir, outpost+".*.tmp")
		if err != nil {
			return nil, fmt.Errorf("lab: world of %s: %w", outpost, err)
		}
		_, werr := tmp.Write(b)
		cerr := tmp.Close()
		if err := firstErr(werr, cerr, os.Chmod(tmp.Name(), 0o644)); err != nil {
			_ = os.Remove(tmp.Name())
			return nil, fmt.Errorf("lab: world of %s: %w", outpost, err)
		}
		if err := os.Rename(tmp.Name(), filepath.Join(dir, outpost+".json")); err != nil {
			_ = os.Remove(tmp.Name())
			return nil, fmt.Errorf("lab: world of %s: %w", outpost, err)
		}
		names = append(names, outpost)
	}
	sort.Strings(names)
	return names, nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
