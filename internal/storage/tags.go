package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/tag"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Tag-layer sentinel errors, mapped by the API to status: the non-disclosing
// 404, the readable-but-not-actionable 403, the duplicate 409, and the request
// faults (422).
var (
	ErrTagNotFound         = errors.New("storage: tag key not found")
	ErrTagExists           = errors.New("storage: tag key already exists")
	ErrTagForbidden        = errors.New("storage: action not permitted on this tag")
	ErrTagKeyInvalid       = errors.New("storage: tag key invalid")
	ErrTagAppliesToInvalid = errors.New("storage: tag applies_to invalid")
	ErrTagValueInvalid     = errors.New("storage: tag value invalid")
	ErrTagKindNotAllowed   = errors.New("storage: tag key does not apply to this entity kind")
	ErrTagBindingNotFound  = errors.New("storage: tag binding not found")
	ErrTagValueNotAllowed  = errors.New("storage: value not in this key's allowed set")
)

// Tag is one key in the governed vocabulary: its normalized name, the entity
// kinds it may apply to (empty = universal), and whether its bindings cascade to
// descendants. It owns no value; values live in tag_binding.
type Tag struct {
	ID            string
	Name          string
	AppliesTo     []string
	Propagates    bool
	AllowedValues []string // the value enum; empty means the key is free-text
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// TagSpec is the create/update input for a key.
type TagSpec struct {
	Name          string
	AppliesTo     []string
	Propagates    bool
	AllowedValues []string
}

// TagBinding is one bound value: the key it sets, the value, and the owner on
// the exclusive arc it is bound at.
type TagBinding struct {
	ID        string
	Key       string
	OwnerKind string
	OwnerID   *string
	OwnerName string
	Value     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ResolvedTag is one entry in a component's effective-tags cascade: the
// winning-or-shadowed binding, the owner it comes from, and where that owner
// sits in the chain. Band orders the tiers (0 platform, 1 location, 2 system, 3
// component) and Depth is the distance up that tier's tree; the highest band
// then lowest depth wins per key. Winner marks the resolved value; the shadowed
// entries come back too so the surface can teach the override.
type ResolvedTag struct {
	Key       string
	Value     string
	OwnerKind string
	OwnerID   *string
	OwnerName string
	Band      int
	Depth     int
	Winner    bool
}

const tagCols = `id, name, applies_to, propagates, allowed_values, created_at, updated_at`

// tagBindingConflictArc maps an owner kind to the ON CONFLICT target that
// matches its partial unique index, so an upsert lands on the one-value-per-owner
// index for that arc. The values are compile-time constants (never user input),
// keyed by an owner kind the write path has already validated.
var tagBindingConflictArc = map[string]string{
	"platform":  "(tag_id) where owner_kind = 'platform'",
	"component": "(tag_id, component_id) where owner_kind = 'component'",
	"system":    "(tag_id, system_id) where owner_kind = 'system'",
	"location":  "(tag_id, location_id) where owner_kind = 'location'",
	"node":      "(tag_id, node_id) where owner_kind = 'node'",
}

// CreateTag mints a new key in the governed vocabulary. Minting is a tenant-wide
// governance action, so it needs an all-scope create grant (tag:create); the key
// name is normalized-validated and the applies_to set checked before the write.
func (p *PG) CreateTag(ctx context.Context, actorID string, spec TagSpec, create scope.Set) (*Tag, error) {
	if !create.All {
		return nil, ErrTagForbidden
	}
	if err := tag.ValidateKey(spec.Name); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTagKeyInvalid, err)
	}
	if err := tag.ValidateAppliesTo(spec.AppliesTo); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTagAppliesToInvalid, err)
	}
	if err := tag.ValidateAllowedValues(spec.AllowedValues); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTagValueInvalid, err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create tag: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t, err := scanTagRow(tx.QueryRow(ctx, `
		insert into tag (name, applies_to, propagates, allowed_values)
		values ($1, $2, $3, $4)
		returning `+tagCols,
		spec.Name, normalizeAppliesTo(spec.AppliesTo), spec.Propagates, normalizeAppliesTo(spec.AllowedValues)))
	if err != nil {
		return nil, mapTagWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "tag", t.ID, nil, auditTag(t)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create tag: %w", err)
	}
	return t, nil
}

