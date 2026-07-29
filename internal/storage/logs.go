package storage

import (
	"context"
	"fmt"
	"time"
)

// LogLineWrite is one raw log line to persist (ADR-0066): untyped arrival, owned
// through the exclusive arc (OwnerKind picks the arc column, OwnerID is the estate
// address). Severity and facility are the retention/routing axes (empty is stored
// as NULL so the partial indexes stay tight); Attributes and Labels are freeform
// jsonb, nil when absent. A log line is not an event: no registry gate.
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

// LogLine is a stored log row (read side). Severity, Facility, and CorrelationID
// come back "" when NULL.
type LogLine struct {
	ID            int64
	TS            time.Time
	OwnerKind     string
	Instance      string
	Source        string
	Severity      string
	Facility      string
	Message       string
	Attributes    []byte
	Labels        []byte
	CorrelationID string
}

// InsertLogLines writes raw log rows in one transaction. Each row sets exactly its
// owner arc column (the CHECK enforces the rest). Mirrors InsertEvents, minus the
// event_type registry lookup: the log lane is untyped.
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
		col, err := ownerColumn(l.OwnerKind)
		if err != nil {
			return fmt.Errorf("storage: log line %s: %w", l.OwnerID, err)
		}
		ts := l.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		arc, err := p.ownerArcValue(ctx, tx, l.OwnerKind, l.OwnerID)
		if err != nil {
			return fmt.Errorf("storage: log line %s: %w", l.OwnerID, err)
		}
		// Empty severity/facility/correlation store as NULL (the partial indexes
		// only cover set rows); attributes/labels ride jsonb as raw JSON text so
		// pgx does not encode a []byte as bytea.
		var sev, fac, corr, attrs, labels any
		if l.Severity != "" {
			sev = l.Severity
		}
		if l.Facility != "" {
			fac = l.Facility
		}
		if l.CorrelationID != "" {
			corr = l.CorrelationID
		}
		if len(l.Attributes) > 0 {
			attrs = string(l.Attributes)
		}
		if len(l.Labels) > 0 {
			labels = string(l.Labels)
		}
		sql := fmt.Sprintf(`insert into log_line (ts, owner_kind, %s, instance, source, severity, facility, message, attributes, labels, correlation_id)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, col)
		if _, err := tx.Exec(ctx, sql, ts, l.OwnerKind, arc, l.Instance, l.Source, sev, fac, l.Message, attrs, labels, corr); err != nil {
			return fmt.Errorf("storage: insert log line %s: %w", l.OwnerID, err)
		}
	}
	return tx.Commit(ctx)
}

// ListComponentLogs returns a component's raw log lines newest-first, from since,
// capped at limit. Mirrors ListComponentEvents.
func (p *PG) ListComponentLogs(ctx context.Context, componentName string, since time.Time, limit int) ([]LogLine, error) {
	rows, err := p.pool.Query(ctx, `
		select id, ts, owner_kind, instance, source,
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
		if err := rows.Scan(&l.ID, &l.TS, &l.OwnerKind, &l.Instance, &l.Source, &l.Severity, &l.Facility, &l.Message, &l.Attributes, &l.Labels, &l.CorrelationID); err != nil {
			return nil, fmt.Errorf("storage: scan log %s: %w", componentName, err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate logs %s: %w", componentName, err)
	}
	return out, nil
}

// ListNodeLogs returns a node's own log lines newest-first, from since, capped at
// limit. Owner arc is the node (node_id = the node's principal_id): these are the
// self-logs a node ships back over the ingest lane (ADR-0066), not a component's.
func (p *PG) ListNodeLogs(ctx context.Context, nodeName string, since time.Time, limit int) ([]LogLine, error) {
	rows, err := p.pool.Query(ctx, `
		select id, ts, owner_kind, instance, source,
			coalesce(severity, ''), coalesce(facility, ''), message, attributes, labels, coalesce(correlation_id, '')
		from log_line
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
		if err := rows.Scan(&l.ID, &l.TS, &l.OwnerKind, &l.Instance, &l.Source, &l.Severity, &l.Facility, &l.Message, &l.Attributes, &l.Labels, &l.CorrelationID); err != nil {
			return nil, fmt.Errorf("storage: scan node log %s: %w", nodeName, err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate node logs %s: %w", nodeName, err)
	}
	return out, nil
}
