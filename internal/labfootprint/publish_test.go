package labfootprint

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The publish read (#1746) sums the server's push lane turn counts over the
// window: the calls that reached the turn and the calls it refused, one
// `calls|refused` line.
func TestPublishQueryReadsThePushLaneTurnCounts(t *testing.T) {
	q := PublishQuery(at(0), at(600))
	for _, want := range []string{"'" + writerclass.PushLanePublishCount + "'", "'" + writerclass.PushLaneRefusalCount + "'",
		"to_timestamp(" + epoch(at(0)) + ")", "to_timestamp(" + epoch(at(600)) + ")", "coalesce("} {
		if !strings.Contains(q, want) {
			t.Errorf("the publish query has no %q:\n%s", want, q)
		}
	}
}

// The parse reads the one line; a lab with no pusher reads 0 calls, and a
// count that is not a number, or refusals past the calls, is an error.
func TestParsePublish(t *testing.T) {
	got, err := ParsePublish("12|3\n")
	if err != nil || got != (PublishCount{Calls: 12, Refused: 3}) {
		t.Errorf("parsed %+v (%v), want 12 calls, 3 refused", got, err)
	}
	if got, err := ParsePublish("0|0\n"); err != nil || got != (PublishCount{}) {
		t.Errorf("an idle lane parsed %+v (%v), want nothing", got, err)
	}
	for _, bad := range []string{"twelve|3", "12", "2|3"} {
		if _, err := ParsePublish(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// No call reached the turn: no publish row, not a fake healthy 0.
func TestPublishRowAbsentWhenNothingPushed(t *testing.T) {
	if rows := PublishRowOf(PublishCount{}); len(rows) != 0 {
		t.Errorf("rows = %+v, want none", rows)
	}
}

// TestScaleDeliveryReadsARefusedPushAsRefused (#1746): a pusher answered
// 503 because the publish turn's wait ran out reads as the push lane's
// publish row's loss, cause refused-turn, ahead of the lane's ingest rows.
// It minted nothing, so the ingest row over the same boot reads 0 and the
// table names nothing missing on the push lane.
func TestScaleDeliveryReadsARefusedPushAsRefused(t *testing.T) {
	m := func(n, before uint64) delivery.Declared {
		return delivery.Declared{delivery.Metric: {Count: n, Before: before}}
	}
	run := deliveryRun(
		receipt("b1", 1, 100, m(40, 0), nil),
		receipt("b1", 2, 700, m(40, 40), nil),
		// The refused call between seq 1 and 2 minted nothing: seq stays dense.
		pushed(1, 100, m(3, 0), nil),
		pushed(2, 300, m(3, 3), nil),
		pushed(3, 700, m(3, 6), nil),
	)
	run.Delivery.Publish = PublishCount{Calls: 4, Refused: 1}
	rep := SummarizeScale(run)
	if len(rep.Delivery) != 4 {
		t.Fatalf("delivery rows = %+v, want the node lane's two, then push publish and push ingest", rep.Delivery)
	}
	pub, ing := rep.Delivery[2], rep.Delivery[3]
	if pub.Lane != LanePush || pub.Hop != delivery.Publish || pub.In != 4 || pub.Out != 3 || pub.Loss == nil || *pub.Loss != 0.25 ||
		pub.Outcomes[delivery.RefusedTurn] != 1 || pub.Outcomes[delivery.TurnTaken] != 3 || pub.Cause() != delivery.RefusedTurn {
		t.Errorf("publish row = %+v, want 4 calls in, 3 out, 1 refused-turn", pub)
	}
	if ing.Lane != LanePush || ing.Hop != delivery.Ingest || ing.Loss == nil || *ing.Loss != 0 || ing.Outcomes[delivery.Missing] != 0 {
		t.Errorf("push ingest row = %+v, want 0 loss, nothing missing", ing)
	}
	if got := rep.DeliveryEndToEnd[delivery.Metric]; got != 0 {
		t.Errorf("node end to end metric = %v, want 0: the push lane's publish row is its own", got)
	}
	table := rep.Table()
	for _, want := range []string{
		"pushes      push  publish  4        3        0.25     -      refused-turn",
		"publish     push  " + publishLabel,
	} {
		if !strings.Contains(table, want) {
			t.Errorf("the table has no %q:\n%s", want, table)
		}
	}
	if strings.Contains(table, "missing     push") {
		t.Errorf("a refused push reads as missing:\n%s", table)
	}
	b, _ := json.Marshal(rep)
	if !strings.Contains(string(b), `"lane":"push","hop":"publish","in":4,"out":3`) ||
		!strings.Contains(string(b), `"refused-turn":1`) {
		t.Errorf("the JSON publish row is not marked by its lane and cause: %s", b)
	}
}
