package storage_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// platformGateway opens a gateway over a boot-seeded database, so the
// platform system exists (#1625), cloned from a template the seed runs into
// once.
func platformGateway(t *testing.T) (*storage.PG, context.Context) {
	t.Helper()
	gw, ctx, _ := platformGatewayDSN(t)
	return gw, ctx
}

// platformGatewayDSN is platformGateway with its database's dsn, for a test
// that reads or arranges rows past the gateway.
func platformGatewayDSN(t *testing.T) (*storage.PG, context.Context, string) {
	t.Helper()
	dsn := storagetest.NewDSNFrom(t, "platform_seed", func(ctx context.Context, dsn string) error {
		gw, err := storage.NewPG(ctx, dsn)
		if err != nil {
			return err
		}
		defer gw.Close()
		return seed.Run(ctx, gw)
	})
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	return gw, ctx, dsn
}

// platformNode registers a node and returns its platform component.
func platformNode(t *testing.T, ctx context.Context, gw *storage.PG, name string) *storage.Component {
	t.Helper()
	n, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: name}, all, all)
	if err != nil {
		t.Fatalf("create node %s: %v", name, err)
	}
	if n.PlatformComponentID == nil {
		t.Fatalf("node %s has no platform component", name)
	}
	c, err := gw.GetComponent(ctx, *n.PlatformComponentID, all)
	if err != nil {
		t.Fatalf("read node %s's platform component: %v", name, err)
	}
	return c
}

func TestCreateNodeCreatesItsPlatformComponent(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	c := platformNode(t, ctx, gw, "edge-a")
	if c.Name != "edge-a" {
		t.Errorf("platform component name = %q, want the node's name edge-a", c.Name)
	}
	if c.PrimarySystem == nil || *c.PrimarySystem != storage.PlatformSystemName {
		t.Errorf("platform component's primary system = %v, want %s", c.PrimarySystem, storage.PlatformSystemName)
	}
	if c.ParentID != nil || c.LocationID != nil {
		t.Errorf("platform component is placed (parent %v, location %v); it is unplaced like its system", c.ParentID, c.LocationID)
	}
	if c.ProductHandle == nil || *c.ProductHandle != storage.NodeProductName {
		t.Errorf("platform component product = %v, want %s", c.ProductHandle, storage.NodeProductName)
	}
}

func TestDeleteNodeRemovesItsPlatformComponent(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	c := platformNode(t, ctx, gw, "edge-gone")
	if err := gw.DeleteNode(ctx, "", "edge-gone", all, all); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	if _, err := gw.GetComponent(ctx, c.ID, all); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Errorf("read the deleted node's component = %v, want ErrComponentNotFound", err)
	}
}

// A node whose name an operator's unplaced component already holds cannot
// take it for its platform component; the create is refused whole, so no
// node exists without its component.
func TestCreateNodeRefusesANameAnUnplacedComponentHolds(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "edge-taken"}, all)
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-taken"}, all, all); !errors.Is(err, storage.ErrPlatformComponentNameTaken) {
		t.Fatalf("create node = %v, want ErrPlatformComponentNameTaken", err)
	}
	if _, err := gw.GetNode(ctx, "edge-taken", all); !errors.Is(err, storage.ErrNodeNotFound) {
		t.Errorf("the refused node exists: %v", err)
	}
}

func TestPlatformSystemRefusesRename(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	if _, err := gw.RenameSystem(ctx, "", storage.PlatformSystemName, "my-platform", all, all); !errors.Is(err, storage.ErrPlatformSystemReserved) {
		t.Errorf("rename = %v, want ErrPlatformSystemReserved", err)
	}
	if _, err := gw.ResetSystemName(ctx, "", storage.PlatformSystemName, all, all); !errors.Is(err, storage.ErrPlatformSystemReserved) {
		t.Errorf("reset name = %v, want ErrPlatformSystemReserved", err)
	}
}

