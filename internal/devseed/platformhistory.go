package devseed

import (
	"context"
	"fmt"
	"hash/fnv"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/hyperscaleav/omniglass/internal/logline"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// PlatformHistoryMinutes is how much platform history a capture seeds: one
// sample a minute for the hour before now, the span a sparkline draws.
const PlatformHistoryMinutes = 60

// platformHistorySource names the seeded samples, so they stay apart from the
// figures the server measures and a second run can find them.
const platformHistorySource = "devseed"

// historySeries is one (key, instance) the history writes and the shape of
// its hour: a base value, a deterministic swing around it as a share of the
// base, and a climb that the hour rises by into its newest minute (a size that
// grows). A whole series is a count, rounded to an integer.
type historySeries struct {
	instance string
	base     float64
	swing    float64
	climb    float64
	whole    bool
}

// one is a single unsplit series.
func one(base, swing float64) []historySeries {
	return []historySeries{{base: base, swing: swing}}
}

// count is a single unsplit count.
func count(base, swing float64) []historySeries {
	return []historySeries{{base: base, swing: swing, whole: true}}
}

// zero is a single unsplit series that reads 0 the whole hour: a failure or a
// queue that is healthy.
func zero() []historySeries { return []historySeries{{}} }

// consoleRoutes is the routes a console polling platform health calls, each
// with its typical slowest response in milliseconds, read off a live lab. A
// capture lab has no node, so these are the routes its server serves.
var consoleRoutes = []struct {
	route string
	ms    float64
}{
	{"GET /api/v1/views/platform 2xx", 10.6},
	{"GET /api/v1/views/platform/simulation 2xx", 1.8},
	{"GET /api/v1/auth/me 2xx", 1.6},
	{"GET /api/v1/auth/me/notifications 2xx", 2.4},
	{"GET /api/v1/auth/status 2xx", 0.5},
	{"GET /api/v1/settings/me 2xx", 2},
	{"GET /api/v1/simulation 2xx", 2.3},
	{"GET /api/v1/systems 2xx", 11.8},
	{"GET /api/v1/views/fleet 2xx", 11.2},
	{"GET /api/v1/components 2xx", 24},
}

// retentionLaneNames is every lane a retention pass reports, as the pass names
// them (bus.retentionLanes): the sample lanes, the trend lane, then each log
// table's bands.
func retentionLaneNames() []string {
	lanes := []string{"metric", "property", "metric_trend"}
	for _, table := range []string{"log_line", "node_log"} {
		for _, band := range logline.RetentionBands() {
			lanes = append(lanes, table+"/"+band)
		}
	}
	return lanes
}

// platformHistory is the hour each server panel key draws, keyed by metric
// key. Every value is a healthy small lab's: failures and queues read 0, the
// pool sits low against its size. The sizes are measured instead
// (platformHistoryMeasured). A key on a panel that is in none of the three
// fails TestPlatformHistoryCoversEveryServerPanelKey.
var platformHistory = func() map[string][]historySeries {
	h := map[string][]historySeries{
		writerclass.DBPoolAcquired:        count(0.6, 1),
		writerclass.DBPoolMax:             count(12, 0),
		writerclass.DBPoolWaitCount:       zero(),
		writerclass.BusConsumerPending:    zero(),
		writerclass.BusConsumerAckPending: zero(),
		writerclass.CalcPendingDepth:      zero(),
		writerclass.OutboxQueuedDepth:     zero(),
		writerclass.OutboxDueAge:          zero(),
		writerclass.IngestLaneBusy:        one(0.05, 0.4),
		writerclass.RetentionWindowDays:   {{instance: "metric", base: 14, whole: true}},
	}
	for _, r := range consoleRoutes {
		h[writerclass.HTTPRequestDurationMax] = append(h[writerclass.HTTPRequestDurationMax], historySeries{instance: r.route, base: r.ms, swing: 0.3})
		h[writerclass.HTTPRequestCount] = append(h[writerclass.HTTPRequestCount], historySeries{instance: r.route, base: 1.5, swing: 0.4, whole: true})
	}
	for _, lane := range retentionLaneNames() {
		h[writerclass.RetentionDeletedCount] = append(h[writerclass.RetentionDeletedCount], historySeries{instance: lane})
		h[writerclass.RetentionBehind] = append(h[writerclass.RetentionBehind], historySeries{instance: lane})
	}
	for _, c := range []string{"metric_trend compaction", "metric_block compaction"} {
		h[writerclass.RetentionBehind] = append(h[writerclass.RetentionBehind], historySeries{instance: c})
	}
	return h
}()

// platformHistoryMeasured is each server panel key whose hour is anchored on
// what the database measures as the seed runs (RetentionStatus, the read the
// retention pass reports from), so the hour ends on the size the server's
// next pass writes and draws no cliff into it.
var platformHistoryMeasured = map[string]bool{
	writerclass.MetricOldestDayAge: true,
	writerclass.DatabaseBytes:      true,
	writerclass.TableBytes:         true,
}

// windowUnwritten is why a window key a capture lab never writes is left out:
// with no node, nothing is ingested or pushed and no query runs slow, and a
// window key past two report windows reads "not reported" however much
// history it has, so a seeded hour of it would never draw.
const windowUnwritten = "a window key no capture lab writes: it reads not reported once its newest sample is two report windows old, so a seeded hour would never draw"

// streamPosition is why a stream position is left to the server.
const streamPosition = "a stream position: the lab's stream starts over, so a seeded hour would draw a cliff into the live head"

// platformHistoryExempt is each server panel key the history leaves to the
// server, and why.
var platformHistoryExempt = map[string]string{
	writerclass.BusConsumerAckFloor:  streamPosition,
	writerclass.BusStreamLastSeq:     streamPosition,
	writerclass.DBSlowQueryCount:     windowUnwritten,
	writerclass.IngestLag:            windowUnwritten,
	writerclass.IngestLagMax:         windowUnwritten,
	writerclass.IngestBatchCount:     windowUnwritten,
	writerclass.IngestSampleCount:    windowUnwritten,
	writerclass.PushLaneRefusalCount: windowUnwritten,
	writerclass.PushLanePublishCount: windowUnwritten,
	writerclass.PushLaneTurnWaitP99:  windowUnwritten,
	writerclass.PushLaneTurnWaitMax:  windowUnwritten,
}

// historyFor is the whole hour ending at end: the profiles, plus the sizes
// and the oldest partition's age anchored on status. A size climbs into the
// figure measured; the age is read off the oldest partition at each minute.
func historyFor(status storage.RetentionStatus, end time.Time) map[string][]historySeries {
	h := maps.Clone(platformHistory)
	h[writerclass.DatabaseBytes] = []historySeries{{base: float64(status.DatabaseBytes), climb: 0.005, whole: true}}
	for _, table := range slices.Sorted(maps.Keys(status.TableBytes)) {
		h[writerclass.TableBytes] = append(h[writerclass.TableBytes],
			historySeries{instance: table, base: float64(status.TableBytes[table]), climb: 0.02, whole: true})
	}
	if len(status.Partitions) > 0 {
		// The newest minute is a minute before end; the oldest is the
		// hour's span of a day before that.
		age := end.Add(-time.Minute).Sub(status.Partitions[0].From).Hours() / 24
		span := float64(PlatformHistoryMinutes-1) / (24 * 60)
		if age > 0 {
			h[writerclass.MetricOldestDayAge] = []historySeries{{base: age, climb: span / age}}
		}
	}
	return h
}

// historyValue is series s's value in minute i of n, oldest first: the base,
// raised toward the newest minute by the climb, swung by a wave whose phase is
// fixed per series so two series do not move in step. Never negative.
func historyValue(key string, s historySeries, i, n int) float64 {
	f := 0.0
	if n > 1 {
		f = float64(i-(n-1)) / float64(n-1)
	}
	h := fnv.New32a()
	h.Write([]byte(key + "\x00" + s.instance))
	salt := float64(h.Sum32()%628) / 100
	v := s.base * (1 + s.climb*f) * (1 + s.swing*math.Sin(0.9*float64(i)+salt))
	if s.whole {
		v = math.Round(v)
	}
	return math.Max(v, 0)
}

// SeedPlatformHistory lands an hour of the named server's platform figures,
// a sample a minute per key and instance a server panel reads, on its
// platform component, ending at the minute before now. It is for a capture
// lab only (lab --platform-history, #1790): a learner's lab reads the figures
// its own server measured. The seeded samples are their own sentinel, so a
// second run writes nothing.
func SeedPlatformHistory(ctx context.Context, gw storage.Gateway, identity string, now time.Time) error {
	component, err := gw.EnsureServerComponent(ctx, identity)
	if err != nil {
		return fmt.Errorf("platform history: server component: %w", err)
	}
	prior, err := gw.ListMetricSeries(ctx, "component", component, writerclass.DBPoolMax, 0, 100000)
	if err != nil {
		return fmt.Errorf("platform history: read sentinel: %w", err)
	}
	for _, s := range prior {
		if s.Source == platformHistorySource {
			return nil
		}
	}
	status, err := gw.RetentionStatus(ctx)
	if err != nil {
		return fmt.Errorf("platform history: measure sizes: %w", err)
	}
	end := now.Truncate(time.Minute)
	history := historyFor(status, end)
	var rows []storage.PlatformMetric
	for _, key := range slices.Sorted(maps.Keys(history)) {
		for _, s := range history[key] {
			for i := range PlatformHistoryMinutes {
				rows = append(rows, storage.PlatformMetric{
					ComponentID: component, Key: key, Instance: s.instance,
					Value:  historyValue(key, s, i, PlatformHistoryMinutes),
					TS:     end.Add(-time.Duration(PlatformHistoryMinutes-i) * time.Minute),
					Source: platformHistorySource,
				})
			}
		}
	}
	if err := gw.RecordPlatformMetrics(ctx, rows); err != nil {
		return fmt.Errorf("platform history: record: %w", err)
	}
	return nil
}

// PlatformHistoryKeys is every key the history can write, sorted.
func PlatformHistoryKeys() []string {
	keys := slices.Collect(maps.Keys(platformHistory))
	for k := range platformHistoryMeasured {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// PlatformHistorySeries is every (key, instance) the history writes into a
// database that measures status at now.
func PlatformHistorySeries(status storage.RetentionStatus, now time.Time) [][2]string {
	history := historyFor(status, now.Truncate(time.Minute))
	var out [][2]string
	for _, k := range slices.Sorted(maps.Keys(history)) {
		for _, s := range history[k] {
			out = append(out, [2]string{k, s.instance})
		}
	}
	return out
}
