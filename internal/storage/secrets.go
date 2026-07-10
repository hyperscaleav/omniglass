package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Secret-layer sentinel errors. The API maps them to status: the non-disclosing
// 404, the readable-but-not-actionable 403, the request faults (422), and the
// misconfiguration 500 when the server has no key provider.
var (
	ErrSecretNotFound      = errors.New("storage: secret not found")
	ErrSecretForbidden     = errors.New("storage: action not permitted on this secret")
	ErrSecretExists        = errors.New("storage: secret name already exists at this scope")
	ErrUnknownSecretType   = errors.New("storage: unknown secret_type")
	ErrSecretOwnerNotFound = errors.New("storage: secret owner not found")
	ErrSecretFieldInvalid  = errors.New("storage: secret field invalid for its type")
	ErrNoSecretProvider    = errors.New("storage: no secret key provider configured")
)

// SecretType is a registry row: the named, per-field-typed shape a secret takes
// (snmp-community, basic-auth, ...). Fields carry the per-field secrecy and
// origin that drive encryption, masking, and the create gate. official marks the
// ship-with set, mirroring the other type registries.
type SecretType struct {
	ID          string
	Official    bool
	DisplayName string
	Fields      []secret.Field
}

// Shape adapts the registry row to the pure secret.Shape the primitive validates
// and masks against, so the crypto core never imports storage.
func (st SecretType) Shape() secret.Shape {
	return secret.Shape{Name: st.ID, Official: st.Official, Fields: st.Fields}
}

