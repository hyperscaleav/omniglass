package bus

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The server's housekeeping narration (#1708): what a retention pass or the
// boot's partition check did lands as log lines on the server's platform
// component, beside the figures #1629 records there, under the server's
// product name as source and the retention facility. A log line, not an
// event: a pass is routine, and an event per pass would flood the event
// stream with non-conditions.
const (
	housekeepingSource   = storage.ServerProductName
	housekeepingFacility = "retention"
)

// housekeepingNote is one thing housekeeping did or failed at, told from one
// value to the process's log and to the server's component log, so the two
// never say different things. Severity is a canonical log level.
type housekeepingNote struct {
	Severity string
	Message  string
	Attrs    []slog.Attr
}

// retentionNotes is what one retention pass tells, in order: its failure,
// when err is set (a warning carrying it); the rows it pruned, when it
// removed any or left a lane behind; the hours it closed into trends; the
// days and weeks it folded into blocks; the partitions it rolled. A pass that
// did nothing tells nothing.
func retentionNotes(rep retentionReport, err error) []housekeepingNote {
	var notes []housekeepingNote
	if err != nil {
		notes = append(notes, housekeepingNote{"warning", "sample retention pass failed",
			[]slog.Attr{slog.String("error", err.Error())}})
	}
	var total int64
	for _, n := range rep.Deleted {
		total += n
	}
	if total > 0 || len(rep.Behind) > 0 {
		notes = append(notes, housekeepingNote{"info", "rows pruned past their retention window", []slog.Attr{
			slog.Any("deleted", rep.Deleted), slog.Int64("total", total), slog.Any("behind", rep.Behind)}})
	}
	if t := rep.Trends; t.Hours > 0 || t.Recomputed > 0 {
		notes = append(notes, housekeepingNote{"info", "metric hours closed into trends", []slog.Attr{
			slog.Int("hours", t.Hours), slog.Time("through", t.Through), slog.Int64("recomputed", t.Recomputed)}})
	}
	if b := rep.Blocks; b.Days > 0 || b.Weeks > 0 {
		notes = append(notes, housekeepingNote{"info", "metric days folded into blocks", []slog.Attr{
			slog.Int("days", b.Days), slog.Time("through", b.Through), slog.Int("weeks", b.Weeks),
			slog.Time("week_through", b.WeekThrough), slog.Int64("blocks", b.Blocks)}})
	}
	if len(rep.Created) > 0 || len(rep.Dropped) > 0 {
		notes = append(notes, housekeepingNote{"info", "metric partitions rolled", []slog.Attr{
			slog.Any("created", rep.Created), slog.Any("dropped", rep.Dropped)}})
	}
	return notes
}

// partitionNotes is what the boot's partition check tells: the metric
// partitions it made, nothing when it made none.
func partitionNotes(created []string) []housekeepingNote {
	if len(created) == 0 {
		return nil
	}
	return []housekeepingNote{{"info", "metric partitions created", []slog.Attr{slog.Any("created", created)}}}
}

// housekeepingLines maps notes to log lines on component at now, each note's
// attributes its line's: none for a server with no platform component.
func housekeepingLines(component string, notes []housekeepingNote, now time.Time) []storage.LogLineWrite {
	if component == "" || len(notes) == 0 {
		return nil
	}
	lines := make([]storage.LogLineWrite, 0, len(notes))
	for _, n := range notes {
		attrs := make(map[string]any, len(n.Attrs))
		for _, a := range n.Attrs {
			attrs[a.Key] = a.Value.Resolve().Any()
		}
		// Every value here is a number, a string, a time, or a slice or map
		// of them, which JSON always encodes; a failure would only lose the
		// line's attributes, never the line.
		raw, _ := json.Marshal(attrs)
		lines = append(lines, storage.LogLineWrite{OwnerKind: "component", OwnerID: component, Source: housekeepingSource,
			Severity: n.Severity, Facility: housekeepingFacility, Message: n.Message, Attributes: raw, TS: now})
	}
	return lines
}

// logLineWriter is the store half the narration writes through: the Storage
// Gateway's component log write.
type logLineWriter interface {
	InsertLogLines(ctx context.Context, lines []storage.LogLineWrite) error
}

// tellHousekeeping logs each note to the process's log, then writes them as
// log lines on component (none when it is empty) under a deadline of
// timeout (none when zero). A failed write is one warning: it never fails
// the housekeeping it narrates, and is not retried, so a store that is down
// costs the narration of one pass. A server shutting down (ctx done) writes
// nothing and warns of nothing.
func tellHousekeeping(ctx context.Context, store logLineWriter, component string, notes []housekeepingNote, now time.Time, timeout time.Duration) {
	for _, n := range notes {
		level := slog.LevelInfo
		if n.Severity == "warning" {
			level = slog.LevelWarn
		}
		slog.LogAttrs(ctx, level, n.Message, append([]slog.Attr{slog.String("facility", housekeepingFacility)}, n.Attrs...)...)
	}
	lines := housekeepingLines(component, notes, now)
	if len(lines) == 0 || ctx.Err() != nil {
		return
	}
	wctx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		wctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if err := store.InsertLogLines(wctx, lines); err != nil && ctx.Err() == nil {
		slog.Warn("housekeeping log lines not written", "facility", housekeepingFacility, "component", component, "error", err.Error())
	}
}
