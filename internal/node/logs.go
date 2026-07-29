package node

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// capturedLog is one of the node's own log records, held for shipment as a
// self-log (ADR-0066). It is the severity + message + attrs distilled from an
// slog.Record, decoupled from slog so the mapping to a LogLine stays pure.
type capturedLog struct {
	ts       time.Time
	severity string
	message  string
	attrs    map[string]any
}

// maxBufferedLogs bounds the self-log buffer between drains. The run loop installs
// the sink as the process default logger, so EVERY node slog call buffers; a chatty
// stretch between ticks (or a publish that stops draining) would otherwise grow
// without bound on a long-lived edge process. At the cap the oldest records are
// dropped (the newest are the ones worth shipping) and the count is reported.
const maxBufferedLogs = 1000

// logSink is an slog.Handler that both forwards records to an inner handler (the
// node console) and buffers them for shipment as node self-logs. It is the node
// half of the raw log lane: the run loop drains the buffer each tick and
// publishes the lines on the node's telemetry subject, where the ingest consumer
// lands them owner-bound to the node. The buffer, its drop counter, and the lock
// are shared by pointer across WithAttrs/WithGroup clones so every derived logger
// (and the process default) appends to one bounded queue.
type logSink struct {
	inner   slog.Handler
	base    []slog.Attr
	mu      *sync.Mutex
	buf     *[]capturedLog
	dropped *int
}

// newLogSink wraps an inner handler with the self-log capture buffer.
func newLogSink(inner slog.Handler) *logSink {
	return &logSink{inner: inner, mu: &sync.Mutex{}, buf: &[]capturedLog{}, dropped: new(int)}
}

func (h *logSink) Enabled(ctx context.Context, l slog.Level) bool { return h.inner.Enabled(ctx, l) }

// Handle captures the record (its bound and inline attrs merged) into the buffer,
// then forwards to the inner handler so the record still reaches the console.
func (h *logSink) Handle(ctx context.Context, r slog.Record) error {
	attrs := map[string]any{}
	for _, a := range h.base {
		attrs[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	if len(*h.buf) >= maxBufferedLogs {
		// At the cap, drop the oldest so the newest (the ones that explain what the
		// node is doing right now) survive. The console still saw every record.
		*h.buf = append((*h.buf)[:0], (*h.buf)[len(*h.buf)-maxBufferedLogs+1:]...)
		*h.dropped++
	}
	*h.buf = append(*h.buf, capturedLog{ts: r.Time, severity: severityOf(r.Level), message: r.Message, attrs: attrs})
	h.mu.Unlock()
	return h.inner.Handle(ctx, r)
}

// WithAttrs binds attrs onto both the inner handler (console) and the captured
// base (self-logs). WithGroup shares the buffer but does not prefix captured keys:
// the node does not group its self-logs, so the simple path is the correct one.
func (h *logSink) WithAttrs(as []slog.Attr) slog.Handler {
	return &logSink{inner: h.inner.WithAttrs(as), base: append(append([]slog.Attr{}, h.base...), as...), mu: h.mu, buf: h.buf, dropped: h.dropped}
}

func (h *logSink) WithGroup(name string) slog.Handler {
	return &logSink{inner: h.inner.WithGroup(name), base: h.base, mu: h.mu, buf: h.buf, dropped: h.dropped}
}

// drain returns the buffered records and clears the buffer. When records were
// dropped at the cap since the last drain, the batch leads with a synthetic
// overflow line carrying the count, so a truncated window is visible on the lane
// instead of silently short. The notice is built here, not emitted through slog,
// so reporting a drop cannot itself feed the buffer.
func (h *logSink) drain() []capturedLog {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := *h.buf
	if *h.dropped > 0 {
		var ts time.Time // an empty batch leaves it zero, and storage stamps now()
		if len(out) > 0 {
			ts = out[0].ts
		}
		notice := capturedLog{
			ts:       ts,
			severity: "warning",
			message:  "self-log buffer overflow, oldest lines dropped",
			attrs:    map[string]any{"facility": "node", "dropped": *h.dropped},
		}
		out = append([]capturedLog{notice}, out...)
		*h.dropped = 0
	}
	*h.buf = nil
	return out
}

// severityOf maps an slog level to the log_line severity vocabulary.
func severityOf(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warning"
	case l >= slog.LevelInfo:
		return "info"
	default:
		return "debug"
	}
}

// selfLogEvent maps captured node records to a telemetry Event carrying only log
// lines (no samples, no task): the node's self-logs on the raw ingest lane. Pure:
// no I/O. The reserved "source" and "facility" attrs are lifted into the LogLine
// columns (source defaults to "node"); the rest ride the attributes JSON. Returns
// nil when there is nothing to ship.
func selfLogEvent(node string, recs []capturedLog) *ogv1.Event {
	if len(recs) == 0 {
		return nil
	}
	logs := make([]*ogv1.LogLine, 0, len(recs))
	for _, r := range recs {
		source := "node"
		if s, ok := r.attrs["source"].(string); ok && s != "" {
			source = s
		}
		facility, _ := r.attrs["facility"].(string)
		rest := map[string]any{}
		for k, v := range r.attrs {
			if k == "source" || k == "facility" {
				continue
			}
			rest[k] = v
		}
		var attrs []byte
		if len(rest) > 0 {
			if b, err := json.Marshal(rest); err == nil {
				attrs = b
			}
		}
		logs = append(logs, &ogv1.LogLine{
			Message:    r.message,
			Source:     source,
			Severity:   r.severity,
			Facility:   facility,
			Attributes: attrs,
			Ts:         timestamppb.New(r.ts),
		})
	}
	return &ogv1.Event{NodeId: node, Logs: logs}
}
