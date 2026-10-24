// Command adrrenumber moves a decision to a new ADR number and rewrites every
// reference to it. Since #1108 a decision takes its issue's number, so two PRs
// clash only when both add a second decision to one issue (both take
// ADR-NNNN.2); the decisions lint names both files, and the branch that lands
// second moves its decision:
//
//	make adr-renumber FROM=NNNN.2 TO=NNNN.3                       # before merging main
//	make adr-renumber FROM=NNNN.2 TO=NNNN.3 FILE=adr-NNNN.2-<slug>.md  # both in the tree
//
// With FILE, only the lines the commits after SINCE (default origin/main) wrote
// are rewritten, so the other decision's references keep the number.
package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/hyperscaleav/omniglass/internal/docslint"
)

func main() {
	from := flag.String("from", "", "the ADR number to move, as its heading writes it (NNNN.2, or a legacy number)")
	to := flag.String("to", "", "the free ADR number to move it to (NNNN.3)")
	file := flag.String("file", "", "the decision file to move, when two files hold the number")
	since := flag.String("since", "", "with -file: rewrite only lines written by commits after this revision (default origin/main)")
	flag.Parse()
	if *from == "" || *to == "" {
		log.Fatal("usage: make adr-renumber FROM=NNNN.2 TO=NNNN.3 [FILE=adr-NNNN.2-<slug>.md [SINCE=<rev>]]")
	}
	changed, err := docslint.RenumberADR(".", *from, *to, docslint.RenumberOptions{File: *file, Since: *since})
	if err != nil {
		log.Fatalf("adr-renumber: %v", err)
	}
	fmt.Printf("ADR-%s is now ADR-%s; %d file(s) changed:\n", *from, *to, len(changed))
	for _, path := range changed {
		fmt.Println("  " + path)
	}
	fmt.Println("Review the diff (git diff), stage the renamed file, then run go test ./internal/docslint/.")
}
