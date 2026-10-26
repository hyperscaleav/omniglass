package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/logline"
	"github.com/jackc/pgx/v5"
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
	ID int64
	TS time.Time
	// Component is the writing component's NAME, populated by the
	// system-scoped read (#793) where one list mixes writers; the
	// single-component read leaves it empty.
	Component     string
	Instance      string
	Source        string
	Severity      string
	Facility      string
	Message       string
	Attributes    []byte
	Labels        []byte
	CorrelationID string
	// ReceivedAt is when the server received a component's line (#1145),
	// beside TS, the time the line claims; nil on a node row and on a line
	// stored before the column existed.
	ReceivedAt *time.Time
}

// logArrival is when a component's line arrived, the axis its reads window
// and order on (#1145): the time a line claims may come from its payload, so
// a component whose clock reset would otherwise log into the past, out of
// every window; a line stored before received_at was kept falls back to its
// own time. The expression matches the (component_id, arrival) index.
const logArrival = "coalesce(log_line.received_at, log_line.ts)"

// LogFilter narrows a component or system log read (#1145): Severity is a
// threshold on the canonical set (that level and every more severe one; a
// line with no severity is left out), Source an exact channel. Empty fields
// filter nothing.
type LogFilter struct {
	Severity string
	Source   string
}

// ErrUnknownSeverity is a filter naming a severity outside the canonical set.
var ErrUnknownSeverity = errors.New("storage: unknown severity")

// where renders the filter as SQL conditions over log_line, appending its
// arguments after args (the query's own).
func (f LogFilter) where(args []any) (string, []any, error) {
	cond := ""
	if f.Severity != "" {
		levels, ok := logline.AtLeast(f.Severity)
		if !ok {
			return "", nil, fmt.Errorf("%w %q: want one of %s", ErrUnknownSeverity, f.Severity, strings.Join(logline.Levels(), ", "))
		}
		args = append(args, levels)
		cond += fmt.Sprintf(" and log_line.severity = any($%d)", len(args))
	}
	if f.Source != "" {
		args = append(args, f.Source)
		cond += fmt.Sprintf(" and log_line.source = $%d", len(args))
	}
	return cond, args, nil
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
	return p.sampleWrite(ctx, "insert log lines", func(tx pgx.Tx) error { return p.writeLogLines(ctx, tx, lines) })
}

