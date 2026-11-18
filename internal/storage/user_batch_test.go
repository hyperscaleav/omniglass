package storage

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The batched user load (#671), measured against the single-row loader it
// replaces on the LIST path. Internal (package storage) for the same reason
// path_batch_test.go is: loadUsers and loadUser are unexported, and no
// exported entry point reaches the single-row one for a whole page, which is the
// comparison that matters.
//
// Equality, not improvement. A batched loader that costs three statements but
// answers differently from the loader every other read path still uses is not a
// fix, it is a second implementation of the same fact. loadUser is the
// oracle here exactly as PathOf was after #648, and it stays in the tree for
// that reason as much as for GetUser's sake.

// userBatchPool opens a pool on a fresh migrated database. Not boot-seeded:
// the fixture inserts the two roles it needs itself, so the directory it builds
// is exactly the shape being asserted.
func userBatchPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if HarnessDSN == nil {
		t.Fatal("HarnessDSN is nil: the external TestMain no longer assigns it")
	}
	pool, err := pgxpool.New(context.Background(), HarnessDSN(t))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// userBatchFixture builds a directory carrying every shape the two loaders
// could disagree about:
//
//   - all three profiles (person, machine, outpost), because the batch reads
//     them as one union over three differently shaped tables where the single-row
//     path branches on kind;
//   - a user with direct grants only, one with inherited grants only, one
//     with both, and one with neither, because the effective-grant read is a
//     union of two disjoint sources and the ORDER within a user (direct
//     first, then each group's, oldest first) is part of the answer;
//   - two groups, one with a label and one without, because the label is
//     a coalesce and only a null one proves which arm ran;
//   - an archived user, which the directory hides by default but GetUser
//     still loads.
//
// Grant created_at values are set explicitly and distinctly. Both queries order
// by created_at within a group and neither breaks a tie, so a fixture whose
// grants shared a timestamp would compare two unspecified orderings.
func userBatchFixture(t *testing.T, pool *pgxpool.Pool) []User {
	t.Helper()
	ctx := context.Background()

	var viewerID, editorID string
	for _, r := range []struct {
		name string
		into *string
	}{{"viewer", &viewerID}, {"editor", &editorID}} {
		if err := pool.QueryRow(ctx,
			`insert into role (name, permissions) values ($1, '{"component:read"}') returning id`,
			r.name).Scan(r.into); err != nil {
			t.Fatalf("insert role %s: %v", r.name, err)
		}
	}

	var withLabel, bare string
	if err := pool.QueryRow(ctx,
		`insert into user_group (name, label) values ('alpha', 'Alpha Team') returning id`).Scan(&withLabel); err != nil {
		t.Fatalf("insert group alpha: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`insert into user_group (name) values ('bravo') returning id`).Scan(&bare); err != nil {
		t.Fatalf("insert group bravo: %v", err)
	}

	mint := func(kind string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `insert into users (kind) values ($1) returning id`, kind).Scan(&id); err != nil {
			t.Fatalf("insert %s user: %v", kind, err)
		}
		return id
	}
	human := func(username, email, display string, mustChange bool, avatar bool) string {
		t.Helper()
		id := mint("person")
		var av *string
		if avatar {
			s := "data"
			av = &s
		}
		if _, err := pool.Exec(ctx,
			`insert into person (user_id, username, email, label, must_change_password, avatar, avatar_updated_at)
			 values ($1, $2, $3, $4, $5, $6::text, case when $6::text is null then null else now() end)`,
			id, username, email, display, mustChange, av); err != nil {
			t.Fatalf("insert human %s: %v", username, err)
		}
		return id
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seq := 0
	grantTo := func(col, owner, roleID, scopeKind string, scopeID *string, op string) {
		t.Helper()
		seq++
		if _, err := pool.Exec(ctx,
			fmt.Sprintf(`insert into user_grant (%s, role_id, scope_kind, scope_id, scope_op, created_at) values ($1, $2, $3, $4, $5, $6)`, col),
			owner, roleID, scopeKind, scopeID, op, base.Add(time.Duration(seq)*time.Minute)); err != nil {
			t.Fatalf("insert grant on %s: %v", owner, err)
		}
	}
	join := func(groupID, userID string) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`insert into user_group_member (group_id, user_id) values ($1, $2)`, groupID, userID); err != nil {
			t.Fatalf("join group: %v", err)
		}
	}

	// Group grants first, so an inherited grant is OLDER than the direct grants
	// below it: the ordering rule is "direct first" (group_id nulls first), not
	// "oldest first", and a fixture where the two agree would not tell them apart.
	grantTo("group_id", withLabel, viewerID, "all", nil, "subtree")
	grantTo("group_id", bare, editorID, "location", strptr2("11111111-1111-1111-1111-111111111111"), "self")

	// direct grants only
	direct := human("dana", "dana@example.test", "Dana", false, true)
	grantTo("user_id", direct, viewerID, "system", strptr2("22222222-2222-2222-2222-222222222222"), "subtree")
	grantTo("user_id", direct, editorID, "all", nil, "subtree")

	// inherited only, from both groups (so the per-group ordering is exercised)
	inherited := human("ivan", "", "", true, false)
	join(withLabel, inherited)
	join(bare, inherited)

	// both, plus membership of one group
	both := human("bella", "bella@example.test", "Bella", false, false)
	grantTo("user_id", both, editorID, "component", strptr2("33333333-3333-3333-3333-333333333333"), "subtree_excl_root")
	join(withLabel, both)

	// neither: no grants, no groups
	quiet := human("quinn", "", "Quinn", false, false)

	svc := mint("machine")
	if _, err := pool.Exec(ctx, `insert into machine (user_id, name) values ($1, 'ingest')`, svc); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	grantTo("user_id", svc, viewerID, "all", nil, "subtree")

	nd := mint("machine")
	if _, err := pool.Exec(ctx, `insert into outpost (user_id, name) values ($1, 'edge-1')`, nd); err != nil {
		t.Fatalf("insert outpost: %v", err)
	}
	join(bare, nd)

	archived := human("zed", "", "Zed", false, false)
	if _, err := pool.Exec(ctx, `update users set archived_at = now(), active = false where id = $1`, archived); err != nil {
		t.Fatalf("archive user: %v", err)
	}

	ids := []string{direct, inherited, both, quiet, svc, nd, archived}
	out := make([]User, 0, len(ids))
	for _, id := range ids {
		pr := User{ID: id}
		if err := pool.QueryRow(ctx, `select kind, active, archived_at from users where id = $1`, id).
			Scan(&pr.Kind, &pr.Active, &pr.ArchivedAt); err != nil {
			t.Fatalf("read user %s: %v", id, err)
		}
		out = append(out, pr)
	}
	return out
}

