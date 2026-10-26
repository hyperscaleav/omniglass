package collection

import (
	"slices"
	"testing"

	"github.com/google/uuid"
)

// TestNewBatchIDIsTimeOrdered: a batch id is a UUIDv7, so the ids one
// publisher mints sort in the order it minted them. The server keys its
// receipt by subject and id (ADR-0311), so each publisher's receipts append
// at the right edge of the index rather than dirtying a random page of it
// on every batch, and the retention pass trims the left edge.
func TestNewBatchIDIsTimeOrdered(t *testing.T) {
	ids := make([]string, 200)
	for i := range ids {
		ids[i] = NewBatchID()
		u, err := uuid.Parse(ids[i])
		if err != nil || u.Version() != 7 {
			t.Fatalf("id %q is not a UUIDv7 (%v)", ids[i], err)
		}
	}
	if !slices.IsSorted(ids) {
		t.Fatal("successive batch ids do not sort in the order they were minted")
	}
	if len(slices.Compact(slices.Clone(ids))) != len(ids) {
		t.Fatal("two batch ids are the same")
	}
}
