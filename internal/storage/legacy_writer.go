package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// What was stored before the one-writer ruling (ADR-0161) is held to it where
// it would act: a driver spec saved, or a task derived, before the emit
// refusal never hands a node an emit the ruling refuses (the worklist strips
// it, a re-derivation never bakes it), and the boot audit names every stored
// declaration the ruling now refuses, so an operator can mend it.

// emitAdmitted reports whether a driver's emit may name key: the node lane
// carries a driver's emits for collected keys alone, never a reserved key (the
// node's own probe keys, the platform's, a command's lifecycle event) or a
// calc rule's output.
func emitAdmitted(key string, calcOwned map[string]bool) bool {
	return writerclass.Of(key, calcOwned[key]) == writerclass.Collected
}

// rulingEmits drops from a baked task spec every emit the ruling refuses and
// names what it dropped. Pure. The spec comes back byte for byte when nothing
// is dropped, and when it does not parse (the node faults such a task itself).
func rulingEmits(spec []byte, calcOwned map[string]bool) ([]byte, []string) {
	fn, err := driver.ParseBaked(spec)
	if err != nil {
		return spec, nil
	}
	var dropped []string
	kept := fn.Emits[:0:0]
	for _, em := range fn.Emits {
		if emitAdmitted(em.Name, calcOwned) {
			kept = append(kept, em)
			continue
		}
		dropped = append(dropped, em.Name)
	}
	if len(dropped) == 0 {
		return spec, nil
	}
	fn.Emits = kept
	out, err := json.Marshal(fn)
	if err != nil {
		return spec, nil
	}
	return out, dropped
}

