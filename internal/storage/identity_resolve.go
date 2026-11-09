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

// ResolveIdentityBindings resolves, in one read, the identities named on each
// conduit (by its id) against that conduit's bindings alone: a node's tick
// names a conduit per task group (#1582), a push names one. The result holds
// a resolution per conduit that exists; a conduit that is no uuid, or no
// longer exists, is absent and binds nothing.
func (p *PG) ResolveIdentityBindings(ctx context.Context, ids map[string][]IdentityKey) (map[string]*IdentityResolution, error) {
	var conduits, on, keys, values []string
	for c, cids := range ids {
		if !isUUID(c) || len(cids) == 0 {
			continue
		}
		conduits = append(conduits, c)
		for _, id := range cids {
			on, keys, values = append(on, c), append(keys, id.Key), append(values, id.Value)
		}
	}
	if len(conduits) == 0 {
		return nil, nil
	}
	rows, err := p.pool.Query(ctx, `
		select c.id::text, c.name, b.k, b.v, b.owner_kind, b.owner
		from component c
		left join lateral (
			select t.name as k, ib.value as v, ib.owner_kind, coalesce(ib.component_id, ib.system_id)::text as owner
			from identity_binding ib
			join property_type t on t.id = ib.property_type_id
			join unnest($2::uuid[], $3::text[], $4::text[]) as u(c, k, v) on u.c = c.id and u.k = t.name and u.v = ib.value
			where ib.conduit_id = c.id) b on true
		where c.id = any($1::uuid[])`, conduits, on, keys, values)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve identity bindings: %w", err)
	}
	defer rows.Close()
	out := make(map[string]*IdentityResolution, len(conduits))
	for rows.Next() {
		var id, name string
		var k, v, kind, owner *string
		if err := rows.Scan(&id, &name, &k, &v, &kind, &owner); err != nil {
			return nil, fmt.Errorf("storage: resolve identity bindings: %w", err)
		}
		res := out[id]
		if res == nil {
			res = &IdentityResolution{ConduitName: name, Owners: map[IdentityKey]IdentityOwner{}}
			out[id] = res
		}
		if k != nil {
			res.Owners[IdentityKey{Key: *k, Value: *v}] = IdentityOwner{Kind: *kind, ID: *owner}
		}
	}
	return out, rows.Err()
}
