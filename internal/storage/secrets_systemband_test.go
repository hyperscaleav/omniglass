package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestSecretSystemBandRetired pins ADR-0052 at every storage layer: secrets
// lost the system band, so a system-owned secret can neither be created
// through the gateway nor inserted past the schema CHECK. Variables keep
// their system band; this is secret-only.
func TestSecretSystemBandRetired(t *testing.T) {
	gw, dsn := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys"}, all, all); err != nil {
		t.Fatalf("system: %v", err)
	}

	// The gateway refuses the kind outright: the owner cannot resolve.
	_, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "dead", SecretType: "snmp-community", OwnerKind: "system", OwnerName: strptr("sys"),
		Fields: map[string]string{"community": "x"},
	}, all, true)
	if !errors.Is(err, storage.ErrSecretOwnerNotFound) {
		t.Errorf("create system-owned secret = %v, want ErrSecretOwnerNotFound", err)
	}

	// The schema refuses it too: a raw insert violates the narrowed CHECK, so
	// even a future gateway bug cannot mint the dead row.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, `
		insert into secret (name, secret_type, owner_kind, system_id, value)
		select 'dead-raw', st.id, 'system', sy.id, '{}'::jsonb
		from secret_type st, system sy
		where st.name = 'snmp-community' and sy.name = 'sys'`)
	if err == nil || !strings.Contains(err.Error(), "secret_owner_kind_check") {
		t.Errorf("raw system-owned insert error = %v, want secret_owner_kind_check violation", err)
	}
}
