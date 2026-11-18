package storage

// The identifier policy's seams for the external test package, the same shim
// pattern ExportGenerateName and ExportLocationsOver established: this lives in
// a _test.go file, so it exists only in the test binary and ships in nothing.
//
// The three shapes are unexported because nothing outside the gateway may bind
// its SQL, and the invariant between them (user_ident_test.go) can only be
// asserted by driving all three, so it needs a door.

// ExportUserIdent resolves the identifier in Go.
func ExportUserIdent(vals ...*string) string { return userIdent(vals...) }

// ExportUserIdentCols is the sub-select projection a Go resolution reads.
func ExportUserIdentCols(param string) string { return userIdentCols(param) }

// ExportUserIdentSQL is the same order folded into one bound expression.
func ExportUserIdentSQL(param string) string { return userIdentSQL(param) }

// ExportUserIdentJoins, ExportUserIdentJoinedCols and
// ExportUserIdentJoinedSQL are the join shape, which the invariant drives
// alongside the sub-select shape: two plan shapes of one policy drift exactly as
// readily as two spellings of it.
func ExportUserIdentJoins(alias, idExpr string) string {
	return userIdentJoins(alias, idExpr)
}
func ExportUserIdentJoinedCols(alias string) string { return userIdentJoinedCols(alias) }
func ExportUserIdentJoinedSQL(alias string) string  { return userIdentJoinedSQL(alias) }
