package storage_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// secretGateway opens a Gateway wired with a deterministic test KEK provider,
// seeds the reference data (including the official secret_types), and returns
// the gateway plus the raw DSN for at-rest inspection.
func secretGateway(t *testing.T) (storage.Gateway, string) {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	prov := secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(prov))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return gw, dsn
}

// TestSecretRegistrySeed asserts the boot seed installs the official secret_types
// with their per-field shape intact.
func TestSecretRegistrySeed(t *testing.T) {
	gw, _ := secretGateway(t)
	ctx := context.Background()

	st, err := gw.GetSecretType(ctx, "snmp-community")
	if err != nil {
		t.Fatalf("get secret_type: %v", err)
	}
	if !st.Official || len(st.Fields) != 1 {
		t.Fatalf("snmp-community = %+v, want official 1-field", st)
	}
	f := st.Fields[0]
	if f.Name != "community" || !f.Secret || f.Origin != secret.OriginOperator {
		t.Errorf("community field = %+v, want secret operator field", f)
	}
	if _, err := gw.GetSecretType(ctx, "nope"); !errors.Is(err, storage.ErrUnknownSecretType) {
		t.Errorf("unknown secret_type = %v, want ErrUnknownSecretType", err)
	}
}

// TestSecretSealRoundTrip is the merge gate: a secret field survives seal ->
// jsonb -> scan -> unseal against real Postgres, is masked everywhere but the
// reveal, and is never stored in plaintext.
func TestSecretSealRoundTrip(t *testing.T) {
	gw, dsn := secretGateway(t)
	ctx := context.Background()

	created, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "snmp", SecretType: "snmp-community", OwnerKind: "global",
		Fields: map[string]string{"community": "s3cr3t-ro"},
	}, all, true)
	if err != nil {
		t.Fatalf("create secret: %v", err)
	}

	// Masked on the create projection.
	if len(created.Fields) != 1 || created.Fields[0].Value != secret.Masked {
		t.Errorf("created fields = %+v, want masked community", created.Fields)
	}

	// The reveal decrypts to the exact plaintext (the real-crypto path).
	got, err := gw.RevealSecret(ctx, "", created.ID, all, all, true)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if got["community"] != "s3cr3t-ro" {
		t.Errorf("revealed community = %q, want s3cr3t-ro", got["community"])
	}

	// Encrypted at rest: the plaintext is nowhere in the stored value column.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("raw connect: %v", err)
	}
	defer conn.Close(ctx)
	var raw string
	if err := conn.QueryRow(ctx, `select value::text from secret where id = $1`, created.ID).Scan(&raw); err != nil {
		t.Fatalf("read raw value: %v", err)
	}
	if strings.Contains(raw, "s3cr3t-ro") {
		t.Errorf("plaintext leaked into stored value: %s", raw)
	}
}

// TestSecretFieldValidation covers the create gate: an unknown field and a
// missing operator field are both refused before any write.
func TestSecretFieldValidation(t *testing.T) {
	gw, _ := secretGateway(t)
	ctx := context.Background()

	_, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "bad", SecretType: "snmp-community", OwnerKind: "global",
		Fields: map[string]string{"nope": "x"},
	}, all, true)
	if !errors.Is(err, storage.ErrSecretFieldInvalid) {
		t.Errorf("unknown field = %v, want ErrSecretFieldInvalid", err)
	}
	_, err = gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "empty", SecretType: "snmp-community", OwnerKind: "global",
		Fields: map[string]string{},
	}, all, true)
	if !errors.Is(err, storage.ErrSecretFieldInvalid) {
		t.Errorf("missing field = %v, want ErrSecretFieldInvalid", err)
	}
	// A multi-field type stores the non-secret field in the clear, masks the secret one.
	s, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "web", SecretType: "basic-auth", OwnerKind: "global",
		Fields: map[string]string{"username": "admin", "password": "hunter2"},
	}, all, true)
	if err != nil {
		t.Fatalf("create basic-auth: %v", err)
	}
	byName := map[string]storage.ResolvedField{}
	for _, f := range s.Fields {
		byName[f.Name] = f
	}
	if byName["username"].Value != "admin" || byName["username"].Secret {
		t.Errorf("username = %+v, want plaintext non-secret", byName["username"])
	}
	if byName["password"].Value != secret.Masked || !byName["password"].Secret {
		t.Errorf("password = %+v, want masked secret", byName["password"])
	}
}

