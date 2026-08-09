package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Component-layer sentinel errors, mirroring the location/system sets.
var (
	ErrComponentNotFound       = errors.New("storage: component not found")
	ErrComponentForbidden      = errors.New("storage: action not permitted on this component")
	ErrComponentOccupied       = errors.New("storage: component has child components")
	ErrComponentExists         = errors.New("storage: component name already exists")
	ErrParentComponentNotFound = errors.New("storage: parent component not found")
	ErrProductNotFound         = errors.New("storage: product not found")
	ErrComponentCycle          = errors.New("storage: cannot move a component under itself or a descendant")

	// ErrComponentExistsUnderParent / ErrComponentExistsInLocation /
	// ErrComponentExistsUnplaced name which placement bucket a 23505 collided
	// in (#627 scopes name uniqueness to placement, not the whole estate: a
	// name is unique under a given parent, or at a given location when
	// unparented, or in the unplaced/root bucket, but not across all three at
	// once). Each wraps ErrComponentExists via %w, so errors.Is(err,
	// ErrComponentExists) still matches any of them generically; a caller
	// that wants the specific bucket (a later task's 409 message) checks the
	// specific sentinel instead.
	ErrComponentExistsUnderParent = fmt.Errorf("storage: a component with this name already exists under this parent: %w", ErrComponentExists)
	ErrComponentExistsInLocation  = fmt.Errorf("storage: a component with this name already exists at this location: %w", ErrComponentExists)
	ErrComponentExistsUnplaced    = fmt.Errorf("storage: an unplaced component with this name already exists: %w", ErrComponentExists)
)

