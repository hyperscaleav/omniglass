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
