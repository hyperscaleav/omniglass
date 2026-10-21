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
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

// CalcRule is the authored config that derives one output key on an owner
// from named inputs (#995, epic #525): a sibling of EventRule (ADR-0144
// ruling 1). It reads Inputs (sample keys, each named by As), folds them with
// Reduce (a named reducer or an expression over the names), and writes
// OutputKey on each owner of OwnerKind its Selector picks. Version bumps on
// every update. Official rules would ship with the boot seed and are
// read-only; none ships yet.
type CalcRule struct {
	ID          string
	Name        string
	Label       string
	Description string
	OwnerKind   string
	Selector    EventRuleSelector
	Inputs      []rules.CalcInput
	Reduce      rules.CalcReduce
	OutputKey   string
	OutputLane  rules.Lane
	Enabled     bool
	Official    bool
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// CalcError is why the stored reduce does not compile against the
	// registries as they are now (an input key deleted or retyped, a row
	// written outside the gateway); nil when it compiles. Read beside the
	// rule, never stored or audited.
	CalcError *CalcFault `json:"-"`
	// outputType is the output's value type, from the registry row the rule
	// holds by id on its recorded lane, never re-resolved by name.
	outputType string
}

// CalcFault is a stored rule's compile error as a reader sees it: the field
// at fault, the 1-based line and column when the reduce's expression is what
// failed (0 otherwise), and the message.
type CalcFault struct {
	Field   string
	Line    int
	Column  int
	Message string
}

// CalcRuleSpec is a rule to create; its selector's components may be given
// by name or id.
type CalcRuleSpec struct {
	Name        string
	Label       string
	Description string
	OwnerKind   string
	Selector    EventRuleSelector
	Inputs      []rules.CalcInput
	Reduce      rules.CalcReduce
	OutputKey   string
	Enabled     *bool
}

// CalcRulePatch carries an update; nil is unchanged. The owner kind is fixed
// at create: a rule for components and one for systems read their inputs
// differently, so changing it is a new rule.
type CalcRulePatch struct {
	Label       *string
	Description *string
	Selector    *EventRuleSelector
	Inputs      *[]rules.CalcInput
	Reduce      *rules.CalcReduce
	OutputKey   *string
	Enabled     *bool
}

// Calc rule sentinels.
var (
	ErrCalcRuleNotFound  = errors.New("storage: calc rule not found")
	ErrCalcRuleExists    = errors.New("storage: a calc rule already has that name")
	ErrCalcRuleOfficial  = errors.New("storage: an official calc rule is read-only")
	ErrCalcRuleInvalid   = errors.New("storage: calc rule is invalid")
	ErrCalcRuleForbidden = errors.New("storage: writing a calc rule needs an all-scope grant")
)

// CalcCycleError is a rule refused because it would close a loop in the key
// graph: it is ErrCalcRuleInvalid and rules.ErrCalcCycle, naming the chain.
type CalcCycleError struct {
	Cycle *rules.Cycle
}

func (e *CalcCycleError) Error() string {
	return fmt.Sprintf("%v: the rule closes a cycle in the calc key graph: %s (rules %s)",
		ErrCalcRuleInvalid, e.Cycle, strings.Join(e.Cycle.Rules, ", "))
}

func (e *CalcCycleError) Unwrap() []error { return []error{ErrCalcRuleInvalid, rules.ErrCalcCycle} }

// invalidCalc is a refusal naming the field at fault.
func invalidCalc(field, format string, a ...any) error {
	return fmt.Errorf("%w: %w", ErrCalcRuleInvalid, &rules.CalcError{Field: field, Err: fmt.Errorf(format, a...)})
}

// calcRuleSelect reads a rule with its output key's name, from whichever
// registry its lane names.
const calcRuleSelect = `select c.id, c.name, coalesce(c.label, ''), c.description, c.owner_kind, c.selector, c.inputs, c.reduce,
	c.output_lane, coalesce(pt.name, mt.name, ''), c.enabled, c.official, c.version, c.created_at, c.updated_at,
	case when c.output_lane = 'metric' then 'number' when pt.data_type = 'bool' then 'bool' else 'text' end
	from calc_rule c
	left join property_type pt on pt.id = c.output_property_type_id
	left join metric_type mt on mt.id = c.output_metric_type_id`

