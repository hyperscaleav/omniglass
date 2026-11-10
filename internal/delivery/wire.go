package delivery

import (
	"slices"

	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Proto is the stamp as a batch carries it, its kinds in Kinds order.
func (s Stamp) Proto() *ogv1.DeliveryLedger {
	l := &ogv1.DeliveryLedger{
		Boot:     s.Boot,
		Seq:      s.Seq,
		Minted:   timestamppb.New(s.Minted),
		Resolved: s.Resolved,
	}
	for _, k := range Kinds {
		if c, ok := s.Kinds[k]; ok {
			l.Kinds = append(l.Kinds, &ogv1.KindCount{Kind: string(k), Count: c.Count, Before: c.Before})
		}
	}
	for _, d := range s.Loss.Dropped {
		run := &ogv1.DroppedRun{Cause: string(d.Cause), FromSeq: d.From, ToSeq: d.To}
		for _, k := range kindsOf(d.Items) {
			run.Kinds = append(run.Kinds, &ogv1.KindCount{Kind: string(k), Count: d.Items[k]})
		}
		l.Dropped = append(l.Dropped, run)
	}
	for _, c := range causesOf(s.Loss.Lost) {
		for _, k := range kindsOf(s.Loss.Lost[c]) {
			l.Lost = append(l.Lost, &ogv1.LostCount{Cause: string(c), Kind: string(k), Count: s.Loss.Lost[c][k]})
		}
	}
	return l
}

// kindsOf is the kinds counted in m, in Kinds order and then any other kind
// by name, so a stamp encodes the same bytes every time.
func kindsOf(m map[Kind]uint64) []Kind {
	var out []Kind
	for _, k := range Kinds {
		if m[k] > 0 {
			out = append(out, k)
		}
	}
	var rest []Kind
	for k, n := range m {
		if n > 0 && !slices.Contains(Kinds, k) {
			rest = append(rest, k)
		}
	}
	slices.Sort(rest)
	return append(out, rest...)
}

// causesOf is the causes in m, in Outcomes order and then any other by name.
func causesOf(m map[Outcome]map[Kind]uint64) []Outcome {
	var out []Outcome
	for _, c := range Outcomes {
		if _, ok := m[c]; ok {
			out = append(out, c)
		}
	}
	var rest []Outcome
	for c := range m {
		if !slices.Contains(Outcomes, c) {
			rest = append(rest, c)
		}
	}
	slices.Sort(rest)
	return append(out, rest...)
}

// FromProto is the stamp a batch's ledger carries. A kind the package does
// not know is kept under its own name, so a newer node's count is not lost.
func FromProto(l *ogv1.DeliveryLedger) Stamp {
	s := Stamp{
		Boot:     l.GetBoot(),
		Seq:      l.GetSeq(),
		Resolved: l.GetResolved(),
		Kinds:    Declared{},
	}
	if l.GetMinted() != nil {
		s.Minted = l.GetMinted().AsTime()
	}
	for _, c := range l.GetKinds() {
		s.Kinds[Kind(c.GetKind())] = Count{Count: c.GetCount(), Before: c.GetBefore()}
	}
	for _, r := range l.GetDropped() {
		d := Drop{Cause: Outcome(r.GetCause()), From: r.GetFromSeq(), To: r.GetToSeq(), Items: map[Kind]uint64{}}
		for _, c := range r.GetKinds() {
			d.Items[Kind(c.GetKind())] += c.GetCount()
		}
		s.Loss.Dropped = append(s.Loss.Dropped, d)
	}
	for _, c := range l.GetLost() {
		if s.Loss.Lost == nil {
			s.Loss.Lost = map[Outcome]map[Kind]uint64{}
		}
		cause := Outcome(c.GetCause())
		if s.Loss.Lost[cause] == nil {
			s.Loss.Lost[cause] = map[Kind]uint64{}
		}
		s.Loss.Lost[cause][Kind(c.GetKind())] += c.GetCount()
	}
	return s
}

// GroupItems adds a task group's items per kind to into: its samples by
// lane, its task log lines as logs.
func GroupItems(into map[Kind]int, g *ogv1.TaskGroup) {
	into[Metric] += len(g.GetMetrics())
	into[Property] += len(g.GetProperties())
	into[Event] += len(g.GetEvents())
	into[Log] += len(g.GetTaskLogs())
}

// BatchItems is a batch's items per kind, what its ledger declares and what
// the server decodes: its node-self samples are metrics, its self-logs and
// task log lines logs, and each group counts as GroupItems does.
func BatchItems(ev *ogv1.TelemetryBatch) map[Kind]int {
	k := map[Kind]int{
		Metric:   len(ev.GetMetrics()) + len(ev.GetNodeSelf().GetMetrics()),
		Property: len(ev.GetProperties()),
		Event:    len(ev.GetEvents()),
		Log:      len(ev.GetLogs()) + len(ev.GetTaskLogs()),
	}
	for _, g := range ev.GetGroups() {
		GroupItems(k, g)
	}
	return k
}