// Secret is one cascaded, encrypted value: its name (the cascade key), its type,
// its owner on the exclusive arc, and its per-field value map. A secret field's
// entry holds the {ct, nonce, wdek, kid} envelope; a non-secret field's entry
// holds its plaintext scalar. The plaintext of a secret field never leaves the
// envelope in this struct.
type Secret struct {
	ID         string
	Name       string
	SecretType string
	OwnerKind  string  // global | component | system | location
	OwnerID    *string // the owning entity id; nil for the global singleton
	OwnerName  string  // the owning entity's name (empty for global), for display
	Fields     []ResolvedField
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SecretSpec is the create input. OwnerName is the owning entity's name
// (resolved to its id), nil for a global secret. Fields is the operator's
// plaintext field map, validated and sealed against the type's shape.
type SecretSpec struct {
	Name       string
	SecretType string
	OwnerKind  string
	OwnerName  *string
	Fields     map[string]string
}

// ResolvedField is one field as displayed: its name, its display value (the
// plaintext for a non-secret field, the fixed mask for a secret one), and
// whether it is secret. The real value of a secret field materializes only on
// the audited reveal path, never here.
type ResolvedField struct {
	Name   string
	Value  string
	Secret bool
}

// ResolvedSecret is one entry in a component's effective-secrets cascade: the
// winning-or-shadowed secret, the owner it comes from, and where that owner sits
// in the chain. Band orders the tiers (0 global, 1 location, 2 system, 3
// component) and Depth is the distance up that tier's tree (0 at the nearest
// node); the highest band then lowest depth wins. Winner marks the resolved
// value; the shadowed entries are returned too so the surface can teach the
// override.
type ResolvedSecret struct {
	ID         string
	Name       string
	SecretType string
	OwnerKind  string
	OwnerID    *string
	OwnerName  string
	Band       int
	Depth      int
	Winner     bool
	Fields     []ResolvedField
}

// --- secret_type registry ----------------------------------------------------

func (p *PG) UpsertSecretType(ctx context.Context, st SecretType) error {
	schema, err := json.Marshal(st.Fields)
	if err != nil {
		return fmt.Errorf("storage: marshal secret_type %q schema: %w", st.ID, err)
	}
	if _, err := p.pool.Exec(ctx, `
		insert into secret_type (id, official, display_name, schema)
		values ($1, $2, $3, $4)
		on conflict (id) do update
			set official = excluded.official, display_name = excluded.display_name, schema = excluded.schema`,
		st.ID, st.Official, st.DisplayName, schema); err != nil {
		return fmt.Errorf("storage: upsert secret_type %q: %w", st.ID, err)
	}
	return nil
}

func (p *PG) ListSecretTypes(ctx context.Context) ([]SecretType, error) {
	rows, err := p.pool.Query(ctx, `select id, official, display_name, schema from secret_type order by id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list secret_types: %w", err)
	}
	defer rows.Close()
	var out []SecretType
	for rows.Next() {
		st, err := scanSecretType(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	return out, rows.Err()
}

func (p *PG) GetSecretType(ctx context.Context, id string) (*SecretType, error) {
	st, err := scanSecretType(p.pool.QueryRow(ctx,
		`select id, official, display_name, schema from secret_type where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnknownSecretType
	}
	return st, err
}

func scanSecretType(row pgx.Row) (*SecretType, error) {
	var st SecretType
	var schema []byte
	if err := row.Scan(&st.ID, &st.Official, &st.DisplayName, &schema); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(schema, &st.Fields); err != nil {
		return nil, fmt.Errorf("storage: unmarshal secret_type %q schema: %w", st.ID, err)
	}
	return &st, nil
}

// --- secret CRUD -------------------------------------------------------------

const secretCols = `id, name, secret_type, owner_kind, component_id, system_id, location_id, value, created_at, updated_at`

// CreateSecret seals a new secret at its owner scope. The owner is resolved and
// scope-checked (a global secret needs an all create scope; a scoped one needs
// its owner within the create scope), the fields are validated and sealed
// against the type shape, and the row plus its audit are written in one
// transaction. Secret fields are AES-256-GCM sealed with their (owner, name,
// field) bound as AAD, so a ciphertext cannot be lifted into another row.
func (p *PG) CreateSecret(ctx context.Context, actorID string, spec SecretSpec, create scope.Set) (*Secret, error) {
	if p.secret == nil {
		return nil, ErrNoSecretProvider
	}
	st, err := p.GetSecretType(ctx, spec.SecretType)
	if err != nil {
		return nil, err // ErrUnknownSecretType -> 422
	}
	shape := st.Shape()
	if err := shape.ValidateInput(spec.Fields); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSecretFieldInvalid, err)
	}
	for _, f := range shape.OperatorFields() {
		if _, ok := spec.Fields[f.Name]; !ok {
			return nil, fmt.Errorf("%w: missing field %q", ErrSecretFieldInvalid, f.Name)
		}
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create secret: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ownerID, err := p.resolveSecretOwner(ctx, tx, spec.OwnerKind, spec.OwnerName, create)
	if err != nil {
		return nil, err
	}

	value, err := p.sealFields(ctx, shape, spec.OwnerKind, ownerID, spec.Name, spec.Fields)
	if err != nil {
		return nil, err
	}
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("storage: marshal secret value: %w", err)
	}

	compID, sysID, locID := arcColumns(spec.OwnerKind, ownerID)
	s, err := scanSecretRow(tx.QueryRow(ctx, `
		insert into secret (name, secret_type, owner_kind, component_id, system_id, location_id, value)
		values ($1, $2, $3, $4, $5, $6, $7)
		returning `+secretCols,
		spec.Name, spec.SecretType, spec.OwnerKind, compID, sysID, locID, valueJSON), shape)
	if err != nil {
		return nil, mapSecretWriteErr(err)
	}
	// The audit records the metadata only; the sealed value is never logged.
	if err := writeAuditRes(ctx, tx, actorID, "create", "secret", s.ID, nil, auditSecret(s)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create secret: %w", err)
	}
	s.OwnerName = ownerNameOf(spec.OwnerName)
	return s, nil
}

// ListSecrets returns every secret with masked fields (the admin directory).
// Requires an all-scope read: a secret is owned across three trees plus a global
// tier, so slice-1 lists it only for the all-scope operator; the scoped,
// per-component view is ResolveSecrets. A non-all read is ErrSecretForbidden.
func (p *PG) ListSecrets(ctx context.Context, read scope.Set) ([]Secret, error) {
	if !read.All {
		return nil, ErrSecretForbidden
	}
	rows, err := p.pool.Query(ctx, `
		select `+secretColsQualified("s")+`,
		       coalesce(c.name, sy.name, l.name, '') as owner_name
		from secret s
		left join component c on s.component_id = c.id
		left join system    sy on s.system_id   = sy.id
		left join location  l on s.location_id  = l.id
		order by s.name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list secrets: %w", err)
	}
	defer rows.Close()
	shapes, err := p.shapeIndex(ctx)
	if err != nil {
		return nil, err
	}
	var out []Secret
	for rows.Next() {
		s, name, err := scanSecretListRow(rows, shapes)
		if err != nil {
			return nil, err
		}
		s.OwnerName = name
		out = append(out, *s)
	}
	return out, rows.Err()
}

// DeleteSecret removes a secret by id, audited. The owner must be within the
// action scope (all for a global secret); an unknown id or one out of read scope
// is the non-disclosing ErrSecretNotFound.
func (p *PG) DeleteSecret(ctx context.Context, actorID, id string, read, action scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete secret: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := p.secretForAction(ctx, tx, id, read, action)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from secret where id = $1`, before.ID); err != nil {
		return fmt.Errorf("storage: delete secret: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "secret", before.ID, auditSecret(before), nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete secret: %w", err)
	}
	return nil
}

