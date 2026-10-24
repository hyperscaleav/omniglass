// Command adrrenumber moves a decision to a new ADR number and rewrites every
// reference to it (#1095): the fix for two PRs that took the same next number,
// which the decisions lint reports naming both files. Run it from the
// repository root on the branch that lands second, before merging main:
//
//	make adr-renumber FROM=0160 TO=0161
package main

import (
	"flag"
	"fmt"
	"log"
	"strconv"

	"github.com/hyperscaleav/omniglass/internal/docslint"
)

func main() {
	from := flag.String("from", "", "the ADR number to move, like 0160")
	to := flag.String("to", "", "the free ADR number to move it to, like 0161")
	flag.Parse()
	f, errF := strconv.Atoi(*from)
	t, errT := strconv.Atoi(*to)
	if errF != nil || errT != nil {
		log.Fatal("usage: make adr-renumber FROM=NNNN TO=MMMM")
	}
	changed, err := docslint.RenumberADR(".", f, t)
	if err != nil {
		log.Fatalf("adr-renumber: %v", err)
	}
	fmt.Printf("ADR-%04d is now ADR-%04d; %d file(s) changed:\n", f, t, len(changed))
	for _, path := range changed {
		fmt.Println("  " + path)
	}
	fmt.Println("Check the decision's Superseded/Amends links read right, then run go test ./internal/docslint/.")
}
