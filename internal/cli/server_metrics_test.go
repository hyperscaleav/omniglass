package cli

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// fakeSelfStore is the gateway as the server's self-report sees it.
type fakeSelfStore struct {
	collection    settings.CollectionSettings
	collectionErr error
	database      settings.DatabaseSettings
	databaseErr   error
	pool          storage.PoolStat
	outbox        storage.OutboxStatus
	outboxErr     error
	written       []storage.PlatformMetric
}

func (f *fakeSelfStore) RecordPlatformMetrics(_ context.Context, rows []storage.PlatformMetric) error {
	f.written = append(f.written, rows...)
	return nil
}

func (f *fakeSelfStore) CollectionSettings(context.Context) (settings.CollectionSettings, error) {
	return f.collection, f.collectionErr
}

func (f *fakeSelfStore) DatabaseSettings(context.Context) (settings.DatabaseSettings, error) {
	return f.database, f.databaseErr
}

func (f *fakeSelfStore) PoolStat() storage.PoolStat { return f.pool }

func (f *fakeSelfStore) OutboxStatus(context.Context) (storage.OutboxStatus, error) {
	return f.outbox, f.outboxErr
}

// TestServerReportWindowFollowsTheCollectionWindow: the server's self-report
// arms on collection.report_seconds as the webhook receiver does, and on a
// minute when the setting cannot be read.
func TestServerReportWindowFollowsTheCollectionWindow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if got := serverReportWindow(ctx, &fakeSelfStore{collection: settings.CollectionSettings{ReportSeconds: 30}}); got != 30*time.Second {
		t.Errorf("window = %v, want 30s", got)
	}
	if got := serverReportWindow(ctx, &fakeSelfStore{collectionErr: errors.New("down")}); got != time.Minute {
		t.Errorf("window on an unreadable setting = %v, want a minute", got)
	}
}

// TestApplySlowQueryFollowsTheSetting: database.slow_query_ms moves the
// tracer, 0 turns it off, and a setting that cannot be read keeps what the
// tracer had.
func TestApplySlowQueryFollowsTheSetting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := newServerMetrics(slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now)
	if got := m.tracer.Threshold(); got != storage.DefaultSlowQuery {
		t.Fatalf("boot threshold = %v, want the default %v", got, storage.DefaultSlowQuery)
	}
	m.applySlowQuery(ctx, &fakeSelfStore{database: settings.DatabaseSettings{SlowQueryMS: 250}})
	if got := m.tracer.Threshold(); got != 250*time.Millisecond {
		t.Errorf("threshold = %v, want 250ms", got)
	}
	m.applySlowQuery(ctx, &fakeSelfStore{databaseErr: errors.New("down")})
	if got := m.tracer.Threshold(); got != 250*time.Millisecond {
		t.Errorf("threshold after an unreadable setting = %v, want it kept at 250ms", got)
	}
	m.applySlowQuery(ctx, &fakeSelfStore{database: settings.DatabaseSettings{SlowQueryMS: 0}})
	if got := m.tracer.Threshold(); got != 0 {
		t.Errorf("threshold = %v, want 0 (off)", got)
	}
}

// TestServerReporterGaugesThePoolAndReReadsTheSetting: each window lands the
// pool's gauges on the server's component and re-reads the slow-query
// setting, so a change takes within a window without a restart.
func TestServerReporterGaugesThePoolAndReReadsTheSetting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := newServerMetrics(slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now)
	gw := &fakeSelfStore{database: settings.DatabaseSettings{SlowQueryMS: 900},
		outbox: storage.OutboxStatus{Queued: 7, DueAge: 45 * time.Second}}
	sampled := false
	rp := m.reporter(gw, "server-1", nil, func(context.Context, *platformmetric.Recorder) { sampled = true })
	gw.pool = storage.PoolStat{Acquired: 3, Max: 12, EmptyAcquireCount: 4}
	if err := rp.Report(ctx); err != nil {
		t.Fatalf("report: %v", err)
	}
	if got := m.tracer.Threshold(); got != 900*time.Millisecond {
		t.Errorf("threshold after a window = %v, want the setting's 900ms", got)
	}
	got := map[string]float64{}
	for _, r := range gw.written {
		if r.ComponentID != "server-1" {
			t.Errorf("%s landed on %q, want the server's component", r.Key, r.ComponentID)
		}
		got[r.Key] = r.Value
	}
	if got[writerclass.DBPoolAcquired] != 3 || got[writerclass.DBPoolMax] != 12 || got[writerclass.DBPoolWaitCount] != 4 {
		t.Errorf("pool gauges = %v", got)
	}
	if got[writerclass.OutboxQueuedDepth] != 7 || got[writerclass.OutboxDueAge] != 45 {
		t.Errorf("outbox gauges = %v, want depth 7 and due age 45 s", got)
	}
	if _, ok := got[writerclass.ProcessGoroutineCount]; !ok {
		t.Errorf("the window read no runtime: %v", got)
	}
	if !sampled {
		t.Error("the bus sampler the reporter was handed never ran")
	}
}

// TestServerReporterSaysNothingOfAnUnreadOutbox: an outbox read that fails
// costs its window's outbox figures, not a zero depth that was never read.
func TestServerReporterSaysNothingOfAnUnreadOutbox(t *testing.T) {
	t.Parallel()
	m := newServerMetrics(slog.New(slog.NewTextHandler(io.Discard, nil)), time.Now)
	gw := &fakeSelfStore{outboxErr: errors.New("down")}
	if err := m.reporter(gw, "server-1", nil).Report(context.Background()); err != nil {
		t.Fatalf("report: %v", err)
	}
	for _, r := range gw.written {
		if r.Key == writerclass.OutboxQueuedDepth || r.Key == writerclass.OutboxDueAge {
			t.Errorf("an unread outbox reported %s = %v", r.Key, r.Value)
		}
	}
}