// UpdateSecret replaces the given field values on a secret, re-sealing any
// secret fields, audited. Only field values change; name, type, and owner are
// fixed at creation. A partial map merges over the stored value, so an omitted
// field keeps its value (a secret field left blank stays as it was). Requires the
// owner within the action scope; an unknown or out-of-scope id is
// ErrSecretNotFound.
func (p *PG) UpdateSecret(ctx context.Context, actorID, id string, fields map[string]string, read, action scope.Set) (*Secret, error) {
	if p.secret == nil {
		return nil, ErrNoSecretProvider
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update secret: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, shape, err := p.secretRowForAction(ctx, tx, id, read, action)
	if err != nil {
		return nil, err
	}
	if err := shape.ValidateInput(fields); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSecretFieldInvalid, err)
	}
	// Seal the provided fields (same owner|name|field AAD) and merge over the
	// stored value, so the untouched fields are preserved.
	sealed, err := p.sealFields(ctx, shape, row.ownerKind, row.ownerID, row.name, fields)
	if err != nil {
		return nil, err
	}
	for k, v := range sealed {
		row.value[k] = v
	}
	valueJSON, err := json.Marshal(row.value)
	if err != nil {
		return nil, fmt.Errorf("storage: marshal secret value: %w", err)
	}
	s, err := scanSecretRow(tx.QueryRow(ctx, `
		update secret set value = $2, updated_at = now()
		where id = $1
		returning `+secretCols, id, valueJSON), shape)
	if err != nil {
		return nil, mapSecretWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "secret", s.ID, nil, auditSecret(s)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update secret: %w", err)
	}
	return s, nil
}

// RevealSecret decrypts a secret's fields and returns their plaintext, auditing
// the decrypt. This is the real-crypto read path: secret fields are unsealed
// through the provider (the (owner, name, field) AAD must match), non-secret
// fields are returned as-is. Requires the owner within the action scope; an
// unknown or out-of-scope id is ErrSecretNotFound. Every reveal is audited (no
// token cache in slice 1).
func (p *PG) RevealSecret(ctx context.Context, actorID, id string, read, action scope.Set) (map[string]string, error) {
	if p.secret == nil {
		return nil, ErrNoSecretProvider
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin reveal secret: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, shape, err := p.secretRowForAction(ctx, tx, id, read, action)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(row.value))
	for _, f := range shape.Fields {
		raw, ok := row.value[f.Name]
		if !ok {
			continue
		}
		if !f.Secret {
			var v string
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, fmt.Errorf("storage: decode secret field %q: %w", f.Name, err)
			}
			out[f.Name] = v
			continue
		}
		var env secret.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("storage: decode envelope %q: %w", f.Name, err)
		}
		pt, err := secret.Open(ctx, p.secret, env, secretAAD(row.ownerKind, row.ownerID, row.name, f.Name))
		if err != nil {
			return nil, fmt.Errorf("storage: unseal secret field %q: %w", f.Name, err)
		}
		out[f.Name] = string(pt)
	}
	if err := writeAuditRes(ctx, tx, actorID, "reveal", "secret", row.id, nil, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit reveal secret: %w", err)
	}
	return out, nil
}

// --- cascade resolver --------------------------------------------------------

