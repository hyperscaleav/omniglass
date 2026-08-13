package storagetest

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// The collision #662 is filed against, stated as a property of the names rather
// than of two processes: two binaries mint their names independently, and their
// counters both start at 1, so a name that is a function of the counter alone
// collides on the first database each of them creates. That is invisible on the
// container path and it is what makes the OMNIGLASS_TEST_ADMIN_DSN hatch
// unusable at any -p above 1.
//
// Driven through the tag rather than through two processes: the tag IS the
// scoping, so two tags is exactly the situation, and a test that forked a
// second `go test` to prove it would be testing the operating system.
func TestNamesAreScopedToTheProcess(t *testing.T) {
	a, b := newProcTag(), newProcTag()
	if a == b {
		t.Fatalf("two tags are identical (%q); every binary would mint the same names again", a)
	}
	for _, tag := range []string{a, b} {
		pid, _, ok := strings.Cut(tag, "_")
		if !ok {
			t.Fatalf("tag %q carries no pid; a leftover database cannot be traced to the run that left it", tag)
		}
		if got, err := strconv.Atoi(pid); err != nil || got != os.Getpid() {
			t.Fatalf("tag %q leads with %q, want this process's pid %d", tag, pid, os.Getpid())
		}
	}

	if templateNameFor(a) == templateNameFor(b) {
		t.Errorf("two binaries build the template under one name %q; one drops it out from under the other mid-run", templateNameFor(a))
	}
	// Both counters at 1, which is where two binaries really do start.
	if testDBNameFor(a, 1) == testDBNameFor(b, 1) {
		t.Errorf("two binaries name their first database %q; the second CREATE DATABASE fails", testDBNameFor(a, 1))
	}
	// And within one binary the counter still separates them.
	if testDBNameFor(a, 1) == testDBNameFor(a, 2) {
		t.Errorf("one binary named two databases %q", testDBNameFor(a, 1))
	}

	// Postgres truncates an identifier at 63 bytes, which would silently undo
	// the scoping by cutting the tag off the end of a long name.
	for _, name := range []string{templateNameFor(a), testDBNameFor(a, 999)} {
		if len(name) > 63 {
			t.Errorf("name %q is %d bytes, over Postgres's 63-byte identifier limit", name, len(name))
		}
	}
}

// TestDropTemplateTakesOnlyTheOneNamed is the teardown half. A template built
// by ANOTHER binary must survive this one's reclaim, which is the failure #649's
// template build introduced: a fixed name meant "drop the stale template" and
// "drop the template a sibling binary is copying from right now" were the same
// statement.
//
// The sibling is simulated by building a second template under a second tag,
// against the same Postgres, which is exactly the shape the hatch produces.
func TestDropTemplateTakesOnlyTheOneNamed(t *testing.T) {
	if testing.Short() {
		t.Skip("storagetest: skipped under -short (Postgres testcontainer)")
	}
	ctx := context.Background()
	NewDSN(t) // force the container and this binary's template into existence

	sibling := templateNameFor(newProcTag())
	admin := connect(t, ctx, adminDSN)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{sibling}.Sanitize()); err != nil {
		t.Fatalf("create the sibling's template: %v", err)
	}
	t.Cleanup(func() {
		cleanup := connect(t, context.Background(), adminDSN)
		_ = dropTemplate(context.Background(), cleanup, sibling)
	})

	// This binary's reclaim runs. It must not reach the sibling.
	if err := dropTemplate(ctx, admin, templateName); err != nil {
		t.Fatalf("drop this binary's template: %v", err)
	}
	if exists(t, ctx, admin, templateName) {
		t.Errorf("%s survived its own binary's reclaim", templateName)
	}
	if !exists(t, ctx, admin, sibling) {
		t.Fatalf("%s was dropped by another binary's reclaim, which is the collision this closes", sibling)
	}

	// Put this binary's template back, since the rest of the package provisions
	// from it. buildTemplate is idempotent by construction (it drops first), and
	// going through it rather than a raw CREATE is what keeps this test from
	// leaving a template the next NewDSN cannot copy.
	if err := buildTemplate(ctx); err != nil {
		t.Fatalf("rebuild this binary's template: %v", err)
	}
	if _, err := pgx.Connect(ctx, NewDSN(t)); err != nil {
		t.Fatalf("provision from the rebuilt template: %v", err)
	}
}

func exists(t *testing.T, ctx context.Context, admin *pgx.Conn, name string) bool {
	t.Helper()
	var ok bool
	if err := admin.QueryRow(ctx,
		"select exists (select 1 from pg_database where datname = $1)", name).Scan(&ok); err != nil {
		t.Fatalf("look up database %s: %v", name, err)
	}
	return ok
}
