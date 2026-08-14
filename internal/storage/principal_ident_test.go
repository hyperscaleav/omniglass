package storage_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// What names a principal is now the gateway's answer and not the database's
// (#564). These are the three things that has to be true: the order is what it
// was, the two shapes the gateway renders agree with each other, and the stored
// function is actually gone rather than merely unused.

// TestPrincipalIdentPrefersTheUsername pins the order in the one place it can
// be checked without a database at all: a human's username outranks a service
// account's name, which outranks a node's name, and a principal with none of
// the three reads as empty rather than as something invented.
func TestPrincipalIdentPrefersTheUsername(t *testing.T) {
	for _, tc := range []struct {
		name     string
		username *string
		service  *string
		node     *string
		want     string
	}{
		{"a human", strp("jordan"), nil, nil, "jordan"},
		{"a service account", nil, strp("ingest-bot"), nil, "ingest-bot"},
		{"a node", nil, nil, strp("edge-1"), "edge-1"},
		{"none of them, which is a purged principal", nil, nil, nil, ""},
		{"a human that somehow also has a service row", strp("jordan"), strp("ingest-bot"), nil, "jordan"},
		{"a service row that somehow also has a node row", nil, strp("ingest-bot"), strp("edge-1"), "ingest-bot"},
		// Absence is NULL and nothing else, which is what the bound coalesce
		// means by it. All three columns are NOT NULL, so this state cannot
		// exist; it is pinned because treating "" as absent here and not there
		// is a disagreement between two shapes this file says agree.
		{"an empty username is present, not absent, exactly as coalesce reads it", strp(""), strp("ingest-bot"), nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := storage.ExportPrincipalIdent(tc.username, tc.service, tc.node); got != tc.want {
				t.Errorf("principalIdent(%v, %v, %v) = %q, want %q",
					deref(tc.username), deref(tc.service), deref(tc.node), got, tc.want)
			}
		})
	}
}

// TestPrincipalIdentAgreesWithTheExpressionItBinds is the invariant that makes
// two shapes of one policy safe.
//
// The read paths project both sources and resolve them in Go; the audit insert
// binds the same order as one expression, because resolving it in Go there
// would cost a round trip inside the caller's transaction. Two readers of one
// fact drift unless something compares them, so this drives every principal
// kind through both and fails on the first disagreement.
func TestPrincipalIdentAgreesWithTheExpressionItBinds(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// Each case carries the answer it expects as well as the agreement it
	// asserts. An agreement test alone cannot fail when BOTH shapes are wrong the
	// same way, which is exactly what naming the wrong column in
	// principalIdentSources would do, so the expected value is stated here rather
	// than left to the callers' tests to imply.
	//
	// A node's answer is its own name (#738): it is the third profiled kind and
	// the third source, so every kind the schema can produce resolves to
	// something an operator recognises and only a purged principal reads empty.
	for _, tc := range []struct {
		kind string
		id   string
		want string
	}{
		{"human", insertPrincipalOfKind(t, conn, "human"), "jordan-ops"},
		{"service", insertPrincipalOfKind(t, conn, "service"), "ingest-bot"},
		{"node", insertPrincipalOfKind(t, conn, "node"), "edge-1"},
		{"unknown", "00000000-0000-0000-0000-000000000000", ""},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			// The sub-select shape: projected, then folded in Go.
			var subUser, subSvc, subNode *string
			if err := conn.QueryRow(ctx,
				`select `+storage.ExportPrincipalIdentCols("$1::uuid"), tc.id).Scan(&subUser, &subSvc, &subNode); err != nil {
				t.Fatalf("project the sub-select sources: %v", err)
			}
			// The same shape folded into one bound expression, which is what a
			// write binds where Go cannot reach.
			var bound *string
			if err := conn.QueryRow(ctx,
				`select `+storage.ExportPrincipalIdentSQL("$1::uuid"), tc.id).Scan(&bound); err != nil {
				t.Fatalf("bind the expression: %v", err)
			}
			// The JOIN shape, which is a different PLAN of the same policy and
			// drifts from it exactly as readily: projected, and folded in SQL for
			// the ORDER BY position.
			var joinUser, joinSvc, joinNode, joinFold *string
			if err := conn.QueryRow(ctx,
				`select `+storage.ExportPrincipalIdentJoinedCols("pi")+`, `+storage.ExportPrincipalIdentJoinedSQL("pi")+
					` from principal p`+storage.ExportPrincipalIdentJoins("pi", "p.id")+` where p.id = $1::uuid`,
				tc.id).Scan(&joinUser, &joinSvc, &joinNode, &joinFold); err != nil && tc.kind != "unknown" {
				t.Fatalf("project the joined sources: %v", err)
			}

			inGo := storage.ExportPrincipalIdent(subUser, subSvc, subNode)
			for _, other := range []struct {
				shape string
				got   string
			}{
				{"the expression it binds", orEmpty(bound)},
				{"the join it projects", storage.ExportPrincipalIdent(joinUser, joinSvc, joinNode)},
				{"the join it sorts on", orEmpty(joinFold)},
			} {
				// The unknown id matches no principal row at all, so the join
				// shape returns no ROW rather than a null one; the sub-select
				// shape is what resolves an id with nothing behind it, and it is
				// the shape the audit write uses for exactly that reason.
				if tc.kind == "unknown" && other.shape != "the expression it binds" {
					continue
				}
				if inGo != other.got {
					t.Errorf("the gateway resolves a %s principal to %q in Go and %q in %s: "+
						"the shapes of one policy have drifted", tc.kind, inGo, other.got, other.shape)
				}
			}
			if inGo != tc.want {
				t.Errorf("a %s principal resolves to %q, want %q: the sources name the wrong column, "+
					"and they name it in every shape so the agreements above cannot see it", tc.kind, inGo, tc.want)
			}
		})
	}
}