// ResolveSecrets returns the effective secrets for a component: every secret
// that resolves onto it down the structural cascade (global -> location tree ->
// system tree -> component tree, deepest and most-specific winning), each masked,
// with the shadowed candidates included so the surface can teach the override.
// The component must be within the read scope (a secret that cascades from a
// broader tier is legitimately visible on a component the caller can see); an
// out-of-scope component is the non-disclosing ErrComponentNotFound.
func (p *PG) ResolveSecrets(ctx context.Context, componentID string, read scope.Set) ([]ResolvedSecret, error) {
	in, err := inScopeTree(ctx, p.pool, componentTable, componentID, read)
	if err != nil {
		return nil, err
	}
	if !in {
		return nil, ErrComponentNotFound
	}
	shapes, err := p.shapeIndex(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, resolveSecretsSQL, componentID)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve secrets: %w", err)
	}
	defer rows.Close()
	var out []ResolvedSecret
	for rows.Next() {
		var (
			r         ResolvedSecret
			ownerID   *string
			ownerName string
			rnk       int
			value     []byte
		)
		if err := rows.Scan(&r.ID, &r.Name, &r.SecretType, &r.OwnerKind, &ownerID,
			&r.Band, &r.Depth, &rnk, &ownerName, &value); err != nil {
			return nil, fmt.Errorf("storage: scan resolved secret: %w", err)
		}
		r.OwnerID = ownerID
		r.OwnerName = ownerName
		r.Winner = rnk == 1
		r.Fields = maskValue(shapes[r.SecretType], value)
		out = append(out, r)
	}
	return out, rows.Err()
}

// resolveSecretsSQL walks the three owner trees up from a component, tags each
// owner with its cascade band and depth, joins the secrets owned at those scopes,
// and ranks per name (highest band, then nearest depth wins). It returns the
// winner and every shadowed candidate, each with its owner's display name. The
// CYCLE guards protect against a corrupted parent edge.
const resolveSecretsSQL = `
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
    select s.id, s.name, s.secret_type, s.owner_kind, o.owner_id, o.band, o.depth, s.value,
           row_number() over (partition by s.name order by o.band desc, o.depth asc) as rnk
    from secret s
    join owners o
      on o.owner_kind = s.owner_kind
     and o.owner_id is not distinct from coalesce(s.component_id, s.system_id, s.location_id)
)
select r.id, r.name, r.secret_type, r.owner_kind, r.owner_id, r.band, r.depth, r.rnk,
       coalesce(c.name, sy.name, l.name, '') as owner_name,
       r.value
from ranked r
left join component c on r.owner_kind = 'component' and c.id = r.owner_id
left join system    sy on r.owner_kind = 'system'   and sy.id = r.owner_id
left join location  l on r.owner_kind = 'location'  and l.id = r.owner_id
order by r.name, r.band desc, r.depth asc`

// --- helpers -----------------------------------------------------------------

// resolveSecretOwner turns an owner kind + optional name into the owning id,
// enforcing the create scope: a global secret needs an all create scope; a
// scoped one resolves its owner in the matching tree and requires it within the
// create scope. Returns a nil id for a global owner.
func (p *PG) resolveSecretOwner(ctx context.Context, q querier, kind string, name *string, create scope.Set) (*string, error) {
	if kind == "global" {
		if !create.All {
			return nil, ErrSecretForbidden
		}
		return nil, nil
	}
	if name == nil {
		return nil, ErrSecretOwnerNotFound
	}
	var (
		id  string
		err error
	)
	switch kind {
	case "component":
		var c *Component
		c, err = scopedByName(ctx, q, componentConfig, *name)
		if c != nil {
			id = c.ID
		}
	case "system":
		var s *System
		s, err = scopedByName(ctx, q, systemConfig, *name)
		if s != nil {
			id = s.ID
		}
	case "location":
		var l *Location
		l, err = scopedByName(ctx, q, locationConfig, *name)
		if l != nil {
			id = l.ID
		}
	default:
		return nil, ErrSecretOwnerNotFound
	}
	if err != nil {
		if errors.Is(err, ErrComponentNotFound) || errors.Is(err, ErrSystemNotFound) || errors.Is(err, ErrLocationNotFound) {
			return nil, ErrSecretOwnerNotFound
		}
		return nil, err
	}
	tbl, _ := scopeKindTable(kind)
	inScope, err := inScopeTree(ctx, q, tbl, id, create)
	if err != nil {
		return nil, err
	}
	if !inScope {
		return nil, ErrSecretForbidden
	}
	return &id, nil
}

