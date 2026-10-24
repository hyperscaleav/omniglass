// Command adrrenumber moves a decision to a new ADR number and rewrites every
// reference to it (#1095): the fix for two PRs that took the same next number,
// which the decisions lint reports naming both files. Run it from the
// repository root on the branch that lands second:
//
//	make adr-renumber FROM=NNNN TO=MMMM                       # before merging main
//	make adr-renumber FROM=NNNN TO=MMMM FILE=adr-NNNN-<slug>.md  # both decisions in the tree
//
// With FILE, only the lines the commits after SINCE (default origin/main) wrote
// are rewritten, so the other decision's references keep the number.
package main

import (
	"flag"
	"fmt"
	"log"
	"strconv"

	"github.com/hyperscaleav/omniglass/internal/docslint"
)

func main() {
	from := flag.String("from", "", "the ADR number to move (four digits)")
	to := flag.String("to", "", "the free ADR number to move it to (four digits)")
	file := flag.String("file", "", "the decision file to move, when two files hold the number")
	since := flag.String("since", "", "with -file: rewrite only lines written by commits after this revision (default origin/main)")
	flag.Parse()
	f, errF := strconv.Atoi(*from)
	t, errT := strconv.Atoi(*to)
	if errF != nil || errT != nil {
		log.Fatal("usage: make adr-renumber FROM=NNNN TO=MMMM [FILE=adr-NNNN-<slug>.md [SINCE=<rev>]]")
	}
	changed, err := docslint.RenumberADR(".", f, t, docslint.RenumberOptions{File: *file, Since: *since})
	if err != nil {
		log.Fatalf("adr-renumber: %v", err)
	}
	fmt.Printf("ADR-%04d is now ADR-%04d; %d file(s) changed:\n", f, t, len(changed))
	for _, path := range changed {
		fmt.Println("  " + path)
	}
	fmt.Println("Review the diff (git diff), stage the renamed file, then run go test ./internal/docslint/.")
}
