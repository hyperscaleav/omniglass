package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The platform system (#1094, first built by #1625) is the one system that
// holds Omniglass's own parts: a component per node today, the server's in a
// later slice. The boot seed creates it, of the reserved og system type
// `platform`, unplaced, so it resolves through the unplaced-root path and only
// an all-scope read sees it. Node registration owns each node's component in
// it. An operator may relabel either, and reshape neither: the Gateway refuses
// every rename, retype, move, delete, membership change or reclassify with one
// of the typed errors below.
const (
	// PlatformSystemName is the reserved system's name.
	PlatformSystemName = "platform"
	// platformTypeName is the reserved og system_type the platform system
	// has and no other system may take.
	platformTypeName = "platform"
	// NodeProductName is the og product every node's platform component is.
	NodeProductName = "omniglass-node"
)

var (
	// ErrPlatformSystemReserved refuses an operator reshaping the platform
	// system: a rename, retype, standard, move, delete, or a child system.
	ErrPlatformSystemReserved = errors.New("storage: the platform system is reserved to Omniglass")
	// ErrPlatformTypeReserved refuses giving the platform system type to any
	// other system.
	ErrPlatformTypeReserved = errors.New("storage: the platform system type is reserved to the platform system")
	// ErrPlatformComponentReserved refuses an operator reshaping a node's
	// platform component: a rename, reclassify, move, delete, a child, or a
	// membership in an operator system.
	ErrPlatformComponentReserved = errors.New("storage: a node's platform component is reserved to its node")
	// ErrPlatformMemberReserved refuses an operator changing the platform
	// system's membership: it holds node platform components and nothing else.
	ErrPlatformMemberReserved = errors.New("storage: the platform system holds only node platform components")
	// ErrPlatformComponentNameTaken refuses registering a node whose name an
	// operator's unplaced component already holds: the node's platform
	// component takes the node's name in the same unplaced bucket.
	ErrPlatformComponentNameTaken = errors.New("storage: an unplaced component already holds this node's name")
)

// platformSystemID returns the platform system's id, or nil when the boot
// seed has not created it (an unseeded database, which most gateway tests
// run on): node registration then creates no component, and the next boot's
// EnsurePlatform adopts the node.
func platformSystemID(ctx context.Context, q querier) (*string, error) {
	var id string
	err := q.QueryRow(ctx, `
		select s.id from system s join system_type st on st.id = s.system_type_id
		where st.namespace = 'og' and st.name = $1`, platformTypeName).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("storage: read the platform system: %w", err)
	}
	return &id, nil
}

// isPlatformSystem reports whether systemID is the platform system.
func isPlatformSystem(ctx context.Context, q querier, systemID string) (bool, error) {
	var ok bool
	if err := q.QueryRow(ctx, `
		select exists(select 1 from system s join system_type st on st.id = s.system_type_id
			where s.id = $1 and st.namespace = 'og' and st.name = $2)`, systemID, platformTypeName).Scan(&ok); err != nil {
		return false, fmt.Errorf("storage: platform system check: %w", err)
	}
	return ok, nil
}

// isPlatformType reports whether typeID is the reserved platform system type.
func isPlatformType(ctx context.Context, q querier, typeID string) (bool, error) {
	var ok bool
	if err := q.QueryRow(ctx, `
		select exists(select 1 from system_type where id = $1 and namespace = 'og' and name = $2)`, typeID, platformTypeName).Scan(&ok); err != nil {
		return false, fmt.Errorf("storage: platform type check: %w", err)
	}
	return ok, nil
}

// isPlatformComponent reports whether componentID is a node's platform
// component: the node row names it, so nothing matches on names.
func isPlatformComponent(ctx context.Context, q querier, componentID string) (bool, error) {
	var ok bool
	if err := q.QueryRow(ctx, `select exists(select 1 from node where platform_component_id = $1)`, componentID).Scan(&ok); err != nil {
		return false, fmt.Errorf("storage: platform component check: %w", err)
	}
	return ok, nil
}

// refusePlatformSystem is ErrPlatformSystemReserved for the platform system.
func refusePlatformSystem(ctx context.Context, q querier, systemID string) error {
	ok, err := isPlatformSystem(ctx, q, systemID)
	if err != nil {
		return err
	}
	if ok {
		return ErrPlatformSystemReserved
	}
	return nil
}

// refusePlatformType is ErrPlatformTypeReserved for the platform type.
func refusePlatformType(ctx context.Context, q querier, typeID string) error {
	ok, err := isPlatformType(ctx, q, typeID)
	if err != nil {
		return err
	}
	if ok {
		return ErrPlatformTypeReserved
	}
	return nil
}