// sealFields builds the stored value map: a secret field is sealed into its
// envelope (AAD-bound to owner|name|field), a non-secret field is stored as its
// plaintext scalar. Only fields the operator supplied are stored.
func (p *PG) sealFields(ctx context.Context, shape secret.Shape, ownerKind string, ownerID *string, name string, fields map[string]string) (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(fields))
	for _, f := range shape.Fields {
		v, ok := fields[f.Name]
		if !ok {
			continue
		}
		if !f.Secret {
			raw, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("storage: encode secret field %q: %w", f.Name, err)
			}
			out[f.Name] = raw
			continue
		}
		env, err := secret.Seal(ctx, p.secret, []byte(v), secretAAD(ownerKind, ownerID, name, f.Name))
		if err != nil {
			return nil, fmt.Errorf("storage: seal secret field %q: %w", f.Name, err)
		}
		raw, err := json.Marshal(env)
		if err != nil {
			return nil, fmt.Errorf("storage: encode envelope %q: %w", f.Name, err)
		}
		out[f.Name] = raw
	}
	return out, nil
}

// secretAAD binds a sealed field to its owner arc, name, and field, so a
// ciphertext authenticates only in the exact row it was sealed for.
func secretAAD(ownerKind string, ownerID *string, name, field string) []byte {
	oid := "global"
	if ownerID != nil {
		oid = *ownerID
	}
	return []byte(ownerKind + "|" + oid + "|" + name + "|" + field)
}

// arcColumns maps an owner kind + id to the three nullable arc columns, exactly
// one set for a scoped owner, all null for the global singleton.
func arcColumns(kind string, id *string) (comp, sys, loc *string) {
	switch kind {
	case "component":
		return id, nil, nil
	case "system":
		return nil, id, nil
	case "location":
		return nil, nil, id
	}
	return nil, nil, nil
}

func ownerNameOf(name *string) string {
	if name == nil {
		return ""
	}
	return *name
}

// secretRow is the raw scanned secret plus its decoded value map, used by the
// action-scoped read paths (delete, reveal) before masking or unsealing.
type secretRow struct {
	id        string
	name      string
	ownerKind string
	ownerID   *string
	value     map[string]json.RawMessage
}

// secretForAction fetches a secret by id and enforces the read-then-action scope
// split on its owner, returning the masked Secret for the audit before-image.
func (p *PG) secretForAction(ctx context.Context, q querier, id string, read, action scope.Set) (*Secret, error) {
	row, shape, err := p.secretRowForAction(ctx, q, id, read, action)
	if err != nil {
		return nil, err
	}
	s := &Secret{
		ID: row.id, Name: row.name, OwnerKind: row.ownerKind, OwnerID: row.ownerID,
		Fields: maskValueMap(shape, row.value),
	}
	return s, nil
}

// secretRowForAction is the shared fetch-and-scope-check: the secret is read
// (owner in read scope, else the non-disclosing not-found) then gated for the
// action (owner in action scope, else forbidden). A global secret needs the
// all scope on each leg.
func (p *PG) secretRowForAction(ctx context.Context, q querier, id string, read, action scope.Set) (secretRow, secret.Shape, error) {
	var (
		row       secretRow
		secType   string
		comp, sys, loc *string
		value     []byte
	)
	err := q.QueryRow(ctx, `
		select id, name, secret_type, owner_kind, component_id, system_id, location_id, value
		from secret where id = $1`, id).
		Scan(&row.id, &row.name, &secType, &row.ownerKind, &comp, &sys, &loc, &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return secretRow{}, secret.Shape{}, ErrSecretNotFound
	}
	if err != nil {
		return secretRow{}, secret.Shape{}, fmt.Errorf("storage: get secret: %w", err)
	}
	row.ownerID = firstNonNil(comp, sys, loc)
	if err := json.Unmarshal(value, &row.value); err != nil {
		return secretRow{}, secret.Shape{}, fmt.Errorf("storage: decode secret value: %w", err)
	}
	if ok, err := p.secretOwnerInScope(ctx, q, row.ownerKind, row.ownerID, read); err != nil {
		return secretRow{}, secret.Shape{}, err
	} else if !ok {
		return secretRow{}, secret.Shape{}, ErrSecretNotFound
	}
	if ok, err := p.secretOwnerInScope(ctx, q, row.ownerKind, row.ownerID, action); err != nil {
		return secretRow{}, secret.Shape{}, err
	} else if !ok {
		return secretRow{}, secret.Shape{}, ErrSecretForbidden
	}
	st, err := p.GetSecretType(ctx, secType)
	if err != nil {
		return secretRow{}, secret.Shape{}, err
	}
	return row, st.Shape(), nil
}

