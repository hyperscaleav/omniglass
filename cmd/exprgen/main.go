// Command exprgen renders the expression sites (#958) from their declaration
// in internal/rules: each site's bindings (read from its environment's Go
// struct), the builtins it admits and its run budget, into
// docs/src/generated/expressions.json, which the expressions page renders.
// No binding is hand-listed; TestSitesFactsMatchCommitted fails when the
// committed file goes stale.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/rules"
)

const dst = "docs/src/generated/expressions.json"

func main() {
	b, err := rules.SitesFactsJSON()
	if err != nil {
		log.Fatalf("exprgen: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		log.Fatalf("exprgen: %v", err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		log.Fatalf("exprgen: %v", err)
	}
	log.Printf("wrote the expression sites to %s", dst)
}
