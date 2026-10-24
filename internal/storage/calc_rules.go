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

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// CalcRule is the authored config that derives one output key on an owner
// from aliased inputs (#995, epic #525): a sibling of EventRule (ADR-0144
// ruling 1). It reads Inputs (sample keys held by id, each bound by its
// Alias, #1018), reduces them with Reduce (an expression over the aliases),
// and writes OutputKey on each owner of OwnerKind its Selector picks. Version bumps on
// every update. Official rules would ship with the boot seed and are
// read-only; none ships yet.
type CalcRule struct {
	ID          string
	Name        string
	Label       string
	Description string
	OwnerKind   string
	Selector    CalcRuleSelector
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
	// CalcWarnings are the warnings the stored reduce compiles with
	// (#1021): notes, never a fault; the rule computes all the same.
	CalcWarnings []CalcFault `json:"-"`
	// outputType is the output's value type, from the registry row the rule
	// holds by id on its recorded lane, never re-resolved by name; outputID
	// is that row's id.
	outputType string
	outputID   string
	// inputTypes are the inputs' value types, in order, from the registry
	// rows the rule holds them by.
	inputTypes []string
	// candidate is a preview's unsaved rule: compiled each time, never
	// cached under an id and version it does not have.
	candidate bool
}

// CalcFault is a stored rule's compile error, or a warning, as a reader
// sees it: the field at fault, the 1-based line and column when the reduce's
// expression is what failed (0 otherwise), the message, and the diagnostic's
// code when a registered rule (#1021) gave it.
type CalcFault struct {
	Field   string
	Line    int
	Column  int
	Message string
	Code    string
}

// CalcRuleSelector picks a rule's owners. A component rule's is the event
// rule's shape: component types (a component whose type falls within one)
// and components; a system rule's mirrors it over systems: system types (a
// system whose type falls within one, subtypes included) and systems. Names
// or ids on write, ids on read for the entities; both lists empty is every
// owner of the rule's kind. A rule fills only its own kind's pair.
type CalcRuleSelector struct {
	ComponentTypes []string `json:"component_types,omitempty"`
	Components     []string `json:"components,omitempty"`
	SystemTypes    []string `json:"system_types,omitempty"`
	Systems        []string `json:"systems,omitempty"`
}

func (s CalcRuleSelector) components() EventRuleSelector {
	return EventRuleSelector{ComponentTypes: s.ComponentTypes, Components: s.Components}
}

