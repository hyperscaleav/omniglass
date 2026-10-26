package api_test

import (
	"net/http"
	"testing"
)

// TestAnUnknownStandardListsNoRoles is the read-side residue of #505. Declaring
// a role on a standard that does not exist has been a 422 that writes nothing
// since #647's owner-arc check (TestUnknownOwnerRefused pins it); reading the
// roles of one still answered 200 with an empty list, which reads as "this
// standard declares nothing". It is a 404, and a real standard that declares
// nothing is still an empty 200.
func TestAnUnknownStandardListsNoRoles(t *testing.T) {
	f := newStaffingFixture(t)
	if st, raw := f.c.send(f.owner, http.MethodGet, "/standards/no-such-standard/roles", nil); st != http.StatusNotFound {
		t.Fatalf("list the roles of an unknown standard = %d, want 404\nbody: %s", st, raw)
	}
	f.c.do(f.owner, http.MethodPost, "/standards", map[string]any{"name": "bare-std", "label": "Bare"}, http.StatusCreated)
	f.c.do(f.owner, http.MethodGet, "/standards/bare-std/roles", nil, http.StatusOK)
}
