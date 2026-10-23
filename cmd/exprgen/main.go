// Command exprgen renders the expression sites (#958) from their declaration
// in internal/rules: each site's bindings (read from its environment's Go
// struct), the builtins it admits and its run budget, into
// docs/src/generated/expressions.json, which the expressions page renders,
// the calc site's alias rules to web/src/lib/calc_names.gen.ts, which the
// console's calc form and editor check aliases by, and the diagnostics
// registry (internal/expression) to docs/src/generated/expression-diagnostics.json,
// which the expressions page's diagnostics table renders.
// No binding is hand-listed; TestSitesFactsMatchCommitted fails when the
// committed file goes stale.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/expression"
	"github.com/hyperscaleav/omniglass/internal/rules"
)

const (
	dst      = "docs/src/generated/expressions.json"
	namesDst = "web/src/lib/calc_names.gen.ts"
)

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
	if err := os.WriteFile(namesDst, rules.CalcNamesTS(), 0o644); err != nil {
		log.Fatalf("exprgen: %v", err)
	}
	log.Printf("wrote the calc alias rules to %s", namesDst)
	diags, err := expression.DiagnosticsFactsJSON()
	if err != nil {
		log.Fatalf("exprgen: %v", err)
	}
	if err := os.WriteFile(expression.DiagnosticsFactsPath, diags, 0o644); err != nil {
		log.Fatalf("exprgen: %v", err)
	}
	log.Printf("wrote the expression diagnostics to %s", expression.DiagnosticsFactsPath)
}