// secretOwnerInScope reports whether a secret's owner falls within a scope set:
// a global secret needs the all scope; a scoped one defers to the owner tree.
func (p *PG) secretOwnerInScope(ctx context.Context, q querier, kind string, id *string, set scope.Set) (bool, error) {
	if kind == "global" {
		return set.All, nil
	}
	if id == nil {
		return false, nil
	}
	tbl, ok := scopeKindTable(kind)
	if !ok {
		return false, nil
	}
	return inScopeTree(ctx, q, tbl, *id, set)
}

// shapeIndex loads every secret_type shape keyed by id, for masking a batch of
// resolved or listed secrets without a query per row.
func (p *PG) shapeIndex(ctx context.Context) (map[string]secret.Shape, error) {
	types, err := p.ListSecretTypes(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]secret.Shape, len(types))
	for _, st := range types {
		out[st.ID] = st.Shape()
	}
	return out, nil
}

// maskValue turns a raw jsonb value into masked display fields in shape order:
// a secret field renders as the mask, a non-secret field as its plaintext.
func maskValue(shape secret.Shape, value []byte) []ResolvedField {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(value, &m); err != nil {
		return nil
	}
	return maskValueMap(shape, m)
}

func maskValueMap(shape secret.Shape, m map[string]json.RawMessage) []ResolvedField {
	out := make([]ResolvedField, 0, len(shape.Fields))
	for _, f := range shape.Fields {
		raw, ok := m[f.Name]
		if !ok {
			continue
		}
		rf := ResolvedField{Name: f.Name, Secret: f.Secret, Value: secret.Masked}
		if !f.Secret {
			var v string
			_ = json.Unmarshal(raw, &v)
			rf.Value = v
		}
		out = append(out, rf)
	}
	return out
}

func firstNonNil(vals ...*string) *string {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// scanSecretRow scans a CREATE returning-row into a masked Secret.
func scanSecretRow(row pgx.Row, shape secret.Shape) (*Secret, error) {
	var (
		s              Secret
		comp, sys, loc *string
		value          []byte
	)
	if err := row.Scan(&s.ID, &s.Name, &s.SecretType, &s.OwnerKind, &comp, &sys, &loc, &value, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	s.OwnerID = firstNonNil(comp, sys, loc)
	s.Fields = maskValue(shape, value)
	return &s, nil
}

func scanSecretListRow(row pgx.Row, shapes map[string]secret.Shape) (*Secret, string, error) {
	var (
		s              Secret
		comp, sys, loc *string
		value          []byte
		ownerName      string
	)
	if err := row.Scan(&s.ID, &s.Name, &s.SecretType, &s.OwnerKind, &comp, &sys, &loc, &value, &s.CreatedAt, &s.UpdatedAt, &ownerName); err != nil {
		return nil, "", err
	}
	s.OwnerID = firstNonNil(comp, sys, loc)
	s.Fields = maskValue(shapes[s.SecretType], value)
	return &s, ownerName, nil
}

func secretColsQualified(alias string) string {
	return alias + ".id, " + alias + ".name, " + alias + ".secret_type, " + alias + ".owner_kind, " +
		alias + ".component_id, " + alias + ".system_id, " + alias + ".location_id, " +
		alias + ".value, " + alias + ".created_at, " + alias + ".updated_at"
}

// auditSecret is the audit projection: metadata only, never the sealed value.
func auditSecret(s *Secret) map[string]any {
	return map[string]any{
		"name":        s.Name,
		"secret_type": s.SecretType,
		"owner_kind":  s.OwnerKind,
		"owner_id":    s.OwnerID,
	}
}

func mapSecretWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ErrSecretExists
		case "23503":
			return ErrUnknownSecretType
		}
	}
	return fmt.Errorf("storage: secret write: %w", err)
}