// refusePlatformComponent is ErrPlatformComponentReserved for a node's
// platform component.
func refusePlatformComponent(ctx context.Context, q querier, componentID string) error {
	ok, err := isPlatformComponent(ctx, q, componentID)
	if err != nil {
		return err
	}
	if ok {
		return ErrPlatformComponentReserved
	}
	return nil
}

// createPlatformComponent gives the node named nodeName its platform
// component in the platform system systemID, inside tx, and names it on the
// node row. The component takes the node's name, unplaced like its system,
// classified as the og omniglass-node product with a generated label. Its
// membership is written directly, past addMemberTx's refusal of the platform
// system, because this is the one writer that system admits.
func (p *PG) createPlatformComponent(ctx context.Context, tx pgx.Tx, actorID, nodeName, systemID string) (string, error) {
	var productID string
	if err := tx.QueryRow(ctx, `select id from product where namespace = 'og' and name = $1`, NodeProductName).Scan(&productID); err != nil {
		return "", fmt.Errorf("storage: resolve the %s product: %w", NodeProductName, err)
	}
	c, err := scanComponent(tx.QueryRow(ctx, `
		insert into component (name, label, parent_id, location_id, product_id, name_generated, ordinal, label_generated)
		values ($1, null, null, null, $2, false, null, true)
		returning `+componentCols, nodeName, productID))
	if err != nil {
		if errors.Is(mapComponentWriteErr(err), ErrComponentExists) {
			return "", ErrPlatformComponentNameTaken
		}
		return "", fmt.Errorf("storage: create platform component for node %q: %w", nodeName, err)
	}
	if _, err := tx.Exec(ctx, `insert into system_member (system_id, component_id, is_primary) values ($1, $2, true)`, systemID, c.ID); err != nil {
		return "", fmt.Errorf("storage: join node %q's component to the platform system: %w", nodeName, err)
	}
	if c, err = scanComponent(tx.QueryRow(ctx, `select `+componentCols+` from component where id = $1`, c.ID)); err != nil {
		return "", fmt.Errorf("storage: re-read platform component: %w", err)
	}
	if c, err = p.stampComponentLabel(ctx, tx, c); err != nil {
		return "", err
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "component", c.ID, nil, c); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `update node set platform_component_id = $2 where name = $1`, nodeName, c.ID); err != nil {
		return "", fmt.Errorf("storage: name node %q's platform component: %w", nodeName, err)
	}
	return c.ID, nil
}

// EnsurePlatform is the boot seed's platform step: it creates the platform
// system if it is absent (of the reserved og type, which the seed's system
// types put in place first), then gives every node that has no platform
// component its component. Idempotent, so it runs on every boot; the second
// half adopts a node registered while the database had no platform system.
// A system an operator already named `platform` in the unplaced bucket stops
// the boot with that name, rather than the platform taking it over.
func (p *PG) EnsurePlatform(ctx context.Context) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin ensure platform: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	sysID, err := platformSystemID(ctx, tx)
	if err != nil {
		return err
	}
	if sysID == nil {
		var typeID string
		if err := tx.QueryRow(ctx, `select id from system_type where namespace = 'og' and name = $1`, platformTypeName).Scan(&typeID); err != nil {
			return fmt.Errorf("storage: the %s system type is not seeded: %w", platformTypeName, err)
		}
		s, err := scanSystem(tx.QueryRow(ctx, `
			insert into system (name, label, system_type_id, name_generated, label_generated)
			values ($1, null, $2, false, true)
			returning `+systemCols, PlatformSystemName, typeID))
		if err != nil {
			if errors.Is(mapSystemWriteErr(err), ErrSystemExists) {
				return fmt.Errorf("storage: an unplaced system already holds the reserved name %q: %w", PlatformSystemName, ErrPlatformSystemReserved)
			}
			return fmt.Errorf("storage: create the platform system: %w", err)
		}
		if s, err = p.stampSystemLabel(ctx, tx, s); err != nil {
			return err
		}
		if err := writeAuditRes(ctx, tx, "", "create", "system", s.ID, nil, s); err != nil {
			return err
		}
		sysID = &s.ID
	}

	rows, err := tx.Query(ctx, `select name from node where platform_component_id is null order by name`)
	if err != nil {
		return fmt.Errorf("storage: list nodes without a platform component: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("storage: list nodes without a platform component: %w", err)
	}
	for _, name := range names {
		if _, err := p.createPlatformComponent(ctx, tx, "", name, *sysID); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit ensure platform: %w", err)
	}
	return nil
}

// refusePlatformMembership guards every operator membership write: the
// platform system admits no member through it (node registration writes its
// own membership directly), and a node's platform component joins no other
// system.
func refusePlatformMembership(ctx context.Context, q querier, systemID, componentID string) error {
	if ok, err := isPlatformSystem(ctx, q, systemID); err != nil {
		return err
	} else if ok {
		return ErrPlatformMemberReserved
	}
	return refusePlatformComponent(ctx, q, componentID)
}
