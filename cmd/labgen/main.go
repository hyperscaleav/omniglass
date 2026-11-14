// Command labgen renders the lab's generated facts. The lab guide's
// (docs/src/generated/lab.json) come from internal/lab's planner and the
// walkthrough manifests: the topology table (every container, its privilege,
// the one publish) and the guided walkthroughs a learner follows on the lab.
// The system page reads `lab measure` times (web/src/lib/system_page_reads.gen.ts)
// come from internal/labfootprint, for the page's test to hold the page to.
// The perf gate's figure and size tables (docs/src/generated/perfgate.json)
// come from internal/labfootprint's gate tables, for the lab guide to render.
// The collection-method table (docs/src/generated/methods.json) comes from
// internal/devseed's fixture and MethodGaps, for the simulation page and the
// lab guide to render (#1419).
// Each has a drift test that fails when the committed file goes stale.
package main

import (
	"log"
	"os"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab"
	"github.com/hyperscaleav/omniglass/internal/labfootprint"
)

const (
	dst      = "docs/src/generated/lab.json"
	readsDst = "web/src/lib/system_page_reads.gen.ts"
	perfDst  = "docs/src/generated/perfgate.json"
	methDst  = "docs/src/generated/methods.json"
)

func main() {
	facts, err := lab.FactsJSON(os.DirFS("web/e2e/walkthroughs"))
	if err != nil {
		log.Fatalf("labgen: %v", err)
	}
	if err := os.WriteFile(dst, facts, 0o644); err != nil {
		log.Fatalf("labgen: write %s: %v", dst, err)
	}
	log.Printf("wrote the lab guide's facts to %s", dst)
	if err := os.WriteFile(readsDst, labfootprint.SystemPageReadsTS(), 0o644); err != nil {
		log.Fatalf("labgen: write %s: %v", readsDst, err)
	}
	log.Printf("wrote the system page's reads to %s", readsDst)
	perf, err := labfootprint.PerfGateFactsJSON()
	if err != nil {
		log.Fatalf("labgen: %v", err)
	}
	if err := os.WriteFile(perfDst, perf, 0o644); err != nil {
		log.Fatalf("labgen: write %s: %v", perfDst, err)
	}
	log.Printf("wrote the perf gate's tables to %s", perfDst)
	methods, err := devseed.MethodFactsJSON()
	if err != nil {
		log.Fatalf("labgen: %v", err)
	}
	if err := os.WriteFile(methDst, methods, 0o644); err != nil {
		log.Fatalf("labgen: write %s: %v", methDst, err)
	}
	log.Printf("wrote the collection-method table to %s", methDst)
}
