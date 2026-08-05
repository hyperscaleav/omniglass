package storage

import (
	"context"
	"fmt"
	"time"
)

// LogLineWrite is one raw log line to persist (ADR-0066): untyped arrival,
// component-owned (#589 narrowed log_line to the one owner that emits logs).
// The binding stays explicit (OwnerKind + OwnerID) so a caller that still
// carries another owner is refused by name (reject-not-project) instead of
// silently projected onto the component arm; a node's self-logs are
// NodeLogWrite rows on node_log, not this. Severity and facility are the
// retention/routing axes (empty is stored as NULL so the partial indexes stay
// tight); Attributes and Labels are freeform jsonb, nil when absent. A log
// line is not an event: no registry gate.
type LogLineWrite struct {
	OwnerKind     string
	OwnerID       string
	Instance      string
	Source        string
	Severity      string
	Facility      string
	Message       string
	Attributes    []byte
	Labels        []byte
	CorrelationID string
	TS            time.Time
}

// NodeLogWrite is one of a node's own log lines to persist: the self-log lane
// (ADR-0066) after the #589 split gave it an origin-true table. Node addresses
// the emitting node by name (or principal uuid); the payload columns mirror
// LogLineWrite, minus the arc a single-origin table does not need and minus
// Instance, which only a component's device output discriminates.
type NodeLogWrite struct {
	Node          string
	Source        string
	Severity      string
	Facility      string
	Message       string
	Attributes    []byte
	Labels        []byte
	CorrelationID string
	TS            time.Time
}

// LogLine is a stored log row (read side), shared by the component and node
// reads: both lanes store the same payload shape, only the table differs.
// Severity, Facility, and CorrelationID come back "" when NULL; Instance stays
// "" on a node row (node_log has no instance column).
type LogLine struct {
	ID            int64
	TS            time.Time
	Instance      string
	Source        string
	Severity      string
	Facility      string
	Message       string
	Attributes    []byte
	Labels        []byte
	CorrelationID string
}

// InsertLogLines writes raw component log rows in one transaction. A binding
// carrying any other owner is refused loudly: log_line is component-only
// (#589), and a named error here beats a NULL tripping the NOT NULL opaquely.
// Mirrors InsertEvents, minus the event_type registry lookup: the log lane is
// untyped.
func (p *PG) InsertLogLines(ctx context.Context, lines []LogLineWrite) error {
	if len(lines) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin insert log lines: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, l := range lines {
		if l.OwnerKind != "component" {
			return fmt.Errorf("storage: log line %s: log_line is component-owned (#589), refusing owner_kind %q (a node's self-logs land in node_log)", l.OwnerID, l.OwnerKind)
		}
		ts := l.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		arc, err := p.ownerArcValue(ctx, tx, "component", l.OwnerID)
		if err != nil {
			return fmt.Errorf("storage: log line %s: %w", l.OwnerID, err)
		}
		sev, fac, corr, attrs, labels := nullableLogFields(l.Severity, l.Facility, l.CorrelationID, l.Attributes, l.Labels)
		if _, err := tx.Exec(ctx, `insert into log_line (ts, component_id, instance, source, severity, facility, message, attributes, labels, correlation_id)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			ts, arc, l.Instance, l.Source, sev, fac, l.Message, attrs, labels, corr); err != nil {
			return fmt.Errorf("storage: insert log line %s: %w", l.OwnerID, err)
		}
	}
	return tx.Commit(ctx)
}

// InsertNodeLogs writes a node's own log rows in one transaction: the self-log
// sink (ADR-0066) on node_log. Mirrors InsertLogLines minus the arc; the node
// is resolved to its principal_id so an unknown node is a named error rather
// than an FK violation.
func (p *PG) InsertNodeLogs(ctx context.Context, lines []NodeLogWrite) error {
	if len(lines) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin insert node logs: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, l := range lines {
		ts := l.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		pid, err := p.ownerArcValue(ctx, tx, "node", l.Node)
		if err != nil {
			return fmt.Errorf("storage: node log %s: %w", l.Node, err)
		}
		sev, fac, corr, attrs, labels := nullableLogFields(l.Severity, l.Facility, l.CorrelationID, l.Attributes, l.Labels)
		if _, err := tx.Exec(ctx, `insert into node_log (ts, node_id, source, severity, facility, message, attributes, labels, correlation_id)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			ts, pid, l.Source, sev, fac, l.Message, attrs, labels, corr); err != nil {
			return fmt.Errorf("storage: insert node log %s: %w", l.Node, err)
		}
	}
	return tx.Commit(ctx)
}

