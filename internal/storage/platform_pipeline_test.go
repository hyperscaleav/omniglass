package storage

import (
	"math"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// pipelineWindow is one component's window of one kind at a hop's instance:
// the three keys' newest samples and their last-hour sums.
func pipelineWindow(id, name, class, kind string, in, out uint64, hourIn, hourOut uint64, at time.Time) []pipelineSample {
	loss, _ := delivery.Loss(in, out)
	return []pipelineSample{
		{ComponentID: id, ComponentName: name, Class: class, Key: writerclass.DeliveryInCount, Instance: kind, Newest: float64(in), NewestAt: at, HourSum: float64(hourIn)},
		{ComponentID: id, ComponentName: name, Class: class, Key: writerclass.DeliveryOutCount, Instance: kind, Newest: float64(out), NewestAt: at, HourSum: float64(hourOut)},
		{ComponentID: id, ComponentName: name, Class: class, Key: writerclass.DeliveryLossRatio, Instance: kind, Newest: loss, NewestAt: at},
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

func hopOf(t *testing.T, p PlatformPipeline, id delivery.Hop) PlatformPipelineHopFigures {
	t.Helper()
	for _, h := range p.Hops {
		if h.Hop.ID == id {
			return h
		}
	}
	t.Fatalf("hop %s absent from %+v", id, p.Hops)
	return PlatformPipelineHopFigures{}
}

func kindOf(cells []PlatformPipelineCell, kind delivery.Kind) (PlatformPipelineCell, bool) {
	for _, c := range cells {
		if c.Kind == kind {
			return c, true
		}
	}
	return PlatformPipelineCell{}, false
}

// TestPipelineComposesAHopFromItsCountsNeverItsRatios: two components, one
// losing 1 of 2 metrics (0.5) and one losing none of 98, are one hop that lost
// 1 of 100: 0.01. Averaging the two ratios would read 0.25 and summing them
// 0.5; the ratio is (in - out) / in over the summed counts, both for the
// newest window and the last hour.
func TestPipelineComposesAHopFromItsCountsNeverItsRatios(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	var samples []pipelineSample
	samples = append(samples, pipelineWindow("c1", "edge-alpha", PlatformClassNode, "metric", 2, 1, 2, 1, at)...)
	samples = append(samples, pipelineWindow("c2", "edge-bravo", PlatformClassNode, "metric", 98, 98, 98, 98, at)...)
	p := composePipeline(PlatformPipelineHops, samples)

	node := hopOf(t, p, delivery.Node)
	m, ok := kindOf(node.Kinds, delivery.Metric)
	if !ok {
		t.Fatalf("node hop kinds = %+v, want a metric row", node.Kinds)
	}
	if m.Newest.In != 100 || m.Newest.Out != 99 || m.Newest.Loss == nil || !near(*m.Newest.Loss, 0.01) {
		t.Errorf("node metric newest = %+v (loss %v), want 100 in, 99 out, loss 0.01", m.Newest, lossText(m.Newest.Loss))
	}
	if m.LastHour.In != 100 || m.LastHour.Out != 99 || m.LastHour.Loss == nil || !near(*m.LastHour.Loss, 0.01) {
		t.Errorf("node metric last hour = %+v (loss %v), want 100 in, 99 out, loss 0.01", m.LastHour, lossText(m.LastHour.Loss))
	}
	if len(node.ByComponent) != 2 {
		t.Fatalf("by component = %+v, want edge-alpha and edge-bravo", node.ByComponent)
	}
	alpha := node.ByComponent[0]
	if alpha.Name != "edge-alpha" {
		t.Fatalf("first component = %s, want edge-alpha (name order)", alpha.Name)
	}
	if am, _ := kindOf(alpha.Kinds, delivery.Metric); am.Newest.Loss == nil || !near(*am.Newest.Loss, 0.5) {
		t.Errorf("edge-alpha newest loss = %v, want its own key's 0.5", lossText(am.Newest.Loss))
	}
	var in, out uint64
	for _, c := range node.ByComponent {
		in += c.Total.LastHour.In
		out += c.Total.LastHour.Out
	}
	if in != node.Total.LastHour.In || out != node.Total.LastHour.Out {
		t.Errorf("components sum to %d in, %d out, hop total %+v", in, out, node.Total.LastHour)
	}
}

// TestPipelineEndToEndComposesEveryHopsSurvival: the end-to-end figure per
// kind is delivery.EndToEnd over the hops the kind crosses, 1 - prod(1 - l),
// not the sum of the hops' ratios.
func TestPipelineEndToEndComposesEveryHopsSurvival(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	var samples []pipelineSample
	samples = append(samples, pipelineWindow("n1", "edge-alpha", PlatformClassNode, "metric", 10, 9, 10, 9, at)...)
	samples = append(samples, pipelineWindow("s1", "lab-server", PlatformClassServer, "metric", 9, 6, 9, 6, at)...)
	p := composePipeline(PlatformPipelineHops, samples)

	want := 1 - (1-0.1)*(1-1.0/3)
	if !near(want, delivery.EndToEnd(0.1, 1.0/3)) {
		t.Fatalf("the survival composition moved: %v", delivery.EndToEnd(0.1, 1.0/3))
	}
	e, ok := endOf(p.EndToEnd, delivery.Metric)
	if !ok || e.Newest == nil || !near(*e.Newest, want) || e.LastHour == nil || !near(*e.LastHour, want) {
		t.Errorf("metric end to end = %+v, want %v both ways", e, want)
	}
	if p.EndToEndTotal.LastHour == nil || !near(*p.EndToEndTotal.LastHour, want) {
		t.Errorf("all kinds end to end = %v, want %v (metrics are the only kind)", lossText(p.EndToEndTotal.LastHour), want)
	}
}

// TestPipelineReadsAnAbsentKindAsAbsent: a kind with no series at a hop has
// no row there and contributes nothing to the end to end; a kind with series
// but nothing in has a row and no ratio, never a fake 0.
func TestPipelineReadsAnAbsentKindAsAbsent(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	var samples []pipelineSample
	samples = append(samples, pipelineWindow("n1", "edge-alpha", PlatformClassNode, "metric", 10, 10, 10, 10, at)...)
	samples = append(samples, pipelineWindow("n1", "edge-alpha", PlatformClassNode, "log", 0, 0, 0, 0, at)...)
	samples = append(samples, pipelineWindow("s1", "lab-server", PlatformClassServer, "metric", 10, 8, 10, 8, at)...)
	samples = append(samples, pipelineWindow("s1", "lab-server", PlatformClassServer, "event", 4, 2, 4, 2, at)...)
	p := composePipeline(PlatformPipelineHops, samples)

	node := hopOf(t, p, delivery.Node)
	if _, ok := kindOf(node.Kinds, delivery.Event); ok {
		t.Errorf("node hop has an event row with no event series: %+v", node.Kinds)
	}
	l, ok := kindOf(node.Kinds, delivery.Log)
	if !ok || l.Newest.In != 0 || l.Newest.Loss != nil || l.LastHour.Loss != nil {
		t.Errorf("node log = %+v (ok %v), want a row with nothing in and no ratio", l, ok)
	}
	e, ok := endOf(p.EndToEnd, delivery.Event)
	if !ok || e.LastHour == nil || !near(*e.LastHour, 0.5) {
		t.Errorf("event end to end = %+v, want the ingest hop's 0.5 alone", e)
	}
	if lg, ok := endOf(p.EndToEnd, delivery.Log); ok && lg.LastHour != nil {
		t.Errorf("log end to end = %v, want absent: nothing went in anywhere", *lg.LastHour)
	}
	if _, ok := endOf(p.EndToEnd, delivery.Property); ok {
		t.Error("property end to end present with no property series on any hop")
	}
	if c := node.ByComponent[0]; len(c.Kinds) != 2 {
		t.Errorf("edge-alpha kinds = %+v, want metric and log only", c.Kinds)
	}
}

// TestPipelineIgnoresAnInstanceNoHopSelects: a series whose instance is no
// hop's form on its component's class (an earlier hop's, or a stray) is not a
// row: the hop's instance form is what selects it.
func TestPipelineIgnoresAnInstanceNoHopSelects(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	samples := pipelineWindow("n1", "edge-alpha", PlatformClassNode, "widget", 10, 1, 10, 1, at)
	p := composePipeline(PlatformPipelineHops, samples)
	for _, h := range p.Hops {
		if len(h.Kinds) != 0 || len(h.ByComponent) != 0 {
			t.Errorf("hop %s = %+v, want no rows for an instance no hop selects", h.Hop.ID, h)
		}
	}
}

// TestNodeLossComposesTheNodesOwnHops: a node's Loss is the end to end, over
// the last hour, of the hops its own component records, all kinds summed per
// hop; a node with no samples on any node-recorded hop has none.
func TestNodeLossComposesTheNodesOwnHops(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	var samples []pipelineSample
	samples = append(samples, pipelineWindow("n1", "edge-alpha", PlatformClassNode, "metric", 3, 3, 30, 27, at)...)
	samples = append(samples, pipelineWindow("n1", "edge-alpha", PlatformClassNode, "log", 0, 0, 70, 70, at)...)
	samples = append(samples, pipelineWindow("s1", "lab-server", PlatformClassServer, "metric", 9, 0, 90, 0, at)...)
	p := composePipeline(PlatformPipelineHops, samples)

	if l := nodeLoss(p, "n1"); l == nil || !near(*l, 0.03) {
		t.Errorf("edge-alpha loss = %v, want 3 lost of 100 over the hour, 0.03; the server's ingest is not the node's", lossText(l))
	}
	if l := nodeLoss(p, "n2"); l != nil {
		t.Errorf("a node with no samples reads %v, want absent", *l)
	}
	hops := nodeLossHops()
	if len(hops) != 1 || hops[0] != delivery.Node {
		t.Errorf("node loss hops = %v, want the node hop alone", hops)
	}
}

func endOf(ends []PlatformPipelineEnd, kind delivery.Kind) (PlatformPipelineEnd, bool) {
	for _, e := range ends {
		if e.Kind == kind {
			return e, true
		}
	}
	return PlatformPipelineEnd{}, false
}

func lossText(f *float64) any {
	if f == nil {
		return "absent"
	}
	return *f
}

// TestPipelineNewestLeavesOutAWindowPastTheHorizon: a node that stopped
// reporting keeps its last window as its newest sample forever. That window
// is not the hop's newest: the hop's newest and the end to end's sum only the
// windows still current, while the stopped node's last hour still counts and
// its row still reads, with no newest figure of its own.
func TestPipelineNewestLeavesOutAWindowPastTheHorizon(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	var samples []pipelineSample
	samples = append(samples, pipelineWindow("n1", "edge-alpha", PlatformClassNode, "metric", 10, 10, 30, 30, at)...)
	gone := pipelineWindow("n2", "edge-bravo", PlatformClassNode, "metric", 10, 0, 10, 0, at.Add(-30*time.Minute))
	for i := range gone {
		gone[i].Stale = true
	}
	samples = append(samples, gone...)
	p := composePipeline(PlatformPipelineHops, samples)

	node := hopOf(t, p, delivery.Node)
	m, ok := kindOf(node.Kinds, delivery.Metric)
	if !ok {
		t.Fatalf("node hop kinds = %+v, want a metric row", node.Kinds)
	}
	if m.Newest.In != 10 || m.Newest.Out != 10 || m.Newest.Loss == nil || *m.Newest.Loss != 0 {
		t.Errorf("node metric newest = %+v (loss %v), want edge-alpha's current 10 of 10, 0", m.Newest, lossText(m.Newest.Loss))
	}
	if m.LastHour.In != 40 || m.LastHour.Out != 30 {
		t.Errorf("node metric last hour = %+v, want both nodes' hour, 40 in, 30 out", m.LastHour)
	}
	if e, ok := endOf(p.EndToEnd, delivery.Metric); !ok || e.Newest == nil || *e.Newest != 0 {
		t.Errorf("metric end to end newest = %+v, want 0: the stopped node's last window is not current", e)
	}
	if len(node.ByComponent) != 2 {
		t.Fatalf("by component = %+v, want both nodes", node.ByComponent)
	}
	bravo := node.ByComponent[1]
	bm, _ := kindOf(bravo.Kinds, delivery.Metric)
	if bravo.Name != "edge-bravo" || bm.Newest.In != 0 || bm.Newest.Loss != nil || bm.LastHour.In != 10 {
		t.Errorf("edge-bravo = %+v, want no newest figure and its hour's 10 in", bravo)
	}
	if bm.NewestAt == nil || !bm.NewestAt.Equal(at.Add(-30*time.Minute)) {
		t.Errorf("edge-bravo newest at = %v, want when it last wrote", bm.NewestAt)
	}
}
