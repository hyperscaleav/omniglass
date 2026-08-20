package storage

import "testing"

// TestTheHealthLockKeyPartitionsByID pins the fact ADR-0056 documented wrongly
// until #717: the health advisory lock hashes the owner's ID, not its name.
//
// The two halves of that are one fact. A lock partitions the fleet only while
// its key does, and the identity epic (#627) scoped a location's name uniqueness
// to its PLACEMENT, so two rooms under different buildings may both be `415a`.
// A name-keyed lock hashes those two unrelated owners to one key, which is a
// silent loss of concurrency, and a mixed currency (a name here, an id there)
// hashes two keys for one owner, which is a silent loss of the serialization the
// compare-then-act recompute depends on for correctness.
//
// It is a unit test on the key rather than an integration test on the lock
// because the property is about the STRING: whether two owners contend is
// decided before any SQL runs, and the deadlock-order half is already covered by
// TestLockOrderIsByID against a real database.
func TestTheHealthLockKeyPartitionsByID(t *testing.T) {
	const (
		roomA = "0198f000-0000-7000-8000-00000000000a"
		roomB = "0198f000-0000-7000-8000-00000000000b"
	)
	// The two same-named rooms #627 made legal.
	if healthLockKey("location", roomA) == healthLockKey("location", roomB) {
		t.Error("two locations share one health lock key: a name-keyed lock would do this, and 415a under two buildings is legal since #627")
	}
	if healthLockKey("location", roomA) != healthLockKey("location", roomA) {
		t.Error("one owner does not key consistently, so nothing serializes")
	}
	if healthLockKey("component", roomA) == healthLockKey("system", roomA) {
		t.Error("the owner KIND is not part of the key; two tiers of one id would contend")
	}
	if got, want := healthLockKey("system", roomA), "health/system/"+roomA; got != want {
		t.Errorf("healthLockKey = %q, want %q (the shape ADR-0056 documents)", got, want)
	}
}
