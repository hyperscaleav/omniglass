package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestUnresolvedRefMessage pins the operator-facing 404 for a name that names
// no single resource (#1133): it always names what was looked up, lists the
// spellings that resolve when more than one row answers, and caps that list so
// a name a whole fleet shares does not answer with a page of addresses.
func TestUnresolvedRefMessage(t *testing.T) {
	t.Parallel()
	if got, want := unresolvedRefMessage(&storage.ErrUnresolvedRef{Kind: "endpoint", Ref: "panel-1/https-admin"}),
		`endpoint "panel-1/https-admin" not found`; got != want {
		t.Errorf("no candidates = %q, want %q", got, want)
	}
	two := &storage.ErrUnresolvedRef{Kind: "secret", Ref: "demo-panel-login", Candidates: []string{"$platform/demo-panel-login", "hq.huddle/demo-panel-login"}}
	if got, want := unresolvedRefMessage(two),
		`secret "demo-panel-login" not found: 2 you can see answer to it; address one as $platform/demo-panel-login or hq.huddle/demo-panel-login`; got != want {
		t.Errorf("two candidates = %q, want %q", got, want)
	}
	hint := &storage.ErrUnresolvedRef{Kind: "endpoint", Ref: "https", Hint: storage.EndpointRefHint}
	if got := unresolvedRefMessage(hint); !strings.HasSuffix(got, "; "+storage.EndpointRefHint) {
		t.Errorf("a hint = %q, want it appended", got)
	}
	var many []string
	for i := 1; i <= 36; i++ {
		many = append(many, fmt.Sprintf("hq.room-%d.$comp.panel-1/tcp", i))
	}
	got := unresolvedRefMessage(&storage.ErrUnresolvedRef{Kind: "endpoint", Ref: "panel-1/tcp", Candidates: many})
	if !strings.Contains(got, "36 you can see answer to it") || !strings.Contains(got, "hq.room-10.$comp.panel-1/tcp (and 26 more)") ||
		strings.Contains(got, "hq.room-11.") {
		t.Errorf("36 candidates = %q, want ten listed and the rest counted", got)
	}
}

// TestAmbiguousRefErrorWithoutCandidates pins that an ambiguous name with no
// candidates (the no-caller-scope path) is a plain 409 message with no
// path.name detail, so a client never reads "matches 0 rows" (#2334).
func TestAmbiguousRefErrorWithoutCandidates(t *testing.T) {
	t.Parallel()
	err := ambiguousRefError(ambiguousAsUnresolved(&storage.ErrAmbiguousName{Kind: "component", Ref: "dup-1"}))
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != 409 {
		t.Fatalf("err = %#v, want a 409 status error", err)
	}
	if em, ok := err.(*huma.ErrorModel); ok && len(em.Errors) != 0 {
		t.Fatalf("errors = %+v, want no path.name detail without candidates", em.Errors)
	}
	if !strings.Contains(err.Error(), "dup-1") {
		t.Fatalf("message = %q, want it to name the reference", err.Error())
	}
}