// nullableLogFields maps the empty-means-absent log columns to their stored
// form: "" becomes NULL (the partial indexes only cover set rows) and the
// jsonb payloads ride as raw JSON text so pgx does not encode a []byte as
// bytea. Shared by both log sinks so the two lanes cannot drift.
func nullableLogFields(severity, facility, correlationID string, attributes, labelsIn []byte) (sev, fac, corr, attrs, labels any) {
	if severity != "" {
		sev = severity
	}
	if facility != "" {
		fac = facility
	}
	if correlationID != "" {
		corr = correlationID
	}
	if len(attributes) > 0 {
		attrs = string(attributes)
	}
	if len(labelsIn) > 0 {
		labels = string(labelsIn)
	}
	return sev, fac, corr, attrs, labels
}

// ListComponentLogs returns a component's raw log lines newest-first, from since,
// capped at limit. Mirrors ListComponentEvents.
func (p *PG) ListComponentLogs(ctx context.Context, componentName string, since time.Time, limit int) ([]LogLine, error) {
	rows, err := p.pool.Query(ctx, `
		select id, ts, instance, source,
			coalesce(severity, ''), coalesce(facility, ''), message, attributes, labels, coalesce(correlation_id, '')
		from log_line
		where component_id = (select id from component where name = $1) and ts >= $2
		order by ts desc
		limit $3`, componentName, since, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: list logs %s: %w", componentName, err)
	}
	defer rows.Close()

	var out []LogLine
	for rows.Next() {
		var l LogLine
		if err := rows.Scan(&l.ID, &l.TS, &l.Instance, &l.Source, &l.Severity, &l.Facility, &l.Message, &l.Attributes, &l.Labels, &l.CorrelationID); err != nil {
			return nil, fmt.Errorf("storage: scan log %s: %w", componentName, err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate logs %s: %w", componentName, err)
	}
	return out, nil
}

// ListNodeLogs returns a node's own log lines newest-first, from since, capped
// at limit: the self-logs a node ships back over the ingest lane (ADR-0066),
// read from node_log since the #589 split. Instance stays "" (a node log has
// none); the row shape is otherwise the component read's, which is what lets
// the API serve both panels through one body mapper.
func (p *PG) ListNodeLogs(ctx context.Context, nodeName string, since time.Time, limit int) ([]LogLine, error) {
	rows, err := p.pool.Query(ctx, `
		select id, ts, '' as instance, source,
			coalesce(severity, ''), coalesce(facility, ''), message, attributes, labels, coalesce(correlation_id, '')
		from node_log
		where node_id = (select principal_id from node where name = $1) and ts >= $2
		order by ts desc
		limit $3`, nodeName, since, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: list node logs %s: %w", nodeName, err)
	}
	defer rows.Close()

	var out []LogLine
	for rows.Next() {
		var l LogLine
		if err := rows.Scan(&l.ID, &l.TS, &l.Instance, &l.Source, &l.Severity, &l.Facility, &l.Message, &l.Attributes, &l.Labels, &l.CorrelationID); err != nil {
			return nil, fmt.Errorf("storage: scan node log %s: %w", nodeName, err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate node logs %s: %w", nodeName, err)
	}
	return out, nil
}
