// Command simgen writes the simulator catalog's docs facts (#2837):
// docs/src/generated/simulation.json, read from the typed family specs, their
// model tables and the coverage accounting, for the simulation page to render.
// Every family, model, face and behavior the page names comes from here, so
// none of it is hand-kept; a drift test fails when the committed file goes stale.
package main

import (
	"log"
	"os"

	"github.com/hyperscaleav/omniglass/internal/simfacts"
)

const dst = "docs/src/generated/simulation.json"

func main() {
	b, err := simfacts.JSON()
	if err != nil {
		log.Fatalf("simgen: %v", err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		log.Fatalf("simgen: write %s: %v", dst, err)
	}
	log.Printf("wrote the simulator catalog's facts to %s", dst)
}
