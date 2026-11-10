package delivery

import (
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
	return l
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