// TestEverySurfaceNamesTheKindThatActed is #738's acceptance, and it drives all
// three profiled kinds through all three surfaces rather than the node alone.
//
// The invariant test above proves the SHAPES agree and what they agree on. It
// cannot prove that the surfaces reading those shapes hand the answer back:
// every one of them projects the sources and folds them in Go, so a fold reading
// one column fewer than its query returned is a defect only a surface can show.
// A node acting is the reason to write it (it read blank everywhere before this
// slice), and a human and a service account ride beside it because a surface
// that named nobody would otherwise pass on the node row alone.
func TestEverySurfaceNamesTheKindThatActed(t *testing.T) {
	gw, dsn := secretGateway(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	kinds := []struct {
		kind string
		want string
	}{
		{"human", "jordan-ops"},
		{"service", "ingest-bot"},
		{"node", "edge-1"},
	}
	ids := make(map[string]string, len(kinds))
	for _, k := range kinds {
		ids[k.kind] = insertPrincipalOfKind(t, conn, k.kind)
	}

	// One group to roster all three in; each kind gets its own component and
	// alarm below, since the one-open invariant is per (component, dedup_key).
	group, err := gw.CreateGroup(ctx, "", storage.GroupSpec{Name: "ident-roster"}, all)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	for _, k := range kinds {
		t.Run(k.kind, func(t *testing.T) {
			id := ids[k.kind]

			// The audit trail, both halves at once: WriteAuthEvent binds the
			// sub-select shape to denormalize the actor at write time, and
			// ListAuditLog resolves it live through the join shape.
			if err := gw.WriteAuthEvent(ctx, id, "login"); err != nil {
				t.Fatalf("write the auth event: %v", err)
			}
			var snapshot string
			if err := conn.QueryRow(ctx,
				`select coalesce(actor_username, '') from audit_log where actor_principal_id = $1::uuid order by ts desc limit 1`,
				id).Scan(&snapshot); err != nil {
				t.Fatalf("read the denormalized actor: %v", err)
			}
			if snapshot != k.want {
				t.Errorf("the audit write denormalized %q, want %q: the trail survives a purge on this column alone", snapshot, k.want)
			}
			entries, err := gw.ListAuditLog(ctx, storage.AuditFilter{Verb: "login"})
			if err != nil {
				t.Fatalf("list the audit log: %v", err)
			}
			var read string
			for _, e := range entries {
				if e.ActorID == id {
					read = e.ActorName
				}
			}
			if read != k.want {
				t.Errorf("the audit read named the actor %q, want %q", read, k.want)
			}

			// The alarm read, the shape a join cannot reach (an UPDATE ...
			// RETURNING).
			comp := "codec-" + k.kind
			if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: comp}, all, all, all, all); err != nil {
				t.Fatalf("create the component: %v", err)
			}
			raised, err := gw.RaiseAlarm(ctx, "", comp, storage.AlarmSpec{
				Severity: "critical", Message: "no signal", DedupKey: "no-signal",
			})
			if err != nil {
				t.Fatalf("raise the alarm: %v", err)
			}
			acked, err := gw.AcknowledgeAlarm(ctx, id, comp, raised.ID)
			if err != nil {
				t.Fatalf("acknowledge: %v", err)
			}
			if acked.AcknowledgedBy != k.want {
				t.Errorf("the alarm read named the acknowledger %q, want %q", acked.AcknowledgedBy, k.want)
			}

			// The group roster, the join shape over many rows.
			if err := gw.AddGroupMember(ctx, "", group.ID, id, all); err != nil {
				t.Fatalf("add the member: %v", err)
			}
			members, err := gw.ListGroupMembers(ctx, group.ID, all)
			if err != nil {
				t.Fatalf("list the members: %v", err)
			}
			var roster string
			for _, m := range members {
				if m.PrincipalID == id {
					roster = m.Name
				}
			}
			if roster != k.want {
				t.Errorf("the group roster named the member %q, want %q", roster, k.want)
			}
		})
	}
}

