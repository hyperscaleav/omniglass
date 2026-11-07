package storagetest

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
)

// A derived template is a second layer of the template idea. The base template
// holds the migrated schema, so a test never replays a migration. A package
// whose tests all start from the same populated fixture (a boot seed, a seeded
// fleet) would still pay for that fixture once per test, and in internal/devseed
// that was the dominant cost of the package (#1537). A derived template is the
// base template plus one build step, run once per test binary, sealed, and
// cloned per test exactly as the base is. A test that only READS the seeded
// state takes a clone; a test that verifies the seeding itself still seeds.

// derivedKeyPattern holds a key to a plain lowercase identifier: it becomes part
// of a database name, so it is validated rather than quoted into something
// surprising. The length cap keeps the whole name inside Postgres's 63-byte
// identifier limit after the tag and prefix.
var derivedKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,23}$`)

// derivedNameFor is the database name of key's derived template, a function of
// the tag for the same reason [templateNameFor] is. The prefix differs from the
// base template's so "one template per process" stays a checkable fact.
func derivedNameFor(tag, key string) string { return "og_derived_" + tag + "_" + key }

// derivedTemplate is one key's build state. Its own mutex means two keys build
// concurrently, while concurrent callers of one key build it once.
type derivedTemplate struct {
	mu    sync.Mutex
	ready bool
}

var (
	derivedMu  sync.Mutex
	derivedSet = map[string]*derivedTemplate{}
)

func derivedFor(key string) *derivedTemplate {
	derivedMu.Lock()
	defer derivedMu.Unlock()
	d, ok := derivedSet[key]
	if !ok {
		d = &derivedTemplate{}
		derivedSet[key] = d
	}
	return d
}

// NewDSNFrom returns the DSN of a fresh, isolated database that is a copy of
// the derived template for key, building that template with build the first
// time any test in the binary asks for it. build receives the DSN of the
// not-yet-sealed template (the migrated schema, empty) and leaves it holding
// the fixture; it must close every connection it opens.
//
// Everything [NewDSN] promises holds: skipped under -short, refused outside
// [Main], dropped WITH (FORCE) at cleanup. As there, only success is cached: a
// failing build fails the test that hit it, and the next test retries from a
// clean database.
func NewDSNFrom(t testing.TB, key string, build func(ctx context.Context, dsn string) error) string {
	t.Helper()
	if err := reclaimGuard(); err != nil {
		t.Fatal(err)
	}
	if testing.Short() {
		t.Skip("storage: skipped under -short (Postgres testcontainer)")
	}
	if err := ensureDerived(key, build); err != nil {
		t.Fatal(err)
	}
	return cloneDatabase(t, derivedNameFor(procTag, key))
}

// ensureDerived builds key's derived template once. It is the derived
// counterpart of [ensureTemplate], and like it caches only success.
func ensureDerived(key string, build func(ctx context.Context, dsn string) error) error {
	if !derivedKeyPattern.MatchString(key) {
		return fmt.Errorf("storagetest: derived template key %q must match %s", key, derivedKeyPattern)
	}
	if err := ensureTemplate(); err != nil {
		return err
	}
	d := derivedFor(key)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ready {
		return nil
	}
	if err := buildDerived(context.Background(), derivedNameFor(procTag, key), build); err != nil {
		return fmt.Errorf("build derived template %q: %w", key, err)
	}
	d.ready = true
	return nil
}

// buildDerived copies the base template, runs build against the copy, and seals
// it. A copy left by an earlier failed attempt is dropped first, so a retry
// never builds on top of a half-built fixture.
func buildDerived(ctx context.Context, name string, build func(ctx context.Context, dsn string) error) error {
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return fmt.Errorf("admin connect: %w", err)
	}
	err = dropTemplate(ctx, admin, name)
	if err == nil {
		_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+
			" TEMPLATE "+pgx.Identifier{templateName}.Sanitize())
	}
	if cerr := admin.Close(ctx); err == nil && cerr != nil {
		err = fmt.Errorf("close admin connection: %w", cerr)
	}
	if err != nil {
		return err
	}
	if err := build(ctx, withDBName(adminDSN, name)); err != nil {
		return err
	}
	return sealDatabase(ctx, name)
}

// derivedNames lists the derived templates this binary built, for
// [reclaimTemplate].
func derivedNames() []string {
	derivedMu.Lock()
	defer derivedMu.Unlock()
	var out []string
	// A failed attempt leaves a half-built database too, so this is every key
	// that was asked for, not only the ones that finished.
	for key := range derivedSet {
		out = append(out, derivedNameFor(procTag, key))
	}
	return out
}
