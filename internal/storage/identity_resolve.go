package storage

import (
	"context"
	"fmt"
)

// The fan-out's read (#1282, ADR-1282 point 5): every identity one batch
// names, resolved against its conduit's bindings in one statement. It runs
// in the ingest's system mode, as the rest of ingest does: the binding is
// the fence, checked when it was written (both ends' update), and the
// gateway re-reads it as the fan-out writer lands each row.

// IdentityKey is one identity a sample names: its match key and value.
type IdentityKey struct {
	Key   string
	Value string
}

// IdentityOwner is the owner a binding names, by kind and id.
type IdentityOwner struct {
	Kind string
	ID   string
}

// IdentityResolution is one batch's identities resolved on one conduit:
// the conduit's name (a fanned sample names it in source) and the owner of
// each identity bound; an identity absent from Owners is unbound.
type IdentityResolution struct {
	ConduitName string
	Owners      map[IdentityKey]IdentityOwner
}

// ResolveIdentityBindings resolves ids on the conduit (its id) in one read.
// A conduit that is no uuid, or no longer exists, resolves to nil and binds
// nothing.
func (p *PG) ResolveIdentityBindings(ctx context.Context, conduitID string, ids []IdentityKey) (*IdentityResolution, error) {
	if !isUUID(conduitID) {
		return nil, nil
	}
	keys, values := make([]string, len(ids)), make([]string, len(ids))
	for i, id := range ids {
		keys[i], values[i] = id.Key, id.Value
	}
	rows, err := p.pool.Query(ctx, `
		select c.name, b.k, b.v, b.owner_kind, b.owner
		from component c
		left join lateral (
			select t.name as k, ib.value as v, ib.owner_kind, coalesce(ib.component_id, ib.system_id)::text as owner
			from identity_binding ib
			join property_type t on t.id = ib.property_type_id
			join unnest($2::text[], $3::text[]) as u(k, v) on u.k = t.name and u.v = ib.value
			where ib.conduit_id = c.id) b on true
		where c.id = $1::uuid`, conduitID, keys, values)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve identity bindings: %w", err)
	}
	defer rows.Close()
	var res *IdentityResolution
	for rows.Next() {
		var name string
		var k, v, kind, owner *string
		if err := rows.Scan(&name, &k, &v, &kind, &owner); err != nil {
			return nil, fmt.Errorf("storage: resolve identity bindings: %w", err)
		}
		if res == nil {
			res = &IdentityResolution{ConduitName: name, Owners: map[IdentityKey]IdentityOwner{}}
		}
		if k != nil {
			res.Owners[IdentityKey{Key: *k, Value: *v}] = IdentityOwner{Kind: *kind, ID: *owner}
		}
	}
	return res, rows.Err()
}