// orEmpty reads a SQL NULL as the empty string, which is what the Go resolution
// returns for the same absence, so the two are compared on the same footing.
func orEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// insertPrincipalOfKind creates a principal of the given kind with its profile
// row, and returns its id. The profile tables are written directly because two
// of the three kinds have no gateway create path at all.
func insertPrincipalOfKind(t *testing.T, conn *pgx.Conn, kind string) string {
	t.Helper()
	ctx := context.Background()
	var pid string
	if err := conn.QueryRow(ctx,
		`insert into principal (kind) values ($1) returning id`, kind).Scan(&pid); err != nil {
		t.Fatalf("insert %s principal: %v", kind, err)
	}
	var profile string
	switch kind {
	case "human":
		profile = `insert into human (principal_id, username) values ($1, 'jordan-ops')`
	case "service":
		profile = `insert into service (principal_id, name) values ($1, 'ingest-bot')`
	case "node":
		profile = `insert into node (principal_id, name) values ($1, 'edge-1')`
	}
	if _, err := conn.Exec(ctx, profile, pid); err != nil {
		t.Fatalf("insert %s profile: %v", kind, err)
	}
	return pid
}

// TestPrincipalLabelIsNotAStoredFunction is the half of #564 that is about
// where logic lives rather than about what it returns. A gateway that resolved
// the identifier in Go while the stored function sat unused in the schema would
// have moved nothing: the exception to "logic lives in Go, never the database"
// is the OBJECT, so the object has to be gone.
func TestPrincipalLabelIsNotAStoredFunction(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	var n int
	if err := conn.QueryRow(ctx,
		`select count(*) from pg_proc where proname = 'principal_label'`).Scan(&n); err != nil {
		t.Fatalf("read pg_proc: %v", err)
	}
	if n != 0 {
		t.Errorf("the schema still defines principal_label(): %d overload(s). "+
			"Resolving the identifier in Go moves nothing while the stored function is still there to be called", n)
	}
}