func TestPlatformSystemRefusesRetype(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	room := "room"
	if _, err := gw.UpdateSystem(ctx, "", storage.PlatformSystemName, storage.SystemPatch{SystemTypeID: &room}, all, all); !errors.Is(err, storage.ErrPlatformSystemReserved) {
		t.Errorf("retype = %v, want ErrPlatformSystemReserved", err)
	}
	// Its label is the operator's to word, like any row's.
	label := "Omniglass itself"
	if _, err := gw.UpdateSystem(ctx, "", storage.PlatformSystemName, storage.SystemPatch{Label: &label}, all, all); err != nil {
		t.Errorf("relabel the platform system: %v", err)
	}
}

func TestPlatformSystemRefusesMove(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "host-system"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	parent := "host-system"
	if _, err := gw.MoveSystem(ctx, "", storage.PlatformSystemName, storage.SystemMove{ParentName: &parent}, all, all, all); !errors.Is(err, storage.ErrPlatformSystemReserved) {
		t.Errorf("move = %v, want ErrPlatformSystemReserved", err)
	}
}

func TestPlatformSystemRefusesDelete(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	platformNode(t, ctx, gw, "edge-held")
	if err := gw.DeleteSystem(ctx, "", storage.PlatformSystemName, all, all); !errors.Is(err, storage.ErrPlatformSystemReserved) {
		t.Errorf("delete = %v, want ErrPlatformSystemReserved", err)
	}
}

func TestPlatformTypeRefusedOnAnOperatorSystem(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	platform := "platform"
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "fake-platform", SystemTypeID: &platform}, all, all); !errors.Is(err, storage.ErrPlatformTypeReserved) {
		t.Errorf("create with the platform type = %v, want ErrPlatformTypeReserved", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "ordinary"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	if _, err := gw.UpdateSystem(ctx, "", "ordinary", storage.SystemPatch{SystemTypeID: &platform}, all, all); !errors.Is(err, storage.ErrPlatformTypeReserved) {
		t.Errorf("retype to the platform type = %v, want ErrPlatformTypeReserved", err)
	}
}

func TestPlatformComponentRefusesRename(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	c := platformNode(t, ctx, gw, "edge-r")
	if _, err := gw.RenameComponent(ctx, "", c.Name, "edge-renamed", all, all); !errors.Is(err, storage.ErrPlatformComponentReserved) {
		t.Errorf("rename = %v, want ErrPlatformComponentReserved", err)
	}
	if _, err := gw.ResetComponentName(ctx, "", c.Name, all, all); !errors.Is(err, storage.ErrPlatformComponentReserved) {
		t.Errorf("reset name = %v, want ErrPlatformComponentReserved", err)
	}
	generic := "generic-app"
	if _, err := gw.UpdateComponent(ctx, "", c.Name, storage.ComponentPatch{ProductName: &generic}, all, all); !errors.Is(err, storage.ErrPlatformComponentReserved) {
		t.Errorf("reclassify = %v, want ErrPlatformComponentReserved", err)
	}
	label := "Edge R"
	if _, err := gw.UpdateComponent(ctx, "", c.Name, storage.ComponentPatch{Label: &label}, all, all); err != nil {
		t.Errorf("relabel a platform component: %v", err)
	}
}

func TestPlatformComponentRefusesDelete(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	c := platformNode(t, ctx, gw, "edge-d")
	if err := gw.DeleteComponent(ctx, "", c.Name, all, all); !errors.Is(err, storage.ErrPlatformComponentReserved) {
		t.Errorf("delete = %v, want ErrPlatformComponentReserved", err)
	}
}

func TestPlatformComponentRefusesReparent(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	c := platformNode(t, ctx, gw, "edge-m")
	host := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "rack-host"}, all)
	if _, err := gw.MoveComponent(ctx, "", c.Name, storage.ComponentMove{ParentName: &host.Name}, all, all, all); !errors.Is(err, storage.ErrPlatformComponentReserved) {
		t.Errorf("re-parent the platform component = %v, want ErrPlatformComponentReserved", err)
	}
	// Nor can an operator component be hung under it.
	if _, err := gw.MoveComponent(ctx, "", host.Name, storage.ComponentMove{ParentName: &c.Name}, all, all, all); !errors.Is(err, storage.ErrPlatformComponentReserved) {
		t.Errorf("move a component under the platform component = %v, want ErrPlatformComponentReserved", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "hung", ParentName: &c.Name}, all, all, all, all); !errors.Is(err, storage.ErrPlatformComponentReserved) {
		t.Errorf("create under the platform component = %v, want ErrPlatformComponentReserved", err)
	}
}

