package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// The view reads: the fleet-wide scoped queries the default views run. Each is
// one round trip with the caller's resolved read scope injected into the query
// itself (the same subtree-expansion arms scopedListSQL uses), so a view can
// never return a row its caller could not read one by one.

// InterfaceReachability is one component-reachability row: an interface with
// its latest interface.reachable verdict. A never-probed interface has an
// empty Value and a nil TS; the view layer renders that as the explicit
// unknown state.
type InterfaceReachability struct {
	Component string     // the owning component name
	Interface string     // the interface name (the sample instance)
	Type      string     // the interface type word (icmp, tcp, ...)
	Value     string     // the latest verdict (up/down), empty when never probed
	TS        *time.Time // when the latest verdict was observed, nil when never probed
}

// ListInterfaceReachability returns every in-scope component's interfaces with
// the latest interface.reachable verdict per interface (observation time
// ordered, id-tiebroken, exactly LatestState's rule), ordered by component
// then interface name. The scope filters components with the same
// subtree-plus-self expansion every component list uses; an empty scope
// returns no rows.
func (p *PG) ListInterfaceReachability(ctx context.Context, read scope.Set) ([]InterfaceReachability, error) {
	const cols = `
		select c.name, i.name,
			(select it.name from interface_type it where it.id = i.type),
			coalesce(s.value, ''), s.ts
		from component c
		join interface i on i.component = c.id
		left join lateral (
			select st.value, st.ts
			from state st
			where st.component_id = c.id
			  and st.property_type_id = (select id from property_type where name = 'interface.reachable')
			  and st.instance = i.name
			order by st.ts desc, st.id desc
			limit 1
		) s on true`
	var (
		sql  string
		args []any
	)
	if read.All {
		sql = cols + ` order by c.name, i.name`
	} else {
		roots := uuidRoots(read.IDs)
		selfIDs := uuidRoots(read.SelfIDs)
		if len(roots) == 0 && len(selfIDs) == 0 {
			return nil, nil
		}
		sql = `
		with recursive sub(id) as (
			select id from component where id = any($1::uuid[])
			union all
			select t.id from component t join sub on t.parent_id = sub.id
		) cycle id set is_cycle using path` + cols + `
		where c.id in (select id from sub) or c.id = any($2::uuid[])
		order by c.name, i.name`
		args = []any{roots, selfIDs}
	}
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list interface reachability: %w", err)
	}
	defer rows.Close()
	var out []InterfaceReachability
	for rows.Next() {
		var r InterfaceReachability
		if err := rows.Scan(&r.Component, &r.Interface, &r.Type, &r.Value, &r.TS); err != nil {
			return nil, fmt.Errorf("storage: scan interface reachability: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate interface reachability: %w", err)
	}
	return out, nil
}