// ListTags returns the whole key vocabulary, ordered by name. The registry is
// tenant-wide (not ABAC-scoped), so any reader past the tag:read floor sees
// every key; the middleware is the gate.
func (p *PG) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := p.pool.Query(ctx, `select `+tagCols+` from tag order by name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list tags: %w", err)
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		t, err := scanTagRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// DistinctTagValues returns the distinct values already bound for a key, ordered,
// for the value-stage autocomplete on a free-text key (an enum key carries its
// allowed set on the row instead). An unknown key is the non-disclosing
// ErrTagNotFound; a key with no bindings yet is an empty slice.
func (p *PG) DistinctTagValues(ctx context.Context, key string) ([]string, error) {
	if _, err := loadTagByName(ctx, p.pool, key); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
		select distinct b.value
		from tag_binding b join tag t on t.id = b.tag_id
		where t.name = $1
		order by b.value`, key)
	if err != nil {
		return nil, fmt.Errorf("storage: distinct tag values: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("storage: scan distinct value: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// UpdateTag replaces a key's governance fields (applies_to, propagates); the
// name is fixed at creation. An all-scope update grant is required (tag:update).
func (p *PG) UpdateTag(ctx context.Context, actorID, name string, spec TagSpec, action scope.Set) (*Tag, error) {
	if !action.All {
		return nil, ErrTagForbidden
	}
	if err := tag.ValidateAppliesTo(spec.AppliesTo); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTagAppliesToInvalid, err)
	}
	if err := tag.ValidateAllowedValues(spec.AllowedValues); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTagValueInvalid, err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update tag: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t, err := scanTagRow(tx.QueryRow(ctx, `
		update tag set applies_to = $2, propagates = $3, allowed_values = $4, updated_at = now()
		where name = $1
		returning `+tagCols,
		name, normalizeAppliesTo(spec.AppliesTo), spec.Propagates, normalizeAppliesTo(spec.AllowedValues)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTagNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: update tag: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "tag", t.ID, nil, auditTag(t)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update tag: %w", err)
	}
	return t, nil
}

// DeleteTag removes a key from the vocabulary, cascading its bindings (the
// tag_binding FK is on delete cascade). An all-scope delete grant is required
// (tag:delete); an unknown key is the non-disclosing ErrTagNotFound.
func (p *PG) DeleteTag(ctx context.Context, actorID, name string, action scope.Set) error {
	if !action.All {
		return ErrTagForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete tag: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t, err := scanTagRow(tx.QueryRow(ctx, `delete from tag where name = $1 returning `+tagCols, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTagNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: delete tag: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "tag", t.ID, auditTag(t), nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete tag: %w", err)
	}
	return nil
}

// SetTagBinding upserts a value for a key at an owner on the exclusive arc.
// Binding a value is the ordinary entity write, so the gate is the owner's own
// update permission: the caller passes the entity's read/update scopes (a platform
// binding needs an all-scope action). The key must exist and apply to the
// owner's kind, and the value is validated. A binding already present at that
// owner has its value replaced.
func (p *PG) SetTagBinding(ctx context.Context, actorID, key, ownerKind string, ownerName *string, value string, read, action scope.Set) (*TagBinding, error) {
	if err := tag.ValidateValue(value); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTagValueInvalid, err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set tag binding: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t, err := loadTagByName(ctx, tx, key)
	if err != nil {
		return nil, err
	}
	if ownerKind != "platform" && !tag.AppliesToKind(t.AppliesTo, tag.EntityKind(ownerKind)) {
		return nil, ErrTagKindNotAllowed
	}
	if !tag.ValueAllowed(t.AllowedValues, value) {
		return nil, ErrTagValueNotAllowed
	}
	ownerID, ownerDisplay, err := resolveTagBindingOwner(ctx, tx, ownerKind, ownerName, read, action)
	if err != nil {
		return nil, err
	}

	// Atomic upsert on the per-owner partial unique index: two concurrent
	// first-time binds of the same (key, owner) both take the same conflict arc,
	// so the loser updates the winner's row instead of racing to a 500. The
	// conflict target is a compile-time constant keyed by the (validated) owner
	// kind, never user input, so interpolating it is injection-safe.
	conflict, ok := tagBindingConflictArc[ownerKind]
	if !ok {
		return nil, ErrTagForbidden
	}
	compID, sysID, locID := arcColumns(ownerKind, ownerID)
	nodeID := nodeArc(ownerKind, ownerID)
	b, err := scanBindingRow(tx.QueryRow(ctx, `
		insert into tag_binding (tag_id, owner_kind, component_id, system_id, location_id, node_id, value)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict `+conflict+` do update set value = excluded.value, updated_at = now()
		returning id, owner_kind, component_id, system_id, location_id, node_id, value, created_at, updated_at`,
		t.ID, ownerKind, compID, sysID, locID, nodeID, value))
	if err != nil {
		return nil, fmt.Errorf("storage: write tag binding: %w", err)
	}
	b.Key = key
	b.OwnerName = ownerDisplay
	if err := writeAuditRes(ctx, tx, actorID, "set", "tag_binding", b.ID, nil, auditBinding(b)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set tag binding: %w", err)
	}
	return b, nil
}

// DeleteTagBinding removes a key's value at an owner. Gated like SetTagBinding by
// the owner's update permission; an unknown key or a key with no binding at that
// owner is the non-disclosing ErrTagBindingNotFound.
func (p *PG) DeleteTagBinding(ctx context.Context, actorID, key, ownerKind string, ownerName *string, read, action scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete tag binding: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t, err := loadTagByName(ctx, tx, key)
	if err != nil {
		if errors.Is(err, ErrTagNotFound) {
			return ErrTagBindingNotFound
		}
		return err
	}
	ownerID, _, err := resolveTagBindingOwner(ctx, tx, ownerKind, ownerName, read, action)
	if err != nil {
		return err
	}
	compID, sysID, locID := arcColumns(ownerKind, ownerID)
	nodeID := nodeArc(ownerKind, ownerID)
	b, err := scanBindingRow(tx.QueryRow(ctx, `
		delete from tag_binding
		where tag_id = $1 and owner_kind = $2
		  and component_id is not distinct from $3
		  and system_id    is not distinct from $4
		  and location_id  is not distinct from $5
		  and node_id      is not distinct from $6
		returning id, owner_kind, component_id, system_id, location_id, node_id, value, created_at, updated_at`,
		t.ID, ownerKind, compID, sysID, locID, nodeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTagBindingNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: delete tag binding: %w", err)
	}
	b.Key = key
	if err := writeAuditRes(ctx, tx, actorID, "unset", "tag_binding", b.ID, auditBinding(b), nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete tag binding: %w", err)
	}
	return nil
}

// ListEntityTags returns the tags bound directly on one entity (not the resolved
// cascade), ordered by key. The entity must be within the read scope; an unknown
// or out-of-scope owner is the non-disclosing not-found for its kind. A platform
// listing (ownerName nil) needs an all-scope read.
func (p *PG) ListEntityTags(ctx context.Context, ownerKind string, ownerName *string, read scope.Set) ([]TagBinding, error) {
	ownerID, ownerDisplay, err := resolveTagBindingOwner(ctx, p.pool, ownerKind, ownerName, read, read)
	if err != nil {
		return nil, err
	}
	compID, sysID, locID := arcColumns(ownerKind, ownerID)
	nodeID := nodeArc(ownerKind, ownerID)
	rows, err := p.pool.Query(ctx, `
		select b.id, b.owner_kind, b.component_id, b.system_id, b.location_id, b.node_id, b.value, b.created_at, b.updated_at, t.name
		from tag_binding b
		join tag t on t.id = b.tag_id
		where b.owner_kind = $1
		  and b.component_id is not distinct from $2
		  and b.system_id   is not distinct from $3
		  and b.location_id is not distinct from $4
		  and b.node_id     is not distinct from $5
		order by t.name`,
		ownerKind, compID, sysID, locID, nodeID)
	if err != nil {
		return nil, fmt.Errorf("storage: list entity tags: %w", err)
	}
	defer rows.Close()
	var out []TagBinding
	for rows.Next() {
		b, key, err := scanBindingListRow(rows)
		if err != nil {
			return nil, err
		}
		b.Key = key
		b.OwnerName = ownerDisplay
		out = append(out, *b)
	}
	return out, rows.Err()
}

// ResolveTags returns the effective tags for a component: every key that
// resolves onto it down the structural cascade (platform -> location tree ->
// system tree -> component tree), keys unioning and values overriding
// most-specific-wins, with the shadowed candidates included so the surface can
// teach the override. A non-propagating key resolves only from a binding on the
// component itself. The component must be within the read scope; an out-of-scope
// component is the non-disclosing ErrComponentNotFound.
func (p *PG) ResolveTags(ctx context.Context, componentID, forSystem string, read scope.Set) ([]ResolvedTag, error) {
	in, err := inScopeTree(ctx, p.pool, componentTable, componentID, read)
	if err != nil {
		return nil, err
	}
	if !in {
		return nil, ErrComponentNotFound
	}
	rows, err := p.pool.Query(ctx, resolveTagsSQL, componentID, forSystem)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve tags: %w", err)
	}
	defer rows.Close()
	var out []ResolvedTag
	for rows.Next() {
		var (
			r         ResolvedTag
			ownerID   *string
			ownerName string
			rnk       int
		)
		if err := rows.Scan(&r.Key, &r.OwnerKind, &ownerID, &r.Band, &r.Depth, &rnk, &ownerName, &r.Value); err != nil {
			return nil, fmt.Errorf("storage: scan resolved tag: %w", err)
		}
		r.OwnerID = ownerID
		r.OwnerName = ownerName
		r.Winner = rnk == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// resolveTagsSQL walks the three owner trees up from a component, tags each owner
// with its cascade band and depth, joins the bindings owned at those scopes, and
// ranks per key (highest band, then nearest depth wins). A non-propagating key
// is admitted only from the component itself (band 3, depth 0), the flat-set
// behavior. It returns the winner and every shadowed candidate, each with its
// owner's display name. The CYCLE guards protect against a corrupted parent edge.
const resolveTagsSQL = `
with recursive
target as (
    select id, name, location_id from component where id = $1
),
-- The system band is seeded from MEMBERSHIP. Given a system ($2), it resolves
-- against that one, and only if the component is actually a member: naming a
-- system it has no binding to must not lend it configuration. Given none, it
-- falls back to the component's PRIMARY membership, which is what makes the
-- default a convenience for callers with no system in hand rather than the rule.
-- The chain stays single-valued because the rank below has no tiebreaker after
-- depth, so two seeds at the same band would resolve nondeterministically.
seed_sys as (
    select s.id
    from system s
    join system_member m on m.system_id = s.id
    join target t on t.id = m.component_id
    where case when $2::text = '' then m.is_primary else s.name = $2::text end
),
comp_chain(id, depth) as (
    select id, 0 from component where id = $1
    union all
    select c.parent_id, cc.depth + 1
    from component c join comp_chain cc on c.id = cc.id
    where c.parent_id is not null
) cycle id set comp_cyc using comp_path,
sys_chain(id, depth) as (
    select id, 0 from seed_sys
    union all
    select s.parent_id, sc.depth + 1
    from system s join sys_chain sc on s.id = sc.id
    where s.parent_id is not null
) cycle id set sys_cyc using sys_path,
loc_chain(id, depth) as (
    select location_id, 0 from target where location_id is not null
    union all
    select l.parent_id, lc.depth + 1
    from location l join loc_chain lc on l.id = lc.id
    where l.parent_id is not null
) cycle id set loc_cyc using loc_path,
owners(owner_kind, owner_id, band, depth) as (
                select 'platform',  null::uuid, 0, 0
    union all   select 'location',  id,         1, depth from loc_chain
    union all   select 'system',    id,         2, depth from sys_chain
    union all   select 'component', id,         3, depth from comp_chain
),
ranked as (
    select t.id as tag_id, t.name as key, b.owner_kind, o.owner_id, o.band, o.depth, b.value,
           row_number() over (partition by t.id order by o.band desc, o.depth asc) as rnk
    from tag_binding b
    join tag t on t.id = b.tag_id
    join owners o
      on o.owner_kind = b.owner_kind
     and o.owner_id is not distinct from coalesce(b.component_id, b.system_id, b.location_id)
    where t.propagates or (o.owner_kind = 'component' and o.depth = 0)
)
select r.key, r.owner_kind, r.owner_id, r.band, r.depth, r.rnk,
       coalesce(c.name, sy.name, l.name, '') as owner_name,
       r.value
from ranked r
left join component c on r.owner_kind = 'component' and c.id = r.owner_id
left join system    sy on r.owner_kind = 'system'   and sy.id = r.owner_id
left join location  l on r.owner_kind = 'location'  and l.id = r.owner_id
order by r.key, r.band desc, r.depth asc`

// EffectiveTags resolves the winning effective tags for a batch of owners of one
// kind in a single query: for each id, the key -> winning value map its cascade
// produces (the same union-on-key, override-on-value rule ResolveTags applies to
// one component, batched and reduced to winners only). It is the read that feeds
// the directory Tags column, so it is deliberately scopeless: the caller passes
// ids already filtered to the read scope by the list query (the same contract as
// the rowActions batch), and this resolver adds no per-id scope check. Each kind
// resolves the bands that apply to it: a component the full arc (platform, its
// location tree, its system tree, its component tree); a system platform, its own
// location tree, and its system tree (a system placed in a location inherits that
// location's tags); a location platform and its own location tree. A non-propagating
// key resolves only from the owner itself. Ids that are not valid uuids are
// dropped. Owners with no effective tags are simply absent from the map.
func (p *PG) EffectiveTags(ctx context.Context, kind string, ownerIDs []string) (map[string]map[string]string, error) {
	out := make(map[string]map[string]string, len(ownerIDs))
	ids := uuidRoots(ownerIDs)
	if len(ids) == 0 {
		return out, nil
	}
	var sql string
	switch kind {
	case "component":
		sql = effectiveComponentTagsSQL
	case "system":
		sql = effectiveSystemTagsSQL
	case "location":
		sql = effectiveLocationTagsSQL
	case "node":
		sql = effectiveNodeTagsSQL
	default:
		return nil, fmt.Errorf("storage: effective tags: unknown kind %q", kind)
	}
	rows, err := p.pool.Query(ctx, sql, ids)
	if err != nil {
		return nil, fmt.Errorf("storage: effective tags (%s): %w", kind, err)
	}
	defer rows.Close()
	for rows.Next() {
		var target, key, value string
		if err := rows.Scan(&target, &key, &value); err != nil {
			return nil, fmt.Errorf("storage: scan effective tag: %w", err)
		}
		m := out[target]
		if m == nil {
			m = make(map[string]string)
			out[target] = m
		}
		m[key] = value
	}
	return out, rows.Err()
}

// The three batch effective-tags resolvers, one per owner kind. Each threads a
// target_id through the recursive ancestor chains so many owners resolve in one
// pass, ranks per (target, key) by band desc then depth asc, and returns only the
// winner (rnk = 1). The propagates predicate admits a non-propagating key only
// from the owner itself (its own tree's band at depth 0). The CYCLE guards are
// per derivation path, so owners sharing an ancestor never trip a false cycle.

const effectiveComponentTagsSQL = `
with recursive
targets as (
    select id as target_id, id, name, location_id from component where id = any($1)
),
-- A list read has no system in hand, so every target seeds from its primary
-- membership. A component in one system, which is nearly all of them, is
-- unaffected by the distinction.
seed_sys as (
    select t.target_id, s.id
    from system s
    join system_member m on m.system_id = s.id
    join targets t on t.id = m.component_id
    where m.is_primary
),
comp_chain(target_id, id, depth) as (
    select target_id, id, 0 from targets
    union all
    select cc.target_id, c.parent_id, cc.depth + 1
    from component c join comp_chain cc on c.id = cc.id
    where c.parent_id is not null
) cycle id set comp_cyc using comp_path,
sys_chain(target_id, id, depth) as (
    select target_id, id, 0 from seed_sys
    union all
    select sc.target_id, s.parent_id, sc.depth + 1
    from system s join sys_chain sc on s.id = sc.id
    where s.parent_id is not null
) cycle id set sys_cyc using sys_path,
loc_chain(target_id, id, depth) as (
    select target_id, location_id, 0 from targets where location_id is not null
    union all
    select lc.target_id, l.parent_id, lc.depth + 1
    from location l join loc_chain lc on l.id = lc.id
    where l.parent_id is not null
) cycle id set loc_cyc using loc_path,
owners(target_id, owner_kind, owner_id, band, depth) as (
                select target_id, 'platform',  null::uuid, 0, 0 from targets
    union all   select target_id, 'location',  id, 1, depth from loc_chain
    union all   select target_id, 'system',    id, 2, depth from sys_chain
    union all   select target_id, 'component', id, 3, depth from comp_chain
),
ranked as (
    select o.target_id, t.name as key, b.value,
           row_number() over (partition by o.target_id, t.id order by o.band desc, o.depth asc) as rnk
    from tag_binding b
    join tag t on t.id = b.tag_id
    join owners o
      on o.owner_kind = b.owner_kind
     and o.owner_id is not distinct from coalesce(b.component_id, b.system_id, b.location_id)
    where t.propagates or (o.owner_kind = 'component' and o.depth = 0)
)
select target_id::text, key, value from ranked where rnk = 1 order by target_id, key`

// A node is estate-wide, not a scope tree, so its effective tags are just the
// platform layer plus its own direct bindings (a node-direct value wins over a
// propagating platform). No recursion: there is nothing above a node to inherit
// from. Targets and owner ids are node.principal_id.
const effectiveNodeTagsSQL = `
with
targets as (
    select principal_id as target_id from node where principal_id = any($1)
),
owners(target_id, owner_kind, owner_id, band) as (
                select target_id, 'platform', null::uuid, 0 from targets
    union all   select target_id, 'node',     target_id, 1 from targets
),
ranked as (
    select o.target_id, t.name as key, b.value,
           row_number() over (partition by o.target_id, t.id order by o.band desc) as rnk
    from tag_binding b
    join tag t on t.id = b.tag_id
    join owners o
      on o.owner_kind = b.owner_kind
     and o.owner_id is not distinct from b.node_id
    where t.propagates or (o.owner_kind = 'node' and o.band = 1)
)
select target_id::text, key, value from ranked where rnk = 1 order by target_id, key`

const effectiveSystemTagsSQL = `
with recursive
targets as (
    select id as target_id, id, location_id from system where id = any($1)
),
sys_chain(target_id, id, depth) as (
    select target_id, id, 0 from targets
    union all
    select sc.target_id, s.parent_id, sc.depth + 1
    from system s join sys_chain sc on s.id = sc.id
    where s.parent_id is not null
) cycle id set sys_cyc using sys_path,
loc_chain(target_id, id, depth) as (
    select target_id, location_id, 0 from targets where location_id is not null
    union all
    select lc.target_id, l.parent_id, lc.depth + 1
    from location l join loc_chain lc on l.id = lc.id
    where l.parent_id is not null
) cycle id set loc_cyc using loc_path,
owners(target_id, owner_kind, owner_id, band, depth) as (
                select target_id, 'platform', null::uuid, 0, 0 from targets
    union all   select target_id, 'location', id, 1, depth from loc_chain
    union all   select target_id, 'system',   id, 2, depth from sys_chain
),
ranked as (
    select o.target_id, t.name as key, b.value,
           row_number() over (partition by o.target_id, t.id order by o.band desc, o.depth asc) as rnk
    from tag_binding b
    join tag t on t.id = b.tag_id
    join owners o
      on o.owner_kind = b.owner_kind
     and o.owner_id is not distinct from coalesce(b.component_id, b.system_id, b.location_id)
    where t.propagates or (o.owner_kind = 'system' and o.depth = 0)
)
select target_id::text, key, value from ranked where rnk = 1 order by target_id, key`

const effectiveLocationTagsSQL = `
with recursive
loc_chain(target_id, id, depth) as (
    select id, id, 0 from location where id = any($1)
    union all
    select lc.target_id, l.parent_id, lc.depth + 1
    from location l join loc_chain lc on l.id = lc.id
    where l.parent_id is not null
) cycle id set loc_cyc using loc_path,
owners(target_id, owner_kind, owner_id, band, depth) as (
                select distinct target_id, 'platform', null::uuid, 0, 0 from loc_chain
    union all   select target_id, 'location', id, 1, depth from loc_chain
),
ranked as (
    select o.target_id, t.name as key, b.value,
           row_number() over (partition by o.target_id, t.id order by o.band desc, o.depth asc) as rnk
    from tag_binding b
    join tag t on t.id = b.tag_id
    join owners o
      on o.owner_kind = b.owner_kind
     and o.owner_id is not distinct from coalesce(b.component_id, b.system_id, b.location_id)
    where t.propagates or (o.owner_kind = 'location' and o.depth = 0)
)
select target_id::text, key, value from ranked where rnk = 1 order by target_id, key`

// --- helpers -----------------------------------------------------------------

// resolveTagBindingOwner turns an owner kind + optional name into the owning id,
// enforcing the read-then-action scope split on the owner entity: a platform
// owner needs an all-scope action (and read); a scoped one resolves its entity
// in the matching tree and requires it within read (else not-found) then action
// (else forbidden). Returns a nil id and empty display name for a platform owner.
func resolveTagBindingOwner(ctx context.Context, q querier, kind string, name *string, read, action scope.Set) (*string, string, error) {
	if kind == "platform" {
		if !action.All || !read.All {
			return nil, "", ErrTagForbidden
		}
		return nil, "", nil
	}
	if name == nil {
		return nil, "", ErrTagForbidden
	}
	switch kind {
	case "component":
		c, err := resolveScoped(ctx, q, componentConfig, *name, read, action)
		if err != nil {
			return nil, "", err
		}
		return &c.ID, c.Name, nil
	case "system":
		s, err := resolveScoped(ctx, q, systemConfig, *name, read, action)
		if err != nil {
			return nil, "", err
		}
		return &s.ID, s.Name, nil
	case "location":
		l, err := resolveScoped(ctx, q, locationConfig, *name, read, action)
		if err != nil {
			return nil, "", err
		}
		return &l.ID, l.Name, nil
	case "node":
		// A node is estate-wide (not a scope tree), so tagging it needs an all
		// scope on both legs, like a global owner; the owner id is its principal_id.
		if !read.All || !action.All {
			return nil, "", ErrNodeForbidden
		}
		var pid string
		err := q.QueryRow(ctx, `select principal_id from node where name = $1`, *name).Scan(&pid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrNodeNotFound
		} else if err != nil {
			return nil, "", fmt.Errorf("storage: resolve node tag owner %q: %w", *name, err)
		}
		return &pid, *name, nil
	}
	return nil, "", ErrTagForbidden
}

// loadTagByName loads a key row by name, returning ErrTagNotFound if absent.
func loadTagByName(ctx context.Context, q querier, name string) (*Tag, error) {
	t, err := scanTagRow(q.QueryRow(ctx, `select `+tagCols+` from tag where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTagNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: load tag %q: %w", name, err)
	}
	return t, nil
}

// normalizeAppliesTo returns a non-nil slice so a nil applies_to writes an empty
// text[] rather than SQL null (the column is not null).
func normalizeAppliesTo(kinds []string) []string {
	if kinds == nil {
		return []string{}
	}
	return kinds
}

func scanTagRow(row pgx.Row) (*Tag, error) {
	var t Tag
	if err := row.Scan(&t.ID, &t.Name, &t.AppliesTo, &t.Propagates, &t.AllowedValues, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	if t.AppliesTo == nil {
		t.AppliesTo = []string{}
	}
	if t.AllowedValues == nil {
		t.AllowedValues = []string{}
	}
	return &t, nil
}

func scanBindingRow(row pgx.Row) (*TagBinding, error) {
	var (
		b                    TagBinding
		comp, sys, loc, node *string
	)
	if err := row.Scan(&b.ID, &b.OwnerKind, &comp, &sys, &loc, &node, &b.Value, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return nil, err
	}
	b.OwnerID = firstNonNil(comp, sys, loc, node)
	return &b, nil
}

// nodeArc is the node leg of the tag-binding owner arc: the owner id when the
// owner is a node, else nil (the shared arcColumns covers component/system/
// location, which a node binding leaves null).
func nodeArc(kind string, id *string) *string {
	if kind == "node" {
		return id
	}
	return nil
}

func scanBindingListRow(row pgx.Row) (*TagBinding, string, error) {
	var (
		b                    TagBinding
		comp, sys, loc, node *string
		key                  string
	)
	if err := row.Scan(&b.ID, &b.OwnerKind, &comp, &sys, &loc, &node, &b.Value, &b.CreatedAt, &b.UpdatedAt, &key); err != nil {
		return nil, "", err
	}
	b.OwnerID = firstNonNil(comp, sys, loc, node)
	return &b, key, nil
}

func auditTag(t *Tag) map[string]any {
	return map[string]any{
		"name":           t.Name,
		"applies_to":     t.AppliesTo,
		"propagates":     t.Propagates,
		"allowed_values": t.AllowedValues,
	}
}

func auditBinding(b *TagBinding) map[string]any {
	return map[string]any{
		"key":        b.Key,
		"owner_kind": b.OwnerKind,
		"owner_id":   b.OwnerID,
		"value":      b.Value,
	}
}

func mapTagWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrTagExists
	}
	return fmt.Errorf("storage: tag write: %w", err)
}
