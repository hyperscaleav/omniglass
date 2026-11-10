package storage

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

// FingerprintMax is the most runes of a statement a slow-query log carries.
const FingerprintMax = 200

// DefaultSlowQuery is the slow-query threshold before the platform setting
// is read: the setting's own default.
const DefaultSlowQuery = 500 * time.Millisecond

// SlowQueryTracer is a pgx query tracer that logs a query running past its
// threshold as a WARN and counts it (#1627). It logs the statement's
// fingerprint and duration, never its arguments: those carry operator data
// and secrets. A zero threshold turns it off. The threshold is held
// atomically, so the platform setting can move it while queries run.
type SlowQueryTracer struct {
	log       *slog.Logger
	now       func() time.Time
	onSlow    func()
	threshold atomic.Int64
}

// NewSlowQueryTracer logs to log, times with now, and calls onSlow once per
// slow query. It starts at DefaultSlowQuery.
func NewSlowQueryTracer(log *slog.Logger, now func() time.Time, onSlow func()) *SlowQueryTracer {
	if onSlow == nil {
		onSlow = func() {}
	}
	t := &SlowQueryTracer{log: log, now: now, onSlow: onSlow}
	t.threshold.Store(int64(DefaultSlowQuery))
	return t
}

// SetThreshold moves the threshold; zero or less turns the tracer off.
func (t *SlowQueryTracer) SetThreshold(d time.Duration) { t.threshold.Store(int64(d)) }

// Threshold is the threshold now.
func (t *SlowQueryTracer) Threshold() time.Duration { return time.Duration(t.threshold.Load()) }

// slowQueryStart is what a traced query's context carries from its start to
// its end.
type slowQueryStart struct {
	at  time.Time
	sql string
}

type slowQueryKey struct{}

// TraceQueryStart stamps the query's start.
func (t *SlowQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if t.Threshold() <= 0 {
		return ctx
	}
	return context.WithValue(ctx, slowQueryKey{}, slowQueryStart{at: t.now(), sql: data.SQL})
}

// TraceQueryEnd logs and counts the query when it ran past the threshold.
func (t *SlowQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	start, ok := ctx.Value(slowQueryKey{}).(slowQueryStart)
	threshold := t.Threshold()
	if !ok || threshold <= 0 {
		return
	}
	took := t.now().Sub(start.at)
	if took < threshold {
		return
	}
	t.onSlow()
	t.log.Warn("slow query",
		"duration_ms", float64(took)/float64(time.Millisecond),
		"threshold_ms", float64(threshold)/float64(time.Millisecond),
		"fingerprint", Fingerprint(start.sql),
		"errored", data.Err != nil)
}

// Fingerprint is a statement as a log line names it: its whitespace
// collapsed, bounded at FingerprintMax runes with an ellipsis past it.
func Fingerprint(sql string) string {
	f := strings.Join(strings.Fields(sql), " ")
	if r := []rune(f); len(r) > FingerprintMax {
		return string(r[:FingerprintMax]) + "…"
	}
	return f
}
