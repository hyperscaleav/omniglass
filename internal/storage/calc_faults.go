package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hyperscaleav/omniglass/internal/expression"
	"github.com/hyperscaleav/omniglass/internal/rules"
)

// calcCompiles holds each calc rule's reduce compile result per (rule,
// version, input value types, output value type), the shape ruleJudges keeps
// for event rules: a read recompiles a rule once per version and key typing,
// never per read, and a failure is held like a success. Bounded: past
// calcCompileCap entries the map starts over. The zero value is ready; safe
// for concurrent use.
type calcCompiles struct {
	mu      sync.Mutex
	results map[calcCompileKey]error
}

type calcCompileKey struct {
	id      string
	version int
	inputs  string // each input's value type, in order, comma-joined
	output  string
}

const calcCompileCap = 4096

func (c *calcCompiles) compile(r CalcRule, typed []rules.TypedInput, output string) error {
	types := make([]string, len(typed))
	for i, t := range typed {
		types[i] = t.ValueType
	}
	k := calcCompileKey{id: r.ID, version: r.Version, inputs: strings.Join(types, ","), output: output}
	c.mu.Lock()
	if err, ok := c.results[k]; ok {
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	_, err := rules.CompileCalc(r.OwnerKind, typed, r.Reduce, output)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.results == nil || len(c.results) >= calcCompileCap {
		c.results = map[calcCompileKey]error{}
	}
	c.results[k] = err
	return err
}

// keyValueTypes reads every registered key's value type, by name (a
// property wins over a metric of the same name, as resolveSampleKey takes
// them). The registries are small, so a list reads them once rather than
// per rule.
func keyValueTypes(ctx context.Context, q querier) (map[string]string, error) {
	rows, err := q.Query(ctx, `select name, 'metric', '' from metric_type
		union all select name, 'property', data_type from property_type`)
	if err != nil {
		return nil, fmt.Errorf("storage: read key types: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, lane, dataType string
		if err := rows.Scan(&name, &lane, &dataType); err != nil {
			return nil, fmt.Errorf("storage: read key types: %w", err)
		}
		out[name] = rules.ValueTypeFor(rules.Lane(lane), dataType)
	}
	return out, rows.Err()
}

// calcFault is why a stored rule's reduce does not compile against the
// registries as they are now, or nil when it compiles.
func (p *PG) calcFault(r CalcRule, types map[string]string) *CalcFault {
	typed := make([]rules.TypedInput, len(r.Inputs))
	for i, in := range r.Inputs {
		vt, ok := types[in.Key]
		if !ok {
			return &CalcFault{Field: fmt.Sprintf("inputs[%d].key", i), Message: fmt.Sprintf("%q is not a registered property or metric", in.Key)}
		}
		typed[i] = rules.TypedInput{As: in.As, Key: in.Key, ValueType: vt}
	}
	err := p.calcs.compile(r, typed, types[r.OutputKey])
	if err == nil {
		return nil
	}
	f := &CalcFault{Message: err.Error()}
	var ce *rules.CalcError
	if errors.As(err, &ce) {
		f.Field, f.Message = ce.Field, ce.Err.Error()
	}
	var ee *expression.Error
	if errors.As(err, &ee) {
		f.Line, f.Column, f.Message = ee.Line, ee.Column, ee.Message
	}
	return f
}

// ErrKeyWrittenByCalcRule is a property or metric type that cannot be
// deleted because a calc rule writes it.
var ErrKeyWrittenByCalcRule = errors.New("storage: a calc rule writes this key")

// CalcOutputInUseError names the calc rules (enabled or not) whose output a
// type delete would remove.
type CalcOutputInUseError struct {
	Key   string
	Rules []string
}

func (e *CalcOutputInUseError) Error() string {
	return fmt.Sprintf("%s is written by the calc rule %s; delete or repoint the rule first", e.Key, strings.Join(e.Rules, ", "))
}

func (e *CalcOutputInUseError) Unwrap() error { return ErrKeyWrittenByCalcRule }

// calcOutputInUse turns a type delete's foreign-key refusal from calc_rule's
// output reference into a CalcOutputInUseError naming the rules, or returns
// nil when err is anything else.
func calcOutputInUse(ctx context.Context, q querier, lane rules.Lane, key string, err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" || !strings.HasPrefix(pgErr.ConstraintName, "calc_rule_output_") {
		return nil
	}
	col, table := "output_metric_type_id", "metric_type"
	if lane == rules.LaneProperty {
		col, table = "output_property_type_id", "property_type"
	}
	rows, qerr := q.Query(ctx, `select c.name from calc_rule c join `+table+` t on t.id = c.`+col+`
		where t.name = $1 order by c.name`, key)
	if qerr != nil {
		return fmt.Errorf("storage: read the calc rules writing %q: %w", key, qerr)
	}
	names, qerr := pgx.CollectRows(rows, pgx.RowTo[string])
	if qerr != nil {
		return fmt.Errorf("storage: read the calc rules writing %q: %w", key, qerr)
	}
	return &CalcOutputInUseError{Key: key, Rules: names}
}
