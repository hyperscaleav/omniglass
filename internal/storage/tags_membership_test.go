package storage_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/secret"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The cascade's system band is seeded from MEMBERSHIP, not from a pointer on the
// component. These are the cases a single pointer could not answer, and they are
// written first because the failure mode here is silent: deleting or mis-seeding
// the sys_chain leaves valid SQL that simply returns fewer rows, so a system-owned
// tag stops reaching its components and the resolution blade shows the location
// winner as though the system band never had a candidate. Nothing errors.

// A component that is a member of a system inherits that system's tags, with no
// pointer involved anywhere.
func TestMembershipSeedsTheSystemBand(t *testing.T) {
	ctx := context.Background()
	f := newResolveFixture(t, ctx)

	if err := f.gw.AddMember(ctx, "", "room-a", "roamer", f.all); err != nil {
		t.Fatalf("add member: %v", err)
	}
	mustBind(t, f.gw, "environment", "system", strptr("room-a"), "prod")

	got := f.effective(t, ctx, "roamer", "")
	if got["environment"] != "prod" {
		t.Fatalf("environment = %q, want prod: a member inherits its system's tags", got["environment"])
	}
}

// The resolution takes the system to resolve AGAINST. This is the case the old
// model could not express at all: one component, two systems, the same key bound
// differently on each, and a different answer for each.
func TestResolutionIsPerMembership(t *testing.T) {
	ctx := context.Background()
	f := newResolveFixture(t, ctx)

	for _, s := range []string{"room-a", "room-b"} {
		if err := f.gw.AddMember(ctx, "", s, "roamer", f.all); err != nil {
			t.Fatalf("add member %s: %v", s, err)
		}
	}
	mustBind(t, f.gw, "environment", "system", strptr("room-a"), "prod")
	mustBind(t, f.gw, "environment", "system", strptr("room-b"), "lab")

	if got := f.effective(t, ctx, "roamer", "room-a")["environment"]; got != "prod" {
		t.Errorf("resolved for room-a = %q, want prod", got)
	}
	if got := f.effective(t, ctx, "roamer", "room-b")["environment"]; got != "lab" {
		t.Errorf("resolved for room-b = %q, want lab", got)
	}
}

// Asked with no system in hand, resolution falls back to the component's PRIMARY
// membership. That is what makes the default a convenience for context-free
// callers rather than a resolution rule, and moving the default moves the answer.
func TestContextFreeResolutionFollowsThePrimary(t *testing.T) {
	ctx := context.Background()
	f := newResolveFixture(t, ctx)

	for _, s := range []string{"room-a", "room-b"} {
		if err := f.gw.AddMember(ctx, "", s, "roamer", f.all); err != nil {
			t.Fatalf("add member %s: %v", s, err)
		}
	}
	mustBind(t, f.gw, "environment", "system", strptr("room-a"), "prod")
	mustBind(t, f.gw, "environment", "system", strptr("room-b"), "lab")

	// room-a was the first membership, so it holds the default.
	if got := f.effective(t, ctx, "roamer", "")["environment"]; got != "prod" {
		t.Fatalf("context-free = %q, want prod (the primary)", got)
	}
	if err := f.gw.SetPrimaryMember(ctx, "", "room-b", "roamer", f.all); err != nil {
		t.Fatalf("set primary: %v", err)
	}
	if got := f.effective(t, ctx, "roamer", "")["environment"]; got != "lab" {
		t.Errorf("context-free after moving the default = %q, want lab", got)
	}
}

// A component in no system resolves the location and global bands and nothing
// else, without erroring. Most of an estate looks like this before anyone models
// it, so it must not be a special case.
func TestNoMembershipResolvesTheOtherBands(t *testing.T) {
	ctx := context.Background()
	f := newResolveFixture(t, ctx)

	mustBind(t, f.gw, "environment", "system", strptr("room-a"), "prod")
	mustBind(t, f.gw, "environment", "location", strptr("room"), "room-level")

	got := f.effective(t, ctx, "roamer", "")
	if got["environment"] != "room-level" {
		t.Errorf("environment = %q, want room-level: with no membership the location band wins",
			got["environment"])
	}
}

