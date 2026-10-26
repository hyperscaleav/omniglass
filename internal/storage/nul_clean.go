package storage

import "github.com/hyperscaleav/omniglass/internal/logline"

// The sample, event and node-log lanes hold what came off a wire (a device's
// line, a pushed value, a reply a node logged) to what Postgres stores: a NUL
// in any text or jsonb value is replaced with U+FFFD before the insert
// (logline.Clean, logline.CleanAttributes), as the log-line lane does. An
// ingest batch lands all or nothing (LandBatch), so one value Postgres
// refuses would lose every row that rode with it, after the push route had
// answered 202 for it.

// cleanPropertyWrites is evs with NUL replaced in every text field.
func cleanPropertyWrites(evs []PropertySampleWrite) []PropertySampleWrite {
	out := make([]PropertySampleWrite, len(evs))
	for i, ev := range evs {
		ev.Value, ev.Instance, ev.Source = logline.Clean(ev.Value), logline.Clean(ev.Instance), logline.Clean(ev.Source)
		out[i] = ev
	}
	return out
}

// cleanMetricWrites is evs with NUL replaced in every text field.
func cleanMetricWrites(evs []MetricSampleWrite) []MetricSampleWrite {
	out := make([]MetricSampleWrite, len(evs))
	for i, ev := range evs {
		ev.Instance, ev.Source = logline.Clean(ev.Instance), logline.Clean(ev.Source)
		out[i] = ev
	}
	return out
}

// cleanEventWrites is evs with NUL replaced in every text field and every
// string of the attributes.
func cleanEventWrites(evs []EventWrite) []EventWrite {
	out := make([]EventWrite, len(evs))
	for i, ev := range evs {
		ev.Message, ev.Instance, ev.Source = logline.Clean(ev.Message), logline.Clean(ev.Instance), logline.Clean(ev.Source)
		ev.Attributes = logline.CleanAttributes(ev.Attributes)
		out[i] = ev
	}
	return out
}

// cleanNodeLogWrites is lines with NUL replaced in every text field and
// every string of the attributes and labels.
func cleanNodeLogWrites(lines []NodeLogWrite) []NodeLogWrite {
	out := make([]NodeLogWrite, len(lines))
	for i, l := range lines {
		l.Message, l.Source = logline.Clean(l.Message), logline.Clean(l.Source)
		l.Severity, l.Facility, l.CorrelationID = logline.Clean(l.Severity), logline.Clean(l.Facility), logline.Clean(l.CorrelationID)
		l.Attributes, l.Labels = logline.CleanAttributes(l.Attributes), logline.CleanAttributes(l.Labels)
		out[i] = l
	}
	return out
}
