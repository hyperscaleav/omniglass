package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The write-side invariant (ADR-0161), the twin of scope injection: every
// metric, property and event type has exactly one writer class
// (internal/writerclass), and every gateway path that lands a sample or an
// event names its writer and is refused, before anything lands, when the key's
// class does not admit it. The class generalizes the calc rule's ownership of
// its output (WrittenBy, ErrKeyWrittenByCalcRule): one resolution, classOf, is
// what a type read reports and what a write is held to, so the two cannot
// disagree.

// ErrKeyWriterRefused marks a write refused because the key's writer class does
// not admit its writer. The *KeyWriterError carrying it names the key.
var ErrKeyWriterRefused = errors.New("storage: the key's writer class does not admit this writer")

// KeyWriterError is a refused write: the key, its class, the writer that tried,
// and, where it decides the refusal, the calc rule that owns the key or the
// instance a node named against the endpoint its task ran.
type KeyWriterError struct {
	Key      string
	Class    writerclass.Class
	Writer   writerclass.Writer
	Rule     string
	Instance string
	Endpoint string
}

func (e *KeyWriterError) Error() string {
	msg := writerclass.Refusal(e.Class, e.Key, e.Writer, e.Rule)
	if e.Class == writerclass.NodeProbe && e.Writer == writerclass.Node {
		msg += fmt.Sprintf(" (instance %q, the task's endpoint %q)", e.Instance, e.Endpoint)
	}
	return msg
}

func (e *KeyWriterError) Unwrap() error { return ErrKeyWriterRefused }

// KeyWriter is who a write speaks for: the entry point it came through. The two
// a caller outside the gateway constructs are NodeWriter and PushWriter (the
// ingest lanes); every other writer is the gateway's own path (a calc rule's
// tick, the health rollup, a command, an operator's declared value), built
// where that path writes.
type KeyWriter struct {
	Kind writerclass.Writer
	// Endpoint is the endpoint a node's task ran against, the only instance a
	// node-probe key lands under. Node writers only.
	Endpoint string
	// calc is a calc rule's lineage; set only by the calc tick.
	calc *sampleLineage
}

// NodeWriter is the node lane's writer: a node's own collection, under the
// task bound to endpoint (the task's endpoint name, resolved server-side).
func NodeWriter(endpoint string) KeyWriter {
	return KeyWriter{Kind: writerclass.Node, Endpoint: endpoint}
}

// PushWriter is the telemetry push lane's writer.
func PushWriter() KeyWriter { return KeyWriter{Kind: writerclass.Push} }

// gatewayWriter is one of the gateway's own writers.
func gatewayWriter(kind writerclass.Writer) KeyWriter { return KeyWriter{Kind: kind} }

// calcWriter is a calc rule's tick, writing its output with lineage.
func calcWriter(l *sampleLineage) KeyWriter { return KeyWriter{Kind: writerclass.CalcRule, calc: l} }

// provenanceArgs is a sample row's provenance, source_rule, source_rule_version
// and source_rule_id: calculated naming the rule for a calc rule's tick,
// observed otherwise.
func (w KeyWriter) provenanceArgs() (string, any, any, any) {
	if w.Kind != writerclass.CalcRule {
		return "observed", nil, nil, nil
	}
	return w.calc.provenanceArgs()
}

// rule is the calc rule writing, "" for any other writer.
func (w KeyWriter) rule() string {
	if w.Kind != writerclass.CalcRule {
		return ""
	}
	return w.calc.rule()
}

// writeLane is the catalog a written key resolves in.
type writeLane string

const (
	writeMetric   writeLane = "metric"
	writeProperty writeLane = "property"
	writeEvent    writeLane = "event"
)

// calcOwnedNamesSQL lists every metric and property name a calc rule outputs,
// enabled or not: the keys of the calc writer class.
const calcOwnedNamesSQL = `select t.name from calc_rule c join metric_type t on t.id = c.output_metric_type_id
	union select t.name from calc_rule c join property_type t on t.id = c.output_property_type_id`

// keyWriterOf is a key's resolved writer: its class, and for a calc key the
// rules that output it (id to name; one enabled rule writes a key, a disabled
// one keeps its claim).
type keyWriterOf struct {
	class writerclass.Class
	rules map[string]string
}

