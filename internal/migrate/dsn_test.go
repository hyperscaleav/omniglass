package migrate_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
)

// TestAMalformedDSNNeverEchoesItsPassword: every direct-database command
// (server, migrate, bootstrap, token, set-password, seed-dev) runs the
// migrations first, and a DSN whose password carries a reserved character
// does not parse; the parser's error quoted the whole DSN, so a server in a
// crash loop wrote the database password to its logs on every restart (#979's
// review). It fails before any connection, so no database is needed.
func TestAMalformedDSNNeverEchoesItsPassword(t *testing.T) {
	err := migrate.Run("postgres://og:s3c/ret@127.0.0.1:1/og")
	if err == nil {
		t.Fatal("an unparseable DSN migrated")
	}
	for _, piece := range []string{"s3c", "ret@"} {
		if strings.Contains(err.Error(), piece) {
			t.Errorf("the migrate error %q carries %q, a piece of the password", err, piece)
		}
	}
	if !strings.Contains(err.Error(), "DSN") {
		t.Errorf("the migrate error %q does not name the DSN", err)
	}
}
