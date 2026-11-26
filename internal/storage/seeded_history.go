package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/health"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// Seeded history (#2727): a throwaway lab's seed lands the derived state a
// fleet that had been running would carry, a system's health edges and the
// alarms that explained them, at past times. The platform derives both itself
// (recordHealth on every change, raiseAlarmTx and the clears), stamping now;
// these writes stamp the past instead, write no audit row, recompute nothing
// and notify no one, and label every row with the seed's source so it stays
// apart from what the platform recorded. They are a lab's, refused outside a
// throwaway lab by the lab plan (lab --health-history needs --ephemeral), as
// --platform-history is (#1790); epic #1796 is to replace them with history the
// real rules derive on a virtual clock.

// SeededHealthEdge is one past health edge for an owner (a component, system or
// location by id): the health it took at TS.
type SeededHealthEdge struct {
	OwnerKind string
	OwnerID   string
	TS        time.Time
	Value     string
}

// SeededAlarm is one past alarm on a component: raised by the named event rule
// (its dedup key is the rule's, with Instance, as a rule's own raise would key
// it) at RaisedAt and cleared at ClearedAt.
type SeededAlarm struct {
	ComponentID string
	Rule        string
	Instance    string
	Severity    string
	Message     string
	RaisedAt    time.Time
	ClearedAt   time.Time
}

var errSeedSource = errors.New("storage: a seeded write names its source")

