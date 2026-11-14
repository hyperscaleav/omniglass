package storage

import (
	"context"
	"testing"
)

// The locks a batch takes up front are recorded on its own transaction alone:
// a savepoint's record would outlive a rollback that released them, so a lane
// would skip a lock it no longer holds. lockGroups refuses a savepoint before
// it takes or records anything (#1928).
func TestLockGroupsRefusesASavepoint(t *testing.T) {
	t.Parallel()
	sp := newBatchTx(nil).savepoint(nil)
	if err := (&PG{}).lockGroups(context.Background(), sp, nil); err == nil {
		t.Fatal("lockGroups took a savepoint, whose rollback would release locks the batch still records")
	}
	if len(sp.ruleKeys)+len(sp.rules)+len(sp.marked)+len(sp.series) != 0 {
		t.Fatal("a refused savepoint recorded a lock")
	}
}
