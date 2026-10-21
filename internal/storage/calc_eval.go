package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/rules"
)

// The calc rule evaluation loop (#996, epic #525; ADR-0148). A sample write
// marks every enabled calc rule that reads one of its keys pending on the
// sample's owner (calc_pending, in the write's own transaction); the server's
// calc tick drains the marks, one transaction per mark, reduces the rule's
// inputs' current values on the owner, and writes the output back through
// the same sample-write path as a calculated sample, where the event rules
// judge it and downstream calc rules are marked in turn. A rule never marks
// itself (ADR-0069 at runtime: the write knows its producer).
//
// Lock order, the same on every path: the pending row (the tick's FOR
// UPDATE, or a write's insert of a mark), then the calc rule's row (FOR
// SHARE, skipped when a rule write holds it), then the event rule rows and
// series and health locks the sample write takes. A sample write marks
// before it inserts or judges anything, so it never waits on a mark while
// holding a series or health lock.

// calcMarks is a write's owners and the keys each sample on them carried.
type calcMarks struct {
	keys map[string]map[string]bool // component id -> keys
}

func newCalcMarks() *calcMarks { return &calcMarks{keys: map[string]map[string]bool{}} }

// add records a sample: only component-owned samples mark (system rules
// arrive with #997).
func (m *calcMarks) add(ownerKind, ownerID, key string) {
	if ownerKind != rules.OwnerComponent {
		return
	}
	if m.keys[ownerID] == nil {
		m.keys[ownerID] = map[string]bool{}
	}
	m.keys[ownerID][key] = true
}