// LandSeededHealth lands edges as calculated health rows labelled source, in
// one transaction, and returns how many landed. The source is its own sentinel:
// when any health row already carries it, nothing lands and the count is 0, so
// a second seed writes nothing. A value outside health's vocabulary is refused.
//
// The seed is the past, so it goes BEFORE each owner's own record and never
// rewrites it: an edge at or after the owner's first recorded one is refused.
// Every reader of the live health takes the highest id, and the series holds
// two invariants (no value follows itself; id order is time order, see
// recordHealth). A backdated row inserted after the owner's own rows would break
// both, so each owner's series is rebuilt under its health lock: the seeded
// edges in time order, then the owner's own rows copied whole (their ts, value
// and lineage kept, new ids), a value equal to the one before it dropped, and
// the originals deleted. The newest row is still the platform's own.
func (p *PG) LandSeededHealth(ctx context.Context, source string, edges []SeededHealthEdge) (int, error) {
	if source == "" {
		return 0, errSeedSource
	}
	for _, e := range edges {
		if health.ParseValue(e.Value).String() != e.Value {
			return 0, fmt.Errorf("storage: seeded health %s/%s: %q is not a health value", e.OwnerKind, e.OwnerID, e.Value)
		}
		if _, ok := seededOwnerRank[e.OwnerKind]; !ok {
			return 0, fmt.Errorf("storage: seeded health: owner kind %q is not a component, system or location", e.OwnerKind)
		}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Two seeds of one source serialize here, so the sentinel below is read once.
	if err := lockAdvisory(ctx, tx, "seeded/health/"+source); err != nil {
		return 0, err
	}
	var landed bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from property
		where source = $1 and provenance = 'calculated'
		  and property_type_id = (select id from property_type_in_effect() property_type where name = $2))`,
		source, healthKey).Scan(&landed); err != nil {
		return 0, fmt.Errorf("storage: seeded health sentinel: %w", err)
	}
	if landed || len(edges) == 0 {
		return 0, nil
	}
	cols, err := propertyCopyColumns(ctx, tx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, o := range seededOwners(edges) {
		k, err := p.landOwnerHistory(ctx, tx, source, cols, o)
		if err != nil {
			return 0, err
		}
		n += k
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return n, nil
}

// seededOwnerRank orders owners as the health recompute locks them
// (components, then systems, then locations), so a seed never takes two owner
// locks in the opposite order to a recompute.
var seededOwnerRank = map[string]int{"component": 0, "system": 1, "location": 2}

type seededOwner struct {
	kind, id string
	edges    []SeededHealthEdge // time order
}

// seededOwners groups edges by owner, in lock order, each owner's in time order.
func seededOwners(edges []SeededHealthEdge) []seededOwner {
	idx := map[[2]string]int{}
	var out []seededOwner
	for _, e := range edges {
		k := [2]string{e.OwnerKind, e.OwnerID}
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, seededOwner{kind: e.OwnerKind, id: e.OwnerID})
		}
		out[i].edges = append(out[i].edges, e)
	}
	for i := range out {
		sort.SliceStable(out[i].edges, func(a, b int) bool { return out[i].edges[a].TS.Before(out[i].edges[b].TS) })
	}
	sort.SliceStable(out, func(a, b int) bool {
		if seededOwnerRank[out[a].kind] != seededOwnerRank[out[b].kind] {
			return seededOwnerRank[out[a].kind] < seededOwnerRank[out[b].kind]
		}
		return out[a].id < out[b].id
	})
	return out
}

// propertyCopyColumns is every property column a copied row carries, all but
// its id (a copy takes a new one) and any generated column, so a column added
// later is carried rather than silently dropped.
func propertyCopyColumns(ctx context.Context, q txQuerier) (string, error) {
	var cols string
	if err := q.QueryRow(ctx, `select string_agg(quote_ident(column_name), ', ' order by ordinal_position)
		from information_schema.columns
		where table_schema = current_schema() and table_name = 'property'
		  and column_name <> 'id' and is_generated = 'NEVER'`).Scan(&cols); err != nil {
		return "", fmt.Errorf("storage: property columns: %w", err)
	}
	return cols, nil
}

// landOwnerHistory rebuilds one owner's health series with the seeded edges in
// front of its own record (LandSeededHealth), and returns how many seeded edges
// landed.
func (p *PG) landOwnerHistory(ctx context.Context, tx pgx.Tx, source, cols string, o seededOwner) (int, error) {
	// Health has one writer, the process (ADR-0161); the seed writes as it.
	if err := admitWrites(ctx, tx, writeProperty, gatewayWriter(writerclass.Process), []keyedWrite{{key: healthKey}}); err != nil {
		return 0, err
	}
	if err := lockHealthOwner(ctx, tx, o.kind, o.id); err != nil {
		return 0, err
	}
	col, _ := ownerColumn(o.kind)
	type ownRow struct {
		id    int64
		ts    time.Time
		value string
	}
	rows, err := tx.Query(ctx, fmt.Sprintf(`select id, ts, value #>> '{}' from property
		where %s = $1::uuid and property_type_id = (select id from property_type_in_effect() property_type where name = $2)
		  and instance = '' and provenance = 'calculated'
		order by id asc`, col), o.id, healthKey)
	if err != nil {
		return 0, fmt.Errorf("storage: seeded health: read %s/%s: %w", o.kind, o.id, err)
	}
	var own []ownRow
	for rows.Next() {
		var r ownRow
		if err := rows.Scan(&r.id, &r.ts, &r.value); err != nil {
			rows.Close()
			return 0, fmt.Errorf("storage: seeded health: scan %s/%s: %w", o.kind, o.id, err)
		}
		own = append(own, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(own) > 0 {
		for _, e := range o.edges {
			if !e.TS.Before(own[0].ts) {
				return 0, fmt.Errorf("storage: seeded health %s/%s: an edge at %v is not before its own record, which begins at %v",
					o.kind, o.id, e.TS, own[0].ts)
			}
		}
	}

	batch := &pgx.Batch{}
	last, n := "", 0
	for _, e := range o.edges {
		if e.Value == last {
			continue
		}
		batch.Queue(fmt.Sprintf(`insert into property (ts, owner_kind, %s, property_type_id, instance, value, provenance, source_rule, source)
			values ($1, $2::text, $3::uuid, (select id from property_type_in_effect() property_type where name = $4::text), '', to_jsonb($5::text), 'calculated', $6::text, $7::text)`, col),
			e.TS, o.kind, o.id, healthKey, e.Value, healthRule, source)
		last, n = e.Value, n+1
	}
	// The owner's own record follows, copied whole with new ids; its first value
	// drops when it only carries on from the seeded last one.
	var ids []int64
	for _, r := range own {
		ids = append(ids, r.id)
		if r.value == last {
			continue
		}
		batch.Queue(`insert into property (`+cols+`) select `+cols+` from property where id = $1`, r.id)
		last = r.value
	}
	if len(ids) > 0 {
		batch.Queue(`delete from property where id = any($1)`, ids)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return 0, fmt.Errorf("storage: land seeded health %s/%s: %w", o.kind, o.id, err)
	}
	return n, nil
}

// LandSeededAlarms lands alarms already raised and cleared, labelled source, in
// one transaction, and returns how many landed. Like LandSeededHealth the
// source is its own sentinel. A rule that does not exist, or a clear that does
// not follow its raise, is refused; the severity must name a level, as any
// alarm's must.
func (p *PG) LandSeededAlarms(ctx context.Context, source string, alarms []SeededAlarm) (int, error) {
	if source == "" {
		return 0, errSeedSource
	}
	for _, a := range alarms {
		if !a.ClearedAt.After(a.RaisedAt) {
			return 0, fmt.Errorf("storage: seeded alarm on %s: cleared %v is not after raised %v", a.ComponentID, a.ClearedAt, a.RaisedAt)
		}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockAdvisory(ctx, tx, "seeded/alarm/"+source); err != nil {
		return 0, err
	}
	var landed bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from alarm where source = $1)`, source).Scan(&landed); err != nil {
		return 0, fmt.Errorf("storage: seeded alarm sentinel: %w", err)
	}
	if landed || len(alarms) == 0 {
		return 0, nil
	}
	rules := map[string]string{}
	for _, a := range alarms {
		if _, ok := rules[a.Rule]; ok {
			continue
		}
		var id string
		if err := tx.QueryRow(ctx, `select r.id::text from event_rule_in_effect() r where r.name = $1`, a.Rule).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, fmt.Errorf("storage: seeded alarm: no event rule %q", a.Rule)
			}
			return 0, fmt.Errorf("storage: seeded alarm rule %q: %w", a.Rule, err)
		}
		rules[a.Rule] = id
	}
	batch := &pgx.Batch{}
	for _, a := range alarms {
		rule := rules[a.Rule]
		batch.Queue(`insert into alarm (component_id, severity, message, dedup_key, raised_by_rule_id, raised_at, cleared_at, source)
			values ($1::uuid, $2, $3, $4, $5::uuid, $6, $7, $8)`,
			a.ComponentID, a.Severity, a.Message, ruleDedupKey(rule, a.Instance), rule, a.RaisedAt, a.ClearedAt, source)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return 0, fmt.Errorf("storage: land seeded alarms: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(alarms), nil
}
