package storage

import (
	"context"
	"encoding/json"
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
	results map[calcCompileKey]calcCompiled
}

type calcCompiled struct {
	prog *rules.CalcProgram
	err  error
}

type calcCompileKey struct {
	id      string
	version int
	inputs  string // each input's value type, in order, comma-joined
	output  string
}

const calcCompileCap = 4096

// program is a rule version's compiled reduce for these key types, compiled
// on first use; a compile error is cached and returned the same way.
func (c *calcCompiles) program(r CalcRule, typed []rules.TypedInput, output string) (*rules.CalcProgram, error) {
	types := make([]string, len(typed))
	for i, t := range typed {
		types[i] = t.ValueType
	}
	k := calcCompileKey{id: r.ID, version: r.Version, inputs: strings.Join(types, ","), output: output}
	c.mu.Lock()
	if got, ok := c.results[k]; ok {
		c.mu.Unlock()
		return got.prog, got.err
	}
	c.mu.Unlock()
	prog, err := rules.CompileCalc(r.OwnerKind, typed, r.Reduce, output)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.results == nil || len(c.results) >= calcCompileCap {
		c.results = map[calcCompileKey]calcCompiled{}
	}
	c.results[k] = calcCompiled{prog: prog, err: err}
	return prog, err
}

// withCalcNotes sets r's CalcError and CalcWarnings against the registries
// as they are now (the key types its read resolved, by the ids it holds):
// why its stored reduce does not compile, or the warnings it compiles with.
func (p *PG) withCalcNotes(r *CalcRule) {
	r.CalcError, r.CalcWarnings = nil, nil
	typed := typedInputs(*r)
	for i, t := range typed {
		if t.ValueType == "" {
			r.CalcError = &CalcFault{Field: fmt.Sprintf("inputs[%d].key", i), Message: fmt.Sprintf("%q is not a registered property or metric", t.Key)}
			return
		}
	}
	prog, err := p.calcs.program(*r, typed, r.outputType)
	if err != nil {
		r.CalcError = calcFault(err)
		return
	}
	for _, w := range prog.Warnings() {
		r.CalcWarnings = append(r.CalcWarnings, CalcFault{Field: w.Field, Line: w.Line, Column: w.Column, Message: w.Message, Code: w.Code})
	}
}

// calcFault is a reduce compile error as a reader sees it.
func calcFault(err error) *CalcFault {
	f := &CalcFault{Message: err.Error()}
	var ce *rules.CalcError
	if errors.As(err, &ce) {
		f.Field, f.Message = ce.Field, ce.Err.Error()
	}
	var ee *expression.Error
	if errors.As(err, &ee) {
		f.Line, f.Column, f.Message, f.Code = ee.Line, ee.Column, ee.Message, ee.Code
	}
	return f
}

// ErrKeyWrittenByCalcRule is a property or metric type that cannot be
// deleted because a calc rule writes it; ErrKeyReadByCalcRule, because one
// reads it (an input holds its key by id, #1018).
var (
	ErrKeyWrittenByCalcRule = errors.New("storage: a calc rule writes this key")
	ErrKeyReadByCalcRule    = errors.New("storage: a calc rule reads this key")
)

// CalcOutputInUseError names the calc rules (enabled or not) whose output,
// or with Reads whose input, a type delete would remove.
type CalcOutputInUseError struct {
	Key   string
	Rules []string
	Reads bool
}

func (e *CalcOutputInUseError) Error() string {
	if e.Reads {
		return fmt.Sprintf("%s is read by the calc rule %s; delete the rule or change its inputs first", e.Key, strings.Join(e.Rules, ", "))
	}
	return fmt.Sprintf("%s is written by the calc rule %s; delete or repoint the rule first", e.Key, strings.Join(e.Rules, ", "))
}

func (e *CalcOutputInUseError) Unwrap() error {
	if e.Reads {
		return ErrKeyReadByCalcRule
	}
	return ErrKeyWrittenByCalcRule
}

// calcOutputInUse turns a type delete's foreign-key refusal from calc_rule's
// output reference, or calc_rule_input's input reference, into a
// CalcOutputInUseError naming the rules, or returns nil when err is anything
// else.
func calcOutputInUse(ctx context.Context, q querier, lane rules.Lane, key string, err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		return nil
	}
	reads := strings.HasPrefix(pgErr.ConstraintName, "calc_rule_input_")
	if !reads && !strings.HasPrefix(pgErr.ConstraintName, "calc_rule_output_") {
		return nil
	}
	col, table := "output_metric_type_id", "metric_type"
	if lane == rules.LaneProperty {
		col, table = "output_property_type_id", "property_type"
	}
	sql := `select c.name from calc_rule c join ` + catalogRows(table, "") + ` t on t.id = c.` + col + ` where t.name = $1 order by c.name`
	if reads {
		col = strings.TrimPrefix(col, "output_")
		sql = `select distinct c.name from calc_rule c join calc_rule_input i on i.rule_id = c.id join ` + catalogRows(table, "") + ` t on t.id = i.` + col + `
			where t.name = $1 order by c.name`
	}
	rows, qerr := q.Query(ctx, sql, key)
	if qerr != nil {
		return fmt.Errorf("storage: read the calc rules writing %q: %w", key, qerr)
	}
	names, qerr := pgx.CollectRows(rows, pgx.RowTo[string])
	if qerr != nil {
		return fmt.Errorf("storage: read the calc rules writing %q: %w", key, qerr)
	}
	return &CalcOutputInUseError{Key: key, Rules: names, Reads: reads}
}

// CalcWriter is the calc rule that writes a key (#1019): its name and id.
type CalcWriter struct {
	Rule, RuleID string
}

// calcWriterCol is the select-list column naming the calc rule that writes a
// type row of table (the enabled writer, else the first disabled one by
// name; one enabled rule writes a key), as jsonb, null when none does. A
// correlated probe on the rule's output reference; rules read like a
// registry, so it is scope-free like the type read itself.
func calcWriterCol(col, table string) string {
	return `(select jsonb_build_object('rule', c.name, 'rule_id', c.id::text) from calc_rule c where c.` + col + ` = ` + table +
		`.id order by c.enabled desc, c.name limit 1)`
}

func parseCalcWriter(raw []byte) *CalcWriter {
	if len(raw) == 0 {
		return nil
	}
	var w struct {
		Rule   string `json:"rule"`
		RuleID string `json:"rule_id"`
	}
	if err := json.Unmarshal(raw, &w); err != nil || w.RuleID == "" {
		return nil
	}
	return &CalcWriter{Rule: w.Rule, RuleID: w.RuleID}
}