// classOf is a catalog row's writer class: the one resolution a type read
// reports and a write is held to.
func classOf(name string, calc *CalcWriter) writerclass.Class {
	return writerclass.Of(name, calc != nil)
}

// keyWriters resolves each key's writer: the reserved class by name, from code;
// else calc when a calc rule outputs it, from the catalog in one read; else
// collected. An event key is reserved or collected: no rule outputs an event.
func keyWriters(ctx context.Context, q querier, lane writeLane, keys []string) (map[string]keyWriterOf, error) {
	out := make(map[string]keyWriterOf, len(keys))
	var lookup []string
	for _, k := range keys {
		if _, seen := out[k]; seen {
			continue
		}
		if c, ok := writerclass.Reserved(k); ok {
			out[k] = keyWriterOf{class: c}
			continue
		}
		out[k] = keyWriterOf{class: writerclass.Collected}
		if lane != writeEvent {
			lookup = append(lookup, k)
		}
	}
	if len(lookup) == 0 {
		return out, nil
	}
	col, table := "output_metric_type_id", "metric_type"
	if lane == writeProperty {
		col, table = "output_property_type_id", "property_type"
	}
	rows, err := q.Query(ctx, `select t.name, c.id::text, c.name from calc_rule c join `+table+` t on t.id = c.`+col+`
		where t.name = any($1)`, lookup)
	if err != nil {
		return nil, fmt.Errorf("storage: read the writers of %v: %w", lookup, err)
	}
	type owned struct{ key, id, name string }
	claims, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (owned, error) {
		var o owned
		return o, r.Scan(&o.key, &o.id, &o.name)
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read the writers of %v: %w", lookup, err)
	}
	for _, c := range claims {
		kw := out[c.key]
		if kw.rules == nil {
			kw = keyWriterOf{class: writerclass.Calc, rules: map[string]string{}}
		}
		kw.rules[c.id] = c.name
		out[c.key] = kw
	}
	return out, nil
}

// keyedWrite is one row a write path is about to land: its key and instance.
type keyedWrite struct {
	key, instance string
}

// admitWrites is the gateway's write check, run by every path that lands a
// sample or an event before it lands anything: each row's key must admit w.
// The first refusal returns, so a batch lands whole or not at all.
func admitWrites(ctx context.Context, q querier, lane writeLane, w KeyWriter, rows []keyedWrite) error {
	if len(rows) == 0 {
		return nil
	}
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r.key
	}
	writers, err := keyWriters(ctx, q, lane, keys)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := admitWrite(writers[r.key], w, r); err != nil {
			return err
		}
	}
	return nil
}

// admitWrite holds one row to its key's resolved writer. Pure. Beyond the
// class's ruling: a node writes a node-probe key only under the endpoint its
// task ran against (a node cannot speak for an endpoint it does not hold), and
// a calc rule writes only a key it outputs.
func admitWrite(kw keyWriterOf, w KeyWriter, r keyedWrite) error {
	refuse := &KeyWriterError{Key: r.key, Class: kw.class, Writer: w.Kind, Instance: r.instance, Endpoint: w.Endpoint}
	for _, name := range kw.rules {
		refuse.Rule = name
		break
	}
	if !writerclass.Admits(kw.class, w.Kind, r.key) {
		return refuse
	}
	switch {
	case kw.class == writerclass.NodeProbe && w.Kind == writerclass.Node:
		if w.Endpoint == "" || r.instance != w.Endpoint {
			return refuse
		}
	case kw.class == writerclass.Calc && w.Kind == writerclass.CalcRule:
		if _, ok := kw.rules[w.rule()]; !ok {
			return refuse
		}
	}
	return nil
}

// reservedName refuses an operator-created type taking a reserved key's name,
// on any lane (ADR-0161): the reserved family's names are declared in code and
// belong to it whether or not its type is seeded yet, so a name a release later
// ships as a reserved key is never an operator's first. The seed's
// authoritative upsert is the only writer of a reserved type.
func reservedName(name string) error {
	if c, ok := writerclass.Reserved(name); ok {
		return fmt.Errorf("%q is a reserved name (writer class %s), closed to an operator-created type on every lane", name, c)
	}
	return nil
}