// TestSecretOwnerScope covers the owner-arc scope gate on create and the
// scope-filtered list (a secret outside the read scope is dropped).
func TestSecretOwnerScope(t *testing.T) {
	gw, _ := secretGateway(t)
	ctx := context.Background()
	// campus is the official type allowed at root; the type is incidental
	// here, only the name (rm) is asserted below.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "rm", LocationType: "campus"}, all); err != nil {
		t.Fatalf("seed location: %v", err)
	}

	// A global secret needs an all create scope.
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "g", SecretType: "snmp-community", OwnerKind: "global",
		Fields: map[string]string{"community": "x"},
	}, scope.Set{}, true); !errors.Is(err, storage.ErrSecretForbidden) {
		t.Errorf("global create without all = %v, want ErrSecretForbidden", err)
	}
	// An unknown owner name is a 422, not a 500.
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "l", SecretType: "snmp-community", OwnerKind: "location", OwnerName: strptr("ghost"),
		Fields: map[string]string{"community": "x"},
	}, all, true); !errors.Is(err, storage.ErrSecretOwnerNotFound) {
		t.Errorf("unknown owner = %v, want ErrSecretOwnerNotFound", err)
	}
	// A location-owned secret lands on the location arc.
	locSec, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "l", SecretType: "snmp-community", OwnerKind: "location", OwnerName: strptr("rm"),
		Fields: map[string]string{"community": "x"},
	}, all, true)
	if err != nil {
		t.Fatalf("create location secret: %v", err)
	}
	if locSec.OwnerKind != "location" || locSec.OwnerName != "rm" {
		t.Errorf("owner = %s/%s, want location/rm", locSec.OwnerKind, locSec.OwnerName)
	}
	// Duplicate name at the same owner is refused.
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "l", SecretType: "snmp-community", OwnerKind: "location", OwnerName: strptr("rm"),
		Fields: map[string]string{"community": "y"},
	}, all, true); !errors.Is(err, storage.ErrSecretExists) {
		t.Errorf("dup owner+name = %v, want ErrSecretExists", err)
	}
	// List is scope-filtered now (not all-scope-only): a scope that does not cover
	// the location's arc sees nothing, while the all scope sees the one secret.
	if got, err := gw.ListSecrets(ctx, scope.Set{IDs: []string{"00000000-0000-0000-0000-000000000000"}}, true); err != nil || len(got) != 0 {
		t.Errorf("out-of-scope list = %d, err %v, want 0", len(got), err)
	}
	all, err := gw.ListSecrets(ctx, all, true)
	if err != nil || len(all) != 1 {
		t.Fatalf("list = %d, err %v, want 1", len(all), err)
	}
}

