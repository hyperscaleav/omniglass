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
)

// Tag is one key in the governed vocabulary: its normalized name, the entity
// kinds it may apply to (empty = universal), and whether its bindings cascade to
// descendants. It owns no value; values live in tag_binding.
type Tag struct {
	ID         string
	Name       string
	AppliesTo  []string
	Propagates bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// TagSpec is the create/update input for a key.
type TagSpec struct {
	Name       string
	AppliesTo  []string
	Propagates bool
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
// sits in the chain. Band orders the tiers (0 global, 1 location, 2 system, 3
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

const tagCols = `id, name, applies_to, propagates, created_at, updated_at`

// tagBindingConflictArc maps an owner kind to the ON CONFLICT target that
// matches its partial unique index, so an upsert lands on the one-value-per-owner
// index for that arc. The values are compile-time constants (never user input),
// keyed by an owner kind the write path has already validated.
var tagBindingConflictArc = map[string]string{
	"global":    "(tag_id) where owner_kind = 'global'",
	"component": "(tag_id, component_id) where owner_kind = 'component'",
	"system":    "(tag_id, system_id) where owner_kind = 'system'",
	"location":  "(tag_id, location_id) where owner_kind = 'location'",
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

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create tag: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t, err := scanTagRow(tx.QueryRow(ctx, `
		insert into tag (name, applies_to, propagates)
		values ($1, $2, $3)
		returning `+tagCols,
		spec.Name, normalizeAppliesTo(spec.AppliesTo), spec.Propagates))
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

// UpdateTag replaces a key's governance fields (applies_to, propagates); the
// name is fixed at creation. An all-scope update grant is required (tag:update).
func (p *PG) UpdateTag(ctx context.Context, actorID, name string, spec TagSpec, action scope.Set) (*Tag, error) {
	if !action.All {
		return nil, ErrTagForbidden
	}
	if err := tag.ValidateAppliesTo(spec.AppliesTo); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTagAppliesToInvalid, err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update tag: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t, err := scanTagRow(tx.QueryRow(ctx, `
		update tag set applies_to = $2, propagates = $3, updated_at = now()
		where name = $1
		returning `+tagCols,
		name, normalizeAppliesTo(spec.AppliesTo), spec.Propagates))
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
// update permission: the caller passes the entity's read/update scopes (a global
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
	if ownerKind != "global" && !tag.AppliesToKind(t.AppliesTo, tag.EntityKind(ownerKind)) {
		return nil, ErrTagKindNotAllowed
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
	b, err := scanBindingRow(tx.QueryRow(ctx, `
		insert into tag_binding (tag_id, owner_kind, component_id, system_id, location_id, value)
		values ($1, $2, $3, $4, $5, $6)
		on conflict `+conflict+` do update set value = excluded.value, updated_at = now()
		returning id, owner_kind, component_id, system_id, location_id, value, created_at, updated_at`,
		t.ID, ownerKind, compID, sysID, locID, value))
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
	b, err := scanBindingRow(tx.QueryRow(ctx, `
		delete from tag_binding
		where tag_id = $1 and owner_kind = $2
		  and component_id is not distinct from $3
		  and system_id    is not distinct from $4
		  and location_id  is not distinct from $5
		returning id, owner_kind, component_id, system_id, location_id, value, created_at, updated_at`,
		t.ID, ownerKind, compID, sysID, locID))
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
// or out-of-scope owner is the non-disclosing not-found for its kind. A global
// listing (ownerName nil) needs an all-scope read.
func (p *PG) ListEntityTags(ctx context.Context, ownerKind string, ownerName *string, read scope.Set) ([]TagBinding, error) {
	ownerID, ownerDisplay, err := resolveTagBindingOwner(ctx, p.pool, ownerKind, ownerName, read, read)
	if err != nil {
		return nil, err
	}
	compID, sysID, locID := arcColumns(ownerKind, ownerID)
	rows, err := p.pool.Query(ctx, `
		select b.id, b.owner_kind, b.component_id, b.system_id, b.location_id, b.value, b.created_at, b.updated_at, t.name
		from tag_binding b
		join tag t on t.id = b.tag_id
		where b.owner_kind = $1
		  and b.component_id is not distinct from $2
		  and b.system_id   is not distinct from $3
		  and b.location_id is not distinct from $4
		order by t.name`,
		ownerKind, compID, sysID, locID)
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
// resolves onto it down the structural cascade (global -> location tree ->
// system tree -> component tree), keys unioning and values overriding
// most-specific-wins, with the shadowed candidates included so the surface can
// teach the override. A non-propagating key resolves only from a binding on the
// component itself. The component must be within the read scope; an out-of-scope
// component is the non-disclosing ErrComponentNotFound.
func (p *PG) ResolveTags(ctx context.Context, componentID string, read scope.Set) ([]ResolvedTag, error) {
	in, err := inScopeTree(ctx, p.pool, componentTable, componentID, read)
	if err != nil {
		return nil, err
	}
	if !in {
		return nil, ErrComponentNotFound
	}
	rows, err := p.pool.Query(ctx, resolveTagsSQL, componentID)
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
    select id, system_id, location_id from component where id = $1
),
comp_chain(id, depth) as (
    select id, 0 from component where id = $1
    union all
    select c.parent_id, cc.depth + 1
    from component c join comp_chain cc on c.id = cc.id
    where c.parent_id is not null
) cycle id set comp_cyc using comp_path,
sys_chain(id, depth) as (
    select system_id, 0 from target where system_id is not null
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
                select 'global',    null::uuid, 0, 0
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

// --- helpers -----------------------------------------------------------------

// resolveTagBindingOwner turns an owner kind + optional name into the owning id,
// enforcing the read-then-action scope split on the owner entity: a global
// owner needs an all-scope action (and read); a scoped one resolves its entity
// in the matching tree and requires it within read (else not-found) then action
// (else forbidden). Returns a nil id and empty display name for a global owner.
func resolveTagBindingOwner(ctx context.Context, q querier, kind string, name *string, read, action scope.Set) (*string, string, error) {
	if kind == "global" {
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
	if err := row.Scan(&t.ID, &t.Name, &t.AppliesTo, &t.Propagates, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	if t.AppliesTo == nil {
		t.AppliesTo = []string{}
	}
	return &t, nil
}

func scanBindingRow(row pgx.Row) (*TagBinding, error) {
	var (
		b              TagBinding
		comp, sys, loc *string
	)
	if err := row.Scan(&b.ID, &b.OwnerKind, &comp, &sys, &loc, &b.Value, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return nil, err
	}
	b.OwnerID = firstNonNil(comp, sys, loc)
	return &b, nil
}

func scanBindingListRow(row pgx.Row) (*TagBinding, string, error) {
	var (
		b              TagBinding
		comp, sys, loc *string
		key            string
	)
	if err := row.Scan(&b.ID, &b.OwnerKind, &comp, &sys, &loc, &b.Value, &b.CreatedAt, &b.UpdatedAt, &key); err != nil {
		return nil, "", err
	}
	b.OwnerID = firstNonNil(comp, sys, loc)
	return &b, key, nil
}

func auditTag(t *Tag) map[string]any {
	return map[string]any{
		"name":       t.Name,
		"applies_to": t.AppliesTo,
		"propagates": t.Propagates,
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
