package storage

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The pipeline (#1646): the path a sample takes through Omniglass, hop by
// hop, read from the delivery keys each hop's platform component writes every
// report window (#1659). Every hop reads the same three keys the same way, so
// the read knows nothing about a hop beyond which component class records it
// and which instance form selects it; the ratios are composed here from the
// summed counts, never summed or averaged, and the end to end is #1641's
// survival composition (delivery.EndToEnd).

// The component classes a hop is recorded on: a node's platform component or
// a server's.
const (
	PlatformClassNode   = "node"
	PlatformClassServer = "server"
)

// pipelineKindToken is the place a hop's instance form holds the kind.
const pipelineKindToken = "{kind}"

// pipelineKeys is the three keys every hop reads, in, out and the ratio.
var pipelineKeys = []string{writerclass.DeliveryInCount, writerclass.DeliveryOutCount, writerclass.DeliveryLossRatio}

// PlatformPipelineHop is one hop as the pipeline declares it: its #1641 name,
// what it does in a sentence the page teaches with, the component class that
// records it, and the instance form that selects its series from the three
// keys ("{kind}" is the kind itself).
type PlatformPipelineHop struct {
	ID             delivery.Hop
	Label          string
	Description    string
	ComponentClass string
	InstanceForm   string
}

// Keys is the hop's three keys: every hop reads the same ones.
func (h PlatformPipelineHop) Keys() []string { return pipelineKeys }

// Instance is the series instance that carries kind at this hop.
func (h PlatformPipelineHop) Instance(kind delivery.Kind) string {
	return strings.ReplaceAll(h.InstanceForm, pipelineKindToken, string(kind))
}

// kindOf is the kind an instance carries at this hop, false when the hop's
// form does not select it.
func (h PlatformPipelineHop) kindOf(instance string) (delivery.Kind, bool) {
	for _, k := range delivery.Kinds {
		if h.Instance(k) == instance {
			return k, true
		}
	}
	return "", false
}

// PlatformPipelineHops is the pipeline's hops in the order an item crosses
// them: the one place the page's hop list is written. A hop is here once a
// producer records it. A node records the schedule hop at schedule/{kind}
// since #1693, and the page draws it with #1739; until then only one hop per
// class may use the bare kind (TestPlatformPipelineHopsAreSeeded).
var PlatformPipelineHops = []PlatformPipelineHop{
	{
		ID: delivery.Node, Label: "Node",
		Description:    "The node's spool, from a run's output to the stream. An item settles when the stream acks its batch (out), or when the spool drops it at a cap, the bus refuses it, it never encodes, or (a self-log line) the bounded log buffer drops it (lost).",
		ComponentClass: PlatformClassNode, InstanceForm: pipelineKindToken,
	},
	{
		ID: delivery.Ingest, Label: "Ingest",
		Description:    "The server's ingest, from the stream to the store. An item is out when it is stored, coalesced or had already landed, and lost when it is refused, orphaned, or its batch is terminated whole.",
		ComponentClass: PlatformClassServer, InstanceForm: pipelineKindToken,
	},
}

// nodeLossHops is the hops a node's own platform component records, the ones
// its Loss composes, in pipeline order.
func nodeLossHops() []delivery.Hop {
	var out []delivery.Hop
	for _, h := range PlatformPipelineHops {
		if h.ComponentClass == PlatformClassNode {
			out = append(out, h.ID)
		}
	}
	return out
}

// PlatformPipelineFigure is a span's in, out and loss: Loss is (in - out) / in
// over the summed counts, nil when nothing went in.
type PlatformPipelineFigure struct {
	In   uint64
	Out  uint64
	Loss *float64
}

// PlatformPipelineCell is one kind's figures (Kind "" for every kind summed)
// at a hop or on one component: the newest window, when it was written, and
// the last hour.
type PlatformPipelineCell struct {
	Kind     delivery.Kind
	Newest   PlatformPipelineFigure
	NewestAt *time.Time
	LastHour PlatformPipelineFigure
}

// PlatformPipelineComponent is one component's share of a hop.
type PlatformPipelineComponent struct {
	ComponentID string
	Name        string
	Kinds       []PlatformPipelineCell
	Total       PlatformPipelineCell
}

// PlatformPipelineHopFigures is one hop and what it carried: per kind across
// its components, every kind summed, and each component's split, in name order.
type PlatformPipelineHopFigures struct {
	Hop         PlatformPipelineHop
	Kinds       []PlatformPipelineCell
	Total       PlatformPipelineCell
	ByComponent []PlatformPipelineComponent
}

// PlatformPipelineEnd is the end to end loss of one kind (Kind "" for every
// kind) over the hops it crosses, the newest window's and the last hour's,
// nil where no hop has a ratio.
type PlatformPipelineEnd struct {
	Kind     delivery.Kind
	Newest   *float64
	LastHour *float64
}

// PlatformPipeline is the pipeline read: the three keys' catalog facts, the
// hops in order and the end to end per kind and over every kind.
type PlatformPipeline struct {
	Keys          []PlatformKey
	Hops          []PlatformPipelineHopFigures
	EndToEnd      []PlatformPipelineEnd
	EndToEndTotal PlatformPipelineEnd
}

// pipelineSample is one series as the pipeline reads it: its component and
// that component's class, its key and instance, its newest sample and the sum
// of its samples over the last hour. Stale marks a newest sample past the
// window horizon (platformWindowHorizon): a producer that stopped keeps its
// last window as its newest forever, and that window is not current.
type pipelineSample struct {
	ComponentID   string
	ComponentName string
	Class         string
	Key           string
	Instance      string
	Newest        float64
	NewestAt      time.Time
	HourSum       float64
	Stale         bool
}

// pipelineTally is a cell being summed: the counts, and the producer's own
// newest ratio when one component wrote it.
type pipelineTally struct {
	newestIn, newestOut, hourIn, hourOut uint64
	newestAt                             *time.Time
	newestLoss                           *float64
}

func (t *pipelineTally) add(o pipelineTally) {
	t.newestIn += o.newestIn
	t.newestOut += o.newestOut
	t.hourIn += o.hourIn
	t.hourOut += o.hourOut
	if o.newestAt != nil && (t.newestAt == nil || o.newestAt.After(*t.newestAt)) {
		at := *o.newestAt
		t.newestAt = &at
	}
}

func figure(in, out uint64) PlatformPipelineFigure {
	f := PlatformPipelineFigure{In: in, Out: out}
	if l, ok := delivery.Loss(in, out); ok {
		f.Loss = &l
	}
	return f
}

func (t pipelineTally) cell(kind delivery.Kind) PlatformPipelineCell {
	return PlatformPipelineCell{
		Kind: kind, NewestAt: t.newestAt,
		Newest: figure(t.newestIn, t.newestOut), LastHour: figure(t.hourIn, t.hourOut),
	}
}

// count reads a count sample as a whole number of items.
func count(v float64) uint64 {
	if v <= 0 || math.IsNaN(v) {
		return 0
	}
	return uint64(math.Round(v))
}

// composePipeline lays the samples over the hops: a series belongs to the
// hop whose class records its component and whose instance form selects its
// instance, and to no hop otherwise. A kind with no in series at a hop has no
// row there. A component's newest ratio is its own loss key as written, read
// only when something went in; every ratio across components or windows is
// composed from the summed counts. A stale newest window (pipelineSample.Stale)
// adds nothing to the newest figures; its last hour still counts.
func composePipeline(hops []PlatformPipelineHop, samples []pipelineSample) PlatformPipeline {
	type compKind struct {
		comp string
		kind delivery.Kind
	}
	type hopTallies struct {
		tallies map[compKind]*pipelineTally
		names   map[string]string
	}
	by := make([]hopTallies, len(hops))
	for i := range by {
		by[i] = hopTallies{tallies: map[compKind]*pipelineTally{}, names: map[string]string{}}
	}
	type lossRead struct {
		value float64
	}
	losses := make([]map[compKind]lossRead, len(hops))
	for i := range losses {
		losses[i] = map[compKind]lossRead{}
	}
	for _, s := range samples {
		for i, h := range hops {
			if h.ComponentClass != s.Class {
				continue
			}
			kind, ok := h.kindOf(s.Instance)
			if !ok {
				continue
			}
			key := compKind{s.ComponentID, kind}
			switch s.Key {
			case writerclass.DeliveryLossRatio:
				losses[i][key] = lossRead{s.Newest}
				continue
			case writerclass.DeliveryInCount, writerclass.DeliveryOutCount:
			default:
				continue
			}
			t := by[i].tallies[key]
			if t == nil {
				t = &pipelineTally{}
				by[i].tallies[key] = t
			}
			by[i].names[s.ComponentID] = s.ComponentName
			at := s.NewestAt
			newest := count(s.Newest)
			if s.Stale {
				newest = 0
			}
			if s.Key == writerclass.DeliveryInCount {
				t.newestIn, t.hourIn = newest, count(s.HourSum)
				t.newestAt = &at
			} else {
				t.newestOut, t.hourOut = newest, count(s.HourSum)
			}
		}
	}

	var out PlatformPipeline
	out.Hops = make([]PlatformPipelineHopFigures, 0, len(hops))
	for i, h := range hops {
		hf := PlatformPipelineHopFigures{Hop: h, Kinds: []PlatformPipelineCell{}, ByComponent: []PlatformPipelineComponent{}}
		comps := make([]string, 0, len(by[i].names))
		for id := range by[i].names {
			comps = append(comps, id)
		}
		slices.SortFunc(comps, func(a, b string) int {
			return cmp.Or(cmp.Compare(by[i].names[a], by[i].names[b]), cmp.Compare(a, b))
		})
		var hopTotal pipelineTally
		perKind := map[delivery.Kind]*pipelineTally{}
		for _, id := range comps {
			c := PlatformPipelineComponent{ComponentID: id, Name: by[i].names[id], Kinds: []PlatformPipelineCell{}}
			var compTotal pipelineTally
			for _, k := range delivery.Kinds {
				t, ok := by[i].tallies[compKind{id, k}]
				if !ok || t.newestAt == nil {
					continue
				}
				cell := t.cell(k)
				if l, ok := losses[i][compKind{id, k}]; ok && t.newestIn > 0 {
					v := l.value
					cell.Newest.Loss = &v
				}
				c.Kinds = append(c.Kinds, cell)
				compTotal.add(*t)
				if perKind[k] == nil {
					perKind[k] = &pipelineTally{}
				}
				perKind[k].add(*t)
			}
			if len(c.Kinds) == 0 {
				continue
			}
			c.Total = compTotal.cell("")
			hopTotal.add(compTotal)
			hf.ByComponent = append(hf.ByComponent, c)
		}
		for _, k := range delivery.Kinds {
			if t := perKind[k]; t != nil {
				hf.Kinds = append(hf.Kinds, t.cell(k))
			}
		}
		hf.Total = hopTotal.cell("")
		out.Hops = append(out.Hops, hf)
	}

	out.EndToEnd = []PlatformPipelineEnd{}
	for _, k := range delivery.Kinds {
		var cells []PlatformPipelineCell
		for _, hf := range out.Hops {
			for _, c := range hf.Kinds {
				if c.Kind == k {
					cells = append(cells, c)
				}
			}
		}
		if len(cells) > 0 {
			out.EndToEnd = append(out.EndToEnd, endToEnd(k, cells))
		}
	}
	totals := make([]PlatformPipelineCell, 0, len(out.Hops))
	for _, hf := range out.Hops {
		if len(hf.Kinds) > 0 {
			totals = append(totals, hf.Total)
		}
	}
	out.EndToEndTotal = endToEnd("", totals)
	return out
}

// endToEnd composes the cells' ratios hop by hop (delivery.EndToEnd), a cell
// with no ratio contributing nothing, nil when none has one.
func endToEnd(kind delivery.Kind, cells []PlatformPipelineCell) PlatformPipelineEnd {
	e := PlatformPipelineEnd{Kind: kind}
	e.Newest = composeRatios(cells, func(c PlatformPipelineCell) *float64 { return c.Newest.Loss })
	e.LastHour = composeRatios(cells, func(c PlatformPipelineCell) *float64 { return c.LastHour.Loss })
	return e
}

func composeRatios(cells []PlatformPipelineCell, pick func(PlatformPipelineCell) *float64) *float64 {
	var ls []float64
	for _, c := range cells {
		if l := pick(c); l != nil {
			ls = append(ls, *l)
		}
	}
	if len(ls) == 0 {
		return nil
	}
	if len(ls) == 1 {
		// One hop's end to end is its own ratio, read as written rather than
		// through 1 - (1 - l), which rounds it.
		return &ls[0]
	}
	v := delivery.EndToEnd(ls...)
	return &v
}

// nodeLoss is one node's Loss: the last hour's end to end over the hops its
// own platform component records, every kind summed per hop, nil when the
// component has a ratio on none of them.
func nodeLoss(p PlatformPipeline, componentID string) *float64 {
	var cells []PlatformPipelineCell
	for _, hf := range p.Hops {
		if hf.Hop.ComponentClass != PlatformClassNode {
			continue
		}
		for _, c := range hf.ByComponent {
			if c.ComponentID == componentID {
				cells = append(cells, c.Total)
			}
		}
	}
	return composeRatios(cells, func(c PlatformPipelineCell) *float64 { return c.LastHour.Loss })
}

// pipelineHour is the span the pipeline's last hour sums: an hour rounded up
// to whole report windows on the series read's grid.
const pipelineHour = time.Hour

// platformPipeline reads the pipeline in one statement over the series of the
// three keys on every node's and every server's platform component: each
// series' newest sample (metricNewestSQL, the read EffectiveMetrics makes) and
// the sum of its samples over the last hour, bucketed on the report window's
// grid as the series read buckets (metricReadSQL, so a folded day sums as its
// raw rows did). Per (component, key, instance) the series with the newest
// sample is read, across the provenances that report reality. $3, $4 and $5
// are the bucket grid's (series_buckets.go): the report window, the hour
// rounded up to whole windows, and the in-count key's retention; $6 is the
// window horizon past which a newest sample is stale (platformWindowHorizon),
// read on the database's clock as platformFigures reads it.
func (p *PG) platformPipeline(ctx context.Context) (*PlatformPipeline, error) {
	collection, err := p.CollectionSettings(ctx)
	if err != nil {
		return nil, err
	}
	s, err := p.platformSettings(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	_, days, err := p.bucketType(ctx, "metric_type", writerclass.DeliveryInCount, s.Retention.MetricDays)
	if err != nil {
		return nil, err
	}
	res := time.Duration(max(collection.ReportSeconds, 1)) * time.Second
	span := time.Duration(bucketCount(pipelineHour, res)) * res
	read := metricReadSQL("s.id", bucketFloorSQL, bucketHiSQL, "$2")
	rows, err := p.pool.Query(ctx, `
		with comps as (
			select n.platform_component_id as id, '`+PlatformClassNode+`' as class from node n
			where n.platform_component_id is not null
			union
			select ps.platform_component_id, '`+PlatformClassServer+`' from platform_server ps
		)
		select distinct on (s.component_id, mt.name, s.instance)
		       s.component_id::text, c.name, comps.class, mt.name, s.instance, n.value, n.ts,
		       coalesce((select sum(h.value) from (`+read+`) h), 0),
		       n.ts < now() - $6::interval
		from metric_series s
		join comps on comps.id = s.component_id
		join component c on c.id = s.component_id
		join metric_type mt on mt.id = s.metric_type_id
		cross join lateral (`+metricNewestSQL("s.id")+`) n
		where s.owner_kind = 'component'
		  and mt.namespace = 'og' and mt.name = any($1::text[])
		  and s.provenance in `+measuredProvenance+`
		order by s.component_id, mt.name, s.instance, n.ts desc, n.id desc`,
		pipelineKeys, s.Retention.MetricDays, res.Seconds(), span.Seconds(), days,
		platformWindowHorizon(collection.ReportSeconds))
	if err != nil {
		return nil, fmt.Errorf("storage: platform pipeline: %w", err)
	}
	defer rows.Close()
	var samples []pipelineSample
	for rows.Next() {
		var x pipelineSample
		if err := rows.Scan(&x.ComponentID, &x.ComponentName, &x.Class, &x.Key, &x.Instance, &x.Newest, &x.NewestAt, &x.HourSum, &x.Stale); err != nil {
			return nil, fmt.Errorf("storage: platform pipeline: %w", err)
		}
		samples = append(samples, x)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: platform pipeline: %w", err)
	}
	out := composePipeline(PlatformPipelineHops, samples)
	if out.Keys, err = p.platformKeys(ctx, pipelineKeys); err != nil {
		return nil, err
	}
	return &out, nil
}
