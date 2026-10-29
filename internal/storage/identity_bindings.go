package storage

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Identity binding sentinels (#1385, ADR-1282).
var (
	// ErrIdentityBindingNotFound is a delete naming no binding the caller may
	// see: none on this conduit for the key and value, or one whose owner is
	// outside the caller's read on its tier (the list counts that one without
	// naming it, so a delete does not name it either). A put on such an
	// identity is the move refusal instead, which tells the caller the
	// identity is bound but never to whom: accepted, since the list's hidden
	// count already says the conduit has bindings the caller cannot see.
	ErrIdentityBindingNotFound = errors.New("storage: identity binding not found")
	// ErrIdentityBindingKeyUnregistered is a match key that is no registered
	// property type: the key is typed and shared by every driver reporting it.
	ErrIdentityBindingKeyUnregistered = errors.New("storage: identity binding match key is not a registered property type")
	// ErrIdentityBindingOwnerKind is an owner that is neither a component nor a
	// system: a location or a node owns no observed sample (ADR-0422).
	ErrIdentityBindingOwnerKind = errors.New("storage: an identity binding's owner is a component or a system")
	// ErrIdentityBindingValue is an identity value outside 1 to 256 characters.
	ErrIdentityBindingValue = errors.New("storage: an identity value is 1 to 256 characters")
	// ErrIdentityBindingMoveForbidden is a put naming a different owner for a
	// bound identity, by a caller without update on the owner it is bound to
	// now. It names neither that owner nor its kind: the caller may not be able
	// to read it.
	ErrIdentityBindingMoveForbidden = errors.New("storage: moving an identity binding needs update on the owner it leaves")
	// ErrIdentityBindingOwnerIsConduit is a binding whose owner is its own
	// conduit: a fanned value never lands on the conduit it arrived through
	// (a sample with no identity already does).
	ErrIdentityBindingOwnerIsConduit = errors.New("storage: an identity binding's owner is not its own conduit")
)

// IdentityBindingValueMax is the longest identity value a binding holds, in
// characters; the table's CHECK holds the same bound.
const IdentityBindingValueMax = 256

// IdentityBindingOwner names the owner a put binds an identity to: Kind is
// "component" or "system", Name the owner's name or uuid (ADR-0062, ADR-1133).
type IdentityBindingOwner struct {
	Kind string
	Name string
}

// IdentityBinding is one (conduit, match key, value) tied to its one owner.
// The conduit and the owner each come back as uuid and name: the uuid is the
// address a console navigates by, the name what it shows.
type IdentityBinding struct {
	ID          string
	ConduitID   string
	ConduitName string
	// Key is the match key, a registered property type's name.
	Key   string
	Value string
	// OwnerKind is "component" or "system".
	OwnerKind string
	OwnerID   string
	OwnerName string
	CreatedAt time.Time
}

// IdentityBindingList is a conduit's bindings as the caller may see them:
// Bindings names each owner inside the caller's read on its tier, Hidden
// counts the bindings whose owner is outside it.
type IdentityBindingList struct {
	Bindings []IdentityBinding
	Hidden   int
}

// IdentityBindingScopes are the caller's sets for a binding write. The
// conduit resolves in ComponentRead then ComponentUpdate; a component owner in
// the same pair, a system owner in SystemRead then SystemUpdate.
type IdentityBindingScopes struct {
	ComponentRead, ComponentUpdate scope.Set
	SystemRead, SystemUpdate       scope.Set
}

// IdentityBindingOwnerError wraps a refusal from the OWNER end of a binding
// write (its not-found, forbidden or ambiguity), so the API can tell it from
// the same sentinel out of the conduit end: both ends of a component-owned
// binding are components, and a refusal that blamed the wrong one would send
// the operator to ask for the wrong grant.
type IdentityBindingOwnerError struct {
	Kind string
	Err  error
}

func (e *IdentityBindingOwnerError) Error() string {
	return fmt.Sprintf("identity binding owner (%s): %v", e.Kind, e.Err)
}
func (e *IdentityBindingOwnerError) Unwrap() error { return e.Err }

// identityBindingAudit is the audit image of a binding: both ends and the
// identity, by name, so the row reads as the relationship it records.
type identityBindingAudit struct {
	Conduit   string `json:"conduit"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	OwnerKind string `json:"owner_kind"`
	Owner     string `json:"owner"`
}

func (b *IdentityBinding) audit() identityBindingAudit {
	return identityBindingAudit{Conduit: b.ConduitName, Key: b.Key, Value: b.Value, OwnerKind: b.OwnerKind, Owner: b.OwnerName}
}

const identityBindingCols = `b.id, b.conduit_id, c.name, pt.name, b.value, b.owner_kind,
	coalesce(b.component_id, b.system_id), coalesce(oc.name, os.name), b.created_at`

const identityBindingFrom = ` from identity_binding b
	join component c on c.id = b.conduit_id
	join property_type pt on pt.id = b.property_type_id
	left join component oc on oc.id = b.component_id
	left join system os on os.id = b.system_id`

func scanIdentityBinding(row pgx.Row, extra ...any) (*IdentityBinding, error) {
	var b IdentityBinding
	dest := append([]any{&b.ID, &b.ConduitID, &b.ConduitName, &b.Key, &b.Value, &b.OwnerKind, &b.OwnerID, &b.OwnerName, &b.CreatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	return &b, nil
}

// ListIdentityBindings reads a conduit's bindings. The conduit resolves in
// componentRead (a non-disclosing not-found outside it); each binding is named
// only when its owner is inside the caller's read on its own tier
// (componentRead for a component owner, systemRead for a system owner), and
// the rest are counted. Both filters run in the one query.
func (p *PG) ListIdentityBindings(ctx context.Context, conduit string, componentRead, systemRead scope.Set) (*IdentityBindingList, error) {
	c, err := scopedByNameInScope(ctx, p.pool, componentConfig, conduit, "component", componentRead)
	if err != nil {
		return nil, err
	}
	cf := newNamedReadFilter(componentTable, componentRead, 2, "vis_comp")
	sf := newNamedReadFilter(systemTable, systemRead, 2+len(cf.Args), "vis_sys")
	args := append(append([]any{c.ID}, cf.Args...), sf.Args...)
	rows, err := p.pool.Query(ctx, joinReadFilters(cf, sf)+`
		select `+identityBindingCols+`,
			case b.owner_kind when 'component' then `+cf.Pred("b.component_id")+`
			                  else `+sf.Pred("b.system_id")+` end`+
		identityBindingFrom+`
		where b.conduit_id = $1
		order by pt.name, b.value`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list identity bindings: %w", err)
	}
	defer rows.Close()
	out := &IdentityBindingList{Bindings: []IdentityBinding{}}
	for rows.Next() {
		var visible bool
		b, err := scanIdentityBinding(rows, &visible)
		if err != nil {
			return nil, fmt.Errorf("storage: scan identity binding: %w", err)
		}
		if visible {
			out.Bindings = append(out.Bindings, *b)
		} else {
			out.Hidden++
		}
	}
	return out, rows.Err()
}

// PutIdentityBinding ties (conduit, key, value) to owner, in one transaction
// with its audit row. The conduit resolves in component read then update; the
// owner in its tier's read then update (out of read a not-found, readable but
// not writable a forbidden, each wrapped in IdentityBindingOwnerError). The
// match key must be a registered property type. A put naming the owner the
// identity is already bound to is a no-op and writes no audit row; one naming
// a different owner moves the binding, which also needs update on the owner it
// leaves. The owner is never the conduit itself (ErrIdentityBindingOwnerIsConduit).
func (p *PG) PutIdentityBinding(ctx context.Context, actorID, conduit, key, value string, owner IdentityBindingOwner, sc IdentityBindingScopes) (*IdentityBinding, error) {
	if owner.Kind != "component" && owner.Kind != "system" {
		return nil, ErrIdentityBindingOwnerKind
	}
	if err := validIdentityValue(value); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin put identity binding: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	c, err := resolveScoped(ctx, tx, componentConfig, conduit, sc.ComponentRead, sc.ComponentUpdate)
	if err != nil {
		return nil, err
	}
	keyID, err := identityKeyID(ctx, tx, key)
	if err != nil {
		return nil, err
	}
	ownerID, err := resolveIdentityOwner(ctx, tx, owner, sc)
	if err != nil {
		return nil, err
	}
	if owner.Kind == "component" && ownerID == c.ID {
		return nil, ErrIdentityBindingOwnerIsConduit
	}
	// Serialize writes to one identity, so two puts racing on an unbound value
	// cannot both read "absent" and one of them fail on the unique key.
	if err := lockAdvisory(ctx, tx, identityLockKey(c.ID, keyID, value)); err != nil {
		return nil, err
	}
	before, err := scanIdentityBinding(tx.QueryRow(ctx, `select `+identityBindingCols+identityBindingFrom+`
		where b.conduit_id = $1 and b.property_type_id = $2 and b.value = $3`, c.ID, keyID, value))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		before = nil
	case err != nil:
		return nil, fmt.Errorf("storage: read identity binding: %w", err)
	}

	componentID, systemID := identityOwnerArc(owner.Kind, ownerID)
	var id, verb string
	switch {
	case before == nil:
		verb = "create"
		if err := tx.QueryRow(ctx, `
			insert into identity_binding (conduit_id, property_type_id, value, owner_kind, component_id, system_id)
			values ($1, $2, $3, $4, $5, $6) returning id`,
			c.ID, keyID, value, owner.Kind, componentID, systemID).Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: insert identity binding: %w", err)
		}
		// The identity is placed now: its row in the conduit's inbox of
		// unbound identities (#1282) is let go in the same transaction.
		if _, err := tx.Exec(ctx, `delete from unbound_identity where conduit_id = $1 and property_type_id = $2 and value = $3`,
			c.ID, keyID, value); err != nil {
			return nil, fmt.Errorf("storage: let go of the unbound identity: %w", err)
		}
	case before.OwnerKind == owner.Kind && before.OwnerID == ownerID:
		return before, nil // already bound here: nothing changes, nothing to audit
	default:
		// A move: the owner it leaves is written too (it stops receiving this
		// identity's values), so it needs update there as well.
		ok, err := inScopeTree(ctx, tx, identityOwnerTable(before.OwnerKind), before.OwnerID, identityOwnerUpdate(before.OwnerKind, sc))
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrIdentityBindingMoveForbidden
		}
		verb, id = "update", before.ID
		tag, err := tx.Exec(ctx, `update identity_binding set owner_kind = $2, component_id = $3, system_id = $4 where id = $1`,
			id, owner.Kind, componentID, systemID)
		if err != nil {
			return nil, fmt.Errorf("storage: move identity binding: %w", err)
		}
		// The identity lock makes this unreachable today; if the row went
		// anyway, say so cleanly rather than failing the read-back below.
		if tag.RowsAffected() == 0 {
			return nil, ErrIdentityBindingNotFound
		}
	}
	after, err := scanIdentityBinding(tx.QueryRow(ctx, `select `+identityBindingCols+identityBindingFrom+` where b.id = $1`, id))
	if err != nil {
		return nil, fmt.Errorf("storage: read identity binding: %w", err)
	}
	var old any
	if before != nil {
		old = before.audit()
	}
	if err := writeAuditRes(ctx, tx, actorID, verb, "identity_binding", after.ID, old, after.audit()); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit put identity binding: %w", err)
	}
	return after, nil
}

// DeleteIdentityBinding removes the binding of (conduit, key, value), in one
// transaction with its audit row. The conduit resolves in component read then
// update. The binding's owner must be inside the caller's read on its tier,
// or the binding is not found (the list does not name it either), and inside
// the tier's update, or the delete is forbidden on the owner end: unbinding
// stops the owner receiving the identity's values, so it is a write there too.
func (p *PG) DeleteIdentityBinding(ctx context.Context, actorID, conduit, key, value string, sc IdentityBindingScopes) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete identity binding: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	c, err := resolveScoped(ctx, tx, componentConfig, conduit, sc.ComponentRead, sc.ComponentUpdate)
	if err != nil {
		return err
	}
	keyID, err := identityKeyID(ctx, tx, key)
	if errors.Is(err, ErrIdentityBindingKeyUnregistered) {
		return ErrIdentityBindingNotFound // no such key, so no binding under it
	}
	if err != nil {
		return err
	}
	// The same per-identity lock a put takes, so a delete and a moving put
	// serialize: without it the put could read the row, the delete remove it,
	// and the put's move update nothing.
	if err := lockAdvisory(ctx, tx, identityLockKey(c.ID, keyID, value)); err != nil {
		return err
	}
	before, err := scanIdentityBinding(tx.QueryRow(ctx, `select `+identityBindingCols+identityBindingFrom+`
		where b.conduit_id = $1 and b.property_type_id = $2 and b.value = $3`, c.ID, keyID, value))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIdentityBindingNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: read identity binding: %w", err)
	}
	tbl := identityOwnerTable(before.OwnerKind)
	readable, err := inScopeTree(ctx, tx, tbl, before.OwnerID, identityOwnerRead(before.OwnerKind, sc))
	if err != nil {
		return err
	}
	if !readable {
		return ErrIdentityBindingNotFound
	}
	writable, err := inScopeTree(ctx, tx, tbl, before.OwnerID, identityOwnerUpdate(before.OwnerKind, sc))
	if err != nil {
		return err
	}
	if !writable {
		return &IdentityBindingOwnerError{Kind: before.OwnerKind, Err: identityOwnerForbidden(before.OwnerKind)}
	}
	if _, err := tx.Exec(ctx, `delete from identity_binding where id = $1`, before.ID); err != nil {
		return fmt.Errorf("storage: delete identity binding: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "identity_binding", before.ID, before.audit(), nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete identity binding: %w", err)
	}
	return nil
}

// identityLockKey is the advisory lock every write to one identity takes.
func identityLockKey(conduitID, keyID, value string) string {
	return "identity_binding/" + conduitID + "/" + keyID + "/" + value
}

// identityKeyInUse counts the bindings holding a match key, for a property
// type delete to refuse with (nil when none do).
func identityKeyInUse(ctx context.Context, q txQuerier, key string) error {
	var n int
	if err := q.QueryRow(ctx, `select count(*) from identity_binding b join property_type pt on pt.id = b.property_type_id
		where pt.name = $1`, key).Scan(&n); err != nil {
		return fmt.Errorf("storage: count identity bindings on %q: %w", key, err)
	}
	if n > 0 {
		return &IdentityKeyInUseError{Key: key, Bindings: n}
	}
	return nil
}

// isIdentityKeyFK reports whether err is the match key's foreign key refusing
// a property type delete.
func isIdentityKeyFK(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == "identity_binding_property_type_id_fkey"
}

func validIdentityValue(value string) error {
	if n := utf8.RuneCountInString(value); n < 1 || n > IdentityBindingValueMax {
		return ErrIdentityBindingValue
	}
	return nil
}

// identityKeyID resolves a match key to its registered property type's id.
func identityKeyID(ctx context.Context, q txQuerier, key string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `select id from property_type where name = $1`, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrIdentityBindingKeyUnregistered
	}
	if err != nil {
		return "", fmt.Errorf("storage: resolve identity match key: %w", err)
	}
	return id, nil
}

// resolveIdentityOwner resolves the owner a put names in its own tier's read
// then update scope. The reference arrives in the request body, so it goes
// through the body-reference seam (resolvePlacementWriteRef), and every
// refusal is wrapped as the owner end's.
func resolveIdentityOwner(ctx context.Context, q txQuerier, owner IdentityBindingOwner, sc IdentityBindingScopes) (string, error) {
	var id string
	var err error
	switch owner.Kind {
	case "component":
		var v *Component
		if v, err = resolvePlacementWriteRef(ctx, q, componentConfig, owner.Name, sc.ComponentRead, sc.ComponentUpdate); err == nil {
			id = v.ID
		}
	case "system":
		var v *System
		if v, err = resolvePlacementWriteRef(ctx, q, systemConfig, owner.Name, sc.SystemRead, sc.SystemUpdate); err == nil {
			id = v.ID
		}
	default:
		return "", ErrIdentityBindingOwnerKind
	}
	if err != nil {
		return "", &IdentityBindingOwnerError{Kind: owner.Kind, Err: err}
	}
	return id, nil
}

func identityOwnerArc(kind, id string) (componentID, systemID any) {
	if kind == "component" {
		return id, nil
	}
	return nil, id
}

func identityOwnerTable(kind string) scopeTable {
	if kind == "component" {
		return componentTable
	}
	return systemTable
}

func identityOwnerRead(kind string, sc IdentityBindingScopes) scope.Set {
	if kind == "component" {
		return sc.ComponentRead
	}
	return sc.SystemRead
}

func identityOwnerUpdate(kind string, sc IdentityBindingScopes) scope.Set {
	if kind == "component" {
		return sc.ComponentUpdate
	}
	return sc.SystemUpdate
}

func identityOwnerForbidden(kind string) error {
	if kind == "component" {
		return ErrComponentForbidden
	}
	return ErrSystemForbidden
}

// IdentityKeyInUseError is a delete of a property type that identity
// bindings still use as their match key. It counts them rather than naming
// their conduits: the registry delete carries no component scope to judge
// which conduits the caller may read.
type IdentityKeyInUseError struct {
	Key      string
	Bindings int
}

func (e *IdentityKeyInUseError) Error() string {
	return fmt.Sprintf("%d identity binding(s) use %q as their match key; unbind them before deleting it", e.Bindings, e.Key)
}
