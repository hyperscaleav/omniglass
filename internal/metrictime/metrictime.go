// Package metrictime provides the timestamp precision rule for the metric lane.
package metrictime

import "time"

// MetricTime is a metric sample's ts as it is stored: whole milliseconds
// (#1526). Outpost clocks are NTP-synced, accurate to a few milliseconds, so the
// digits below are noise; a raw row and the compacted block that later holds it
// (int4 millisecond offsets, #1265) then agree exactly. It truncates rather
// than refuses, so a spool replayed from an older outpost lands. The metric lane
// only: the property, event and log lanes keep the ts they were sent.
//
// Defined here so both internal/storage and internal/outpost may import it
// without the outpost depending on the storage package (#2189).
func MetricTime(ts time.Time) time.Time { return ts.Truncate(time.Millisecond) }
