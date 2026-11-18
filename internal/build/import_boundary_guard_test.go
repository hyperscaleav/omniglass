package build

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// goListPkg is the subset of go list -json output the import boundary guard needs.
type goListPkg struct {
	ImportPath string
	Imports    []string
}

// TestImportBoundaries enforces two architecture invariants by reading the
// module's import graph with go list:
//
//  1. Only internal/storage and named tooling (cmd/erdgen, internal/erd,
//     internal/compressbench) may directly import github.com/jackc/pgx or
//     database/sql. Every other package must talk to Postgres through the
//     Storage Gateway.
//
//  2. internal/outpost (production code, not test files) must not import
//     internal/storage or internal/api. The outpost is an edge binary; importing
//     the server's DB or API graph inflates it and couples the edge to the server
//     release cycle.
//
// Each rule carries an allowlist. A stale allowlist entry (a package that no
// longer imports the controlled dependency) fails the test, so the list cannot
// silently rot.
func TestImportBoundaries(t *testing.T) {
	root := filepath.Join("..", "..")
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}

	var pkgs []goListPkg
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p goListPkg
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("decode package list entry: %v", err)
		}
		pkgs = append(pkgs, p)
	}

	testDBImportBoundary(t, pkgs)
	testOutpostIsolation(t, pkgs)
}

// testDBImportBoundary asserts that only the Storage Gateway and named tooling
// import the database packages.
func testDBImportBoundary(t *testing.T, pkgs []goListPkg) {
	t.Helper()

	// allowedDBImporters is the complete allowlist of packages that may directly
	// import github.com/jackc/pgx or database/sql. A stale entry (a package
	// that no longer imports either) fails the test.
	allowedDBImporters := map[string]string{
		"github.com/hyperscaleav/omniglass/internal/storage":                        "the Storage Gateway: the only db path in the application graph",
		"github.com/hyperscaleav/omniglass/internal/storage/storagetest":            "test helper: provisions real Postgres instances for integration tests",
		"github.com/hyperscaleav/omniglass/internal/storage/storagetest/accesspath": "test helper: traces query access paths for integration-test assertions",
		"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount": "test helper: counts queries issued during a Postgres-backed test",
		"github.com/hyperscaleav/omniglass/cmd/erdgen":                              "tooling: generates the entity-relationship diagram from live schema",
		"github.com/hyperscaleav/omniglass/internal/erd":                            "tooling: reads schema to build the erd model for cmd/erdgen",
		"github.com/hyperscaleav/omniglass/internal/compressbench":                  "tooling: benchmarks compression strategies against stored sample data",
	}

	seenAllowed := map[string]bool{}
	var violations []string

	for _, pkg := range pkgs {
		if !importsDB(pkg.Imports) {
			continue
		}
		if _, ok := allowedDBImporters[pkg.ImportPath]; ok {
			seenAllowed[pkg.ImportPath] = true
		} else {
			violations = append(violations, pkg.ImportPath)
		}
	}

	sort.Strings(violations)
	for _, v := range violations {
		t.Errorf("package %s imports github.com/jackc/pgx or database/sql outside the Storage Gateway allowlist; use the Storage Gateway instead", v)
	}
	for path := range allowedDBImporters {
		if !seenAllowed[path] {
			t.Errorf("allowed DB importer %s no longer imports pgx or database/sql; drop it from the allowlist in %s", path, "internal/build/import_boundary_guard_test.go")
		}
	}
}

// testOutpostIsolation asserts that internal/outpost production code does not
// import internal/storage or internal/api.
func testOutpostIsolation(t *testing.T, pkgs []goListPkg) {
	t.Helper()

	// outpostPath is the production package we guard. If it disappears from
	// the module (renamed or deleted) the test fails rather than silently passing.
	const outpostPath = "github.com/hyperscaleav/omniglass/internal/outpost"

	// forbiddenForOutpost lists the packages that internal/outpost must not
	// import in its production (non-test) source. Any package outside this
	// list may be freely imported by the outpost.
	forbiddenForOutpost := []string{
		"github.com/hyperscaleav/omniglass/internal/storage",
		"github.com/hyperscaleav/omniglass/internal/api",
	}

	found := false
	for _, pkg := range pkgs {
		if pkg.ImportPath != outpostPath {
			continue
		}
		found = true
		for _, imp := range pkg.Imports {
			for _, forbidden := range forbiddenForOutpost {
				if imp == forbidden {
					t.Errorf("internal/outpost imports %s; move shared code to a package both the outpost and the server may import (see internal/metric for the established pattern)", forbidden)
				}
			}
		}
	}
	if !found {
		t.Errorf("package %s not found in the module; update the outpost isolation guard if it was renamed", outpostPath)
	}
}

// importsDB reports whether imports contains any database package controlled
// by the Storage Gateway boundary: pgx (any sub-path) or database/sql.
func importsDB(imports []string) bool {
	for _, imp := range imports {
		if strings.HasPrefix(imp, "github.com/jackc/pgx") || imp == "database/sql" {
			return true
		}
	}
	return false
}
