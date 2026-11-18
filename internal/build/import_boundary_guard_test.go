package build

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestOnlyStorageAndToolingImportPgx ensures that only the Storage Gateway
// and a narrow set of named tooling import github.com/jackc/pgx or
// database/sql in non-test Go files. Every other package must talk to
// Postgres through the gateway; a direct pgx import elsewhere would let a
// caller bypass the scope injector and writer-class invariants the gateway
// holds on every query.
//
// The allowlist is stale-entry checked: an entry whose directory no longer
// contains a non-test file that imports pgx or database/sql fails the test,
// so the list cannot silently outlive the code it covers (#2189).
func TestOnlyStorageAndToolingImportPgx(t *testing.T) {
	// Directories (relative to repo root, prefix-matched so subdirs inherit)
	// permitted to hold pgx or database/sql imports in non-test Go files.
	// A stale entry fails, so the list never outlives the code it excuses.
	allowed := map[string]string{
		"internal/storage":       "the Storage Gateway: the only production DB path",
		"internal/erd":           "schema introspection tooling used by cmd/erdgen",
		"internal/compressbench": "compression benchmark, runs against a live Postgres",
		"cmd/erdgen":             "ERD generator: connects to Postgres to read the schema",
	}

	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	var found []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "node_modules", "dist", ".git", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))

		for _, imp := range importPaths(path) {
			if !strings.HasPrefix(imp, "github.com/jackc/pgx") && imp != "database/sql" {
				continue
			}
			// Find the matching allowed directory (most specific prefix wins).
			matchKey := ""
			for key := range allowed {
				if dir == key || strings.HasPrefix(dir, key+"/") {
					if len(key) > len(matchKey) {
						matchKey = key
					}
				}
			}
			if matchKey != "" {
				seen[matchKey] = true
			} else {
				found = append(found, rel+": imports "+imp)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Strings(found)
	for _, f := range found {
		t.Errorf("%s; only internal/storage and named tooling may import pgx or database/sql. "+
			"Add a Storage Gateway call instead, or file an issue to extend the allowlist in "+
			"internal/build/import_boundary_guard_test.go", f)
	}
	for key := range allowed {
		if !seen[key] {
			t.Errorf("allowed directory %s has no non-test file importing pgx or database/sql; "+
				"drop it from the allowlist in TestOnlyStorageAndToolingImportPgx", key)
		}
	}
}

// TestOutpostDoesNotImportStorageOrAPI ensures that non-test files in
// internal/outpost do not import internal/storage or internal/api. The
// outpost is the edge binary; linking the server's DB or API layer into it
// would pull pgx and the full server stack into the edge build. Shared
// helpers that both sides need live in narrower packages neither side owns
// (e.g. internal/metrictime, #2189).
//
// The allowlist is stale-entry checked: an entry that no longer imports the
// forbidden package fails, so the list cannot silently outlive the code it
// covers.
func TestOutpostDoesNotImportStorageOrAPI(t *testing.T) {
	// No exceptions today. The allowlist exists so the stale-entry pattern
	// holds: when an exception is added it must be justified, and when the
	// justification lapses the entry fails.
	allowed := map[string]string{}

	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	var found []string

	outpostDir := filepath.Join(root, "internal", "outpost")
	err := filepath.Walk(outpostDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		const mod = "github.com/hyperscaleav/omniglass/"
		for _, imp := range importPaths(path) {
			if !strings.HasPrefix(imp, mod) {
				continue
			}
			local := imp[len(mod):]
			if local != "internal/storage" && !strings.HasPrefix(local, "internal/storage/") &&
				local != "internal/api" && !strings.HasPrefix(local, "internal/api/") {
				continue
			}
			key := rel + ":" + imp
			if _, ok := allowed[key]; ok {
				seen[key] = true
			} else {
				found = append(found, rel+": imports "+imp+
					"; internal/outpost must not depend on the server DB or API layer")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/outpost: %v", err)
	}
	sort.Strings(found)
	for _, f := range found {
		t.Errorf("%s", f)
	}
	for key := range allowed {
		if !seen[key] {
			t.Errorf("allowed import %s no longer occurs; drop it from the allowlist in "+
				"TestOutpostDoesNotImportStorageOrAPI", key)
		}
	}
}

// importPaths returns the import paths declared in a Go source file.
// It uses parser.ImportsOnly for speed; a file that cannot be parsed is
// silently skipped (the compiler will report the error).
func importPaths(path string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(f.Imports))
	for _, imp := range f.Imports {
		out = append(out, strings.Trim(imp.Path.Value, `"`))
	}
	return out
}