func scanCalcRule(row pgx.Row) (CalcRule, error) {
	var r CalcRule
	var sel, inputs, reduce []byte
	var lane string
	if err := row.Scan(&r.ID, &r.Name, &r.Label, &r.Description, &r.OwnerKind, &sel, &inputs, &reduce, &lane, &r.OutputKey,
		&r.Enabled, &r.Official, &r.Version, &r.CreatedAt, &r.UpdatedAt, &r.outputType); err != nil {
		return r, err
	}
	r.OutputLane = rules.Lane(lane)
	if err := json.Unmarshal(sel, &r.Selector); err != nil {
		return r, fmt.Errorf("storage: calc rule %q selector: %w", r.Name, err)
	}
	if err := json.Unmarshal(inputs, &r.Inputs); err != nil {
		return r, fmt.Errorf("storage: calc rule %q inputs: %w", r.Name, err)
	}
	if err := json.Unmarshal(reduce, &r.Reduce); err != nil {
		return r, fmt.Errorf("storage: calc rule %q reduce: %w", r.Name, err)
	}
	return r, nil
}

// readCalcRule reads one rule by name or uuid (a name is never uuid-shaped),
// optionally locking it.
func readCalcRule(ctx context.Context, q querier, ref string, lock bool) (CalcRule, error) {
	sql := calcRuleSelect + ` where c.name = $1 or c.id::text = $1`
	if lock {
		sql += ` for update of c`
	}
	return readCalcRuleSQL(ctx, q, sql, ref)
}

