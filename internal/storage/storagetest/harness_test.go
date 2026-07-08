package storagetest

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

// TestStartContainerTerminate exercises the harness's container primitive
// against real Docker: a freshly started container is inspectable, and
// terminate removes it. This is the real-implementation integration test that
// closes the capability increment. The shared-container teardown in Main is
// thin glue over the same terminate path proven here, so a leak can only
// return if a consuming package omits its TestMain.
func TestStartContainerTerminate(t *testing.T) {
	if testing.Short() {
		t.Skip("storagetest: skipped under -short (Postgres testcontainer)")
	}
	ctx := context.Background()

	c, dsn, err := startContainer(ctx)
	if err != nil {
		t.Fatalf("startContainer: %v", err)
	}
	// Safety net: force-terminate even if an assertion below fails, so a broken
	// terminate never leaks this test's own container.
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })

	if dsn == "" {
		t.Fatal("startContainer returned an empty admin DSN")
	}
	if _, err := c.Inspect(ctx); err != nil {
		t.Fatalf("container should be inspectable before terminate: %v", err)
	}

	if err := testcontainers.TerminateContainer(c); err != nil {
		t.Fatalf("terminate: %v", err)
	}

	if _, err := c.Inspect(ctx); err == nil {
		t.Fatal("container still inspectable after terminate; expected it to be gone")
	}
}
