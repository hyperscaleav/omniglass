package api_test

import (
	"net/http"
	"testing"
)

// TestAnUnknownStandardDeclaresNoRole is #505: a role declared on a standard
// that does not exist is a 422 and writes nothing, whatever the body carries,
// and two different typos never collide into one ownerless row. Reading the
// roles of a standard that does not exist is a 404, not an empty list that
// reads as "this standard declares nothing".
func TestAnUnknownStandardDeclaresNoRole(t *testing.T) {
	f := newStaffingFixture(t)

	for _, body := range []map[string]any{
		{"label": "Ghost", "quorum": 2},
		{"label": "Ghost", "accepted_types": []string{"display"}},
	} {
		if st, raw := f.c.send(f.owner, http.MethodPatch, "/standards/no-such-standard/roles/ghost", body); st != http.StatusUnprocessableEntity {
			t.Fatalf("declare on an unknown standard %v = %d, want 422\nbody: %s", body, st, raw)
		}
	}
	for _, typo := range []string{"typo-a", "typo-b"} {
		if st, raw := f.c.send(f.owner, http.MethodPatch, "/standards/"+typo+"/roles/mic", map[string]any{"quorum": 2}); st != http.StatusUnprocessableEntity {
			t.Fatalf("declare on %s = %d, want 422\nbody: %s", typo, st, raw)
		}
	}
	if n := f.count(`select count(*) from system_role where name in ('ghost', 'mic')`); n != 0 {
		t.Fatalf("%d role rows written for an unknown standard, want none", n)
	}
	if st, raw := f.c.send(f.owner, http.MethodGet, "/standards/no-such-standard/roles", nil); st != http.StatusNotFound {
		t.Fatalf("list the roles of an unknown standard = %d, want 404\nbody: %s", st, raw)
	}
}
