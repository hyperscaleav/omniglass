// Command labgen renders the lab guide's facts (docs/src/generated/lab.json)
// from internal/lab's planner and the walkthrough manifests: the topology table
// (every container, its privilege, the one publish) and the guided walkthroughs
// a learner follows on the lab. The drift test in internal/lab fails when the
// committed file goes stale.
package main

import (
	"log"
	"os"

	"github.com/hyperscaleav/omniglass/internal/lab"
)

const dst = "docs/src/generated/lab.json"

func main() {
	facts, err := lab.FactsJSON(os.DirFS("web/e2e/walkthroughs"))
	if err != nil {
		log.Fatalf("labgen: %v", err)
	}
	if err := os.WriteFile(dst, facts, 0o644); err != nil {
		log.Fatalf("labgen: write %s: %v", dst, err)
	}
	log.Printf("wrote the lab guide's facts to %s", dst)
}
