package cli

import (
	"context"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// settingsReadTimeout bounds one read of a platform setting the server's
// self-report takes, so a stalled database costs the read its default.
const settingsReadTimeout = 5 * time.Second

// serverMetricsStore is what the server's self-report reads and writes; the
// gateway is one.
type serverMetricsStore interface {
	platformmetric.Store
	CollectionSettings(ctx context.Context) (settings.CollectionSettings, error)
	DatabaseSettings(ctx context.Context) (settings.DatabaseSettings, error)
	PoolStat() storage.PoolStat
}

// serverMetrics is the server's measure of itself (#1627): the recorder the
// API's request middleware and the slow-query tracer count into, and the
// tracer, which is built before the gateway it is installed on.
type serverMetrics struct {
	rec    *platformmetric.Recorder
	tracer *storage.SlowQueryTracer
}

// newServerMetrics is a recorder and a tracer that counts each slow query
// into it, logging to log.
func newServerMetrics(log *slog.Logger, now func() time.Time) *serverMetrics {
	rec := platformmetric.NewRecorder(now)
	return &serverMetrics{
		rec:    rec,
		tracer: storage.NewSlowQueryTracer(log, now, func() { rec.Count(writerclass.DBSlowQueryCount, "", 1) }),
	}
}

// applySlowQuery moves the tracer to database.slow_query_ms. A setting that
// cannot be read keeps the threshold the tracer has.
func (m *serverMetrics) applySlowQuery(ctx context.Context, gw serverMetricsStore) {
	ctx, cancel := context.WithTimeout(ctx, settingsReadTimeout)
	defer cancel()
	s, err := gw.DatabaseSettings(ctx)
	if err != nil {
		return
	}
	m.tracer.SetThreshold(time.Duration(s.SlowQueryMS) * time.Millisecond)
}

// serverReportWindow is the collection report window, as the webhook receiver
// arms on: collection.report_seconds, else a minute when it cannot be read.
func serverReportWindow(ctx context.Context, gw serverMetricsStore) time.Duration {
	ctx, cancel := context.WithTimeout(ctx, settingsReadTimeout)
	defer cancel()
	s, err := gw.CollectionSettings(ctx)
	if err != nil || s.ReportSeconds <= 0 {
		return time.Minute
	}
	return time.Duration(s.ReportSeconds) * time.Second
}

// reporter lands the recorder's windows on the server's component: each
// window it gauges the pool and re-reads the slow-query setting, so a change
// to it takes within a window without a restart.
func (m *serverMetrics) reporter(gw serverMetricsStore, componentID string, log *slog.Logger) *platformmetric.Reporter {
	return platformmetric.NewReporter(m.rec, gw, componentID,
		func(ctx context.Context) time.Duration { return serverReportWindow(ctx, gw) },
		log,
		platformmetric.PoolSampler(gw.PoolStat),
		func(ctx context.Context, _ *platformmetric.Recorder) { m.applySlowQuery(ctx, gw) },
	)
}