// calcOwnedNames is every key a calc rule outputs, enabled or not.
func calcOwnedNames(ctx context.Context, q querier) (map[string]bool, error) {
	rows, err := q.Query(ctx, calcOwnedNamesSQL)
	if err != nil {
		return nil, fmt.Errorf("storage: read the calc rules' outputs: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("storage: read the calc rules' outputs: %w", err)
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out, nil
}

// stripRefusedEmits holds every driver task on a worklist to the ruling
// before the node sees it: an emit the ruling refuses is dropped (the task's
// other emits ride on) and logged once per task and key for the process's
// life, since the worklist is built on every pull.
func (p *PG) stripRefusedEmits(ctx context.Context, node string, tasks []WorklistTask) error {
	var calcOwned map[string]bool
	for i := range tasks {
		if !driver.IsBaked(tasks[i].Spec) {
			continue
		}
		if calcOwned == nil {
			var err error
			if calcOwned, err = calcOwnedNames(ctx, p.pool); err != nil {
				return err
			}
		}
		spec, dropped := rulingEmits(tasks[i].Spec, calcOwned)
		if len(dropped) == 0 {
			continue
		}
		tasks[i].Spec = spec
		for _, key := range dropped {
			if _, logged := p.strippedEmits.LoadOrStore(tasks[i].ID+"\x00"+key, true); !logged {
				slog.Warn("a stored driver task emits a key its writer class refuses; the worklist drops the emit",
					"facility", "collection", "node", node, "task", tasks[i].ID, "emit", key)
			}
		}
	}
	return nil
}

// RulingViolation is one stored declaration the writer ruling now refuses: a
// driver spec's or a derived task's emit, or a contract line, with the key it
// names and the key's class.
type RulingViolation struct {
	// Where names the declaration: "driver <name>", "task <id>",
	// "<product|standard|location type> <name> contract", "event rule
	// <name>" or "command type <name>".
	Where string
	Key   string
	Class writerclass.Class
}

// WriterRulingViolations audits the stored declarations against the ruling,
// for the boot audit: every driver spec and derived task emitting a key the
// node lane may not carry, every contract line on a reserved key, every event
// rule firing or clearing with an event type a rule may not write, and every
// command type targeting a key a command may not. Each is
// already inert where it would act (the worklist strips the emit, the
// effective reads lend the contract no default); this names them so an
// operator can mend them. Ordered by where, then key.
func (p *PG) WriterRulingViolations(ctx context.Context) ([]RulingViolation, error) {
	calcOwned, err := calcOwnedNames(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	var out []RulingViolation
	add := func(where, key string) {
		if c := writerclass.Of(key, calcOwned[key]); c != writerclass.Collected {
			out = append(out, RulingViolation{Where: where, Key: key, Class: c})
		}
	}
	emits, err := p.pool.Query(ctx, `
		select 'driver ' || d.name, e->>'name'
		from driver d
		cross join lateral jsonb_array_elements(coalesce(d.spec->'polls', '[]') || coalesce(d.spec->'listeners', '[]')) f
		cross join lateral jsonb_array_elements(coalesce(f->'emits', '[]')) e
		where d.spec is not null and jsonb_typeof(d.spec) = 'object'
		union all
		select 'task ' || t.id, e->>'name'
		from task t
		cross join lateral jsonb_array_elements(coalesce(t.spec->'emits', '[]')) e
		where t.spec->>'driver' is not null`)
	if err != nil {
		return nil, fmt.Errorf("storage: audit the stored emits: %w", err)
	}
	type named struct{ where, key string }
	found, err := pgx.CollectRows(emits, func(r pgx.CollectableRow) (named, error) {
		var n named
		return n, r.Scan(&n.where, &n.key)
	})
	if err != nil {
		return nil, fmt.Errorf("storage: audit the stored emits: %w", err)
	}
	for _, n := range found {
		add(n.where, n.key)
	}
	contracts, err := p.pool.Query(ctx, `
		select 'product ' || o.name || ' contract', t.name from product_property c join product o on o.id = c.product_id join property_type t on t.id = c.property_type_id
		union all select 'product ' || o.name || ' contract', t.name from product_metric c join product o on o.id = c.product_id join metric_type t on t.id = c.metric_type_id
		union all select 'standard ' || o.name || ' contract', t.name from standard_property c join standard o on o.id = c.standard_id join property_type t on t.id = c.property_type_id
		union all select 'standard ' || o.name || ' contract', t.name from standard_metric c join standard o on o.id = c.standard_id join metric_type t on t.id = c.metric_type_id
		union all select 'location type ' || o.name || ' contract', t.name from location_type_property c join location_type o on o.id = c.location_type_id join property_type t on t.id = c.property_type_id
		union all select 'location type ' || o.name || ' contract', t.name from location_type_metric c join location_type o on o.id = c.location_type_id join metric_type t on t.id = c.metric_type_id`)
	if err != nil {
		return nil, fmt.Errorf("storage: audit the stored contracts: %w", err)
	}
	lines, err := pgx.CollectRows(contracts, func(r pgx.CollectableRow) (named, error) {
		var n named
		return n, r.Scan(&n.where, &n.key)
	})
	if err != nil {
		return nil, fmt.Errorf("storage: audit the stored contracts: %w", err)
	}
	for _, n := range lines {
		if c, reserved := writerclass.Reserved(n.key); reserved {
			out = append(out, RulingViolation{Where: n.where, Key: n.key, Class: c})
		}
	}
	// A rule's fire or clear event, and a command type's target, a writer
	// other than the rule's or the command's own now holds.
	decls, err := p.pool.Query(ctx, `
		select 'event rule ' || r.name, t.name, 'event-rule' from event_rule r join event_type t on t.id in (r.fire_event_type_id, r.clear_event_type_id)
		union all select 'command type ' || c.name, t.name, 'command' from command_type c join property_type t on t.id = c.target_property_type_id
		union all select 'command type ' || c.name, t.name, 'command' from command_type c join metric_type t on t.id = c.target_metric_type_id`)
	if err != nil {
		return nil, fmt.Errorf("storage: audit the stored rules and command types: %w", err)
	}
	type declared struct {
		where, key string
		writer     writerclass.Writer
	}
	targets, err := pgx.CollectRows(decls, func(r pgx.CollectableRow) (declared, error) {
		var d declared
		return d, r.Scan(&d.where, &d.key, &d.writer)
	})
	if err != nil {
		return nil, fmt.Errorf("storage: audit the stored rules and command types: %w", err)
	}
	for _, d := range targets {
		if c := writerclass.Of(d.key, calcOwned[d.key]); !writerclass.Admits(c, d.writer, d.key) {
			out = append(out, RulingViolation{Where: d.where, Key: d.key, Class: c})
		}
	}
	slices.SortFunc(out, func(a, b RulingViolation) int {
		if c := strings.Compare(a.Where, b.Where); c != 0 {
			return c
		}
		return strings.Compare(a.Key, b.Key)
	})
	return slices.Compact(out), nil
}

// HealRetiredSeries re-judges, at boot, every enabled event rule's series a
// retirement took rows from (ADR-0163): a condition (and the alarm with it) a
// retired row raised would otherwise stand, since nothing re-reads a series
// that lost rows rather than gained them. A series left with no sample lets
// its condition go; one with samples is judged against its newest, as the
// reconcile pass judges a drifted series. Idempotent: a series that already
// agrees changes nothing. It reports how many series it judged.
func (p *PG) HealRetiredSeries(ctx context.Context) (int, error) {
	rows, err := p.pool.Query(ctx, `
		select distinct r.id::text, r.name, rs.sample->>'component_id', rs.sample->>'instance'
		from retired_sample rs
		join event_rule r on r.enabled and r.sample_lane = rs.lane
		left join metric_type mt on rs.lane = 'metric' and mt.id::text = rs.sample->>'metric_type_id'
		left join property_type pt on rs.lane = 'property' and pt.id::text = rs.sample->>'property_type_id'
		where rs.sample->>'component_id' is not null and r.sample_key = coalesce(mt.name, pt.name)`)
	if err != nil {
		return 0, fmt.Errorf("storage: read the series a retirement touched: %w", err)
	}
	cands, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ruleCandidate, error) {
		var c ruleCandidate
		return c, r.Scan(&c.rule.ID, &c.rule.Name, &c.component, &c.instance)
	})
	if err != nil {
		return 0, fmt.Errorf("storage: read the series a retirement touched: %w", err)
	}
	for _, c := range cands {
		if err := p.rejudgeSeries(ctx, c, true); err != nil {
			return 0, fmt.Errorf("storage: heal rule %s on %s/%s: %w", c.rule.Name, c.component, c.instance, err)
		}
	}
	return len(cands), nil
}