func (m *calcMarks) allKeys() []string {
	set := map[string]bool{}
	for _, ks := range m.keys {
		for k := range ks {
			set[k] = true
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// calcKeysPath is the jsonpath matching an inputs array that reads any of
// keys, the containment the calc_rule_inputs_idx GIN index answers.
func calcKeysPath(keys []string) string {
	terms := make([]string, len(keys))
	for i, k := range keys {
		q, _ := json.Marshal(k)
		terms[i] = "@.key == " + string(q)
	}
	return "$[*] ? (" + strings.Join(terms, " || ") + ")"
}

// markCalcPending inserts a mark for every enabled component calc rule that
// reads one of the keys a write's samples carried on an owner its selector
// picks, except the rule that produced the write. One indexed probe finds
// the rules; when none reads the keys the write pays nothing more. A mark
// already present is kept (coalescing), and the marks are inserted in
// (rule, owner) order so two writes marking the same rows cannot deadlock.
func (p *PG) markCalcPending(ctx context.Context, tx pgx.Tx, marks *calcMarks, exceptRule string) error {
	keys := marks.allKeys()
	if len(keys) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `select id::text, selector, inputs from calc_rule
		where enabled and owner_kind = 'component' and inputs @? $1::jsonpath and id::text <> $2 order by id`,
		calcKeysPath(keys), exceptRule)
	if err != nil {
		return fmt.Errorf("storage: find calc rules to mark: %w", err)
	}
	type candidate struct {
		id     string
		sel    EventRuleSelector
		inputs map[string]bool
	}
	var found []candidate
	for rows.Next() {
		var c candidate
		var sel, inputs []byte
		if err := rows.Scan(&c.id, &sel, &inputs); err != nil {
			rows.Close()
			return fmt.Errorf("storage: find calc rules to mark: %w", err)
		}
		var ins []rules.CalcInput
		if err := json.Unmarshal(sel, &c.sel); err != nil {
			rows.Close()
			return fmt.Errorf("storage: calc rule %s selector: %w", c.id, err)
		}
		if err := json.Unmarshal(inputs, &ins); err != nil {
			rows.Close()
			return fmt.Errorf("storage: calc rule %s inputs: %w", c.id, err)
		}
		c.inputs = map[string]bool{}
		for _, in := range ins {
			c.inputs[in.Key] = true
		}
		found = append(found, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(found) == 0 {
		return nil
	}
	owners := make([]string, 0, len(marks.keys))
	for id := range marks.keys {
		owners = append(owners, id)
	}
	slices.Sort(owners)
	m := newRuleMatcher(p, nil)
	var ruleIDs, compIDs []string
	for _, c := range found {
		for _, owner := range owners {
			reads := false
			for k := range marks.keys[owner] {
				if c.inputs[k] {
					reads = true
					break
				}
			}
			if !reads {
				continue
			}
			o, err := m.ownerFacts(ctx, tx, owner)
			if err != nil {
				return err
			}
			ok, err := m.selects(ctx, tx, EventRule{ID: c.id, Selector: c.sel}, o)
			if err != nil {
				return err
			}
			if ok {
				ruleIDs = append(ruleIDs, c.id)
				compIDs = append(compIDs, owner)
			}
		}
	}
	if len(ruleIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `insert into calc_pending (rule_id, owner_kind, component_id)
		select r, 'component', c from unnest($1::uuid[], $2::uuid[]) as m(r, c) order by r, c
		on conflict on constraint calc_pending_key do nothing`, ruleIDs, compIDs); err != nil {
		return fmt.Errorf("storage: mark calc rules pending: %w", err)
	}
	return nil
}

// DrainCalcPending serves up to limit pending marks, oldest first, each in
// its own transaction, and returns how many it served. A mark is taken FOR
// UPDATE SKIP LOCKED, so replicas draining at once never serve one mark
// twice, and deleted in the transaction that computes it, so a crash before
// the commit leaves it for the next drain. A pass stops early when the queue
// is empty or the oldest mark's rule is held by a rule write.
func (p *PG) DrainCalcPending(ctx context.Context, limit int) (int, error) {
	served := 0
	for served < limit {
		ok, err := p.serveCalcMark(ctx)
		if err != nil {
			return served, err
		}
		if !ok {
			break
		}
		served++
	}
	return served, nil
}

// serveCalcMark serves the oldest mark it can lock; false when there is none
// to serve now.
func (p *PG) serveCalcMark(ctx context.Context) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("storage: begin calc mark: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var ruleID, ownerKind, compID string
	err = tx.QueryRow(ctx, `select rule_id::text, owner_kind, coalesce(component_id::text, '') from calc_pending
		order by marked_at, rule_id for update skip locked limit 1`).Scan(&ruleID, &ownerKind, &compID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: take a calc mark: %w", err)
	}
	// The rule's row FOR SHARE, SKIP LOCKED: a rule write holding it FOR
	// UPDATE (and, on a delete, about to cascade into this mark) is not
	// waited on while this mark is held. The mark stays for a later pass.
	r, err := readCalcRuleSQL(ctx, tx, calcRuleSelect+` where c.id::text = $1 for share of c skip locked`, ruleID)
	if errors.Is(err, ErrCalcRuleNotFound) {
		var exists bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from calc_rule where id = $1::uuid)`, ruleID).Scan(&exists); err != nil {
			return false, fmt.Errorf("storage: calc mark rule: %w", err)
		}
		if exists {
			return false, nil // a rule write holds it: try again next pass
		}
	} else if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `delete from calc_pending where rule_id = $1::uuid and owner_kind = $2
		and component_id is not distinct from nullif($3, '')::uuid`, ruleID, ownerKind, compID); err != nil {
		return false, fmt.Errorf("storage: clear a calc mark: %w", err)
	}
	if err == nil && r.Enabled && ownerKind == rules.OwnerComponent && r.OwnerKind == rules.OwnerComponent {
		if err := p.computeCalc(ctx, tx, r, compID); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("storage: commit calc mark: %w", err)
	}
	return true, nil
}

// calcOutcome is one computation's result on an owner, for its state row.
type calcOutcome struct {
	value   any
	missing []string
	err     string
}

// computeCalc reduces a component rule's inputs on one component and, when
// the result differs from the output's current value, writes it as a
// calculated sample on the aggregate instance. Every outcome (a value, the
// inputs that had none, an error) is recorded in calc_rule_state; only a
// database fault is returned.
func (p *PG) computeCalc(ctx context.Context, tx pgx.Tx, r CalcRule, compID string) error {
	out, err := p.reduceCalc(ctx, tx, r, compID)
	if err != nil {
		return err
	}
	if out.value != nil && out.err == "" && len(out.missing) == 0 {
		if err := p.writeCalcOutput(ctx, tx, r, compID, out.value); err != nil {
			return err
		}
	}
	return recordCalcState(ctx, tx, r, compID, out)
}

func (p *PG) reduceCalc(ctx context.Context, tx pgx.Tx, r CalcRule, compID string) (calcOutcome, error) {
	types, err := keyValueTypes(ctx, tx)
	if err != nil {
		return calcOutcome{}, err
	}
	typed := make([]rules.TypedInput, len(r.Inputs))
	for i, in := range r.Inputs {
		vt, ok := types[in.Key]
		if !ok {
			return calcOutcome{err: fmt.Sprintf("input %s: %q is not a registered property or metric", in.As, in.Key)}, nil
		}
		typed[i] = rules.TypedInput{As: in.As, Key: in.Key, ValueType: vt}
	}
	prog, err := p.calcs.program(r, typed, types[r.OutputKey])
	if err != nil {
		return calcOutcome{err: err.Error()}, nil
	}
	values := make([][]any, len(typed))
	var missing []string
	for i, in := range typed {
		vs, err := readCalcInput(ctx, tx, compID, in, prog.Reducer != nil)
		if err != nil {
			var bad *calcValueError
			if errors.As(err, &bad) {
				return calcOutcome{err: err.Error()}, nil
			}
			return calcOutcome{}, err
		}
		if len(vs) == 0 {
			missing = append(missing, in.Key)
		}
		values[i] = vs
	}
	if len(missing) > 0 {
		return calcOutcome{missing: missing}, nil
	}
	v, err := prog.Reduce(values)
	if err != nil {
		return calcOutcome{err: err.Error()}, nil
	}
	return calcOutcome{value: v}, nil
}

// calcValueError is a stored input value its type cannot read.
type calcValueError struct{ msg string }

func (e *calcValueError) Error() string { return e.msg }

// readCalcInput is one input's current values on a component, ignoring
// intended values (calc reads observed and calculated as truth, never
// intended): every instance's newest value for a reducer, the aggregate
// instance's newest value otherwise. Instances are walked one index probe at
// a time (the series index leads with component, type and instance).
func readCalcInput(ctx context.Context, tx pgx.Tx, compID string, in rules.TypedInput, allInstances bool) ([]any, error) {
	table, typeCol, typeTable, value := "property", "property_type_id", "property_type", "value #>> '{}'"
	if in.ValueType == rules.ValueNumber {
		table, typeCol, typeTable, value = "metric", "metric_type_id", "metric_type", "value::text"
	}
	newest := fmt.Sprintf(`(select %[4]s from %[1]s where component_id = $1::uuid and %[2]s = t.id and instance = i.instance
		and provenance <> 'intended' order by ts desc, id desc limit 1)`, table, typeCol, typeTable, value)
	var sql string
	if allInstances {
		sql = fmt.Sprintf(`with recursive t as (select id from %[3]s where name = $2),
			inst(instance) as (
				(select instance from %[1]s, t where component_id = $1::uuid and %[2]s = t.id order by instance limit 1)
				union all
				select (select x.instance from %[1]s x, t where x.component_id = $1::uuid and x.%[2]s = t.id and x.instance > inst.instance
					order by x.instance limit 1) from inst where inst.instance is not null)
			select v from (select %[4]s as v from inst i, t where i.instance is not null) s where v is not null`, table, typeCol, typeTable, newest)
	} else {
		sql = fmt.Sprintf(`with t as (select id from %[3]s where name = $2), i as (select ''::text as instance)
			select v from (select %[4]s as v from i, t) s where v is not null`, table, typeCol, typeTable, newest)
	}
	rows, err := tx.Query(ctx, sql, compID, in.Key)
	if err != nil {
		return nil, fmt.Errorf("storage: read calc input %s: %w", in.Key, err)
	}
	defer rows.Close()
	var out []any
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("storage: read calc input %s: %w", in.Key, err)
		}
		v, err := rules.CalcValue(in.ValueType, raw)
		if err != nil {
			return nil, &calcValueError{msg: fmt.Sprintf("input %s: %v", in.As, err)}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// writeCalcOutput writes a rule's output on a component's aggregate instance
// through the sample-write path, as calculated, unless it equals the
// output's current value (so an unchanged result writes no row, on either
// lane, and storage grows only with change).
func (p *PG) writeCalcOutput(ctx context.Context, tx pgx.Tx, r CalcRule, compID string, v any) error {
	current, err := readCalcInput(ctx, tx, compID, rules.TypedInput{Key: r.OutputKey, ValueType: outputValueType(r, v)}, false)
	if err != nil {
		var bad *calcValueError
		if !errors.As(err, &bad) {
			return err
		}
	} else if len(current) == 1 && current[0] == v {
		return nil
	}
	lineage := &sampleLineage{ruleID: r.ID, ruleName: r.Name, version: r.Version}
	ts := time.Now().UTC()
	if r.OutputLane == rules.LaneMetric {
		return p.writeMetricSamples(ctx, tx, []MetricSampleWrite{{OwnerKind: rules.OwnerComponent, OwnerID: compID, Key: r.OutputKey,
			Value: v.(float64), Source: calcSource, TS: ts}}, lineage)
	}
	return p.writePropertySamples(ctx, tx, []PropertySampleWrite{{OwnerKind: rules.OwnerComponent, OwnerID: compID, Key: r.OutputKey,
		Value: rules.FormatCalcValue(v), Source: calcSource, TS: ts}}, lineage)
}

// calcSource is the source a calculated sample records; its producer is in
// source_rule.
const calcSource = "calc"

func outputValueType(r CalcRule, v any) string {
	switch v.(type) {
	case float64:
		return rules.ValueNumber
	case bool:
		return rules.ValueBool
	}
	return rules.ValueText
}

// recordCalcState upserts a rule's last outcome on a component.
func recordCalcState(ctx context.Context, tx pgx.Tx, r CalcRule, compID string, out calcOutcome) error {
	missing := out.missing
	if missing == nil {
		missing = []string{}
	}
	m, _ := json.Marshal(missing)
	var value any
	if out.value != nil {
		b, _ := json.Marshal(out.value)
		value = b
	}
	if _, err := tx.Exec(ctx, `insert into calc_rule_state (rule_id, owner_kind, component_id, computed_at, rule_version, value, missing, error)
		values ($1::uuid, 'component', $2::uuid, now(), $3, $4::jsonb, $5::jsonb, $6)
		on conflict on constraint calc_rule_state_key do update set computed_at = excluded.computed_at, rule_version = excluded.rule_version,
			value = excluded.value, missing = excluded.missing, error = excluded.error`,
		r.ID, compID, r.Version, value, m, out.err); err != nil {
		return fmt.Errorf("storage: record calc state: %w", err)
	}
	return nil
}
