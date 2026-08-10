package storagetest

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
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

// TestStartFailureTerminatesTheContainer pins the half of a start failure that
// is easy to miss. testcontainers hands the container back ALONGSIDE the error
// when the create succeeded and the start or the wait strategy did not, so the
// caller can read its logs; a caller that only checks the error drops a live
// Postgres on the floor. That was survivable while a failed start was cached
// and never retried. It is not survivable now that the next test retries, since
// each attempt would leak one more.
//
// The failure is injected rather than provoked, because Docker cannot be asked
// for a half-started container on demand. The container is real, and the
// property asserted is that startContainer reclaimed it.
func TestStartFailureTerminatesTheContainer(t *testing.T) {
	if testing.Short() {
		t.Skip("storagetest: skipped under -short (Postgres testcontainer)")
	}
	ctx := context.Background()

	victim, _, err := startContainer(ctx)
	if err != nil {
		t.Fatalf("startContainer: %v", err)
	}
	// Safety net: this test's own container never outlives it, whatever the
	// assertions below do.
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(victim) })

	runPostgres = func(context.Context) (*tcpostgres.PostgresContainer, error) {
		return victim, errors.New("wait strategy timed out")
	}
	t.Cleanup(func() { runPostgres = runPostgresContainer })

	c, dsn, err := startContainer(ctx)
	if err == nil {
		t.Fatal("startContainer returned a nil error for a failed start")
	}
	if c != nil {
		t.Error("startContainer returned a container alongside its error; a caller would store a dead one")
	}
	if dsn != "" {
		t.Errorf("startContainer returned admin DSN %q alongside its error, want empty", dsn)
	}
	if _, err := victim.Inspect(ctx); err == nil {
		t.Fatal("the container from the failed start is still inspectable; a failed start leaks it, once per retry")
	}
}

// TestContainerStartFailureIsNotCached pins the blast radius of a transient
// container start, the same property TestTemplateBuildFailureIsNotCached pins
// one layer up. Starting the container is per-binary work, so a sync.Once over
// it stored the error too: one flap on a box where the container is known to
// flap failed every remaining test in the package with the same replayed
// message, at 0.00s each, burying whatever actually happened. Only success may
// be cached.
//
// The retry is served the container that is already running rather than a
// second Postgres, so the test pins the state machine without paying for, or
// risking a leak of, another container. That the real start path reclaims and
// terminates correctly is proven against Docker by the two tests above.
//
// This mutates harness state, so it restores it and must not run alongside a
// t.Parallel test.
func TestContainerStartFailureIsNotCached(t *testing.T) {
	if testing.Short() {
		t.Skip("storagetest: skipped under -short (Postgres testcontainer)")
	}
	if config.Get("OMNIGLASS_TEST_ADMIN_DSN") != "" {
		t.Skip("storagetest: the escape hatch returns before the container start this pins")
	}
	if err := ensureContainer(); err != nil {
		t.Fatalf("ensureContainer: %v", err)
	}

	live, liveDSN := ctr, adminDSN
	t.Cleanup(func() {
		runPostgres = runPostgresContainer
		ctr, adminDSN, containerReady = live, liveDSN, true
	})

	containerReady = false
	runPostgres = func(context.Context) (*tcpostgres.PostgresContainer, error) {
		return nil, errors.New("transient docker hiccup")
	}

	if err := ensureContainer(); err == nil {
		t.Fatal("ensureContainer returned nil when the start failed")
	}
	if containerReady {
		t.Fatal("a failed start marked the container ready")
	}
	if ctr != live {
		t.Fatal("a failed start replaced the container Main reclaims; the live one would leak")
	}

	// The property under test: the next call retries rather than replaying a
	// stored error.
	runPostgres = func(context.Context) (*tcpostgres.PostgresContainer, error) { return live, nil }
	if err := ensureContainer(); err != nil {
		t.Fatalf("ensureContainer did not recover after a transient start failure: %v", err)
	}
	if !containerReady {
		t.Fatal("a successful start did not mark the container ready")
	}
	if ctr != live {
		t.Fatal("the recovered container is not the one Main will reclaim")
	}
	if adminDSN != liveDSN {
		t.Fatalf("admin DSN after recovery = %q, want %q", adminDSN, liveDSN)
	}
}
