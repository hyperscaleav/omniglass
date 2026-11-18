package seed

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// factsWith renders the seed facts with the property types YAML swapped for
// src, the way make gen would on a branch that edited that file. Not parallel:
// it swaps a package var, and the parallel tests in this package wait for the
// sequential ones to finish before they run.
func factsWith(t *testing.T, src []byte) []byte {
	t.Helper()
	saved := propertyTypesYAML
	propertyTypesYAML = src
	defer func() { propertyTypesYAML = saved }()
	out, err := FactsJSON()
	if err != nil {
		t.Fatalf("FactsJSON: %v", err)
	}
	return out
}

// mergeFile three-way merges ours and theirs over base with git merge-file,
// the same line merge git merge runs on each file. clean is false when the
// result holds conflict markers.
func mergeFile(t *testing.T, base, ours, theirs []byte) (merged []byte, clean bool) {
	t.Helper()
	dir := t.TempDir()
	paths := make([]string, 3)
	for i, b := range [][]byte{ours, base, theirs} {
		paths[i] = filepath.Join(dir, []string{"ours", "base", "theirs"}[i])
		if err := os.WriteFile(paths[i], b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command("git", append([]string{"merge-file", "-p"}, paths...)...).Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out, true
	case errors.As(err, &exit) && exit.ExitCode() > 0 && exit.ExitCode() < 128:
		return out, false
	default:
		t.Fatalf("git merge-file: %v", err)
		return nil, false
	}
}

// propertyRow is one property type entry as a contributor would add it.
func propertyRow(name string) string {
	return "  - name: " + name + "\n" +
		"    data_type: string\n" +
		"    label: " + name + "\n" +
		"    description: >-\n" +
		"      A row one branch adds.\n"
}

// TestSeedFactsMergeCleanly is the #1492 scenario end to end at the file
// level: two branches from one base each add a distinct property type at a
// different place in property_types.yaml and regenerate. git's line merge of
// the two derived seed.json files must come back without a conflict and equal
// a fresh render of the merged YAML, so neither a hand merge nor a gen-drift
// failure follows.
func TestSeedFactsMergeCleanly(t *testing.T) {
	baseYAML := string(propertyTypesYAML)
	anchor := "  - name: tcp-open\n"
	if !strings.Contains(baseYAML, anchor) {
		t.Fatalf("property_types.yaml no longer lists tcp-open; pick another anchor row")
	}
	// Branch A adds its row near the top of the list, branch B at the end.
	oursYAML := strings.Replace(baseYAML, anchor, propertyRow("merge-demo-alpha")+anchor, 1)
	theirsYAML := strings.TrimRight(baseYAML, "\n") + "\n" + propertyRow("merge-demo-bravo")

	mergedYAML, clean := mergeFile(t, []byte(baseYAML), []byte(oursYAML), []byte(theirsYAML))
	if !clean {
		t.Fatalf("the YAML sources conflict, so the derived-file check would not be reached:\n%s", mergedYAML)
	}

	base := factsWith(t, []byte(baseYAML))
	ours := factsWith(t, []byte(oursYAML))
	theirs := factsWith(t, []byte(theirsYAML))
	merged, clean := mergeFile(t, base, ours, theirs)
	if !clean {
		t.Fatalf("seed.json conflicts though the branches added different rows:\n%s", merged)
	}
	if want := factsWith(t, mergedYAML); !bytes.Equal(merged, want) {
		t.Errorf("the merged seed.json differs from a fresh render of the merged YAML; gen-drift would fail it")
	}
	for _, name := range []string{"merge-demo-alpha", "merge-demo-bravo"} {
		if !bytes.Contains(merged, []byte(`"name": "`+name+`"`)) {
			t.Errorf("merged seed.json lost %s", name)
		}
	}
}
