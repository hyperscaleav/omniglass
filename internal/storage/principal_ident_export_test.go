package storage

// The identifier policy's seams for the external test package, the same shim
// pattern ExportGenerateName and ExportLocationsOver established: this lives in
// a _test.go file, so it exists only in the test binary and ships in nothing.
//
// The three shapes are unexported because nothing outside the gateway may bind
// its SQL, and the invariant between them (principal_ident_test.go) can only be
// asserted by driving all three, so it needs a door.

// ExportPrincipalIdent resolves the identifier in Go.
func ExportPrincipalIdent(vals ...*string) string { return principalIdent(vals...) }

// ExportPrincipalIdentCols is the sub-select projection a Go resolution reads.
func ExportPrincipalIdentCols(param string) string { return principalIdentCols(param) }

// ExportPrincipalIdentSQL is the same order folded into one bound expression.
func ExportPrincipalIdentSQL(param string) string { return principalIdentSQL(param) }

// ExportPrincipalIdentJoins, ExportPrincipalIdentJoinedCols and
// ExportPrincipalIdentJoinedSQL are the join shape, which the invariant drives
// alongside the sub-select shape: two plan shapes of one policy drift exactly as
// readily as two spellings of it.
func ExportPrincipalIdentJoins(alias, idExpr string) string {
	return principalIdentJoins(alias, idExpr)
}
func ExportPrincipalIdentJoinedCols(alias string) string { return principalIdentJoinedCols(alias) }
func ExportPrincipalIdentJoinedSQL(alias string) string  { return principalIdentJoinedSQL(alias) }