// Resolving against a system the component is NOT in contributes nothing from that
// system. Membership is what grants the inheritance, so naming a system the
// component has no binding to must not borrow its configuration.
func TestResolvingAgainstAStrangerSystemInheritsNothing(t *testing.T) {
	ctx := context.Background()
	f := newResolveFixture(t, ctx)

	if err := f.gw.AddMember(ctx, "", "room-a", "roamer", f.all); err != nil {
		t.Fatalf("add member: %v", err)
	}
	mustBind(t, f.gw, "environment", "system", strptr("room-b"), "lab")

	if got := f.effective(t, ctx, "roamer", "room-b")["environment"]; got == "lab" {
		t.Error("a system the component is not a member of must not lend it configuration")
	}
}

// A secret is device-facing: it authenticates a session with the device itself. A
// shared component has one password, and the room it happens to serve is the wrong
// owner for it, so the secret cascade carries no system band at all. This is an
// ownership decision, not a tiebreak.
func TestSecretsDoNotInheritFromASystem(t *testing.T) {
	ctx := context.Background()
	f := newResolveFixture(t, ctx)

	if err := f.gw.AddMember(ctx, "", "room-a", "roamer", f.all); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := f.setSecret(ctx, "system", "room-a", "admin-password", "hunter2"); err != nil {
		t.Fatalf("set system secret: %v", err)
	}
	names, err := f.gw.ResolveSecrets(ctx, f.compID(t, ctx, "roamer"), f.all, true)
	if err != nil {
		t.Fatalf("resolve secrets: %v", err)
	}
	for _, s := range names {
		if s.Name == "admin-password" {
			t.Fatalf("a system-owned secret reached a component: credentials belong to the device, "+
				"not to the room it serves (resolved %+v)", s)
		}
	}
}

// resolveFixture is a room holding two systems and one component that belongs to
// NEITHER until a test says so. No pointer is set anywhere: membership is the only
// way into the system band now, so the fixture must not smuggle one in.
type resolveFixture struct {
	gw  *storage.PG
	all scope.Set
}

func (f *resolveFixture) compID(t *testing.T, ctx context.Context, name string) string {
	t.Helper()
	c, err := f.gw.GetComponent(ctx, name, f.all)
	if err != nil {
		t.Fatalf("get component %s: %v", name, err)
	}
	return c.ID
}

// effective resolves the winning tag per key, optionally against a named system.
func (f *resolveFixture) effective(t *testing.T, ctx context.Context, comp, forSystem string) map[string]string {
	t.Helper()
	rows, err := f.gw.ResolveTags(ctx, f.compID(t, ctx, comp), forSystem, f.all)
	if err != nil {
		t.Fatalf("resolve tags: %v", err)
	}
	out := map[string]string{}
	for _, r := range rows {
		if r.Winner {
			out[r.Key] = r.Value
		}
	}
	return out
}

func (f *resolveFixture) setSecret(ctx context.Context, ownerKind, ownerName, name, value string) error {
	owner := ownerName
	_, err := f.gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: name, SecretType: "basic-auth", OwnerKind: ownerKind, OwnerName: &owner,
		Fields: map[string]string{"username": "admin", "password": value},
	}, f.all, true)
	return err
}

func newResolveFixture(t *testing.T, ctx context.Context) *resolveFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t),
		storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f := &resolveFixture{gw: gw, all: scope.Set{All: true}}
	mustTag(t, gw, "environment", nil, true)

	mustLoc(t, gw, "campus", "campus", nil)
	mustLoc(t, gw, "room", "room", strptr("campus"))
	for _, s := range []string{"room-a", "room-b"} {
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
			Name: s, LocationName: strptr("room")}, f.all); err != nil {
			t.Fatalf("system %s: %v", s, err)
		}
	}
	// Placed in the room, in no system. Membership is the only route into the
	// system band, so the fixture deliberately gives it none to start with.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "roamer", LocationName: strptr("room")}, f.all); err != nil {
		t.Fatalf("component: %v", err)
	}
	return f
}