// writeLogLines is the component log-line write path, in the caller's
// transaction: InsertLogLines' own, or an ingest batch's (LandBatch).
func (p *PG) writeLogLines(ctx context.Context, tx pgx.Tx, lines []LogLineWrite) error {
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
		// Every writer's line is held here to what the lane shares (#1145):
		// the severity on RFC 5424's canonical set (a word the table does not
		// know kept as severity_raw), the message and attributes within
		// their caps. The gateway is the one write path, so no writer can
		// land a stray word a retention window by severity would miss.
		// A NUL anywhere is replaced first: Postgres text and jsonb refuse it,
		// and one refused line would fail the batch it rode in.
		severity, attributes := logline.Normalize(logline.Clean(l.Severity), logline.CleanAttributes(l.Attributes))
		message, attributes := logline.Cap(logline.Clean(l.Message), attributes)
		sev, fac, corr, attrs, labels := nullableLogFields(severity, logline.Clean(l.Facility), logline.Clean(l.CorrelationID), attributes, logline.CleanAttributes(l.Labels))
		if _, err := tx.Exec(ctx, `insert into log_line (ts, component_id, instance, source, severity, facility, message, attributes, labels, correlation_id)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			ts, arc, logline.Clean(l.Instance), logline.Clean(l.Source), sev, fac, message, attrs, labels, corr); err != nil {
			return fmt.Errorf("storage: insert log line %s: %w", l.OwnerID, err)
		}
	}
	return nil
}

// InsertNodeLogs writes a node's own log rows in one transaction: the self-log
// sink (ADR-0066) on node_log. Mirrors InsertLogLines minus the arc; the node
// is resolved to its principal_id so an unknown node is a named error rather
// than an FK violation.
func (p *PG) InsertNodeLogs(ctx context.Context, lines []NodeLogWrite) error {
	if len(lines) == 0 {
		return nil
	}
	return p.sampleWrite(ctx, "insert node logs", func(tx pgx.Tx) error { return p.writeNodeLogs(ctx, tx, lines) })
}

// writeNodeLogs is the node self-log write path, in the caller's
// transaction: InsertNodeLogs' own, or an ingest batch's (LandBatch).
func (p *PG) writeNodeLogs(ctx context.Context, tx pgx.Tx, lines []NodeLogWrite) error {
	for _, l := range cleanNodeLogWrites(lines) {
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
	return nil
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

// ListComponentLogs returns a component's raw log lines newest-first, inside
// window (counted back from the DATABASE's clock, tsSince, #719), capped at
// limit and narrowed by filter (#1145). Mirrors ListComponentEvents.
// componentRef is resolved once (name or uuid, ADR-0062); an unknown component
// folds into the same nil-no-error empty result the old inline subquery's
// silent no-match gave.
func (p *PG) ListComponentLogs(ctx context.Context, componentRef string, window time.Duration, limit int, filter LogFilter) ([]LogLine, error) {
	c, err := scopedByName(ctx, p.pool, componentConfig, componentRef)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	bound, args := tsSince(logArrival, window, c.ID, limit)
	cond, args, err := filter.where(args)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, fmt.Sprintf(`
		select id, ts, instance, source,
			coalesce(severity, ''), coalesce(facility, ''), message, attributes, labels, coalesce(correlation_id, ''), received_at
		from log_line
		where component_id = $1::uuid and %s%s
		order by %s desc, id desc
		limit $2`, bound, cond, logArrival), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list logs %s: %w", componentRef, err)
	}
	defer rows.Close()

	var out []LogLine
	for rows.Next() {
		var l LogLine
		if err := rows.Scan(&l.ID, &l.TS, &l.Instance, &l.Source, &l.Severity, &l.Facility, &l.Message, &l.Attributes, &l.Labels, &l.CorrelationID, &l.ReceivedAt); err != nil {
			return nil, fmt.Errorf("storage: scan log %s: %w", componentRef, err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate logs %s: %w", componentRef, err)
	}
	return out, nil
}

// ListComponentLogSources lists the channels a component's lines inside
// window arrived on (#1145), the source filter's facet, sorted.
func (p *PG) ListComponentLogSources(ctx context.Context, componentRef string, window time.Duration) ([]string, error) {
	c, err := scopedByName(ctx, p.pool, componentConfig, componentRef)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	bound, args := tsSince(logArrival, window, c.ID)
	return p.logSources(ctx, fmt.Sprintf(`
		select distinct source from log_line
		where component_id = $1::uuid and source <> '' and %s
		order by source limit 50`, bound), args)
}

// logSources reads a distinct-source query.
func (p *PG) logSources(ctx context.Context, sql string, args []any) ([]string, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list log sources: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// ListNodeLogs returns a node's own log lines newest-first, inside window
// (counted back from the DATABASE's clock, tsSince, #719), capped at limit: the self-logs a node ships back over the ingest lane (ADR-0066),
// read from node_log since the #589 split. Instance stays "" (a node log has
// none); the row shape is otherwise the component read's, which is what lets
// the API serve both panels through one body mapper.
func (p *PG) ListNodeLogs(ctx context.Context, nodeName string, window time.Duration, limit int) ([]LogLine, error) {
	bound, args := tsSince("ts", window, nodeName, limit)
	rows, err := p.pool.Query(ctx, fmt.Sprintf(`
		select id, ts, '' as instance, source,
			coalesce(severity, ''), coalesce(facility, ''), message, attributes, labels, coalesce(correlation_id, '')
		from node_log
		where node_id = (select principal_id from node where name = $1) and %s
		order by ts desc
		limit $2`, bound), args...)
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

// ListSystemLogs returns the members' raw log lines merged newest first
// (#793), each naming the component that wrote it: the workspace's Logs tab,
// narrowed by filter (#1145). Mirrors ListSystemEvents; log lines are
// component-owned (#589), so the system's own arc contributes nothing here.
func (p *PG) ListSystemLogs(ctx context.Context, systemID string, window time.Duration, limit int, filter LogFilter) ([]LogLine, error) {
	bound, args := tsSince(logArrival, window, systemID, limit)
	cond, args, err := filter.where(args)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, fmt.Sprintf(`
		select log_line.id, ts,
			coalesce((select c.name from component c where c.id = log_line.component_id), ''),
			instance, source,
			coalesce(severity, ''), coalesce(facility, ''), message, attributes, labels, coalesce(correlation_id, ''), received_at
		from log_line
		where log_line.component_id in (select m.component_id from system_member m where m.system_id = $1::uuid)
		  and %s%s
		order by %s desc, log_line.id desc
		limit $2`, bound, cond, logArrival), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list system logs %s: %w", systemID, err)
	}
	defer rows.Close()
	var out []LogLine
	for rows.Next() {
		var l LogLine
		if err := rows.Scan(&l.ID, &l.TS, &l.Component, &l.Instance, &l.Source, &l.Severity, &l.Facility, &l.Message, &l.Attributes, &l.Labels, &l.CorrelationID, &l.ReceivedAt); err != nil {
			return nil, fmt.Errorf("storage: scan system log line: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListSystemLogSources lists the channels the members' lines inside window
// arrived on (#1145), sorted.
func (p *PG) ListSystemLogSources(ctx context.Context, systemID string, window time.Duration) ([]string, error) {
	bound, args := tsSince(logArrival, window, systemID)
	return p.logSources(ctx, fmt.Sprintf(`
		select distinct source from log_line
		where component_id in (select m.component_id from system_member m where m.system_id = $1::uuid)
		  and source <> '' and %s
		order by source limit 50`, bound), args)
}
