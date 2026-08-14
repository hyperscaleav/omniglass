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
// account's name, and a principal with neither reads as empty rather than as
// something invented.
func TestPrincipalIdentPrefersTheUsername(t *testing.T) {
	for _, tc := range []struct {
		name     string
		username *string
		service  *string
		want     string
	}{
		{"a human", strp("jordan"), nil, "jordan"},
		{"a service account", nil, strp("ingest-bot"), "ingest-bot"},
		{"neither, which is a node or a purged principal", nil, nil, ""},
		{"a human that somehow also has a service row", strp("jordan"), strp("ingest-bot"), "jordan"},
		{"an empty username does not win over a name", strp(""), strp("ingest-bot"), "ingest-bot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := storage.ExportPrincipalIdent(tc.username, tc.service); got != tc.want {
				t.Errorf("principalIdent(%v, %v) = %q, want %q", deref(tc.username), deref(tc.service), got, tc.want)
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
	// A node's answer is the empty string on purpose: it is the kind the
	// resolution does not read, exactly as principal_label never read it.
	for _, tc := range []struct {
		kind string
		id   string
		want string
	}{
		{"human", insertPrincipalOfKind(t, conn, "human"), "jordan-ops"},
		{"service", insertPrincipalOfKind(t, conn, "service"), "ingest-bot"},
		{"node", insertPrincipalOfKind(t, conn, "node"), ""},
		{"unknown", "00000000-0000-0000-0000-000000000000", ""},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			var username, serviceName *string
			if err := conn.QueryRow(ctx,
				`select `+storage.ExportPrincipalIdentCols("$1::uuid"), tc.id).Scan(&username, &serviceName); err != nil {
				t.Fatalf("project the sources: %v", err)
			}
			var bound *string
			if err := conn.QueryRow(ctx,
				`select `+storage.ExportPrincipalIdentSQL("$1::uuid"), tc.id).Scan(&bound); err != nil {
				t.Fatalf("bind the expression: %v", err)
			}
			inGo := storage.ExportPrincipalIdent(username, serviceName)
			if inGo != orEmpty(bound) {
				t.Errorf("the gateway resolves a %s principal to %q in Go and %q in the expression it binds: "+
					"the two shapes of one policy have drifted", tc.kind, inGo, orEmpty(bound))
			}
			if inGo != tc.want {
				t.Errorf("a %s principal resolves to %q, want %q: the sources name the wrong column, "+
					"and they name it in both shapes so the agreement above cannot see it", tc.kind, inGo, tc.want)
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
