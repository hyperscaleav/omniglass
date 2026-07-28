package erd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// erdPage is the docs page whose generated D2 region cmd/erdgen writes.
const erdPage = "../../docs/src/content/docs/architecture/data-model.md"

// TestERDMatchesSchema regenerates the ERD from the live schema and asserts it
// equals the D2 committed on the data-model page. It is the drift gate for the
// generated diagram: a schema change that forgets `make gen` fails here, the same
// way the gofmt and generated-client checks catch stale artifacts.
func TestERDMatchesSchema(t *testing.T) {
	dsn := storagetest.NewDSN(t) // skips under -short
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	schema, err := Introspect(ctx, conn)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	want, err := Render(schema, Subsystems)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	got, err := committedD2(erdPage)
	if err != nil {
		t.Fatalf("read committed ERD: %v", err)
	}
	if got != want {
		t.Errorf("committed ERD is stale; run `make gen`.\n--- committed ---\n%s\n--- regenerated ---\n%s", got, want)
	}
}

// committedD2 extracts the fenced d2 block from the data-model page, the same
// content cmd/erdgen writes between the markers.
func committedD2(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(raw)
	const open = "```d2\n"
	i := strings.Index(content, open)
	if i < 0 {
		return "", fmt.Errorf("%s: no d2 block", path)
	}
	rest := content[i+len(open):]
	j := strings.Index(rest, "\n```")
	if j < 0 {
		return "", fmt.Errorf("%s: unterminated d2 block", path)
	}
	return rest[:j+1], nil // keep the trailing newline to match Render
}