func strptr2(s string) *string { return &s }

// TestBatchUserLoadMatchesSingle is the equivalence gate: loadUsers
// must fill a page exactly as loadUser fills each of its rows, field for
// field and grant for grant, in order.
func TestBatchUserLoadMatchesSingle(t *testing.T) {
	t.Parallel()
	pool := userBatchPool(t)
	ctx := context.Background()
	pg := &PG{pool: pool}

	bases := userBatchFixture(t, pool)

	batched := append([]User(nil), bases...)
	if err := pg.loadUsers(ctx, batched); err != nil {
		t.Fatalf("loadUsers: %v", err)
	}
	for i := range bases {
		want := bases[i]
		if err := pg.loadUser(ctx, &want); err != nil {
			t.Fatalf("loadUser %s: %v", want.ID, err)
		}
		got := batched[i]
		if !reflect.DeepEqual(got, want) {
			t.Errorf("user %s (%s) loaded differently by the two paths\n  batch:  %s\n  single: %s",
				want.ID, want.Kind, describeUser(got), describeUser(want))
		}
	}
}

// TestBatchUserLoadRejectsAProfilelessUser holds the batch to the
// single-row path's failure as well as its success. A user row of a
// profiled kind with no profile row is a broken invariant that loadUser
// raises as an error, and a batch that answered it with a nil profile would turn
// a loud 500 into a directory row missing its username.
func TestBatchUserLoadRejectsAProfilelessUser(t *testing.T) {
	t.Parallel()
	pool := userBatchPool(t)
	ctx := context.Background()
	pg := &PG{pool: pool}

	var id string
	if err := pool.QueryRow(ctx, `insert into users (kind) values ('person') returning id`).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	orphan := []User{{ID: id, Kind: "person", Active: true}}

	single := pg.loadUser(ctx, &User{ID: id, Kind: "person"})
	batch := pg.loadUsers(ctx, orphan)
	if single == nil {
		t.Fatal("loadUser accepted a human with no human row: the oracle no longer refuses this, so the comparison below is vacuous")
	}
	if batch == nil {
		t.Errorf("loadUsers accepted a human with no human row (loadUser refused it with %v): the batch must not soften a broken invariant into a nil profile", single)
	}
}

// describeUser renders the fields the two loaders fill, so a failure names
// the field that differs rather than printing two pointer-laden structs.
func describeUser(pr User) string {
	s := fmt.Sprintf("kind=%s active=%v", pr.Kind, pr.Active)
	switch {
	case pr.Person != nil:
		s += fmt.Sprintf(" human=%+v", *pr.Person)
	case pr.Machine != nil:
		s += fmt.Sprintf(" service=%+v", *pr.Machine)
	case pr.Outpost != nil:
		s += fmt.Sprintf(" outpost=%+v", *pr.Outpost)
	default:
		s += " profile=nil"
	}
	for _, g := range pr.Grants {
		group := "direct"
		if g.GroupID != nil {
			group = "group:" + deref(g.GroupName) + "/" + deref(g.GroupLabel)
		}
		s += fmt.Sprintf(" grant(%s,%s,%s,%s,%s)", g.Role, g.ScopeKind, deref(g.ScopeID), g.ScopeOp, group)
	}
	for _, gr := range pr.Groups {
		s += " member-of:" + gr.Name
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