func TestPlatformSystemRefusesAnOperatorComponent(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	c := platformNode(t, ctx, gw, "edge-p")
	platform := storage.PlatformSystemName
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "intruder", SystemName: &platform}, all, all, all, all); !errors.Is(err, storage.ErrPlatformMemberReserved) {
		t.Errorf("create into the platform system = %v, want ErrPlatformMemberReserved", err)
	}
	op := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "operator-box"}, all)
	if err := gw.AddMember(ctx, "", platform, op.Name, all, all, all, all); !errors.Is(err, storage.ErrPlatformMemberReserved) {
		t.Errorf("add a member to the platform system = %v, want ErrPlatformMemberReserved", err)
	}
	if err := gw.RemoveMember(ctx, "", platform, c.Name, all, all, all, all); !errors.Is(err, storage.ErrPlatformMemberReserved) {
		t.Errorf("remove a node's component from the platform system = %v, want ErrPlatformMemberReserved", err)
	}
	// A platform component joins no operator system either.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "borrower"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	if err := gw.AddMember(ctx, "", "borrower", c.Name, all, all, all, all); !errors.Is(err, storage.ErrPlatformComponentReserved) {
		t.Errorf("add a platform component to an operator system = %v, want ErrPlatformComponentReserved", err)
	}
}

// TestEnsurePlatformSerializesConcurrentBoots proves two servers booting at
// once leave one platform system and one component per node (#1625): each
// boot's platform step reads "no platform system" and "this node has no
// component" before it writes, so without one lock across the step the
// second boot mints `platform-2` beside the first, and re-adopts a node the
// first already gave a component, orphaning that component.
func TestEnsurePlatformSerializesConcurrentBoots(t *testing.T) {
	t.Parallel()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	// A database from the release before the platform: seeded, no platform
	// system, and nodes registered with no component.
	if _, err := conn.Exec(ctx, `delete from system where name = 'platform'`); err != nil {
		t.Fatalf("take the database back before the platform: %v", err)
	}
	const nodes = 4
	for i := 0; i < nodes; i++ {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: fmt.Sprintf("early-%d", i)}, all, all); err != nil {
			t.Fatalf("create node %d: %v", i, err)
		}
	}

	const boots = 8
	start := make(chan struct{})
	errs := make(chan error, boots)
	var wg sync.WaitGroup
	for i := 0; i < boots; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- gw.EnsurePlatform(ctx)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("a concurrent boot's platform step failed: %v", err)
		}
	}

	var systems int
	if err := conn.QueryRow(ctx, `
		select count(*) from system s join system_type st on st.id = s.system_type_id
		where st.namespace = 'og' and st.name = 'platform'`).Scan(&systems); err != nil {
		t.Fatalf("count platform systems: %v", err)
	}
	if systems != 1 {
		t.Errorf("systems of the platform type = %d after %d concurrent boots, want 1", systems, boots)
	}
	// Every member of a platform system is a node's component, one per node,
	// and every one is the component its node names.
	var members, named int
	if err := conn.QueryRow(ctx, `
		select count(*), count(n.name) from system_member m
		join system s on s.id = m.system_id
		join system_type st on st.id = s.system_type_id and st.namespace = 'og' and st.name = 'platform'
		left join node n on n.platform_component_id = m.component_id`).Scan(&members, &named); err != nil {
		t.Fatalf("count platform members: %v", err)
	}
	if members != nodes {
		t.Errorf("platform members = %d after %d concurrent boots, want one per node (%d)", members, boots, nodes)
	}
	if named != members {
		t.Errorf("%d of %d platform members are no node's component; a second boot re-adopted a node", members-named, members)
	}
}
