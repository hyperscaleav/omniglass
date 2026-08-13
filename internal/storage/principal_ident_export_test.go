package storage

// The identifier policy's seams for the external test package, the same shim
// pattern ExportGenerateName and ExportLocationsOver established: this lives in
// a _test.go file, so it exists only in the test binary and ships in nothing.
//
// The three shapes are unexported because nothing outside the gateway may bind
// its SQL, and the invariant between them (principal_ident_test.go) can only be
// asserted by driving all three, so it needs a door.

// ExportPrincipalIdent resolves the identifier in Go.
func ExportPrincipalIdent(username, serviceName *string) string {
	return principalIdent(username, serviceName)
}

// ExportPrincipalIdentCols is the two-column select list a Go resolution reads.
func ExportPrincipalIdentCols(param string) string { return principalIdentCols(param) }

// ExportPrincipalIdentSQL is the same order folded into one bound expression.
func ExportPrincipalIdentSQL(param string) string { return principalIdentSQL(param) }
