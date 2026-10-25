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
// Lock order. A sample write (the ingest's, or the tick's own output write)
// takes the event rule rows reading its keys FOR SHARE (newRuleEvaluator),
// then its marks (insert or update of calc_pending rows, in rule and owner
// order), then the series and health locks its samples' judging takes. The
// tick takes a mark first (FOR UPDATE SKIP LOCKED, deleting it), then the
// calc rule's row FOR SHARE SKIP LOCKED, then, in its output write, the
// order above. A rule write takes its calc_rule row FOR UPDATE, then the
// rule's marks. Two orders still cross: the tick holds a mark it deleted
// while its output write marks the next rule, and an ingest batch can hold
// that next mark while it waits on the deleted one. Postgres breaks that
// cycle with a deadlock error (40P01), and the ingest retries its whole
// transaction (sampleWriteAttempts), so a batch is never lost to it; a tick
// that loses defers its mark.

// calcMarks is a write's owners and the keys each sample on them carried,
// each key as lane:name (a property and a metric may share a name).
type calcMarks struct {
	keys map[string]map[string]bool // component id -> lane:name
}

func newCalcMarks() *calcMarks { return &calcMarks{keys: map[string]map[string]bool{}} }

// add records a sample: only component-owned samples mark. A system rule
// reads its members' (component) samples, so a system-owned sample, a
// system rule's own output included, feeds no calc rule.
func (m *calcMarks) add(ownerKind, ownerID string, lane rules.Lane, key string) {
	if ownerKind != rules.OwnerComponent {
		return
	}
	if m.keys[ownerID] == nil {
		m.keys[ownerID] = map[string]bool{}
	}
	m.keys[ownerID][string(lane)+":"+key] = true
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

// calcProbeArgs splits lane:name keys into the lanes and names the probe
// binds.
func calcProbeArgs(keys []string) (lanes, names []string) {
	for _, k := range keys {
		lane, name, _ := strings.Cut(k, ":")
		lanes, names = append(lanes, lane), append(names, name)
	}
	return lanes, names
}

// markCalcPending inserts a mark for every enabled component calc rule that
// reads one of the keys a write's samples carried on an owner its selector
// picks, except the rule that produced the write. One indexed probe finds
// the rules; when none reads the keys the write pays nothing more. A mark
// already present keeps its marked_at (coalescing) but is updated, not
// skipped: the update locks the row until this write commits, so a drain
// (SKIP LOCKED) cannot serve it from the values before this write and drop
// the change. The marks are inserted in (rule, owner) order.
func (p *PG) markCalcPending(ctx context.Context, tx pgx.Tx, marks *calcMarks, exceptRule string) error {
	keys := marks.allKeys()
	if len(keys) == 0 {
		return nil
	}
	lanes, names := calcProbeArgs(keys)
	rows, err := tx.Query(ctx, calcProbeSQL, lanes, names, exceptRule)
	if err != nil {
		return fmt.Errorf("storage: find calc rules to mark: %w", err)
	}
	type candidate struct {
		id     string
		kind   string
		sel    CalcRuleSelector
		inputs map[string]bool
	}
	var found []candidate
	var systemRules []string
	for rows.Next() {
		var c candidate
		var sel []byte
		var ins []string
		if err := rows.Scan(&c.id, &c.kind, &sel, &ins); err != nil {
			rows.Close()
			return fmt.Errorf("storage: find calc rules to mark: %w", err)
		}
		if err := json.Unmarshal(sel, &c.sel); err != nil {
			rows.Close()
			return fmt.Errorf("storage: calc rule %s selector: %w", c.id, err)
		}
		c.inputs = map[string]bool{}
		for _, in := range ins {
			c.inputs[in] = true
		}
		if c.kind == rules.OwnerSystem {
			systemRules = append(systemRules, c.id)
			continue
		}
		found = append(found, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(systemRules) > 0 {
		if err := markSystemRulesBySamples(ctx, tx, systemRules, marks); err != nil {
			return err
		}
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
			ok, err := m.selects(ctx, tx, EventRule{ID: c.id, Selector: c.sel.components()}, o)
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
		on conflict on constraint calc_pending_key do update set marked_at = calc_pending.marked_at`, ruleIDs, compIDs); err != nil {
		return fmt.Errorf("storage: mark calc rules pending: %w", err)
	}
	return nil
}

// CalcStaleAfter is the staleness bound on an input's list: an instance
// whose newest sample is older is left out (a collector that stopped
// reporting it should not hold the result), and is named in the rule's state
// as stale.
const CalcStaleAfter = time.Hour

// calcRetryBackoff is how far a mark whose computation failed (a database
// fault) is pushed back, so one bad mark cannot hold the head of the queue.
const calcRetryBackoff = 30 * time.Second

// CalcDrain is one drain pass: the marks it served, the marks it deferred
// after a failure, and the queue's depth after it.
type CalcDrain struct {
	Served   int
	Deferred int
	Depth    int
}

// DrainCalcPending serves up to limit due marks, oldest first, each in its
// own transaction. A mark is taken FOR UPDATE SKIP LOCKED, so replicas
// draining at once never serve one mark twice, and deleted in the
// transaction that computes it, so a crash before the commit leaves it for
// the next drain. A mark whose rule a rule write holds is passed over for
// this pass; a mark whose computation fails is recorded in the rule's state
// and pushed back by calcRetryBackoff, and the pass goes on.
func (p *PG) DrainCalcPending(ctx context.Context, limit int) (CalcDrain, error) {
	var d CalcDrain
	if err := p.markCalcWindowsDue(ctx, limit); err != nil {
		return d, err
	}
	held := []string{}
pass:
	for d.Served+d.Deferred < limit {
		res, err := p.serveCalcMark(ctx, held)
		if err != nil {
			return d, err
		}
		switch res.outcome {
		case markNone:
			break pass
		case markHeld:
			held = append(held, res.ruleID)
		case markServed:
			d.Served++
		case markDeferred:
			d.Deferred++
		}
	}
	if err := p.pool.QueryRow(ctx, `select count(*) from calc_pending`).Scan(&d.Depth); err != nil {
		return d, fmt.Errorf("storage: calc pending depth: %w", err)
	}
	return d, nil
}

// markCalcWindowsDue marks, for the pass, up to limit owners whose windowed
// rule's oldest sample has left its window since the rule last computed there
// (calc_rule_state.window_due_at, set by that computation), so a window that
// ages out with no new sample still recomputes (#1142). Each mark is its own
// statement: it holds no lock while it waits on a mark a serve holds, so it
// joins no lock cycle. A due time stays set until the recompute replaces it,
// so a mark lost to a crash is made again on the next tick.
func (p *PG) markCalcWindowsDue(ctx context.Context, limit int) error {
	rows, err := p.pool.Query(ctx, `select rule_id::text, component_id::text from calc_rule_state
		where window_due_at <= now() and computed_at < window_due_at and component_id is not null
		order by window_due_at, rule_id limit $1`, limit)
	if err != nil {
		return fmt.Errorf("storage: find due calc windows: %w", err)
	}
	type due struct{ rule, comp string }
	var found []due
	for rows.Next() {
		var x due
		if err := rows.Scan(&x.rule, &x.comp); err != nil {
			rows.Close()
			return fmt.Errorf("storage: find due calc windows: %w", err)
		}
		found = append(found, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, x := range found {
		if _, err := p.pool.Exec(ctx, `insert into calc_pending (rule_id, owner_kind, component_id)
			select $1::uuid, 'component', $2::uuid where exists (select 1 from calc_rule where id = $1::uuid and enabled)
			on conflict on constraint calc_pending_key do update set marked_at = calc_pending.marked_at`, x.rule, x.comp); err != nil {
			return fmt.Errorf("storage: mark a due calc window: %w", err)
		}
	}
	return nil
}

type markOutcome int

const (
	markNone markOutcome = iota
	markHeld
	markServed
	markDeferred
)

type markResult struct {
	outcome markOutcome
	ruleID  string
}

// serveCalcMark serves the oldest due mark it can lock whose rule is not in
// held.
func (p *PG) serveCalcMark(ctx context.Context, held []string) (markResult, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return markResult{}, fmt.Errorf("storage: begin calc mark: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var ruleID string
	var owner calcOwner
	err = tx.QueryRow(ctx, `select rule_id::text, owner_kind, coalesce(component_id, system_id)::text from calc_pending
		where marked_at <= now() and not (rule_id = any($1::uuid[]))
		order by marked_at, rule_id for update skip locked limit 1`, held).Scan(&ruleID, &owner.kind, &owner.id)
	if errors.Is(err, pgx.ErrNoRows) {
		return markResult{outcome: markNone}, nil
	}
	if err != nil {
		return markResult{}, fmt.Errorf("storage: take a calc mark: %w", err)
	}
	// The rule's row FOR SHARE, SKIP LOCKED: a rule write holding it FOR
	// UPDATE (and about to reset this rule's marks) is not waited on while
	// this mark is held; the rule's marks are passed over for this pass.
	r, err := readCalcRuleSQL(ctx, tx, calcRuleSelect+` where c.id::text = $1 for share of c skip locked`, ruleID)
	if errors.Is(err, ErrCalcRuleNotFound) {
		return markResult{outcome: markHeld, ruleID: ruleID}, nil
	}
	if err != nil {
		return markResult{}, err
	}
	if _, err := tx.Exec(ctx, `delete from calc_pending where rule_id = $1::uuid and owner_kind = $2
		and `+owner.col()+` = $3::uuid`, ruleID, owner.kind, owner.id); err != nil {
		return markResult{}, fmt.Errorf("storage: clear a calc mark: %w", err)
	}
	if err := p.computeServed(ctx, tx, r, owner); err != nil {
		_ = tx.Rollback(ctx)
		if derr := p.deferCalcMark(ctx, r, owner, err); derr != nil {
			return markResult{}, derr
		}
		return markResult{outcome: markDeferred, ruleID: ruleID}, nil
	}
	if err := tx.Commit(ctx); err != nil {
		if derr := p.deferCalcMark(ctx, r, owner, err); derr != nil {
			return markResult{}, derr
		}
		return markResult{outcome: markDeferred, ruleID: ruleID}, nil
	}
	return markResult{outcome: markServed, ruleID: ruleID}, nil
}

// calcOwner is the owner a mark, a computation or a state row is about: a
// component or a system, by id.
type calcOwner struct {
	kind, id string
}

// col is the owner's arc column on calc_pending, calc_rule_state and the
// sample tables.
func (o calcOwner) col() string {
	if o.kind == rules.OwnerSystem {
		return "system_id"
	}
	return "component_id"
}

// computeServed runs the test hook, then computes an enabled rule on the
// owner its mark names, when the mark's owner kind is the rule's.
func (p *PG) computeServed(ctx context.Context, tx pgx.Tx, r CalcRule, owner calcOwner) error {
	if p.calcHook != nil {
		if err := p.calcHook("taken"); err != nil {
			return err
		}
	}
	if !r.Enabled || owner.kind != r.OwnerKind {
		return nil
	}
	// The rule may no longer select the owner (a system retyped out of its
	// selector while this mark was held): its state there goes, and nothing
	// is computed. Read now, after the write that deselected it committed.
	selected, err := p.calcSelects(ctx, tx, r, owner)
	if err != nil {
		return err
	}
	if !selected {
		if _, err := tx.Exec(ctx, `delete from calc_rule_state where rule_id = $1::uuid and `+owner.col()+` = $2::uuid`, r.ID, owner.id); err != nil {
			return fmt.Errorf("storage: drop deselected calc state: %w", err)
		}
		return nil
	}
	return p.computeCalc(ctx, tx, r, owner)
}

// calcSelects reports whether a rule's selector picks an owner: a system by
// systemSelectedSQL, a component by the event rules' component matcher.
func (p *PG) calcSelects(ctx context.Context, tx pgx.Tx, r CalcRule, owner calcOwner) (bool, error) {
	if owner.kind == rules.OwnerSystem {
		var ok bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from calc_rule r, system s where r.id = $1::uuid and s.id = $2::uuid and `+
			systemSelectedSQL("r", "s")+`)`, r.ID, owner.id).Scan(&ok); err != nil {
			return false, fmt.Errorf("storage: calc rule selects %s: %w", owner.id, err)
		}
		return ok, nil
	}
	m := newRuleMatcher(p, nil)
	o, err := m.ownerFacts(ctx, tx, owner.id)
	if err != nil {
		return false, err
	}
	return m.selects(ctx, tx, EventRule{ID: r.ID, Selector: r.Selector.components()}, o)
}

// deferCalcMark records a failed computation in the rule's state and puts
// its mark back, due after calcRetryBackoff, in a transaction of its own
// (the failed one is rolled back, its mark with it).
func (p *PG) deferCalcMark(ctx context.Context, r CalcRule, owner calcOwner, cause error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin defer calc mark: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `insert into calc_pending (rule_id, owner_kind, `+owner.col()+`, marked_at)
		select $1::uuid, $2, $3::uuid, now() + make_interval(secs => $4)
		where exists (select 1 from calc_rule where id = $1::uuid)
		on conflict on constraint calc_pending_key do update set marked_at = greatest(calc_pending.marked_at, excluded.marked_at)`,
		r.ID, owner.kind, owner.id, calcRetryBackoff.Seconds()); err != nil {
		return fmt.Errorf("storage: defer calc mark: %w", err)
	}
	if err := recordCalcState(ctx, tx, r, owner, calcOutcome{err: cause.Error()}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// calcOutcome is one computation's result on an owner, for its state row.
type calcOutcome struct {
	value   any
	missing []string
	stale   []staleInstance
	skipped []skippedMember
	// noMembers is a system with no role-bound member.
	noMembers bool
	err       string
	// windowDue is when the soonest windowed input's oldest sample leaves its
	// window; nil when no windowed input held a sample.
	windowDue *time.Time
}

// staleInstance is an instance an input's list left out as stale: the component
// it is on (by id; its name resolves at read time, under the reader's
// scope), the key and the instance.
type staleInstance struct {
	Component string `json:"component"`
	Key       string `json:"key"`
	Instance  string `json:"instance"`
}

// skippedMember is a system's member that held no value of an input, by the
// member's id (its name resolves at read time, under the reader's scope).
type skippedMember struct {
	Key       string `json:"key"`
	Component string `json:"component"`
}

// computeCalc reduces a rule's inputs on one owner and, when the result
// differs from the output's current value, writes it as a calculated sample
// on the owner's aggregate instance. Every outcome (a value, the inputs that
// had none, the instances skipped as stale, the members skipped, an error)
// is recorded in calc_rule_state; only a database fault is returned.
func (p *PG) computeCalc(ctx context.Context, tx pgx.Tx, r CalcRule, owner calcOwner) error {
	out, err := p.reduceCalc(ctx, tx, r, owner)
	if err != nil {
		return err
	}
	if out.value != nil && out.err == "" && len(out.missing) == 0 {
		if err := p.writeCalcOutput(ctx, tx, r, owner, out.value); err != nil {
			return err
		}
	}
	return recordCalcState(ctx, tx, r, owner, out)
}

// calcMember is a component an input is read from: the owner itself for a
// component rule, each role-bound member for a system rule.
type calcMember struct {
	id, name string
}

// calcPlan is a rule's reduce ready to run: its inputs typed from the
// registries and its program, read and compiled once per computation (or
// once per preview).
type calcPlan struct {
	typed []rules.TypedInput
	prog  *rules.CalcProgram
}

// calcPlan types a rule's inputs and compiles its reduce; a non-empty fault
// is a rule that cannot reduce (an input key gone, a reduce that no longer
// compiles), recorded rather than returned.
func (p *PG) calcPlan(_ context.Context, _ pgx.Tx, r CalcRule) (calcPlan, string, error) {
	typed := typedInputs(r)
	var prog *rules.CalcProgram
	var err error
	if r.candidate {
		// A preview's candidate has no id or version to cache under.
		prog, err = rules.CompileCalc(r.OwnerKind, typed, r.Reduce, r.outputType)
	} else {
		prog, err = p.calcs.program(r, typed, r.outputType)
	}
	if err != nil {
		return calcPlan{}, err.Error(), nil
	}
	return calcPlan{typed: typed, prog: prog}, "", nil
}

func (p *PG) reduceCalc(ctx context.Context, tx pgx.Tx, r CalcRule, owner calcOwner) (calcOutcome, error) {
	plan, fault, err := p.calcPlan(ctx, tx, r)
	if err != nil {
		return calcOutcome{}, err
	}
	if fault != "" {
		return calcOutcome{err: fault}, nil
	}
	return p.reduceWith(ctx, tx, r, owner, plan)
}

// reduceWith reads a rule's inputs on one owner and reduces them by a plan.
func (p *PG) reduceWith(ctx context.Context, tx pgx.Tx, r CalcRule, owner calcOwner, plan calcPlan) (calcOutcome, error) {
	typed, prog := plan.typed, plan.prog
	var err error
	members := []calcMember{{id: owner.id}}
	if owner.kind == rules.OwnerSystem {
		if members, err = systemMembers(ctx, tx, owner.id); err != nil {
			return calcOutcome{}, err
		}
	}
	var out calcOutcome
	if owner.kind == rules.OwnerSystem && len(members) == 0 {
		// No member to read: nothing is written, so the system keeps its
		// last calculated value, which the state names unsupported.
		out.noMembers = true
		out.missing = inputKeys(r.Inputs)
		return out, nil
	}
	values := make([][]any, len(typed))
	read := make([]rules.CalcMember, len(members))
	for j, m := range members {
		read[j] = rules.CalcMember{ID: m.id, Name: m.name, Values: make([][]any, len(typed)), Record: make([]any, len(typed))}
	}
	for i, in := range typed {
		if in.Window > 0 {
			// A windowed input is a component rule's (rules.checkWindow): one
			// member, the owner.
			vs, due, err := readCalcWindow(ctx, tx, owner, in)
			if err != nil {
				var bad *calcValueError
				if errors.As(err, &bad) {
					return calcOutcome{err: err.Error()}, nil
				}
				return calcOutcome{}, err
			}
			if due != nil && (out.windowDue == nil || due.Before(*out.windowDue)) {
				out.windowDue = due
			}
			values[i] = vs
			if len(vs) == 0 {
				out.missing = append(out.missing, in.Key)
			}
			continue
		}
		for j, m := range members {
			vs, own, stale, err := readCalcInput(ctx, tx, calcOwner{kind: rules.OwnerComponent, id: m.id}, in, false)
			if err != nil {
				var bad *calcValueError
				if errors.As(err, &bad) {
					return calcOutcome{err: err.Error()}, nil
				}
				return calcOutcome{}, err
			}
			out.stale = append(out.stale, stale...)
			if len(vs) == 0 && owner.kind == rules.OwnerSystem {
				out.skipped = append(out.skipped, skippedMember{Key: in.Key, Component: m.id})
			}
			values[i] = append(values[i], vs...)
			read[j].Values[i], read[j].Record[i] = vs, own
		}
		if len(values[i]) == 0 {
			out.missing = append(out.missing, in.Key)
		}
	}
	if len(out.missing) > 0 {
		return out, nil
	}
	var v any
	if owner.kind == rules.OwnerSystem {
		v, err = prog.ReduceSystem(read)
	} else {
		v, err = prog.Reduce(values)
	}
	if err != nil {
		out.err = err.Error()
		return out, nil
	}
	out.value = v
	return out, nil
}

// roleOfSystemSQL is the predicate that a system_role row (alias r) is one
// of a system's roles (alias s): declared on the system, or inherited from
// its standard. The one statement of the fact the health rollup's role
// resolution (resolveHealthRoles), a system rule's members and its marking
// all read.
func roleOfSystemSQL(r, s string) string {
	return fmt.Sprintf(`((%[1]s.owner_kind = 'standard' and %[1]s.standard_id = %[2]s.standard_id)
		or (%[1]s.owner_kind = 'system' and %[1]s.system_id = %[2]s.id))`, r, s)
}

// systemMembers is a system's role-bound members: the components assigned
// to one of its roles, the set the health rollup resolves, by id order.
func systemMembers(ctx context.Context, tx pgx.Tx, systemID string) ([]calcMember, error) {
	rows, err := tx.Query(ctx, `select distinct c.id::text, c.name from system s
		join system_role_assignment ra on ra.system_id = s.id
		join system_role r on r.id = ra.role_id and `+roleOfSystemSQL("r", "s")+`
		join component c on c.id = ra.component_id
		where s.id = $1::uuid order by 1`, systemID)
	if err != nil {
		return nil, fmt.Errorf("storage: system %s members: %w", systemID, err)
	}
	defer rows.Close()
	var out []calcMember
	for rows.Next() {
		var m calcMember
		if err := rows.Scan(&m.id, &m.name); err != nil {
			return nil, fmt.Errorf("storage: system %s members: %w", systemID, err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// calcValueError is a stored input value its type cannot read.
type calcValueError struct{ msg string }

func (e *calcValueError) Error() string { return e.msg }

// calcSeries is the table, type column and value text a lane's samples are
// read by.
func calcSeries(lane rules.Lane) (table, typeCol, value string) {
	if lane == rules.LaneMetric {
		return "metric", "metric_type_id", "value::text"
	}
	return "property", "property_type_id", "value #>> '{}'"
}

// readCalcWindow is one key's samples on an owner over a windowed input's
// window (#1142), oldest first, each a rules.CalcSample: observed and
// calculated values only, as readCalcInput reads; the named instances' when
// the window holds one, else the aggregate instance's; the newest
// rules.MaxCalcWindowSamples kept. due is when the oldest kept sample leaves
// the window (nil for an empty window). One range read of the series index,
// which leads with owner and type.
func readCalcWindow(ctx context.Context, tx pgx.Tx, owner calcOwner, in rules.TypedInput) (samples []any, due *time.Time, err error) {
	table, typeCol, value := calcSeries(in.Lane)
	rows, err := tx.Query(ctx, fmt.Sprintf(`select instance, %[3]s, ts from %[1]s
		where %[4]s = $1::uuid and %[2]s = $2::uuid and provenance in ('observed', 'calculated')
			and ts > now() - make_interval(secs => $3)
		order by ts desc, id desc limit $4`, table, typeCol, value, owner.col()),
		owner.id, in.KeyID, in.Window, rules.MaxCalcWindowSamples)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: read calc window %s: %w", in.Key, err)
	}
	type row struct {
		instance, raw string
		ts            time.Time
	}
	var got []row
	named := false
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.instance, &x.raw, &x.ts); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("storage: read calc window %s: %w", in.Key, err)
		}
		named = named || x.instance != ""
		got = append(got, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	for i := len(got) - 1; i >= 0; i-- {
		x := got[i]
		if named && x.instance == "" {
			continue
		}
		v, err := rules.CalcValue(in.ValueType, x.raw)
		if err != nil {
			return nil, nil, &calcValueError{msg: fmt.Sprintf("input %s: %v", in.Alias, err)}
		}
		if due == nil {
			d := x.ts.Add(time.Duration(in.Window) * time.Second)
			due = &d
		}
		samples = append(samples, rules.CalcSample{Value: v, At: x.ts, Instance: x.instance})
	}
	return samples, due, nil
}

// readCalcInput is one key's current values on an owner, held by its key's
// id on its lane, ignoring intended values (calc reads observed and
// calculated as truth, never intended): each instance's newest value, the
// per-instance values only, the aggregate instance ” joining only when the
// key has no other instance on the owner, and an instance whose newest
// sample is older than CalcStaleAfter left out and returned as stale. own is
// the aggregate instance's newest value (nil when there is none), the value
// a system member's record carries. Instances are walked one index probe at
// a time (the series index leads with owner, type and instance). With
// aggregateOnly it reads the aggregate instance alone, as own (an output's
// current value).
func readCalcInput(ctx context.Context, tx pgx.Tx, owner calcOwner, in rules.TypedInput, aggregateOnly bool) (values []any, own any, stale []staleInstance, err error) {
	table, typeCol, value := calcSeries(in.Lane)
	col := owner.col()
	var sql string
	if !aggregateOnly {
		sql = fmt.Sprintf(`with recursive inst(instance) as (
				(select instance from %[1]s where %[4]s = $1::uuid and %[2]s = $2::uuid order by instance limit 1)
				union all
				select (select x.instance from %[1]s x where x.%[4]s = $1::uuid and x.%[2]s = $2::uuid and x.instance > inst.instance
					order by x.instance limit 1) from inst where inst.instance is not null)
			select i.instance, n.v, n.ts from inst i,
				lateral (select %[3]s as v, ts from %[1]s where %[4]s = $1::uuid and %[2]s = $2::uuid and instance = i.instance
					and provenance in ('observed', 'calculated') order by ts desc, id desc limit 1) n
			where i.instance is not null`, table, typeCol, value, col)
	} else {
		sql = fmt.Sprintf(`select ''::text, n.v, n.ts from
				lateral (select %[3]s as v, ts from %[1]s where %[4]s = $1::uuid and %[2]s = $2::uuid and instance = ''
					and provenance in ('observed', 'calculated') order by ts desc, id desc limit 1) n`, table, typeCol, value, col)
	}
	rows, err := tx.Query(ctx, sql, owner.id, in.KeyID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("storage: read calc input %s: %w", in.Key, err)
	}
	type row struct {
		instance, raw string
		ts            time.Time
	}
	var got []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.instance, &x.raw, &x.ts); err != nil {
			rows.Close()
			return nil, nil, nil, fmt.Errorf("storage: read calc input %s: %w", in.Key, err)
		}
		got = append(got, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	read := func(raw string) (any, error) {
		v, err := rules.CalcValue(in.ValueType, raw)
		if err != nil {
			return nil, &calcValueError{msg: fmt.Sprintf("input %s: %v", in.Alias, err)}
		}
		return v, nil
	}
	var named []row
	for _, x := range got {
		if x.instance == "" {
			if own, err = read(x.raw); err != nil {
				return nil, nil, nil, err
			}
			continue
		}
		named = append(named, x)
	}
	if aggregateOnly {
		return nil, own, nil, nil
	}
	if len(named) == 0 {
		named = got
	}
	cutoff := time.Now().Add(-CalcStaleAfter)
	for _, x := range named {
		if x.ts.Before(cutoff) {
			stale = append(stale, staleInstance{Component: owner.id, Key: in.Key, Instance: x.instance})
			continue
		}
		v, err := read(x.raw)
		if err != nil {
			return nil, nil, nil, err
		}
		values = append(values, v)
	}
	return values, own, stale, nil
}

// currentOutput is a rule's output's current value on an owner (its
// aggregate instance's), nil when it has none or cannot be read.
func currentOutput(ctx context.Context, tx pgx.Tx, r CalcRule, owner calcOwner) (any, error) {
	_, own, _, err := readCalcInput(ctx, tx, owner, rules.TypedInput{Key: r.OutputKey, KeyID: r.outputID, Lane: r.OutputLane, ValueType: r.outputType}, true)
	var bad *calcValueError
	if errors.As(err, &bad) {
		return nil, nil
	}
	return own, err
}

// writeCalcOutput writes a rule's output on its owner's aggregate instance
// through the sample-write path, as calculated, unless it equals the
// output's current value (so an unchanged result writes no row, on either
// lane, and storage grows only with change). The lane and type are the ones
// the rule holds its output by, never re-resolved by name. A system-owned
// output is not judged by event rules yet: the evaluator judges component
// samples only (#1002).
func (p *PG) writeCalcOutput(ctx context.Context, tx pgx.Tx, r CalcRule, owner calcOwner, v any) error {
	current, err := currentOutput(ctx, tx, r, owner)
	if err != nil {
		return err
	}
	if current != nil && current == v {
		return nil
	}
	w := calcWriter(&sampleLineage{ruleID: r.ID, ruleName: r.Name, version: r.Version})
	ts := time.Now().UTC()
	if r.OutputLane == rules.LaneMetric {
		f, ok := v.(float64)
		if !ok {
			return fmt.Errorf("storage: calc rule %s produced %T for metric output %s", r.Name, v, r.OutputKey)
		}
		return p.writeMetricSamples(ctx, tx, w, []MetricSampleWrite{{OwnerKind: owner.kind, OwnerID: owner.id, Key: r.OutputKey,
			Value: f, Source: calcSource, TS: ts}})
	}
	return p.writePropertySamples(ctx, tx, w, []PropertySampleWrite{{OwnerKind: owner.kind, OwnerID: owner.id, Key: r.OutputKey,
		Value: rules.FormatCalcValue(v), Source: calcSource, TS: ts}})
}

// calcSource is the source a calculated sample records; its producer is in
// source_rule.
const calcSource = "calc"

// recordCalcState upserts a rule's last outcome on an owner.
func recordCalcState(ctx context.Context, tx pgx.Tx, r CalcRule, owner calcOwner, out calcOutcome) error {
	jsonList := func(v any, n int) []byte {
		if n == 0 {
			return []byte("[]")
		}
		b, _ := json.Marshal(v)
		return b
	}
	var value any
	if out.value != nil {
		b, _ := json.Marshal(out.value)
		value = b
	}
	if _, err := tx.Exec(ctx, `insert into calc_rule_state (rule_id, owner_kind, `+owner.col()+`, computed_at, rule_version, value, missing, stale, skipped, error,
			no_members_since, window_due_at)
		values ($1::uuid, $2, $3::uuid, now(), $4, $5::jsonb, $6::jsonb, $7::jsonb, $8::jsonb, $9, case when $10::bool then now() end, $11)
		on conflict on constraint calc_rule_state_key do update set computed_at = excluded.computed_at, rule_version = excluded.rule_version,
			value = excluded.value, missing = excluded.missing, stale = excluded.stale, skipped = excluded.skipped, error = excluded.error,
			no_members_since = case when $10::bool then coalesce(calc_rule_state.no_members_since, excluded.no_members_since) end,
			window_due_at = excluded.window_due_at`,
		r.ID, owner.kind, owner.id, r.Version, value, jsonList(out.missing, len(out.missing)), jsonList(out.stale, len(out.stale)),
		jsonList(out.skipped, len(out.skipped)), out.err, out.noMembers, out.windowDue); err != nil {
		return fmt.Errorf("storage: record calc state: %w", err)
	}
	return nil
}

// resetCalcRuleWork brings a rule's pending work and state in line with a
// write that created it or bumped its version, in the write's transaction
// (which holds the rule's row FOR UPDATE): the rule's marks and state rows
// go (a disabled rule keeps neither, a narrowed selector's dropped owners
// lose theirs, and every kept owner is recomputed at the new version), and
// an enabled rule marks its owners, so a rule over values already stored
// computes without waiting for a new sample: a component rule every
// component its selector picks that holds a value of one of its inputs, a
// system rule every system its selector picks. One statement each.
func resetCalcRuleWork(ctx context.Context, tx pgx.Tx, r CalcRule) error {
	if _, err := tx.Exec(ctx, `delete from calc_rule_state where rule_id = $1::uuid`, r.ID); err != nil {
		return fmt.Errorf("storage: reset calc state: %w", err)
	}
	if _, err := tx.Exec(ctx, `delete from calc_pending where rule_id = $1::uuid`, r.ID); err != nil {
		return fmt.Errorf("storage: reset calc marks: %w", err)
	}
	if !r.Enabled {
		return nil
	}
	if r.OwnerKind == rules.OwnerSystem {
		if _, err := tx.Exec(ctx, `insert into calc_pending (rule_id, owner_kind, system_id)
			select r.id, 'system', s.id from calc_rule r, system s where r.id = $1::uuid and `+systemSelectedSQL("r", "s")+`
			order by s.id
			on conflict on constraint calc_pending_key do update set marked_at = calc_pending.marked_at`, r.ID); err != nil {
			return fmt.Errorf("storage: mark a written calc rule: %w", err)
		}
		return nil
	}
	metricIDs, propertyIDs := inputTypeIDs(r.Inputs)
	types := r.Selector.ComponentTypes
	if types == nil {
		types = []string{}
	}
	ids := r.Selector.Components
	if ids == nil {
		ids = []string{}
	}
	if _, err := tx.Exec(ctx, `with recursive accepted(id) as (
			select id from component_type where name = any($3::text[])
			union
			select ct.id from component_type ct join accepted a on ct.parent_id = a.id
		)
		insert into calc_pending (rule_id, owner_kind, component_id)
		select $1::uuid, 'component', c.id from component c left join product p on p.id = c.product_id
		where ((cardinality($2::uuid[]) = 0 and cardinality($3::text[]) = 0)
				or c.id = any($2::uuid[]) or p.component_type_id in (select id from accepted))
			and (exists (select 1 from metric m where m.component_id = c.id and m.metric_type_id = any($4::uuid[]))
				or exists (select 1 from property x where x.component_id = c.id and x.property_type_id = any($5::uuid[])))
		order by c.id
		on conflict on constraint calc_pending_key do update set marked_at = calc_pending.marked_at`,
		r.ID, ids, types, metricIDs, propertyIDs); err != nil {
		return fmt.Errorf("storage: mark a written calc rule: %w", err)
	}
	return nil
}

// systemSelectedSQL is the predicate that a system rule (alias r, its
// selector jsonb) selects a system (alias s): an empty selector selects
// every system; else the system is named by id, or its type falls within a
// named system type (its own type or an ancestor, the type tree walked up).
func systemSelectedSQL(r, s string) string {
	return fmt.Sprintf(`(
		(coalesce(jsonb_array_length(%[1]s.selector->'systems'), 0) = 0
			and coalesce(jsonb_array_length(%[1]s.selector->'system_types'), 0) = 0)
		or coalesce(%[1]s.selector->'systems', '[]'::jsonb) ? %[2]s.id::text
		or exists (
			with recursive up(id, parent_id) as (
				select st.id, st.parent_id from system_type st where st.id = %[2]s.system_type_id
				union
				select st.id, st.parent_id from system_type st join up on st.id = up.parent_id
			)
			select 1 from up join system_type t on t.id = up.id
			where coalesce(%[1]s.selector->'system_types', '[]'::jsonb) ? t.name))`, r, s)
}

// calcProbeSQL finds the enabled rules (either owner kind) reading any of a
// write's keys (lane:name, resolved to their ids in the registries), except
// the write's own rule, with each rule's input keys as lane:name: an index
// probe of calc_rule_input by key id (TestCalcProbeUsesTheInputIndexes), so
// a write no rule reads pays for nothing more.
const calcProbeSQL = `with k(lane, name) as (select * from unnest($1::text[], $2::text[])),
	pk as (select pt.id from property_type pt join k on k.lane = 'property' and k.name = pt.name),
	mk as (select mt.id from metric_type mt join k on k.lane = 'metric' and k.name = mt.name),
	hit as (select i.rule_id from calc_rule_input i join pk on i.property_type_id = pk.id
		union select i.rule_id from calc_rule_input i join mk on i.metric_type_id = mk.id)
	select r.id::text, r.owner_kind, r.selector,
		(select array_agg(i.lane || ':' || coalesce(ipt.name, imt.name)) from calc_rule_input i
			left join property_type ipt on ipt.id = i.property_type_id left join metric_type imt on imt.id = i.metric_type_id
			where i.rule_id = r.id)
	from calc_rule r join hit on hit.rule_id = r.id
	where r.enabled and r.id::text <> $3 order by r.id`

// markSystemRulesBySamples marks, for the system rules a write's keys
// reach, every selected system that a sampled component is a role-bound
// member of and whose rule reads a key that component's samples carried.
// One statement, the marks in (rule, system) order.
func markSystemRulesBySamples(ctx context.Context, tx pgx.Tx, ruleIDs []string, marks *calcMarks) error {
	var comps, lanes, keys []string
	for comp, ks := range marks.keys {
		for k := range ks {
			lane, name, _ := strings.Cut(k, ":")
			comps, lanes, keys = append(comps, comp), append(lanes, lane), append(keys, name)
		}
	}
	if len(comps) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `with samples(component_id, lane, key) as (select * from unnest($2::uuid[], $3::text[], $4::text[])),
		hits as (
			select distinct r.id as rule_id, s.id as system_id
			from calc_rule r
			join calc_rule_input i on i.rule_id = r.id
			left join property_type ipt on ipt.id = i.property_type_id
			left join metric_type imt on imt.id = i.metric_type_id
			join samples x on x.lane = i.lane and x.key = coalesce(ipt.name, imt.name)
			join system_role_assignment ra on ra.component_id = x.component_id
			join system s on s.id = ra.system_id
			join system_role sr on sr.id = ra.role_id and `+roleOfSystemSQL("sr", "s")+`
			where r.id = any($1::uuid[]) and `+systemSelectedSQL("r", "s")+`
		)
		insert into calc_pending (rule_id, owner_kind, system_id)
		select rule_id, 'system', system_id from hits order by rule_id, system_id
		on conflict on constraint calc_pending_key do update set marked_at = calc_pending.marked_at`, ruleIDs, comps, lanes, keys); err != nil {
		return fmt.Errorf("storage: mark system calc rules: %w", err)
	}
	return nil
}

// markSystemCalcRules brings the system rules in line with a write that
// changed which components are a system's role-bound members, or which rules
// select it (a role assigned or unassigned, a role declared or withdrawn, a
// system created, retyped or re-standarded): every enabled system rule
// selecting one of the systems is marked on it, and a rule that no longer
// selects one drops its state and mark there. Called before the write's
// health recompute, so its marks come before any health lock.
func markSystemCalcRules(ctx context.Context, tx pgx.Tx, systemIDs ...string) error {
	if len(systemIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `insert into calc_pending (rule_id, owner_kind, system_id)
		select r.id, 'system', s.id from calc_rule r, system s
		where r.enabled and r.owner_kind = 'system' and s.id = any($1::uuid[]) and `+systemSelectedSQL("r", "s")+`
		order by r.id, s.id
		on conflict on constraint calc_pending_key do update set marked_at = calc_pending.marked_at`, systemIDs); err != nil {
		return fmt.Errorf("storage: mark system calc rules: %w", err)
	}
	// The deselected marks go first and skip a mark a serve holds (the serve
	// rechecks selection and drops its own work: computeServed), so this
	// write never waits on a serve that may wait on it; then the state rows,
	// in the order a serve takes them (mark, then state).
	if _, err := tx.Exec(ctx, `delete from calc_pending d where (d.rule_id, d.system_id) in (
			select p.rule_id, p.system_id from calc_pending p join calc_rule r on r.id = p.rule_id join system s on s.id = p.system_id
			where s.id = any($1::uuid[]) and not `+systemSelectedSQL("r", "s")+`
			for update of p skip locked)`, systemIDs); err != nil {
		return fmt.Errorf("storage: drop deselected system calc marks: %w", err)
	}
	if _, err := tx.Exec(ctx, `delete from calc_rule_state d using calc_rule r, system s
		where d.rule_id = r.id and d.system_id = s.id and s.id = any($1::uuid[]) and not `+systemSelectedSQL("r", "s"),
		systemIDs); err != nil {
		return fmt.Errorf("storage: drop deselected system calc state: %w", err)
	}
	return nil
}

// markComponentCalcRules is markSystemCalcRules for a component whose
// product changed, and with it the type its rules select by (#1015): every
// enabled component rule selecting it is marked there when it holds a value
// of one of the rule's inputs (the condition a written rule marks by, so a
// rule it came into computes over the values already stored), and a rule
// that no longer selects it drops its mark and state there. Selection is the
// event rules' component matcher, the one the serve rechecks by.
func (p *PG) markComponentCalcRules(ctx context.Context, tx pgx.Tx, componentID string) error {
	rows, err := tx.Query(ctx, `select r.id::text, r.selector, exists (select 1 from calc_rule_input i where i.rule_id = r.id and (
			exists (select 1 from metric m where m.component_id = $1::uuid and m.metric_type_id = i.metric_type_id)
			or exists (select 1 from property x where x.component_id = $1::uuid and x.property_type_id = i.property_type_id)))
		from calc_rule r where r.enabled and r.owner_kind = 'component' order by r.id`, componentID)
	if err != nil {
		return fmt.Errorf("storage: find component calc rules: %w", err)
	}
	type candidate struct {
		id   string
		sel  CalcRuleSelector
		held bool
	}
	var found []candidate
	for rows.Next() {
		var c candidate
		var sel []byte
		if err := rows.Scan(&c.id, &sel, &c.held); err != nil {
			rows.Close()
			return fmt.Errorf("storage: find component calc rules: %w", err)
		}
		if err := json.Unmarshal(sel, &c.sel); err != nil {
			rows.Close()
			return fmt.Errorf("storage: calc rule %s selector: %w", c.id, err)
		}
		found = append(found, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	m := newRuleMatcher(p, nil)
	o, err := m.ownerFacts(ctx, tx, componentID)
	if err != nil {
		return err
	}
	marked, dropped := []string{}, []string{}
	for _, c := range found {
		ok, err := m.selects(ctx, tx, EventRule{ID: c.id, Selector: c.sel.components()}, o)
		if err != nil {
			return err
		}
		switch {
		case !ok:
			dropped = append(dropped, c.id)
		case c.held:
			marked = append(marked, c.id)
		}
	}
	if len(marked) > 0 {
		if _, err := tx.Exec(ctx, `insert into calc_pending (rule_id, owner_kind, component_id)
			select r, 'component', $2::uuid from unnest($1::uuid[]) as r order by r
			on conflict on constraint calc_pending_key do update set marked_at = calc_pending.marked_at`, marked, componentID); err != nil {
			return fmt.Errorf("storage: mark component calc rules: %w", err)
		}
	}
	if len(dropped) == 0 {
		return nil
	}
	// Marks first, skipping one a serve holds (it rechecks selection and
	// drops its own work), then the state rows: the system path's order.
	if _, err := tx.Exec(ctx, `delete from calc_pending d where (d.rule_id, d.component_id) in (
			select p.rule_id, p.component_id from calc_pending p where p.component_id = $2::uuid and p.rule_id = any($1::uuid[])
			for update of p skip locked)`, dropped, componentID); err != nil {
		return fmt.Errorf("storage: drop deselected component calc marks: %w", err)
	}
	if _, err := tx.Exec(ctx, `delete from calc_rule_state where component_id = $2::uuid and rule_id = any($1::uuid[])`,
		dropped, componentID); err != nil {
		return fmt.Errorf("storage: drop deselected component calc state: %w", err)
	}
	return nil
}