// TestSecretCascadeResolve is the resolver: a name owned at several tiers
// resolves to the most-specific owner (highest band, then deepest), and the
// shadowed candidates come back too.
func TestSecretCascadeResolve(t *testing.T) {
	gw, _ := secretGateway(t)
	ctx := context.Background()

	// Location tree campus > bldg > room; a system; a component at sys @ room.
	mustLoc(t, gw, "campus", "campus", nil)
	mustLoc(t, gw, "bldg", "building", strptr("campus"))
	mustLoc(t, gw, "room", "room", strptr("bldg"))
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys", SystemType: "meeting-room"}, all); err != nil {
		t.Fatalf("system: %v", err)
	}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "codec-1", SystemName: strptr("sys"), LocationName: strptr("room"),
	}, all)
	if err != nil {
		t.Fatalf("component: %v", err)
	}

	// Same secret name "poll" placed at four tiers; distinct communities so we
	// can tell the winner apart on reveal later.
	mustSecret(t, gw, "poll", "global", nil, "global-val")
	mustSecret(t, gw, "poll", "location", strptr("campus"), "campus-val")
	mustSecret(t, gw, "poll", "location", strptr("room"), "room-val")
	mustSecret(t, gw, "poll", "system", strptr("sys"), "sys-val")
	mustSecret(t, gw, "poll", "component", strptr("codec-1"), "comp-val")

	resolved, err := gw.ResolveSecrets(ctx, comp.ID, all, true)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved) != 5 {
		t.Fatalf("resolved candidates = %d, want 5 (global, 2 loc, sys, comp)", len(resolved))
	}
	var winner *storage.ResolvedSecret
	winners := 0
	for i := range resolved {
		if resolved[i].Winner {
			winners++
			winner = &resolved[i]
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d, want exactly 1", winners)
	}
	if winner.OwnerKind != "component" || winner.OwnerName != "codec-1" {
		t.Errorf("winner = %s/%s, want component/codec-1", winner.OwnerKind, winner.OwnerName)
	}

	// Remove the component-tier secret: the system tier now wins.
	list, _ := gw.ListSecrets(ctx, all, true)
	deleteByOwner(t, gw, list, "component")
	resolved, err = gw.ResolveSecrets(ctx, comp.ID, all, true)
	if err != nil {
		t.Fatalf("resolve 2: %v", err)
	}
	winner = pickWinner(t, resolved)
	if winner.OwnerKind != "system" {
		t.Errorf("winner after comp removed = %s, want system", winner.OwnerKind)
	}

	// Remove the system tier: the deeper location (room) beats campus.
	list, _ = gw.ListSecrets(ctx, all, true)
	deleteByOwner(t, gw, list, "system")
	resolved, err = gw.ResolveSecrets(ctx, comp.ID, all, true)
	if err != nil {
		t.Fatalf("resolve 3: %v", err)
	}
	winner = pickWinner(t, resolved)
	if winner.OwnerKind != "location" || winner.OwnerName != "room" {
		t.Errorf("winner after system removed = %s/%s, want location/room", winner.OwnerKind, winner.OwnerName)
	}

	// A component outside the read scope does not disclose its cascade.
	if _, err := gw.ResolveSecrets(ctx, comp.ID, scope.Set{IDs: []string{"00000000-0000-0000-0000-000000000000"}}, true); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Errorf("out-of-scope resolve = %v, want ErrComponentNotFound", err)
	}
}

func mustLoc(t *testing.T, gw storage.Gateway, name, typ string, parent *string) {
	t.Helper()
	if _, err := gw.CreateLocation(context.Background(), "", storage.LocationSpec{Name: name, LocationType: typ, ParentName: parent}, all); err != nil {
		t.Fatalf("location %s: %v", name, err)
	}
}

func mustSecret(t *testing.T, gw storage.Gateway, name, ownerKind string, ownerName *string, community string) {
	t.Helper()
	if _, err := gw.CreateSecret(context.Background(), "", storage.SecretSpec{
		Name: name, SecretType: "snmp-community", OwnerKind: ownerKind, OwnerName: ownerName,
		Fields: map[string]string{"community": community},
	}, all, true); err != nil {
		t.Fatalf("secret %s@%s: %v", name, ownerKind, err)
	}
}

func deleteByOwner(t *testing.T, gw storage.Gateway, list []storage.Secret, ownerKind string) {
	t.Helper()
	for _, s := range list {
		if s.OwnerKind == ownerKind {
			if err := gw.DeleteSecret(context.Background(), "", s.ID, all, all, true); err != nil {
				t.Fatalf("delete %s: %v", s.ID, err)
			}
		}
	}
}

func pickWinner(t *testing.T, resolved []storage.ResolvedSecret) *storage.ResolvedSecret {
	t.Helper()
	for i := range resolved {
		if resolved[i].Winner {
			return &resolved[i]
		}
	}
	t.Fatalf("no winner in %d resolved", len(resolved))
	return nil
}