func readCalcRuleSQL(ctx context.Context, q querier, sql, ref string) (CalcRule, error) {
	r, err := scanCalcRule(q.QueryRow(ctx, sql, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrCalcRuleNotFound
	}
	if err != nil {
		return r, fmt.Errorf("storage: read calc rule %q: %w", ref, err)
	}
	return r, nil
}

// ListCalcRules lists every rule, by name. Rules read like a registry, so
// every rule is listed whatever the caller's scope.
func (p *PG) ListCalcRules(ctx context.Context) ([]CalcRule, error) {
	rows, err := p.pool.Query(ctx, calcRuleSelect+` order by c.name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list calc rules: %w", err)
	}
	defer rows.Close()
	out := []CalcRule{}
	for rows.Next() {
		r, err := scanCalcRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	types, err := keyValueTypes(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].CalcError = p.calcFault(out[i], types)
	}
	return out, nil
}

// withCalcFault sets r's CalcError from the registries as they are now.
func (p *PG) withCalcFault(ctx context.Context, r *CalcRule) (*CalcRule, error) {
	types, err := keyValueTypes(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	r.CalcError = p.calcFault(*r, types)
	return r, nil
}

// GetCalcRule reads one rule by name or uuid, whatever the caller's scope.
func (p *PG) GetCalcRule(ctx context.Context, ref string) (*CalcRule, error) {
	r, err := readCalcRule(ctx, p.pool, ref, false)
	if err != nil {
		return nil, err
	}
	return p.withCalcFault(ctx, &r)
}

// sampleKey is a key resolved in the registries: its lane, its value type,
// and its id in its lane's registry.
type sampleKey struct {
	lane      rules.Lane
	valueType string
	id        string
}

// resolveSampleKey finds a key in the property registry, else the metric
// registry (the order resolveRuleLane takes).
func resolveSampleKey(ctx context.Context, q querier, key string) (sampleKey, bool, error) {
	var k sampleKey
	var dataType string
	err := q.QueryRow(ctx, `select id::text, data_type from property_type where name = $1`, key).Scan(&k.id, &dataType)
	if err == nil {
		k.lane = rules.LaneProperty
		k.valueType = rules.ValueTypeFor(k.lane, dataType)
		return k, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return k, false, fmt.Errorf("storage: resolve sample key %q: %w", key, err)
	}
	err = q.QueryRow(ctx, `select id::text from metric_type where name = $1`, key).Scan(&k.id)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, false, nil
	}
	if err != nil {
		return k, false, fmt.Errorf("storage: resolve sample key %q: %w", key, err)
	}
	k.lane, k.valueType = rules.LaneMetric, rules.ValueNumber
	return k, true, nil
}

// calcChecks says which of a rule's parts to validate: all of them on
// create; on update only what changed from the stored rule, so a rule whose
// input key was since deleted can still be relabelled or disabled.
type calcChecks struct {
	compile, output, selector bool
	kept                      EventRuleSelector
}

var calcCheckAll = calcChecks{compile: true, output: true, selector: true}

// prepareCalcRule validates a rule, resolving its output key's id and lane
// and its selector's components to ids. The checks, in order:
//
//   - the name, the owner kind and, for a system owner, an empty selector
//     (a system rule runs on every system until system selection lands
//     with system-level evaluation, #525's third slice);
//   - health is neither an input nor the output (#525 decision 3: an alarm
//     moves health, so a rule reading it could feed back through an event
//     rule, and health has its own producer, the rollup);
//   - every input key is registered, and the reduce compiles against the
//     inputs and yields the output key's type (rules.CompileCalc);
//   - an enabled rule closes no loop in the graph of every enabled rule's
//     input keys to its output key (rules.CalcCycle), and is the only
//     enabled rule writing its output key, both read under a transaction
//     lock so two writes cannot each close half of a loop or both take a
//     key (checkCalcGraph);
//   - the output key is one no collection writes (collectedBy).
func prepareCalcRule(ctx context.Context, q txQuerier, r *CalcRule, check calcChecks, outIDs *[2]any) error {
	if err := ValidateName("calc_rule", r.Name); err != nil {
		return fmt.Errorf("%w: %w", ErrCalcRuleInvalid, err)
	}
	if r.OwnerKind != rules.OwnerComponent && r.OwnerKind != rules.OwnerSystem {
		return invalidCalc("owner_kind", "a calc rule derives for a component or a system, not %q", r.OwnerKind)
	}
	if r.OwnerKind == rules.OwnerSystem && (len(r.Selector.ComponentTypes) > 0 || len(r.Selector.Components) > 0) {
		return invalidCalc("selector", "a system rule runs on every system; its selector is empty")
	}
	for i, in := range r.Inputs {
		if in.Key == healthKey {
			return invalidCalc(fmt.Sprintf("inputs[%d].key", i), "%s may not be an input: an alarm moves it, so a rule reading it could feed back through an event rule", healthKey)
		}
	}
	if r.OutputKey == healthKey {
		return invalidCalc("output_key", "%s is written by the health rollup, not by a calc rule", healthKey)
	}
	out, ok, err := resolveSampleKey(ctx, q, r.OutputKey)
	if err != nil {
		return err
	}
	if !ok {
		return invalidCalc("output_key", "%q is not a registered property or metric", r.OutputKey)
	}
	r.OutputLane = out.lane
	*outIDs = [2]any{nil, nil}
	if out.lane == rules.LaneProperty {
		outIDs[0] = out.id
	} else {
		outIDs[1] = out.id
	}
	if check.compile {
		typed := make([]rules.TypedInput, len(r.Inputs))
		for i, in := range r.Inputs {
			k, ok, err := resolveSampleKey(ctx, q, in.Key)
			if err != nil {
				return err
			}
			if !ok {
				return invalidCalc(fmt.Sprintf("inputs[%d].key", i), "%q is not a registered property or metric", in.Key)
			}
			typed[i] = rules.TypedInput{As: in.As, Key: in.Key, ValueType: k.valueType}
		}
		if _, err := rules.CompileCalc(r.OwnerKind, typed, r.Reduce, out.valueType); err != nil {
			return fmt.Errorf("%w: %w", ErrCalcRuleInvalid, err)
		}
	}
	if r.Enabled {
		if err := checkCalcGraph(ctx, q, *r, *outIDs); err != nil {
			return err
		}
	}
	if check.output {
		if err := collectedBy(ctx, q, r.OutputKey); err != nil {
			return err
		}
	}
	if check.selector {
		if err := resolveSelector(ctx, q, &r.Selector, check.kept); err != nil {
			var bad selectorError
			if errors.As(err, &bad) {
				return invalidCalc("selector", "%s", bad.msg)
			}
			return err
		}
	}
	return nil
}

// calcGraphLock serializes the writes that read and change the key graph.
const calcGraphLock = "calc_rule/graph"

// checkCalcGraph reads every other enabled rule, under the graph lock, and
// refuses a rule that closes a loop or that writes an output key another
// enabled rule writes (one writer per key: two would mix their values on an
// owner by newest ts, the same reason a collected key is refused). The loop
// is judged first, so a rule that does both is told about the loop.
func checkCalcGraph(ctx context.Context, q txQuerier, r CalcRule, outIDs [2]any) error {
	if err := lockAdvisory(ctx, q, calcGraphLock); err != nil {
		return err
	}
	rows, err := q.Query(ctx, `select c.id::text, c.name, c.inputs, coalesce(pt.name, mt.name, '')
		from calc_rule c
		left join property_type pt on pt.id = c.output_property_type_id
		left join metric_type mt on mt.id = c.output_metric_type_id
		where c.enabled`)
	if err != nil {
		return fmt.Errorf("storage: read the calc graph: %w", err)
	}
	defer rows.Close()
	var graph []rules.CalcEdge
	for rows.Next() {
		var id, name, output string
		var raw []byte
		if err := rows.Scan(&id, &name, &raw, &output); err != nil {
			return fmt.Errorf("storage: read the calc graph: %w", err)
		}
		if id == r.ID {
			continue
		}
		var inputs []rules.CalcInput
		if err := json.Unmarshal(raw, &inputs); err != nil {
			return fmt.Errorf("storage: calc rule %q inputs: %w", name, err)
		}
		graph = append(graph, rules.CalcEdge{Rule: name, Inputs: inputKeys(inputs), Output: output})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: read the calc graph: %w", err)
	}
	if c := rules.CalcCycle(graph, rules.CalcEdge{Rule: r.Name, Inputs: inputKeys(r.Inputs), Output: r.OutputKey}); c != nil {
		return &CalcCycleError{Cycle: c}
	}
	var writer string
	err = q.QueryRow(ctx, `select name from calc_rule where enabled and id::text <> $1
		and (output_property_type_id = $2::uuid or output_metric_type_id = $3::uuid) order by name limit 1`,
		r.ID, outIDs[0], outIDs[1]).Scan(&writer)
	switch {
	case err == nil:
		return invalidCalc("output_key", "%q is written by the enabled calc rule %s; one enabled rule writes a key", r.OutputKey, writer)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("storage: check calc output %q writers: %w", r.OutputKey, err)
	}
	return nil
}

func inputKeys(inputs []rules.CalcInput) []string {
	out := make([]string, len(inputs))
	for i, in := range inputs {
		out[i] = in.Key
	}
	return out
}

// collectedBy refuses an output key a collection writes (#525 decision 6):
// a current value would mix a collected producer with a calculated one by
// newest ts. It reads declarations only, never sample history (one
// hand-posted or simulated sample would otherwise block a key forever):
//
//   - the native probes' keys (probekeys, the one declaration the probes
//     emit from);
//   - a stored driver whose spec declares the key among a poll's or a
//     listener's emits.
//
// What it cannot catch: a driver created or edited to emit the key after the
// rule exists (the ingest-side guard is its own issue).
func collectedBy(ctx context.Context, q querier, key string) error {
	if probekeys.IsNative(key) {
		return invalidCalc("output_key", "%q is collected: a native probe writes it, and a calc rule may not write a key a collection writes", key)
	}
	var driver string
	err := q.QueryRow(ctx, `select d.name from driver d
		cross join lateral jsonb_array_elements(coalesce(d.spec->'polls', '[]'::jsonb) || coalesce(d.spec->'listeners', '[]'::jsonb)) f
		cross join lateral jsonb_array_elements(coalesce(f->'emits', '[]'::jsonb)) e
		where e->>'name' = $1 order by d.name limit 1`, key).Scan(&driver)
	switch {
	case err == nil:
		return invalidCalc("output_key", "%q is collected: the driver %s emits it, and a calc rule may not write a key a collection writes", key, driver)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("storage: check calc output %q against the drivers: %w", key, err)
	}
	return nil
}

// mapCalcWriteErr turns a write's constraint violation into a sentinel.
func mapCalcWriteErr(err error, name string) error {
	if isUniqueViolation(err) {
		return ErrCalcRuleExists
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return invalidCalc("output_key", "the output key does not exist")
	}
	return fmt.Errorf("storage: write calc rule %q: %w", name, err)
}

func calcArgs(r CalcRule) (sel, inputs, reduce []byte, err error) {
	if sel, err = json.Marshal(r.Selector); err != nil {
		return
	}
	if inputs, err = json.Marshal(r.Inputs); err != nil {
		return
	}
	reduce, err = json.Marshal(r.Reduce)
	return
}

// CreateCalcRule authors an operator rule, audited; it needs an all-scope
// create grant, since a rule's selector reaches the whole fleet.
func (p *PG) CreateCalcRule(ctx context.Context, actorID string, spec CalcRuleSpec, create scope.Set) (*CalcRule, error) {
	if !create.All {
		return nil, ErrCalcRuleForbidden
	}
	r := CalcRule{Name: spec.Name, Label: spec.Label, Description: spec.Description, OwnerKind: spec.OwnerKind,
		Selector: spec.Selector, Inputs: spec.Inputs, Reduce: spec.Reduce, OutputKey: spec.OutputKey, Enabled: true}
	if spec.Enabled != nil {
		r.Enabled = *spec.Enabled
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create calc rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var out [2]any
	if err := prepareCalcRule(ctx, tx, &r, calcCheckAll, &out); err != nil {
		return nil, err
	}
	sel, inputs, reduce, err := calcArgs(r)
	if err != nil {
		return nil, err
	}
	var id string
	if err := tx.QueryRow(ctx, `insert into calc_rule (name, label, description, owner_kind, selector, inputs, reduce,
		output_lane, output_property_type_id, output_metric_type_id, enabled)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9::uuid, $10::uuid, $11) returning id::text`,
		r.Name, labelOrNull(r.Label), r.Description, r.OwnerKind, sel, inputs, reduce, string(r.OutputLane), out[0], out[1], r.Enabled).Scan(&id); err != nil {
		return nil, mapCalcWriteErr(err, r.Name)
	}
	created, err := readCalcRule(ctx, tx, id, false)
	if err != nil {
		return nil, err
	}
	if err := resetCalcRuleWork(ctx, tx, created); err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "calc_rule", created.ID, nil, created); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create calc rule: %w", err)
	}
	return p.withCalcFault(ctx, &created)
}

func lockMutableCalcRule(ctx context.Context, tx pgx.Tx, ref string) (CalcRule, error) {
	r, err := readCalcRule(ctx, tx, ref, true)
	if err != nil {
		return r, err
	}
	if r.Official {
		return r, ErrCalcRuleOfficial
	}
	return r, nil
}

// sameAuthoredCalc reports whether two versions of a rule say the same thing.
func sameAuthoredCalc(a, b CalcRule) bool {
	type authored struct {
		Label, Description, OwnerKind, OutputKey string
		Selector                                 EventRuleSelector
		Inputs                                   []rules.CalcInput
		Reduce                                   rules.CalcReduce
		Enabled                                  bool
	}
	pick := func(r CalcRule) authored {
		return authored{r.Label, r.Description, r.OwnerKind, r.OutputKey, r.Selector, r.Inputs, r.Reduce, r.Enabled}
	}
	return sameRuleJSON(pick(a), pick(b))
}

// UpdateCalcRule patches an operator rule, bumping its version, audited; it
// needs an all-scope update grant. A patch that changes nothing writes
// nothing.
func (p *PG) UpdateCalcRule(ctx context.Context, actorID, ref string, patch CalcRulePatch, action scope.Set) (*CalcRule, error) {
	if !action.All {
		return nil, ErrCalcRuleForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update calc rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, err := lockMutableCalcRule(ctx, tx, ref)
	if err != nil {
		return nil, err
	}
	r := old
	r.Inputs = slices.Clone(old.Inputs)
	if patch.Label != nil {
		r.Label = *patch.Label
	}
	if patch.Description != nil {
		r.Description = *patch.Description
	}
	if patch.Selector != nil {
		r.Selector = *patch.Selector
	}
	if patch.Inputs != nil {
		r.Inputs = *patch.Inputs
	}
	if patch.Reduce != nil {
		r.Reduce = *patch.Reduce
	}
	if patch.OutputKey != nil {
		r.OutputKey = *patch.OutputKey
	}
	if patch.Enabled != nil {
		r.Enabled = *patch.Enabled
	}
	// Judged on what changed from the stored rule: unchanged inputs, reduce
	// and output compiled when they were written.
	outputChanged := r.OutputKey != old.OutputKey
	check := calcChecks{
		compile: outputChanged || !sameRuleJSON(r.Inputs, old.Inputs) || !sameRuleJSON(r.Reduce, old.Reduce) ||
			(r.Enabled && !old.Enabled),
		output:   outputChanged,
		selector: patch.Selector != nil,
		kept:     old.Selector,
	}
	var out [2]any
	if err := prepareCalcRule(ctx, tx, &r, check, &out); err != nil {
		return nil, err
	}
	if sameAuthoredCalc(old, r) {
		return p.withCalcFault(ctx, &old)
	}
	sel, inputs, reduce, err := calcArgs(r)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `update calc_rule set label = $2, description = $3, selector = $4, inputs = $5, reduce = $6,
		output_lane = $7, output_property_type_id = $8::uuid, output_metric_type_id = $9::uuid, enabled = $10,
		version = version + 1, updated_at = now() where id = $1`,
		old.ID, labelOrNull(r.Label), r.Description, sel, inputs, reduce, string(r.OutputLane), out[0], out[1], r.Enabled); err != nil {
		return nil, mapCalcWriteErr(err, old.Name)
	}
	updated, err := readCalcRule(ctx, tx, old.ID, false)
	if err != nil {
		return nil, err
	}
	if err := resetCalcRuleWork(ctx, tx, updated); err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "calc_rule", old.ID, old, updated); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update calc rule: %w", err)
	}
	return p.withCalcFault(ctx, &updated)
}

// RenameCalcRule moves an operator rule's name (ADR-0062, ADR-0076),
// audited; it needs an all-scope update grant. A separate act from the patch:
// it breaks only references held outside the platform. Inside, the audit rows
// hold the uuid, and the key graph is keyed by keys, not names, so a rename
// is not a material change and bumps no version. An official rule keeps its
// name.
func (p *PG) RenameCalcRule(ctx context.Context, actorID, ref, newName string, action scope.Set) (*CalcRule, error) {
	if !action.All {
		return nil, ErrCalcRuleForbidden
	}
	if err := ValidateName("calc_rule", newName); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCalcRuleInvalid, err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin rename calc rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, err := lockMutableCalcRule(ctx, tx, ref)
	if err != nil {
		return nil, err
	}
	if old.Name == newName {
		return p.withCalcFault(ctx, &old)
	}
	if _, err := tx.Exec(ctx, `update calc_rule set name = $2, updated_at = now() where id = $1`, old.ID, newName); err != nil {
		return nil, mapCalcWriteErr(err, old.Name)
	}
	renamed, err := readCalcRule(ctx, tx, old.ID, false)
	if err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "rename", "calc_rule", old.ID, old, renamed); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit rename calc rule: %w", err)
	}
	return p.withCalcFault(ctx, &renamed)
}

// DeleteCalcRule removes an operator rule, audited; it needs an all-scope
// delete grant. The samples it wrote keep its name and version in their
// lineage (source_rule, source_rule_version), so its history outlives it.
func (p *PG) DeleteCalcRule(ctx context.Context, actorID, ref string, action scope.Set) error {
	if !action.All {
		return ErrCalcRuleForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete calc rule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	old, err := lockMutableCalcRule(ctx, tx, ref)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from calc_rule where id = $1`, old.ID); err != nil {
		return fmt.Errorf("storage: delete calc rule %q: %w", old.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "calc_rule", old.ID, old, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
