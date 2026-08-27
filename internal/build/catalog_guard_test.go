package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
)

// TestTestsDoNotBorrowTheSeedCatalog fails any *_test.go that names a seeded
// product SKU outside the files whose job is pinning the seed contract.
//
// The #802 catalog rename broke ~50 tests, and only ~15 of those were pins
// doing their job: the rest were behavior tests that had borrowed a seeded SKU
// ("a product classified display" spelled as a particular display) as a
// fixture of convenience. A behavior test that needs a product mints its own
// (storagetest.MintProduct); a test about the shipped catalog itself lives in
// a pin file below and derives its expectations from the embedded YAML. With
// both rules held, a future catalog change touches the YAML and the pin files
// and nothing else (#804).
//
// The SKU list is read from the seed's own render (seed.FactsJSON), so the
// guard can never drift from the catalog it protects.
func TestTestsDoNotBorrowTheSeedCatalog(t *testing.T) {
	// The files allowed to name a SKU: the seed's own contract pins and the
	// demo fleet, whose fixtures staff rooms with catalog products on purpose.
	pinFiles := map[string]bool{
		"internal/seed/seed_test.go":        true,
		"internal/seed/facts_test.go":       true,
		"internal/devseed/devseed_test.go":  true,
		"internal/devseed/identity_test.go": true,
	}

	raw, err := seed.FactsJSON()
	if err != nil {
		t.Fatalf("render seed facts: %v", err)
	}
	var doc struct {
		Products []struct {
			ID string `json:"id"`
		} `json:"products"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse seed facts: %v", err)
	}
	var skus []string
	for _, p := range doc.Products {
		// The three generics are the catalog's own fallback vocabulary
		// (unclassified writes resolve to them); a test naming one is using
		// the platform's floor, not borrowing a brand.
		if strings.HasPrefix(p.ID, "generic-") {
			continue
		}
		skus = append(skus, p.ID)
	}
	if len(skus) == 0 {
		t.Fatal("found no SKUs, which means this guard is not reading the catalog it thinks it is")
	}

	root := filepath.Join("..", "..")
	violations := map[string][]string{}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
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
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if pinFiles[rel] {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(src)
		for _, sku := range skus {
			if strings.Contains(text, sku) {
				violations[rel] = append(violations[rel], sku)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	files := make([]string, 0, len(violations))
	for f := range violations {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		t.Errorf("%s borrows the seed catalog (%s); mint a product (storagetest.MintProduct) or move the pin into a designated pin file", f, strings.Join(violations[f], ", "))
	}
}
