package bus

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/footprint"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// wantStatementsPerReportBatch is every statement one endpoint's collection
// report batch costs (#1369): the window's poll duration and pushed message
// count, metric lane only. Exact, as the standard batch's is.
const wantStatementsPerReportBatch = 13

// reportBatch is one endpoint's collection report for a window, as the node
// publishes it: an endpoint that polls and holds a listening session, at
// its healthiest, two rows (a failure or an unclaimed count adds one each).
func reportBatch(task footprint.Task, window int, end time.Time) *ogv1.TelemetryBatch {
	ts := timestamppb.New(end)
	return &ogv1.TelemetryBatch{TaskId: task.ID, NodeId: task.Node, Ts: ts, Metrics: []*ogv1.MetricSample{
		{Name: probekeys.PollDuration, Value: 40 + float64(window%7), Ts: ts},
		{Name: probekeys.PushMessageCount, Value: float64(window % 3), Ts: ts},
	}}
}

// TestCollectionReportFootprint holds what the collection telemetry costs
// (#1369): one report batch per endpoint per window, delivered through the
// real handler over the dev scenario's fleet, costs a fixed number of
// statements and lands exactly its two rows. At the platform's one-minute
// window that is two rows per endpoint per minute beside the standard mix's
// checks, logged against them.
func TestCollectionReportFootprint(t *testing.T) {
	f := newFootprintFixture(t)
	f.tick(t, 0) // the endpoints' first seen stamps, as a running fleet has
	const windows = 3
	start := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	rows0 := f.count(t, `select count(*) from metric`)
	f.counter.Reset()
	for w := 1; w <= windows; w++ {
		for _, task := range f.tasks {
			data, err := proto.Marshal(reportBatch(task, w, start.Add(time.Duration(w)*time.Minute)))
			if err != nil {
				t.Fatal(err)
			}
			f.seq++
			m := &deliveredMsg{subject: collection.TelemetrySubject(task.Node), data: data,
				header: nats.Header{jetstream.MsgIDHeader: []string{fmt.Sprintf("fp-report-%d", f.seq)}}}
			f.srv.handleTelemetry(m)
			if !m.acked || m.termed || m.naked {
				t.Fatalf("task %s window %d: acked=%v termed=%v naked=%v (the report did not land)", task.ID, w, m.acked, m.termed, m.naked)
			}
		}
	}
	batches := windows * len(f.tasks)
	statements := f.counter.N()
	rows := f.count(t, `select count(*) from metric`) - rows0
	var checkRows int
	for _, s := range footprint.StandardMix {
		if s.Lane == footprint.MetricLane {
			checkRows++
		}
	}
	perMinute := float64(checkRows) * float64(time.Minute) / float64(30*time.Second)
	t.Logf("collection report footprint, dev scenario, %d endpoints x %d windows = %d batches", len(f.tasks), windows, batches)
	t.Logf("  statements per report batch %6.2f", float64(statements)/float64(batches))
	t.Logf("  metric rows per window      %6.2f per endpoint (%d rows)", float64(rows)/float64(batches), rows)
	t.Logf("  beside the standard mix     %6.1f%% at most: an endpoint that both polls and listens, against %d metric rows a check at 30 s (%.0f a minute)", 100*2/perMinute, checkRows, perMinute)
	if statements != wantStatementsPerReportBatch*batches {
		t.Errorf("statements: %d over %d report batches (%.2f a batch), want exactly %d a batch; one batch issued:\n  %s",
			statements, batches, float64(statements)/float64(batches), wantStatementsPerReportBatch,
			strings.Join(firstBatchStatements(f.counter, statements/batches), "\n  "))
	}
	if want := int64(2 * batches); rows != want {
		t.Errorf("metric rows: %d for %d report batches, want exactly %d: each window lands its two rows", rows, batches, want)
	}
}
