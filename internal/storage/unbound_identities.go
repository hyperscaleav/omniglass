package storage

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
)

// The conduit's inbox of unplaced identities (#1282, ADR-1282 point 7). A
// fanned sample naming an identity its conduit has no binding for lands on
// no owner: the ingest aggregates what it heard per conduit, match key and
// value, and the batch upserts it here in its own transaction, under its
// receipt, so a redelivery counts nothing twice. The inbox is bounded per
// conduit (the least recently heard evicted) and aged out, the shape of the
// unclaimed-source inbox (ADR-1024 decision 10).

// UnboundBound and UnboundMaxAge bound the inbox per conduit.
const (
	UnboundBound  = 1024
	UnboundMaxAge = 7 * 24 * time.Hour
)

// UnboundIdentityWrite is what one batch heard of one identity its conduit
// has no binding for: how many samples named it, first and last, and the
// keys they carried.
type UnboundIdentityWrite struct {
	ConduitID string
	Key       string
	Value     string
	Count     int64
	FirstAt   time.Time
	LastAt    time.Time
	Keys      []string
}

// UnboundIdentity is one row of a conduit's inbox.
type UnboundIdentity struct {
	Key          string
	Value        string
	Count        int64
	FirstHeardAt time.Time
	LastHeardAt  time.Time
	Keys         []string
}

// writeUnboundIdentities upserts a batch's unplaced identities (one statement
// for every row), then holds each conduit it touched to the bound and lets go
// of its aged rows. A key no property type holds any longer is skipped by the
// join: the inbox names only a key a binding could name.
func writeUnboundIdentities(ctx context.Context, tx pgx.Tx, us []UnboundIdentityWrite) error {
	conduits := []string{}
	ids, keys, values, firsts, lasts := make([]string, 0, len(us)), make([]string, 0, len(us)), make([]string, 0, len(us)), make([]time.Time, 0, len(us)), make([]time.Time, 0, len(us))
	counts, carried := make([]int64, 0, len(us)), make([]string, 0, len(us))
	for _, u := range us {
		if !slices.Contains(conduits, u.ConduitID) {
			conduits = append(conduits, u.ConduitID)
		}
		ks := slices.Clone(u.Keys)
		slices.Sort(ks)
		ids, keys, values = append(ids, u.ConduitID), append(keys, u.Key), append(values, u.Value)
		firsts, lasts, counts = append(firsts, u.FirstAt), append(lasts, u.LastAt), append(counts, u.Count)
		carried = append(carried, pgTextArray(slices.Compact(ks)))
	}
	if _, err := tx.Exec(ctx, `
		insert into unbound_identity (conduit_id, property_type_id, value, heard_count, first_heard_at, last_heard_at, keys_carried)
		select u.conduit::uuid, t.id, u.value, u.n, u.first_at, u.last_at, u.keys::text[]
		from unnest($1::text[], $2::text[], $3::text[], $4::bigint[], $5::timestamptz[], $6::timestamptz[], $7::text[])
			as u(conduit, key, value, n, first_at, last_at, keys)
		join property_type t on t.name = u.key
		on conflict (conduit_id, property_type_id, value) do update set
			heard_count = unbound_identity.heard_count + excluded.heard_count,
			first_heard_at = least(unbound_identity.first_heard_at, excluded.first_heard_at),
			last_heard_at = greatest(unbound_identity.last_heard_at, excluded.last_heard_at),
			keys_carried = (select coalesce(array_agg(distinct k order by k), '{}') from unnest(unbound_identity.keys_carried || excluded.keys_carried) k)`,
		ids, keys, values, counts, firsts, lasts, carried); err != nil {
		return fmt.Errorf("storage: record unbound identities: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		delete from unbound_identity u using (
			select conduit_id, property_type_id, value, row_number() over (partition by conduit_id order by last_heard_at desc, value) as n
			from unbound_identity where conduit_id = any($1::uuid[])) r
		where u.conduit_id = r.conduit_id and u.property_type_id = r.property_type_id and u.value = r.value
			and (r.n > $2 or u.last_heard_at < now() - $3::interval)`,
		conduits, UnboundBound, UnboundMaxAge.String()); err != nil {
		return fmt.Errorf("storage: bound unbound identities: %w", err)
	}
	return nil
}

// pgTextArray renders a list as a Postgres text array literal, each element
// quoted, so a batch's rows ride one array of arrays-as-text.
func pgTextArray(xs []string) string {
	out := []byte{'{'}
	for i, x := range xs {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '"')
		for _, r := range []byte(x) {
			if r == '"' || r == '\\' {
				out = append(out, '\\')
			}
			out = append(out, r)
		}
		out = append(out, '"')
	}
	return string(append(out, '}'))
}

// ListUnboundIdentities reads a conduit's inbox, most recently heard first,
// within its age. The conduit resolves inside read (component:read), so one
// outside it is the non-disclosing ErrComponentNotFound: the inbox discloses
// only what that conduit heard.
func (p *PG) ListUnboundIdentities(ctx context.Context, conduitRef string, read scope.Set) ([]UnboundIdentity, error) {
	c, err := scopedByNameInScope(ctx, p.pool, componentConfig, conduitRef, "component", read)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
		select t.name, u.value, u.heard_count, u.first_heard_at, u.last_heard_at, u.keys_carried
		from unbound_identity u join property_type t on t.id = u.property_type_id
		where u.conduit_id = $1 and u.last_heard_at >= now() - $2::interval
		order by u.last_heard_at desc, t.name, u.value`, c.ID, UnboundMaxAge.String())
	if err != nil {
		return nil, fmt.Errorf("storage: list unbound identities: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (UnboundIdentity, error) {
		var u UnboundIdentity
		err := r.Scan(&u.Key, &u.Value, &u.Count, &u.FirstHeardAt, &u.LastHeardAt, &u.Keys)
		return u, err
	})
}
