package storage

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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

// EventFeedQuery bounds one page of the occurrence feed: an optional owner
// filter (a component name), the page size, and the keyset cursor (the last
// row of the previous page). A zero cursor starts at the newest row.
type EventFeedQuery struct {
	Owner    string
	Limit    int
	BeforeTS time.Time
	BeforeID int64
}

// EventFeedRow is one occurrence in the feed, with its owner resolved to a name.
type EventFeedRow struct {
	ID       int64
	TS       time.Time
	Owner    string
	Key      string
	Instance string
	Origin   string
	Message  string
	Source   string
}

// ListEventFeed returns a page of the fleet-wide occurrence feed, newest first,
// bounded by the caller's scope. The order is TOTAL (ts then id), which the
// keyset cursor and the watch detector both require: a same-instant pair would
// otherwise reorder between runs, so a page could repeat or skip a row and a
// watcher would see phantom changes.
//
// Node-owned and platform-owned occurrences ride only with the all scope, the
// same rule the secret and variable arcs follow: the scope kinds are the
// component, system, and location tiers, so no rooted grant can address them.
func (p *PG) ListEventFeed(ctx context.Context, read scope.Set, q EventFeedQuery) ([]EventFeedRow, error) {
	limit := q.Limit
	if limit <= 0 || limit > MaxViewPage {
		limit = MaxViewPage
	}
	sel := `
		select e.id, e.ts, coalesce(c.name, sy.name, l.name, n.name, ''),
			(select et.name from event_type et where et.id = e.event_type_id),
			e.instance, e.origin, e.message, e.source
		from event e
		left join component c on e.component_id = c.id
		left join system    sy on e.system_id   = sy.id
		left join location  l on e.location_id  = l.id
		left join node      n on e.node_id      = n.principal_id
		where true`
	// The cursor is a keyset, not an offset: (ts, id) strictly before the last
	// row of the previous page, so a row written mid-walk cannot shift a page
	// boundary and hide a neighbour.
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if q.Owner != "" {
		sel += ` and e.component_id = (select id from component where name = ` + arg(q.Owner) + `)`
	}
	if !q.BeforeTS.IsZero() {
		sel += ` and (e.ts, e.id) < (` + arg(q.BeforeTS) + `, ` + arg(q.BeforeID) + `)`
	}
	if !read.All {
		inclusive, excluded, selfIDs := arcScopeArgs(read)
		if len(inclusive) == 0 && len(excluded) == 0 && len(selfIDs) == 0 {
			return nil, nil
		}
		inclN, exclN := arg(inclusive), arg(excluded)
		selfN := arg(selfIDs)
		sel = arcScopeCTEs(argIndex(inclN), argIndex(exclN)) + sel + ` and ` + arcScopePredicate("e", argIndex(selfN))
	}
	sel += ` order by e.ts desc, e.id desc limit ` + arg(limit)

	rows, err := p.pool.Query(ctx, sel, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list event feed: %w", err)
	}
	defer rows.Close()
	var out []EventFeedRow
	for rows.Next() {
		var r EventFeedRow
		if err := rows.Scan(&r.ID, &r.TS, &r.Owner, &r.Key, &r.Instance, &r.Origin, &r.Message, &r.Source); err != nil {
			return nil, fmt.Errorf("storage: scan event feed row: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate event feed: %w", err)
	}
	return out, nil
}

// SampleHistoryQuery selects one series: an owner component, a property key,
// an optional instance, and the window's start.
type SampleHistoryQuery struct {
	Owner    string
	Key      string
	Instance string
	Since    time.Time
	Limit    int
}

// SampleHistoryRow is one reading. A numeric (metric) reading carries Value and
// an empty State; a categorical (state) reading carries State and a nil Value,
// so one row shape serves both lanes without a column whose type changes.
type SampleHistoryRow struct {
	TS       time.Time
	Instance string
	Value    *float64
	State    string
}

// ListSampleHistory returns one component's readings for a key over a window,
// OLDEST first so a chart plots left to right without re-sorting. It reads both
// sample lanes: the numeric metric lane and the categorical state lane, keyed
// by the property type's name in either.
//
// The order is total: ts, then the lane, then the row id. The lane
// discriminator matters because the two tables have independent id sequences,
// so a same-instant reading in each could otherwise tie.
//
// The caller verifies the owner is in read scope (a non-disclosing 404 when it
// is not) before calling, exactly as the reachability read does.
func (p *PG) ListSampleHistory(ctx context.Context, _ scope.Set, q SampleHistoryQuery) ([]SampleHistoryRow, error) {
	limit := q.Limit
	if limit <= 0 || limit > MaxViewPage {
		limit = MaxViewPage
	}
	// The cap takes the NEWEST rows and the result is then re-ordered oldest
	// first. Capping the ascending order instead would silently return the
	// oldest page of a dense series and leave the chart's right edge hours in
	// the past, with nothing in the result saying so.
	rows, err := p.pool.Query(ctx, `
		with owner as (select id from component where name = $1),
		     prop  as (select id from property_type where name = $2),
		     newest as (
			select ts, instance, value, null::text as state, 0 as lane, id from metric
			where component_id = (select id from owner) and property_type_id = (select id from prop)
			  and ts >= $3 and ($4 = '' or instance = $4)
			union all
			select ts, instance, null::double precision as value, value as state, 1 as lane, id from state
			where component_id = (select id from owner) and property_type_id = (select id from prop)
			  and ts >= $3 and ($4 = '' or instance = $4)
			order by ts desc, lane desc, id desc
			limit $5
		     )
		select ts, instance, value, state, lane, id from newest
		order by ts asc, lane asc, id asc`, q.Owner, q.Key, q.Since, q.Instance, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: list sample history %s/%s: %w", q.Owner, q.Key, err)
	}
	defer rows.Close()
	var out []SampleHistoryRow
	for rows.Next() {
		var r SampleHistoryRow
		var state *string
		var lane int
		var id int64
		if err := rows.Scan(&r.TS, &r.Instance, &r.Value, &state, &lane, &id); err != nil {
			return nil, fmt.Errorf("storage: scan sample history row: %w", err)
		}
		if state != nil {
			r.State = *state
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate sample history: %w", err)
	}
	return out, nil
}

// EstateCountSet is the component-tier stat tiles: how many components and
// interfaces the caller can see, and how the probed ones currently read. An
// interface with no verdict counts in neither Up nor Down.
type EstateCountSet struct {
	Components     int
	Interfaces     int
	InterfacesUp   int
	InterfacesDown int
}

// EstateCounts returns the stat tiles bounded by the caller's read scope, in
// one round trip. The verdict tiles resolve each interface's LATEST
// interface.reachable exactly as the reachability read does (observation time,
// id-tiebroken), so a tile and the grid beside it can never disagree.
func (p *PG) EstateCounts(ctx context.Context, read scope.Set) (EstateCountSet, error) {
	var (
		visible string
		args    []any
	)
	if read.All {
		visible = `select id from component`
	} else {
		roots := uuidRoots(read.IDs)
		selfIDs := uuidRoots(read.SelfIDs)
		if len(roots) == 0 && len(selfIDs) == 0 {
			return EstateCountSet{}, nil
		}
		visible = `
			select id from component where id in (
				select id from sub
			) or id = any($2::uuid[])`
		args = []any{roots, selfIDs}
	}
	prefix := ""
	if !read.All {
		prefix = `
		with recursive sub(id) as (
			select id from component where id = any($1::uuid[])
			union all
			select t.id from component t join sub on t.parent_id = sub.id
		) cycle id set is_cycle using path`
	}
	sql := prefix + `
		select
			(select count(*) from component where id in (` + visible + `)),
			(select count(*) from interface i where i.component in (` + visible + `)),
			(select count(*) from interface i where i.component in (` + visible + `) and ` + latestVerdictIs("i", "up") + `),
			(select count(*) from interface i where i.component in (` + visible + `) and ` + latestVerdictIs("i", "down") + `)`
	var out EstateCountSet
	if err := p.pool.QueryRow(ctx, sql, args...).Scan(&out.Components, &out.Interfaces, &out.InterfacesUp, &out.InterfacesDown); err != nil {
		return EstateCountSet{}, fmt.Errorf("storage: estate counts: %w", err)
	}
	return out, nil
}

// latestVerdictIs renders the predicate "this interface's latest
// interface.reachable verdict equals want", resolved the same way the
// reachability read resolves it.
func latestVerdictIs(alias, want string) string {
	return `(select st.value
		from state st
		where st.component_id = ` + alias + `.component
		  and st.property_type_id = (select id from property_type where name = 'interface.reachable')
		  and st.instance = ` + alias + `.name
		order by st.ts desc, st.id desc
		limit 1) = '` + want + `'`
}

// argIndex parses a "$N" placeholder back to N, so the shared arc-scope helpers
// (which take positions, not placeholders) can be composed with a dynamically
// built argument list.
func argIndex(placeholder string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(placeholder, "$"))
	return n
}

// MaxViewPage caps any view page, so a caller cannot ask for the whole history
// in one request. Exported because a view must clamp to the same number before
// it decides whether to emit a next-page token: comparing a returned row count
// against an un-clamped requested limit would read a capped page as the last
// one and silently end the walk with rows still unread.
const MaxViewPage = 500
