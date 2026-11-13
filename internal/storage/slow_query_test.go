package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// stepClock advances by step on every read, so a query traced through it
// takes exactly step.
type stepClock struct {
	t    time.Time
	step time.Duration
}

func (c *stepClock) now() time.Time { c.t = c.t.Add(c.step); return c.t }

// traceOne runs one query's start and end through tr.
func traceOne(tr *storage.SlowQueryTracer, sql string, args []any, err error) {
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: sql, Args: args})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: err})
}

// logLines decodes a JSON handler's buffer, one record per line.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// TestSlowQueryTracerLogsOnlyPastTheThreshold: a query that ran past the
// threshold logs one WARN with its duration and fingerprint and counts once;
// one under it does neither. The arguments are never logged: they carry
// operator data and secrets.
func TestSlowQueryTracerLogsOnlyPastTheThreshold(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	clock := &stepClock{t: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC), step: 50 * time.Millisecond}
	var slow atomic.Int64
	tr := storage.NewSlowQueryTracer(slog.New(slog.NewJSONHandler(&buf, nil)), clock.now, func() { slow.Add(1) })
	tr.SetThreshold(100 * time.Millisecond)

	traceOne(tr, "select 1", nil, nil)
	if buf.Len() != 0 || slow.Load() != 0 {
		t.Fatalf("a 50ms query under a 100ms threshold logged %q and counted %d", buf.String(), slow.Load())
	}

	clock.step = 250 * time.Millisecond
	traceOne(tr, "select *\n\t from   secret_store\n where id = $1", []any{"hunter2-password"}, nil)
	lines := logLines(t, &buf)
	if len(lines) != 1 || slow.Load() != 1 {
		t.Fatalf("a 250ms query logged %d lines, counted %d; want one of each", len(lines), slow.Load())
	}
	l := lines[0]
	if l["level"] != "WARN" || l["duration_ms"] != 250.0 || l["fingerprint"] != "select * from secret_store where id = $1" || l["errored"] != false {
		t.Errorf("slow query log = %+v", l)
	}
	if strings.Contains(buf.String(), "hunter2") {
		t.Errorf("the log carried a query argument: %s", buf.String())
	}

	buf.Reset()
	traceOne(tr, "select 2", nil, errors.New("canceled"))
	if l := logLines(t, &buf); len(l) != 1 || l[0]["errored"] != true {
		t.Errorf("a slow failed query logged %+v, want errored true", l)
	}
}

// A zero threshold turns the tracer off: nothing logs or counts however long
// a query runs.
func TestSlowQueryTracerZeroThresholdIsOff(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	clock := &stepClock{t: time.Now(), step: time.Hour}
	var slow atomic.Int64
	tr := storage.NewSlowQueryTracer(slog.New(slog.NewJSONHandler(&buf, nil)), clock.now, func() { slow.Add(1) })
	tr.SetThreshold(0)
	traceOne(tr, "select pg_sleep(3600)", nil, nil)
	if buf.Len() != 0 || slow.Load() != 0 {
		t.Fatalf("an off tracer logged %q and counted %d", buf.String(), slow.Load())
	}
}

// TestFingerprintCollapsesAndBounds: a fingerprint is the statement's text
// with its whitespace collapsed, bounded so a generated statement cannot
// flood the log.
func TestFingerprintCollapsesAndBounds(t *testing.T) {
	t.Parallel()
	if got := storage.Fingerprint("  select\n\ta ,\r\n  b  from x  "); got != "select a , b from x" {
		t.Errorf("Fingerprint = %q", got)
	}
	long := "select " + strings.Repeat("column_name, ", 100) + "1"
	got := storage.Fingerprint(long)
	if n := len([]rune(got)); n > storage.FingerprintMax+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("Fingerprint of a %d-rune statement = %d runes %q, want bounded with an ellipsis", len(long), n, got)
	}
}

// syncBuffer is a log buffer a pool's connections write at once.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestSlowQueryTracerOnARealPool: a real pg_sleep past a lowered threshold,
// on a pool the tracer is installed on, logs its WARN and counts once.
func TestSlowQueryTracerOnARealPool(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var buf syncBuffer
	var slow atomic.Int64
	tr := storage.NewSlowQueryTracer(slog.New(slog.NewJSONHandler(&buf, nil)), time.Now, func() { slow.Add(1) })
	tr.SetThreshold(100 * time.Millisecond)
	cfg, err := pgxpool.ParseConfig(storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.ConnConfig.Tracer = tr
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, "select 1"); err != nil {
		t.Fatalf("select 1: %v", err)
	}
	if _, err := pool.Exec(ctx, "select pg_sleep(0.2)"); err != nil {
		t.Fatalf("pg_sleep: %v", err)
	}
	if got := slow.Load(); got != 1 {
		t.Fatalf("slow count = %d, want 1 (the sleep, not the select 1)", got)
	}
	if !strings.Contains(buf.String(), `"fingerprint":"select pg_sleep(0.2)"`) {
		t.Errorf("log = %s, want the pg_sleep fingerprint", buf.String())
	}
}