// Component is a leaf of the estate: name-addressable, nestable via parent_id,
// belonging to a primary system and located at a location. Its shape (the
// properties it declares) comes from the product it is an instance of.
type Component struct {
	ID          string
	Name        string
	DisplayName string
	ParentID    *string
	// PrimarySystem is the name of the component's default system, and SystemCount
	// how many it belongs to in total. Both are derived from system_member rather
	// than stored: a component can be in several systems, so there is no single
	// pointer to keep. The name rather than an id, because a name is the address
	// the API speaks.
	PrimarySystem   *string
	PrimarySystemID *string
	SystemCount     int
	// ParentName and LocationName are how the API addresses this component's
	// placement. The *ID fields beside them are internal: a uuid is identity, never
	// a reference that leaves the process.
	ParentName   *string
	LocationName *string
	LocationID   *string
	ProductID    *string
	// ProductHandle is the product's kebab name, projected for display beside the id.
	ProductHandle *string
	// NameGenerated marks a name the platform picked (a stem+ordinal generator,
	// #627) rather than one an operator typed. False for every row that
	// existed before the generator landed; the gateway sets it explicitly on
	// insert once that generator writes.
	NameGenerated bool
	// Path, PathSegments, and Renders are the dotted address and its two
	// display-only compact forms (#627 Task 15), attached by attachComponentPaths
	// after every GET or LIST fetch (see scopedConfig.attachPaths). Zero-value
	// (empty) on a row this gateway returns from a write path (create, update,
	// move, rename, resetName): those responses carry Name/NameGenerated fresh,
	// and the console's own query-invalidation refetch picks up Path moments
	// later, so leaving it empty here trades one render frame of staleness for
	// not doubling every write's query count.
	Path         string
	PathSegments []string
	Renders      Renders
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ComponentSpec is the create input. ParentName nil makes a root component;
// SystemName / LocationName optionally bind it to a system and place it;
// ProductName optionally names the product (catalog SKU) it is an instance of.
type ComponentSpec struct {
	Name         string
	DisplayName  string
	ParentName   *string
	SystemName   *string
	LocationName *string
	ProductName  *string
}

// ComponentPatch is the update input: nil fields unchanged.
//
// ProductName does NOT follow the clear convention: the #614 floor made
// product_id required (every component is an instance of a product, the
// "productless component" this comment used to describe no longer exists),
// so nil is unchanged and a name reclassifies, but an explicit empty string
// reclassifies to generic-device rather than clearing (see UpdateComponent's
// product_id CASE). The API refuses an empty-string product outright (422)
// before this is ever reached.
//
// There is deliberately no Name here. A rename is RenameComponent, its own act
// under its own permission, because it breaks the references an operator stored
// outside this system. There is deliberately no LocationName or ParentName
// here either (#627 Task 13): a placement change is its own act, MoveComponent,
// gated by component:move rather than component:update; see that function's
// doc comment for why.
type ComponentPatch struct {
	DisplayName *string
	ProductName *string
}

// ComponentMove is the :move input: nil fields unchanged, an explicit empty
// string clears (an unplaced component, a root component), a name resolves to
// its id. ParentName is cycle-guarded and scope-injected like a location
// reparent: the new parent must be inside the caller's move scope and must not
// be the component itself or one of its own descendants. Clearing ParentName
// to root requires an all-scoped move grant (see MoveComponent's doc comment
// for why).
type ComponentMove struct {
	LocationName *string
	ParentName   *string
}

// --- component CRUD (read/delete via the generic helpers) --------------------

const componentCols = `id, name, coalesce(display_name, ''), parent_id,
	-- The primary membership, both forms: the name for display and the id as the
	-- canonical handle. The arc points at the primary key, so the join is by id.
	(select s.name from system s join system_member m on m.system_id = s.id
	  where m.component_id = component.id and m.is_primary) as primary_system,
	(select m.system_id from system_member m where m.component_id = component.id and m.is_primary) as primary_system_id,
	(select count(*) from system_member m where m.component_id = component.id) as system_count,
	location_id, product_id,
	-- The names the API addresses these by. The ids stay for the scope walks and
	-- tree joins, which are internal; a name is what leaves the process.
	(select p.name from component p where p.id = component.parent_id) as parent_name,
	(select l.name from location l where l.id = component.location_id) as location_name,
	(select pr.name from product pr where pr.id = component.product_id) as product_handle,
	name_generated,
	created_at, updated_at`

func scanComponent(row pgx.Row) (*Component, error) {
	var c Component
	if err := row.Scan(&c.ID, &c.Name, &c.DisplayName, &c.ParentID, &c.PrimarySystem, &c.PrimarySystemID, &c.SystemCount,
		&c.LocationID, &c.ProductID, &c.ParentName, &c.LocationName, &c.ProductHandle, &c.NameGenerated, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

var componentConfig = scopedConfig[Component]{
	table: componentTable, cols: componentCols, resource: "component",
	scan: scanComponent, idOf: func(c *Component) string { return c.ID },
	notFound: ErrComponentNotFound, forbidden: ErrComponentForbidden, occupied: ErrComponentOccupied,
	attachPaths: attachComponentPaths,
}

// attachComponentPaths fills every c's Path/.PathSegments/.Renders (#627
// Task 15): PathsOf's reverse walk for the addresses, whatever the page
// size (#643), and RenderDash/RenderBare's compact forms per row. The bare
// render's abbreviation comes from the component's own product's
// component_type, resolved through the same inherited-from-parent chain
// generateNameForProduct walks (resolveTypeFacts): the identical
// stem-and-abbrev source a generated name itself came from, so a component's
// bare render always compacts with the type its own name was minted against.
// A component with no product (unreachable through the API, which requires
// one, but not through a direct-gateway caller) or whose type resolves no
// abbrev anywhere in its chain gets "" and RenderBare's own no-abbrev
// fallback.
//
// full gates the abbrev resolution (review finding 3, task-15-review.md #2):
// componentTypeIDForProduct plus resolveTypeFacts' own ancestor walk (up to
// maxComponentTypeDepth levels) are two more queries, for renders.bare, a
// field no console surface reads (only renders.dash, which needs no abbrev
// at all). scopedList calls this with full=false; scopedGet, the one-row
// case where the extra cost is a single request's, not the whole estate's,
// passes true. Resolved once per DISTINCT product across the page, not once
// per row: full is only ever true for a single row today, so that changes
// nothing observable now, and it stops the N+1 from returning the day a
// caller passes many rows with full=true.
// Path/PathSegments/Renders.Dash are computed either way, since those cost
// nothing beyond the walk the page already pays for.
func attachComponentPaths(ctx context.Context, q querier, cs []*Component, full bool) error {
	if len(cs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(cs))
	for _, c := range cs {
		ids = append(ids, c.ID)
	}
	paths, err := PathsOf(ctx, q, componentTable, ids)
	if err != nil {
		return err
	}
	abbrevs := make(map[string]string) // product id -> its type's abbrev, resolved once
	for _, c := range cs {
		segs := paths[c.ID]
		c.PathSegments = segs
		c.Path = strings.Join(segs, ".")
		abbrev := ""
		if full && c.ProductID != nil {
			a, done := abbrevs[*c.ProductID]
			if !done {
				if typeID, err := componentTypeIDForProduct(ctx, q, *c.ProductID); err == nil {
					if _, _, resolved, _, err := resolveTypeFacts(ctx, q, typeID); err == nil {
						a = resolved
					}
				}
				abbrevs[*c.ProductID] = a
			}
			abbrev = a
		}
		c.Renders = Renders{Dash: RenderDash(segs), Bare: RenderBare(segs, abbrev)}
	}
	return nil
}

func (p *PG) ListComponents(ctx context.Context, read scope.Set) ([]Component, error) {
	return scopedList(ctx, p, componentConfig, read)
}

func (p *PG) GetComponent(ctx context.Context, name string, read scope.Set) (*Component, error) {
	return scopedGet(ctx, p, componentConfig, name, read)
}

func (p *PG) DeleteComponent(ctx context.Context, actorID, name string, read, action scope.Set) error {
	return scopedDelete(ctx, p, componentConfig, actorID, name, read, action)
}

// ComponentNameTaken reports whether name is already used within the placement
// a create would actually land in (#627: the unique constraint is scoped to
// placement, not global, so availability has to be checked against the same
// bucket the constraint enforces or the advisory would false-positive on a
// name that is free in the operator's own room). parentRef wins over
// locationRef, mirroring CreateComponent's own placement resolution: a parent
// makes it a child (component_parent_name_key), no parent but a location makes
// it a room-level component (component_location_name_key), and neither is the
// unplaced/root bucket (component_orphan_name_key). Gated at the API by
// component:update.
func (p *PG) ComponentNameTaken(ctx context.Context, name string, parentRef, locationRef *string) (bool, error) {
	var exists bool
	switch {
	case parentRef != nil && *parentRef != "":
		parent, err := scopedByName(ctx, p.pool, componentConfig, *parentRef)
		if err != nil {
			// withoutCandidates: this advisory has no caller scope to filter
			// an ambiguous parentRef by (it is intentionally scope-blind, so
			// availability matches the placement bucket asked about rather
			// than the caller's own grant), so an ambiguity refusal here
			// must never name a row the caller might not be able to read.
			return false, withoutCandidates(err)
		}
		if err := p.pool.QueryRow(ctx, `select exists(select 1 from component where parent_id = $1 and name = $2)`, parent.ID, name).Scan(&exists); err != nil {
			return false, fmt.Errorf("storage: component name taken: %w", err)
		}
	case locationRef != nil && *locationRef != "":
		loc, err := scopedByName(ctx, p.pool, locationConfig, *locationRef)
		if err != nil {
			return false, withoutCandidates(err) // see the parentRef case above
		}
		if err := p.pool.QueryRow(ctx, `select exists(select 1 from component where parent_id is null and location_id = $1 and name = $2)`, loc.ID, name).Scan(&exists); err != nil {
			return false, fmt.Errorf("storage: component name taken: %w", err)
		}
	default:
		if err := p.pool.QueryRow(ctx, `select exists(select 1 from component where parent_id is null and location_id is null and name = $1)`, name).Scan(&exists); err != nil {
			return false, fmt.Errorf("storage: component name taken: %w", err)
		}
	}
	return exists, nil
}

// CreateComponent inserts a component under an optional parent, bound to an
// optional system and location, writing the audit row in the same transaction.
// A root component requires an all create scope; a child requires the parent in
// the create scope. spec.Name empty is not an error: it is the operator
// handing the pen to the platform (#627 Task 14), which mints
// "<resolved-stem>-<n>" from the classified product's component_type once
// placement and product are both resolved, below.
func (p *PG) CreateComponent(ctx context.Context, actorID string, spec ComponentSpec, create scope.Set) (*Component, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create component: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// An operator-typed name is validated now, before any other resolve, so a
	// bad slug fails fast (the existing behavior). A generated name is
	// validated too, but later (inside generateComponentName itself, the one
	// place every generation call site funnels through): an empty or bad
	// stem is only knowable once placement and product resolve below, and an
	// invariant a comment merely asserts, rather than a callee enforces, is
	// exactly the shape that let "-1" through review the first time.
	if spec.Name != "" {
		if err := ValidateName("component", spec.Name); err != nil {
			return nil, err
		}
	}

	var parentID *string
	if spec.ParentName == nil {
		if !create.All {
			return nil, ErrComponentForbidden
		}
	} else {
		// resolveScopedRef, not scopedByName-then-inScopeTree: ruling 2
		// (#627) requires ambiguity judged inside create, not estate-wide,
		// and Candidates on a collision must never name a component the
		// caller cannot reach. A parent that exists only outside create
		// scope stays ErrComponentForbidden (preserved, not collapsed into
		// not-found: the caller supplied this reference itself).
		parent, err := resolveScopedRef(ctx, tx, componentConfig, *spec.ParentName, "component", create)
		if errors.Is(err, ErrComponentNotFound) {
			return nil, ErrParentComponentNotFound
		} else if err != nil {
			return nil, err
		}
		parentID = &parent.ID
	}

	// A system named at create becomes a MEMBERSHIP rather than a column on the
	// component: the relation lives in system_member, and this is simply the first
	// one. Resolved here so an unknown name is a 422 before anything is written,
	// and its id is what the membership insert below binds (#627): the
	// component's own name is not yet even the row's final resolved handle at
	// that point, and re-deriving the system's id from *spec.SystemName a
	// second time would risk landing on a different row entirely once names
	// scope to placement.
	var sysID string
	if spec.SystemName != nil {
		// scopedByName, not scopedByNameInScope: this bind is CROSS-tier (the
		// caller's create scope is resolved for "component", and a system's
		// scope tree is its own, unrelated ancestor chain), so create has no
		// id in it inScopeTree could ever match against the system table.
		// Threading it through would not narrow the resolve, it would deny
		// it outright for every non-all caller (the tier-mismatch defect a
		// review caught). Existence-only, as before this task, and
		// withoutCandidates for the same reason the *NameTaken advisories
		// redact: no scope is being checked here, so listing every matching
		// uuid would disclose rows the caller may hold no grant to read.
		sys, err := scopedByName(ctx, tx, systemConfig, *spec.SystemName)
		if err != nil {
			return nil, withoutCandidates(err) // ErrSystemNotFound -> 422
		}
		sysID = sys.ID
	}
	var locationID *string
	if spec.LocationName != nil {
		// scopedByName, not scopedByNameInScope: same cross-tier reasoning as
		// the system bind above.
		loc, err := scopedByName(ctx, tx, locationConfig, *spec.LocationName)
		if err != nil {
			return nil, withoutCandidates(err) // ErrLocationNotFound -> 422
		}
		locationID = &loc.ID
	}

	// product is a catalog, not a scoped tree: resolve by id (product id is its
	// pk/name) with a plain lookup, not scopedByName. An unknown id is
	// ErrProductNotFound -> 422 (the FK below is the belt-and-suspenders).
	var productID *string
	if spec.ProductName != nil {
		var pid string
		err := tx.QueryRow(ctx, `select id from product where `+registryRefCol(*spec.ProductName)+` = $1`, *spec.ProductName).Scan(&pid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProductNotFound
		} else if err != nil {
			return nil, fmt.Errorf("storage: resolve product %q: %w", *spec.ProductName, err)
		}
		productID = &pid
	}

	// name empty means "generate one": resolved now, after placement and
	// product are both known, since the stem comes from the product's
	// component_type and the ordinal from siblings at this exact placement.
	// The effective product id is resolved here even when spec.ProductName was
	// nil (the same generic-device fallback the insert below applies at the
	// SQL level) because the generator needs to know which component_type it
	// is minting from; the API layer requires an explicit product on create,
	// so this fallback only matters to a caller that writes the gateway
	// directly, exactly like the insert's own COALESCE.
	name := spec.Name
	generated := name == ""
	if generated {
		genProductID := productID
		if genProductID == nil {
			id, err := genericDeviceProductID(ctx, tx)
			if err != nil {
				return nil, err
			}
			genProductID = &id
		}
		if name, err = generateNameForProduct(ctx, tx, *genProductID, parentID, locationID, nil); err != nil {
			return nil, err
		}
	}

	// product is required (the floor: every component is an instance of a
	// product), so an unclassified write here defaults to the generic device
	// the product_type_backfill migration guarantees exists. The API layer
	// requires an explicit product on create (a stricter operator-facing
	// gate naming the generics); this default only ever fires below it, for a
	// caller that writes the gateway directly (seed, devseed, tests).
	c, err := scanComponent(tx.QueryRow(ctx, `
		insert into component (name, display_name, parent_id, location_id, product_id, name_generated)
		values ($1, $2, $3, $4, coalesce($5::uuid, (select id from product where name = 'generic-device')), $6)
		returning `+componentCols,
		name, nullize(spec.DisplayName), parentID, locationID, productID, generated))
	if err != nil {
		return nil, mapComponentWriteErr(err)
	}
	// The membership after the row exists, both ids already in hand (sysID
	// above, c.ID from the insert just returned). Re-read so the returned
	// component carries the primary it just gained.
	if spec.SystemName != nil {
		if err := addMemberTx(ctx, tx, sysID, c.ID); err != nil {
			return nil, err
		}
		if c, err = scanComponent(tx.QueryRow(ctx,
			`select `+componentCols+` from component where id = $1`, c.ID)); err != nil {
			return nil, fmt.Errorf("storage: re-read component after membership: %w", err)
		}
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "component", c.ID, nil, c); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create component: %w", err)
	}
	return c, nil
}

// componentIsDescendant reports whether candidateID is targetID or a descendant
// of it: the cycle guard for a reparent, which must not move a component under
// itself or one of its own children. The recursive walk down parent_id mirrors
// locationIsDescendant on the component tree.
func (p *PG) componentIsDescendant(ctx context.Context, q querier, targetID, candidateID string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		with recursive sub(id) as (
			select id from component where id = $1
			union all
			select c.id from component c join sub on c.parent_id = sub.id
		) cycle id set is_cycle using path
		select exists(select 1 from sub where id = $2)`,
		targetID, candidateID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("storage: component descendant check: %w", err)
	}
	return ok, nil
}

// UpdateComponent patches a component by name with the three-way scope split
// and in-transaction audit. Only product is classification here; a product
// swap does not move health any more: a component's own verdict is now purely
// its active alarms (#626), so it changes only its typed-slot classification
// for the NEXT assignment.
//
// Placement (a relocate or a reparent) is NOT here (#627 Task 13): it is its
// own act, MoveComponent, gated by component:move. See that function's doc
// comment.
func (p *PG) UpdateComponent(ctx context.Context, actorID, name string, patch ComponentPatch, read, action scope.Set) (*Component, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update component: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := resolveScoped(ctx, tx, componentConfig, name, read, action)
	if err != nil {
		return nil, err
	}
	// A product reference arrives as a handle or a uuid; the column stores the
	// uuid. An unknown one resolves to nothing and is reported by name.
	var patchProductID *string
	if patch.ProductName != nil && *patch.ProductName != "" {
		var pid string
		err := tx.QueryRow(ctx, `select id from product where `+registryRefCol(*patch.ProductName)+` = $1`, *patch.ProductName).Scan(&pid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProductNotFound
		} else if err != nil {
			return nil, fmt.Errorf("storage: resolve product %q: %w", *patch.ProductName, err)
		}
		patchProductID = &pid
	}
	// A product patch that leaves the component still platform-owned (#627
	// Task 14) may no longer fit its old stem: an interactive-display's
	// "display-1" reclassified to a mic-type product needs a mic stem, not
	// its old one. before.NameGenerated is the read of record; an
	// operator-typed name (RenameComponent clears the flag forever) is never
	// touched by a reclassify. Computed before the write below so both land
	// in the same UPDATE and the same audit image.
	var namePatch *string
	if patch.ProductName != nil && before.NameGenerated {
		finalProductID := patchProductID
		if finalProductID == nil {
			// An explicit empty string reclassifies to generic-device, the
			// same fallback the product_id CASE below applies at the SQL
			// level; the generator needs the id as a plain Go value to find
			// generic-device's own component_type.
			id, err := genericDeviceProductID(ctx, tx)
			if err != nil {
				return nil, err
			}
			finalProductID = &id
		}
		newName, err := generateNameForProduct(ctx, tx, *finalProductID, before.ParentID, before.LocationID, &before.ID)
		if err != nil {
			return nil, err
		}
		namePatch = &newName
	}
	after, err := scanComponent(tx.QueryRow(ctx, `
		update component set
			name         = coalesce($5, name),
			display_name = coalesce($2, display_name),
			-- product_id has no clear state: the floor makes it NOT NULL, so unlike
			-- a three-state placement field there is no empty-string-means-null
			-- branch here to attempt (that used to be the case before the #614
			-- floor, and would now fail loudly on the column's NOT NULL). A nil
			-- field is unchanged; an explicit empty string reclassifies to
			-- generic-device, the same fallback CreateComponent applies to an
			-- unclassified create, rather than attempting a write the column can
			-- no longer accept. Anything else names a product by handle or uuid,
			-- resolved to its id (an unknown one lands as NULL and is caught
			-- below). The API refuses an empty-string product before this is ever
			-- reached (422), so this branch only matters to a caller that writes
			-- the gateway directly.
			product_id   = case
				when $3::text is null then product_id
				when $3 = '' then (select id from product where name = 'generic-device')
				else $4::uuid
			end,
			updated_at   = now()
		where id = $1
		returning `+componentCols,
		before.ID, patch.DisplayName, patch.ProductName, patchProductID, namePatch))
	if err != nil {
		return nil, mapComponentWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "component", after.ID, before, after); err != nil {
		return nil, err
	}
	// A product swap no longer moves health (see the doc comment above): a
	// component's verdict is its own active alarms, unaffected by what it is
	// classified as. The next AssignRole is where a changed product matters.
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update component: %w", err)
	}
	return after, nil
}

// MoveComponent relocates and/or reparents a component by name, the same
// three-way scope split as UpdateComponent, in its own transaction with its
// own DISTINCT audit verb ("move", not "update"). Its own act, not a PATCH
// field (#627 Task 13): splitting placement out of the single four-column
// UPDATE UpdateComponent used to run turns one operator gesture that touched
// both product and placement together into two transactions and two audit
// rows if a caller wants both, the same tradeoff RenameComponent already
// established for name-plus-other-field edits, and is deliberate here for the
// same reason rename earned its own act: a placement change is an
// authorization act, not a label edit, so it deserves its own grant
// (component:move) and its own audit trail entry, not a side effect that
// rides along with a display_name or product PATCH.
//
// The ADR this closes: a PATCH that cleared parent_id used to lift a row out
// of every subtree scope with no check at all (UpdateComponent's old reparent
// branch only guarded the non-empty case), while CREATING the same root
// component already required an all-scoped grant. Clearing ParentName to root
// here now requires action.All too, closing that gap. Health is unaffected: a
// component's own verdict is purely its active alarms (#626), so neither a
// relocate nor a reparent has ever moved it, and this move continues that.
func (p *PG) MoveComponent(ctx context.Context, actorID, name string, move ComponentMove, read, action scope.Set) (*Component, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin move component: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := resolveScoped(ctx, tx, componentConfig, name, read, action)
	if err != nil {
		return nil, err
	}
	// The location arrives as a name, the column holds an id: resolve the set
	// branch to its id while leaving nil (unchanged) and "" (clear) intact, the
	// same three-state the system relocate uses.
	locationPatch := move.LocationName
	if move.LocationName != nil && *move.LocationName != "" {
		// scopedByName, not scopedByNameInScope: cross-tier, same as the
		// create-time binds above. action is resolved for "component", and
		// checking it against the location table's own ancestor chain can
		// never match, so threading it through denies every non-all caller
		// rather than narrowing anything. Existence-only, withoutCandidates
		// for the same no-scope-to-filter-by reason.
		loc, err := scopedByName(ctx, tx, locationConfig, *move.LocationName)
		if err != nil {
			return nil, withoutCandidates(err) // ErrLocationNotFound -> mapped to 422 by the API
		}
		locationPatch = &loc.ID
	}
	// The parent is a reparent within the component tree: resolve within the
	// caller's action scope (resolveScopedRef, ruling 2), which both
	// requires the new parent inside that scope and judges ambiguity inside
	// it too, and cycle-guard against moving the component under itself or a
	// descendant. nil leaves the parent untouched.
	parentPatch := move.ParentName
	if move.ParentName != nil {
		if *move.ParentName == "" {
			// Clearing to root: the same authorization CreateComponent already
			// enforces for a root component. See the doc comment above: the old
			// UpdateComponent patch had no such guard on this branch at all.
			if !action.All {
				return nil, ErrComponentForbidden
			}
		} else {
			parent, err := resolveScopedRef(ctx, tx, componentConfig, *move.ParentName, "component", action)
			if errors.Is(err, ErrComponentNotFound) {
				return nil, ErrParentComponentNotFound
			} else if err != nil {
				return nil, err
			}
			descendant, err := p.componentIsDescendant(ctx, tx, before.ID, parent.ID)
			if err != nil {
				return nil, err
			}
			if descendant {
				return nil, ErrComponentCycle
			}
			parentPatch = &parent.ID
		}
	}
	// A still-platform-owned name (#627 Task 14) is scoped to the OLD
	// placement's siblings; a move changes that scope, so the ordinal (and,
	// crossing into a differently-typed subtree, the stem) may no longer be
	// what a fresh generate would pick. The product does not change on a
	// move, so only the ordinal actually shifts in the common case, but the
	// stem is re-resolved from before.ProductID's component_type anyway
	// (one rule, no special case for "same stem, different ordinal" versus
	// "different stem too"). An operator-typed name (RenameComponent clears
	// the flag forever) is left exactly where the operator put it.
	//
	// effLocationID/effParentID decode the same three-state patches the
	// UPDATE's own CASE expressions apply (nil unchanged, "" clear, else
	// set), computed here in Go because the generator needs the DESTINATION
	// scope to scan siblings in, not the component's placement as it reads
	// right now.
	var namePatch *string
	if before.NameGenerated {
		effLocationID := before.LocationID
		if locationPatch != nil {
			if *locationPatch == "" {
				effLocationID = nil
			} else {
				v := *locationPatch
				effLocationID = &v
			}
		}
		effParentID := before.ParentID
		if parentPatch != nil {
			if *parentPatch == "" {
				effParentID = nil
			} else {
				v := *parentPatch
				effParentID = &v
			}
		}
		productID := ""
		if before.ProductID != nil {
			productID = *before.ProductID
		}
		newName, err := generateNameForProduct(ctx, tx, productID, effParentID, effLocationID, &before.ID)
		if err != nil {
			return nil, err
		}
		namePatch = &newName
	}
	after, err := scanComponent(tx.QueryRow(ctx, `
		update component set
			name        = coalesce($4, name),
			location_id = case
				when $2::text is null then location_id
				when $2 = '' then null
				else $2::uuid
			end,
			parent_id   = case
				when $3::text is null then parent_id
				when $3 = '' then null
				else $3::uuid
			end,
			updated_at  = now()
		where id = $1
		returning `+componentCols,
		before.ID, locationPatch, parentPatch, namePatch))
	if err != nil {
		return nil, mapComponentWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "move", "component", after.ID, before, after); err != nil {
		return nil, err
	}
	// No RecomputeHealth: see the doc comment above.
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit move component: %w", err)
	}
	return after, nil
}

// RenameComponent moves a component's name, scoped exactly as UpdateComponent is
// (the same read-then-action split, so an unreadable target is a non-disclosing
// not-found and a readable-but-unactionable one is forbidden).
//
// It is a separate gateway function, not a branch of the patch, because a rename is
// a separate act: every reference an operator stored outside this system (a
// bookmark, a runbook step, an integration's config) addresses the component by
// name, and moving it breaks them. Inside the system nothing breaks, because every
// arc stores the uuid, which is also why the audit row keys on that uuid: a trail
// keyed on the thing that just changed is a trail the change orphans.
//
// Health is not recomputed. The chain runs component -> systems-it-staffs ->
// locations-over-those-systems and every link of it is an id, so a name move
// changes no verdict.
//
// name_generated is always cleared to false here, whether or not it was
// already (#627 Task 14): an operator who types a specific name is claiming
// the pen, and a rename is the one act whose entire point is an
// operator-chosen name. A later move or product reclassify then leaves this
// name alone, since it is no longer platform-owned; :resetName is how an
// operator hands the pen back.
func (p *PG) RenameComponent(ctx context.Context, actorID, name, newName string, read, action scope.Set) (*Component, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin rename component: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := resolveScoped(ctx, tx, componentConfig, name, read, action)
	if err != nil {
		return nil, err
	}
	if err := ValidateName("component", newName); err != nil {
		return nil, err
	}
	after, err := scanComponent(tx.QueryRow(ctx,
		`update component set name = $2, name_generated = false, updated_at = now() where id = $1 returning `+componentCols,
		before.ID, newName))
	if err != nil {
		return nil, mapComponentWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "rename", "component", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit rename component: %w", err)
	}
	return after, nil
}

// ResetComponentName hands the pen back to the platform (#627 Task 14): it
// regenerates the component's name from its CURRENT type and placement, the
// exact rule CreateComponent applies when no name is given, and marks the
// result name_generated again. It does not matter whether the name was
// already platform-owned or an operator had typed one: either way the
// answer is a fresh "<stem>-<n>". Scoped exactly as RenameComponent (the
// same read-then-action split), because it is the same act from the
// permission's point of view: it changes the name, gated by
// component:rename, no new token.
//
// The audit verb is "reset", distinct from "rename": the row records what
// actually happened (the platform picked the name, not the caller), the
// same reasoning that gave move its own verb apart from update.
func (p *PG) ResetComponentName(ctx context.Context, actorID, name string, read, action scope.Set) (*Component, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin reset component name: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := resolveScoped(ctx, tx, componentConfig, name, read, action)
	if err != nil {
		return nil, err
	}
	productID := ""
	if before.ProductID != nil {
		productID = *before.ProductID
	}
	newName, err := generateNameForProduct(ctx, tx, productID, before.ParentID, before.LocationID, &before.ID)
	if err != nil {
		return nil, err
	}
	after, err := scanComponent(tx.QueryRow(ctx,
		`update component set name = $2, name_generated = true, updated_at = now() where id = $1 returning `+componentCols,
		before.ID, newName))
	if err != nil {
		return nil, mapComponentWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "reset", "component", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit reset component name: %w", err)
	}
	return after, nil
}

// ComponentInterface is one placement-bound connection owned by a component: the
// unit the reachability panel iterates. Params is the raw endpoint jsonb (target,
// port, and settings); NodeName is the probing node (nullable). The verdict,
// layer signals, and history are read separately per (component, key, interface).
type ComponentInterface struct {
	Name     string
	Type     string
	NodeName string
	Params   []byte
}

// ListComponentInterfaces returns a component's interfaces ordered by name, the
// rows the reachability read composes over. It is not scope-injected: the caller
// gates on the component being in read scope (GetComponent) first, then reads its
// interfaces by the verified reference (name or uuid, ADR-0062), resolved once
// here rather than left as a name a second row could now share (#627). An
// unknown component folds into the same nil-no-error empty result the old
// inline subquery's silent no-match gave: devseed's seedReachability uses
// this as its own sentinel read, calling it BEFORE the component it names is
// known to exist yet, on the first run.
func (p *PG) ListComponentInterfaces(ctx context.Context, componentRef string) ([]ComponentInterface, error) {
	c, err := scopedByName(ctx, p.pool, componentConfig, componentRef)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
		select i.name, (select it.name from interface_type it where it.id = i.type), coalesce((select n.name from node n where n.principal_id = i.node_name), ''), i.params
		from interface i
		where i.component = $1::uuid
		order by i.name asc`, c.ID)
	if err != nil {
		return nil, fmt.Errorf("storage: list interfaces for %s: %w", componentRef, err)
	}
	defer rows.Close()
	var out []ComponentInterface
	for rows.Next() {
		var it ComponentInterface
		if err := rows.Scan(&it.Name, &it.Type, &it.NodeName, &it.Params); err != nil {
			return nil, fmt.Errorf("storage: scan interface for %s: %w", componentRef, err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate interfaces for %s: %w", componentRef, err)
	}
	return out, nil
}

func mapComponentWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			switch pgErr.ConstraintName {
			case idxComponentParentName:
				return ErrComponentExistsUnderParent
			case idxComponentLocationName:
				return ErrComponentExistsInLocation
			case idxComponentOrphanName:
				return ErrComponentExistsUnplaced
			}
			return ErrComponentExists
		case "23503":
			switch pgErr.ConstraintName {
			case "component_location_id_fkey":
				return ErrLocationNotFound
			case "component_product_id_fkey":
				return ErrProductNotFound
			}
		}
	}
	return fmt.Errorf("storage: component write: %w", err)
}
