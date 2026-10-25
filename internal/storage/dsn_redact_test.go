package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestAMalformedDSNNeverEchoesItsPasswordFromTheGateway: pgx masks the
// password in the DSN it quotes, but its detail still quotes the piece of the
// password the parser misread (`invalid port ":s3c"` for the password
// `s3c/ret`). Every command migrates first, which refuses the DSN before this,
// but the Gateway is the database's other front door and holds the same line
// (#979's review). It fails before any connection, so no database is needed.
func TestAMalformedDSNNeverEchoesItsPasswordFromTheGateway(t *testing.T) {
	_, err := storage.NewPG(context.Background(), "postgres://og:s3c/ret@127.0.0.1:1/og")
	if err == nil {
		t.Fatal("an unparseable DSN opened a gateway")
	}
	for _, piece := range []string{"s3c", "ret@"} {
		if strings.Contains(err.Error(), piece) {
			t.Errorf("the gateway error %q carries %q, a piece of the password", err, piece)
		}
	}
}

// TestADSNQueryPasswordNeverReachesTheGatewaysError: a valid URL-form DSN may
// carry its password as a query parameter, which pgx's own redaction does not
// mask; a parameter pgx refuses (one lib/pq, and so the migrations, accepts)
// had pgx quote the whole DSN, password and all (#979's review). The error
// keeps pgx's reason and drops the DSN.
func TestADSNQueryPasswordNeverReachesTheGatewaysError(t *testing.T) {
	_, err := storage.NewPG(context.Background(),
		"postgres://postgres@127.0.0.1:1/postgres?password=QSECRETpw&sslmode=disable&connect_timeout=-1")
	if err == nil {
		t.Fatal("a DSN with a negative connect_timeout opened a gateway")
	}
	if strings.Contains(err.Error(), "QSECRETpw") {
		t.Errorf("the gateway error %q carries the DSN's password", err)
	}
	if !strings.Contains(err.Error(), "connect_timeout") {
		t.Errorf("the gateway error %q dropped pgx's reason along with the DSN", err)
	}
}