// CalcRuleSpec is a rule to create; its selector's components may be given
// by name or id.
type CalcRuleSpec struct {
	Name        string
	Label       string
	Description string
	OwnerKind   string
	Selector    CalcRuleSelector
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
	Selector    *CalcRuleSelector
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

// calcRuleSelect reads a rule with its inputs (each key's name, id, lane and
// data type, read now from the row the input holds by id) and its output
// key's name and id, from whichever registry its lane names.
const calcRuleSelect = `select c.id, c.name, coalesce(c.label, ''), c.description, c.owner_kind, c.selector,
	(select coalesce(jsonb_agg(jsonb_build_object('alias', i.alias, 'key', coalesce(ipt.name, imt.name),
			'key_id', coalesce(i.property_type_id, i.metric_type_id), 'lane', i.lane, 'data_type', coalesce(ipt.data_type, ''))
			order by i.position), '[]'::jsonb)
		from calc_rule_input i left join property_type ipt on ipt.id = i.property_type_id
		left join metric_type imt on imt.id = i.metric_type_id where i.rule_id = c.id),
	c.reduce, c.output_lane, coalesce(pt.name, mt.name, ''), coalesce(c.output_property_type_id, c.output_metric_type_id)::text,
	c.enabled, c.official, c.version, c.created_at, c.updated_at,
	case when c.output_lane = 'metric' then 'number' when pt.data_type = 'bool' then 'bool' else 'text' end
	from calc_rule c
	left join property_type pt on pt.id = c.output_property_type_id
	left join metric_type mt on mt.id = c.output_metric_type_id`

func scanCalcRule(row pgx.Row) (CalcRule, error) {
	var r CalcRule
	var sel, inputs, reduce []byte
	var lane string
	if err := row.Scan(&r.ID, &r.Name, &r.Label, &r.Description, &r.OwnerKind, &sel, &inputs, &reduce, &lane, &r.OutputKey, &r.outputID,
		&r.Enabled, &r.Official, &r.Version, &r.CreatedAt, &r.UpdatedAt, &r.outputType); err != nil {
		return r, err
	}
	r.OutputLane = rules.Lane(lane)
	if err := json.Unmarshal(sel, &r.Selector); err != nil {
		return r, fmt.Errorf("storage: calc rule %q selector: %w", r.Name, err)
	}
	var stored []struct {
		rules.CalcInput
		DataType string `json:"data_type"`
	}
	if err := json.Unmarshal(inputs, &stored); err != nil {
		return r, fmt.Errorf("storage: calc rule %q inputs: %w", r.Name, err)
	}
	r.Inputs = make([]rules.CalcInput, len(stored))
	r.inputTypes = make([]string, len(stored))
	for i, in := range stored {
		r.Inputs[i] = in.CalcInput
		r.inputTypes[i] = rules.ValueTypeFor(in.Lane, in.DataType)
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
	for i := range out {
		p.withCalcNotes(&out[i])
	}
	return out, nil
}

// withCalcFault sets r's CalcError from the registries as they are now (the
// key types its read resolved).
func (p *PG) withCalcFault(_ context.Context, r *CalcRule) (*CalcRule, error) {
	p.withCalcNotes(r)
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
// its id in its lane's registry, and its name now.
type sampleKey struct {
	lane      rules.Lane
	valueType string
	dataType  string
	id        string
	name      string
}

// resolveSampleKey finds a key by name, in the property registry, else the
// metric registry (the order resolveRuleLane takes), or by id in either.
func resolveSampleKey(ctx context.Context, q querier, ref string) (sampleKey, bool, error) {
	col := "name"
	if isUUID(ref) {
		col = "id::text"
	}
	var k sampleKey
	err := q.QueryRow(ctx, `select id::text, name, data_type from property_type where `+col+` = $1`, ref).Scan(&k.id, &k.name, &k.dataType)
	if err == nil {
		k.lane = rules.LaneProperty
		k.valueType = rules.ValueTypeFor(k.lane, k.dataType)
		return k, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return k, false, fmt.Errorf("storage: resolve sample key %q: %w", ref, err)
	}
	err = q.QueryRow(ctx, `select id::text, name from metric_type where `+col+` = $1`, ref).Scan(&k.id, &k.name)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, false, nil
	}
	if err != nil {
		return k, false, fmt.Errorf("storage: resolve sample key %q: %w", ref, err)
	}
	k.lane, k.valueType = rules.LaneMetric, rules.ValueNumber
	return k, true, nil
}

// SampleKeyRef is a sample key as the registries hold it: its lane, data
// type (a property's), id and name.
type SampleKeyRef struct {
	Lane         rules.Lane
	DataType, ID string
	Name         string
}

// ResolveSampleKey finds a key by name (a property first, then a metric) or
// by id; ErrSampleKeyNotFound when neither registry holds it.
func (p *PG) ResolveSampleKey(ctx context.Context, ref string) (SampleKeyRef, error) {
	k, ok, err := resolveSampleKey(ctx, p.pool, ref)
	if err != nil {
		return SampleKeyRef{}, err
	}
	if !ok {
		return SampleKeyRef{}, ErrSampleKeyNotFound
	}
	return SampleKeyRef{Lane: k.lane, DataType: k.dataType, ID: k.id, Name: k.name}, nil
}

// resolveCalcInputs resolves each input's key (by its id once held, else by
// the name or id the author gave) to the registry row it reads, setting its
// name, id and lane and the rule's input value types. health is refused by
// its name; a key neither registry holds is refused on its field.
func resolveCalcInputs(ctx context.Context, q querier, r *CalcRule) error {
	r.inputTypes = make([]string, len(r.Inputs))
	for i, in := range r.Inputs {
		ref := in.Key
		if in.KeyID != "" {
			ref = in.KeyID
		}
		k, ok, err := resolveSampleKey(ctx, q, ref)
		if err != nil {
			return err
		}
		if !ok {
			return invalidCalc(fmt.Sprintf("inputs[%d].key", i), "%q is not a registered property or metric", in.Key)
		}
		if k.name == healthKey {
			return invalidCalc(fmt.Sprintf("inputs[%d].key", i), "%s may not be an input: an alarm moves it, so a rule reading it could feed back through an event rule", healthKey)
		}
		r.Inputs[i] = rules.CalcInput{Alias: in.Alias, Key: k.name, KeyID: k.id, Lane: k.lane}
		r.inputTypes[i] = k.valueType
	}
	return nil
}

// calcChecks says which of a rule's parts to validate: all of them on
// create; on update only what changed from the stored rule, so a rule whose
// input key was since deleted can still be relabelled or disabled.
type calcChecks struct {
	compile, output, selector bool
	kept                      CalcRuleSelector
}

var calcCheckAll = calcChecks{compile: true, output: true, selector: true}

// prepareCalcRule validates a rule, resolving its output key's id and lane
// and its selector's components to ids. The checks, in order:
//
//   - the name, the owner kind, and a selector of the owner's own kind (a
//     component rule's component types and components, a system rule's
//     system types and systems);
//   - every input key is registered, resolved to the row it reads by id
//     (resolveCalcInputs), and health is neither an input nor the output
//     (#525 decision 3: an alarm moves health, so a rule reading it could
//     feed back through an event rule, and health has its own producer, the
//     rollup);
//   - the reduce compiles against the inputs and yields the output key's
//     type (rules.CompileCalc);
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
		return invalidCalc("selector", "a system rule selects systems (system_types, systems), not components")
	}
	if r.OwnerKind == rules.OwnerComponent && (len(r.Selector.SystemTypes) > 0 || len(r.Selector.Systems) > 0) {
		return invalidCalc("selector", "a component rule selects components (component_types, components), not systems")
	}
	if err := resolveCalcInputs(ctx, q, r); err != nil {
		return err
	}
	// A calc rule's output is a key it owns, never one of the reserved
	// family's (ADR-0161): a native probe key is the node's observation, and
	// health is the rollup's (#525 decision 3).
	if c, ok := writerclass.Reserved(r.OutputKey); ok {
		return invalidCalc("output_key", "%s", writerclass.Refusal(c, strings.TrimSpace(r.OutputKey), writerclass.CalcRule, ""))
	}
	out, ok, err := resolveSampleKey(ctx, q, r.OutputKey)
	if err != nil {
		return err
	}
	if !ok {
		return invalidCalc("output_key", "%q is not a registered property or metric", r.OutputKey)
	}
	r.OutputLane, r.OutputKey, r.outputID, r.outputType = out.lane, out.name, out.id, out.valueType
	*outIDs = [2]any{nil, nil}
	if out.lane == rules.LaneProperty {
		outIDs[0] = out.id
	} else {
		outIDs[1] = out.id
	}
	if check.compile {
		if _, err := rules.CompileCalc(r.OwnerKind, typedInputs(*r), r.Reduce, out.valueType); err != nil {
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
		var err error
		if r.OwnerKind == rules.OwnerSystem {
			err = resolveSystemSelector(ctx, q, &r.Selector, check.kept)
		} else {
			sel := r.Selector.components()
			err = resolveSelector(ctx, q, &sel, check.kept.components())
			r.Selector.ComponentTypes, r.Selector.Components = sel.ComponentTypes, sel.Components
		}
		if err != nil {
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
	// A preview's candidate reads the graph from its own snapshot and takes
	// no lock: it saves nothing, so it must not hold up a save that does.
	if !r.candidate {
		if err := lockAdvisory(ctx, q, calcGraphLock); err != nil {
			return err
		}
	}
	rows, err := q.Query(ctx, `select c.id::text, c.name,
			c.output_lane || ':' || coalesce(c.output_property_type_id, c.output_metric_type_id)::text, coalesce(pt.name, mt.name, ''),
			(select coalesce(jsonb_agg(jsonb_build_object('node', i.lane || ':' || coalesce(i.property_type_id, i.metric_type_id)::text,
				'name', coalesce(ipt.name, imt.name)) order by i.position), '[]'::jsonb)
				from calc_rule_input i left join property_type ipt on ipt.id = i.property_type_id
				left join metric_type imt on imt.id = i.metric_type_id where i.rule_id = c.id)
		from calc_rule c
		left join property_type pt on pt.id = c.output_property_type_id
		left join metric_type mt on mt.id = c.output_metric_type_id
		where c.enabled`)
	if err != nil {
		return fmt.Errorf("storage: read the calc graph: %w", err)
	}
	defer rows.Close()
	// The graph's nodes are keys by lane and id, so a rename moves nothing
	// and a property and a metric of one name are two keys; names says each
	// node's name now, for the chain a refusal reads.
	names := map[string]string{}
	var graph []rules.CalcEdge
	for rows.Next() {
		var id, name, output, outputName string
		var raw []byte
		if err := rows.Scan(&id, &name, &output, &outputName, &raw); err != nil {
			return fmt.Errorf("storage: read the calc graph: %w", err)
		}
		if id == r.ID {
			continue
		}
		var inputs []struct{ Node, Name string }
		if err := json.Unmarshal(raw, &inputs); err != nil {
			return fmt.Errorf("storage: calc rule %q inputs: %w", name, err)
		}
		e := rules.CalcEdge{Rule: name, Output: output}
		for _, in := range inputs {
			e.Inputs = append(e.Inputs, in.Node)
			names[in.Node] = in.Name
		}
		names[output] = outputName
		graph = append(graph, e)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: read the calc graph: %w", err)
	}
	candidate := rules.CalcEdge{Rule: r.Name, Output: string(r.OutputLane) + ":" + r.outputID}
	names[candidate.Output] = r.OutputKey
	for _, in := range r.Inputs {
		node := string(in.Lane) + ":" + in.KeyID
		candidate.Inputs = append(candidate.Inputs, node)
		names[node] = in.Key
	}
	if c := rules.CalcCycle(graph, candidate); c != nil {
		for i, k := range c.Keys {
			c.Keys[i] = names[k]
		}
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

// inputTypeIDs splits a rule's inputs' key ids by lane.
func inputTypeIDs(inputs []rules.CalcInput) (metrics, properties []string) {
	metrics, properties = []string{}, []string{}
	for _, in := range inputs {
		if in.Lane == rules.LaneMetric {
			metrics = append(metrics, in.KeyID)
		} else {
			properties = append(properties, in.KeyID)
		}
	}
	return metrics, properties
}

// typedInputs are a rule's inputs with the value types its read or its
// resolution found.
func typedInputs(r CalcRule) []rules.TypedInput {
	typed := make([]rules.TypedInput, len(r.Inputs))
	for i, in := range r.Inputs {
		vt := ""
		if i < len(r.inputTypes) {
			vt = r.inputTypes[i]
		}
		typed[i] = rules.TypedInput{Alias: in.Alias, Key: in.Key, KeyID: in.KeyID, Lane: in.Lane, ValueType: vt}
	}
	return typed
}

// writeCalcInputs replaces a rule's input rows with its inputs, in order.
func writeCalcInputs(ctx context.Context, tx pgx.Tx, r CalcRule) error {
	if _, err := tx.Exec(ctx, `delete from calc_rule_input where rule_id = $1::uuid`, r.ID); err != nil {
		return fmt.Errorf("storage: clear calc rule %q inputs: %w", r.Name, err)
	}
	pos := make([]int, len(r.Inputs))
	aliases, lanes, ids := make([]string, len(r.Inputs)), make([]string, len(r.Inputs)), make([]string, len(r.Inputs))
	for i, in := range r.Inputs {
		pos[i], aliases[i], lanes[i], ids[i] = i, in.Alias, string(in.Lane), in.KeyID
	}
	if _, err := tx.Exec(ctx, `insert into calc_rule_input (rule_id, position, alias, lane, property_type_id, metric_type_id)
		select $1::uuid, p, a, l, case when l = 'property' then k end, case when l = 'metric' then k end
		from unnest($2::int[], $3::text[], $4::text[], $5::uuid[]) as x(p, a, l, k)`, r.ID, pos, aliases, lanes, ids); err != nil {
		return mapCalcWriteErr(err, r.Name)
	}
	return nil
}

// collectedBy refuses an output key a collection writes (#525 decision 6):
// a current value would mix a collected producer with a calculated one by
// newest ts. The reserved family (a native probe key, a platform key) is
// refused before it, by name (prepareCalcRule); this reads the one remaining
// declaration, never sample history (one hand-posted or simulated sample would
// otherwise block a key forever): a stored driver whose spec declares the key
// among a poll's or a listener's emits.
//
// The reverse order is held too (ADR-0161): a driver created or edited to emit
// a key a rule already outputs is refused at its spec write (the key is of the
// calc class), and a node lane carrying one is refused at the gateway.
func collectedBy(ctx context.Context, q querier, key string) error {
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
		if strings.HasPrefix(pgErr.ConstraintName, "calc_rule_input_") {
			return invalidCalc("inputs", "a key the rule reads no longer exists")
		}
		return invalidCalc("output_key", "the output key does not exist")
	}
	return fmt.Errorf("storage: write calc rule %q: %w", name, err)
}

func calcArgs(r CalcRule) (sel, reduce []byte, err error) {
	if sel, err = json.Marshal(r.Selector); err != nil {
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
	sel, reduce, err := calcArgs(r)
	if err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `insert into calc_rule (name, label, description, owner_kind, selector, reduce,
		output_lane, output_property_type_id, output_metric_type_id, enabled)
		values ($1, $2, $3, $4, $5, $6, $7, $8::uuid, $9::uuid, $10) returning id::text`,
		r.Name, labelOrNull(r.Label), r.Description, r.OwnerKind, sel, reduce, string(r.OutputLane), out[0], out[1], r.Enabled).Scan(&r.ID); err != nil {
		return nil, mapCalcWriteErr(err, r.Name)
	}
	if err := writeCalcInputs(ctx, tx, r); err != nil {
		return nil, err
	}
	id := r.ID
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
		Selector                                 CalcRuleSelector
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
		r.Inputs = slices.Clone(*patch.Inputs)
		// Resolved now, so an unchanged input given by name compares equal
		// to the stored one held by id.
		if err := resolveCalcInputs(ctx, tx, &r); err != nil {
			return nil, err
		}
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
	sel, reduce, err := calcArgs(r)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `update calc_rule set label = $2, description = $3, selector = $4, reduce = $5,
		output_lane = $6, output_property_type_id = $7::uuid, output_metric_type_id = $8::uuid, enabled = $9,
		version = version + 1, updated_at = now() where id = $1`,
		old.ID, labelOrNull(r.Label), r.Description, sel, reduce, string(r.OutputLane), out[0], out[1], r.Enabled); err != nil {
		return nil, mapCalcWriteErr(err, old.Name)
	}
	if !sameRuleJSON(r.Inputs, old.Inputs) {
		if err := writeCalcInputs(ctx, tx, r); err != nil {
			return nil, err
		}
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

// resolveSystemSelector checks a system rule's selector against the
// registries and resolves its systems to ids, as resolveSelector does for
// components: an entry kept already holds is carried over, and only an
// entry new to the rule must exist.
func resolveSystemSelector(ctx context.Context, q querier, sel *CalcRuleSelector, kept CalcRuleSelector) error {
	for _, t := range sel.SystemTypes {
		if slices.Contains(kept.SystemTypes, t) {
			continue
		}
		var ok bool
		if err := q.QueryRow(ctx, `select exists (select 1 from system_type where name = $1)`, t).Scan(&ok); err != nil {
			return fmt.Errorf("storage: check rule system type: %w", err)
		}
		if !ok {
			return selectorError{fmt.Sprintf("system type %q does not exist", t)}
		}
	}
	ids := make([]string, 0, len(sel.Systems))
	for _, ref := range sel.Systems {
		if slices.Contains(kept.Systems, ref) {
			ids = append(ids, ref)
			continue
		}
		s, err := scopedByName(ctx, q, systemConfig, ref)
		if err != nil {
			return selectorError{fmt.Sprintf("system %q: %v", ref, err)}
		}
		ids = append(ids, s.ID)
	}
	sel.Systems = ids
	return nil
}
