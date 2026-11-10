package storage

// ExportCommandHealthSQL is the command queue health read ($1 the window's
// start), for a test to EXPLAIN.
func ExportCommandHealthSQL() string { return commandHealthSQL }
