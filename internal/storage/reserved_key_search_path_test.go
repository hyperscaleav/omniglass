package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	"github.com/jackc/pgx/v5"
)

// A reserved key held in both tiers resolves to nothing in SQL too (#1447),
// as ResolveNameLane resolves it for the ingest registry. An operator held
// local/ssh-host-key before a release reserved the name and shipped og's row
// beside theirs: no reader serves the operator's frozen values as the outpost's.
// An unreserved name an operator holds beside a shipped one still reaches
// theirs.
func TestAReservedKeyHeldInBothTiersReachesNoReader(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("storage.NewPG: %v", err)
	}
	t.Cleanup(gw.Close)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })

	const reserved, unreserved = "ssh-host-key", "video-input"
	if _, ok := writerclass.Reserved(reserved); !ok {
		t.Fatalf("%s is not reserved: the fixture needs a reserved key", reserved)
	}
	if _, ok := writerclass.Reserved(unreserved); ok {
		t.Fatalf("%s is reserved: the fixture needs an unreserved key", unreserved)
	}

	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	// The pre-release state: the operator's local rows, each with a value
	// recorded under it, before the release lands og's.
	for _, name := range []string{reserved, unreserved} {
		if _, err := conn.Exec(ctx, `insert into property_type (namespace, name, data_type) values ('local', $1, 'string')`, name); err != nil {
			t.Fatalf("plant local/%s: %v", name, err)
		}
		if _, err := conn.Exec(ctx, `
			insert into property (owner_kind, component_id, property_type_id, instance, provenance, value)
			values ('component', (select id from component where name = 'disp-1'),
				(select id from property_type where namespace = 'local' and name = $1), 'ssh', 'observed', to_jsonb('frozen'::text))`, name); err != nil {
			t.Fatalf("plant a value under local/%s: %v", name, err)
		}
	}
	// The release.
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	lane := func(name string) (storage.NameHolder, bool) {
		t.Helper()
		rows, err := conn.Query(ctx, `
			select 'metric', namespace from metric_type where name = $1
			union all select 'property', namespace from property_type where name = $1
			union all select 'event', namespace from event_type where name = $1`, name)
		if err != nil {
			t.Fatalf("holders of %s: %v", name, err)
		}
		holders, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (storage.NameHolder, error) {
			var h storage.NameHolder
			return h, r.Scan(&h.Lane, &h.Namespace)
		})
		if err != nil {
			t.Fatalf("holders of %s: %v", name, err)
		}
		w, _, ok := storage.ResolveNameLane(name, holders)
		return w, ok
	}
	inEffect := func(name string) []string {
		t.Helper()
		rows, err := conn.Query(ctx, `select namespace from property_type_in_effect() where name = $1`, name)
		if err != nil {
			t.Fatalf("property_type_in_effect %s: %v", name, err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatalf("property_type_in_effect %s: %v", name, err)
		}
		return got
	}

	// The reserved key: the registry refuses it, and so does every reader.
	if w, ok := lane(reserved); ok {
		t.Fatalf("ResolveNameLane(%s) = %+v, want nothing: the fixture lost its collision", reserved, w)
	}
	if got := inEffect(reserved); len(got) != 0 {
		t.Errorf("property_type_in_effect() holds %s in %v, want neither tier, as the registry resolves it", reserved, got)
	}
	if got, err := gw.LatestProperty(ctx, "disp-1", reserved, "ssh"); err != nil || got != nil {
		t.Errorf("LatestProperty(%s) = %+v (%v), want nothing", reserved, got, err)
	}
	if got, err := gw.PropertyTransitions(ctx, "disp-1", reserved, "ssh", 0); err != nil || len(got) != 0 {
		t.Errorf("PropertyTransitions(%s) = %+v (%v), want none", reserved, got, err)
	}
	if pin, err := gw.EndpointKeyPin(ctx, "disp-1", "ssh", "ssh"); err != nil || pin != "" {
		t.Errorf("the ssh key pin reads %q (%v), want none: the operator's frozen value is not the outpost's", pin, err)
	}

	// The unreserved key: the operator's row answers, in the registry and in
	// every reader.
	if w, ok := lane(unreserved); !ok || w.Namespace != storage.NamespaceLocal {
		t.Fatalf("ResolveNameLane(%s) = %+v (%v), want the operator's local row", unreserved, w, ok)
	}
	if got := inEffect(unreserved); len(got) != 1 || got[0] != storage.NamespaceLocal {
		t.Errorf("property_type_in_effect() holds %s in %v, want local alone", unreserved, got)
	}
	if got, err := gw.LatestProperty(ctx, "disp-1", unreserved, "ssh"); err != nil || got == nil || got.Value != "frozen" {
		t.Errorf("LatestProperty(%s) = %+v (%v), want the operator's value", unreserved, got, err)
	}
	if got, err := gw.PropertyTransitions(ctx, "disp-1", unreserved, "ssh", 0); err != nil || len(got) != 1 {
		t.Errorf("PropertyTransitions(%s) = %+v (%v), want the operator's one", unreserved, got, err)
	}
}
