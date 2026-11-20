package api

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/tail"
)

func validBinding() viewBindingBody {
	return viewBindingBody{Primitive: "metric", Kind: "component", Refs: []string{"room-1.codec"}, Keys: []string{"icmp-rtt-avg"}, WindowSeconds: 3600}
}

// A binding the contract admits is shaped for the Gateway as given.
func TestToViewBindingShapesAValidBinding(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 1, 1, 10, 0, 0, 0, time.FixedZone("x", 3600))
	to := from.Add(time.Hour)
	b, err := toViewBinding(viewBindingBody{Primitive: "log", Kind: "component", Filter: &viewFilterBody{System: "room-1"},
		From: &from, To: &to, Limit: 50, Page: "1.2", Severity: "warning", Source: "syslog"})
	if err != nil {
		t.Fatalf("a valid log binding: %v", err)
	}
	if b.Primitive != tail.LaneLog || b.Filter.System != "room-1" || !b.From.Equal(from) || b.From.Location() != time.UTC ||
		!b.To.Equal(to) || b.Limit != 50 || b.Page != "1.2" || b.Logs.Severity != "warning" || b.Logs.Source != "syslog" {
		t.Fatalf("shaped %+v", b)
	}
	m, err := toViewBinding(viewBindingBody{Primitive: "metric", Kind: "system", Keys: []string{"k"}, WindowSeconds: 600, StepSeconds: 60})
	if err != nil {
		t.Fatalf("a kind selector with a step: %v", err)
	}
	if m.Window != 10*time.Minute || m.Step != time.Minute || len(m.Refs) != 0 {
		t.Fatalf("shaped %+v", m)
	}
	if _, err := toViewBinding(viewBindingBody{Primitive: "event", Kind: "component", WindowSeconds: 60}); err != nil {
		t.Fatalf("an event binding reads every type when it names none: %v", err)
	}
}

// Every way a binding breaks the contract is refused with the reason, never
// read as something else.
func TestToViewBindingRefusesOffContract(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	before := from.Add(-time.Minute)
	tooLate := from.Add(3 * 365 * 24 * time.Hour)
	cases := []struct {
		name string
		edit func(*viewBindingBody)
		want string
	}{
		{"unknown primitive", func(b *viewBindingBody) { b.Primitive = "trace" }, "primitive"},
		{"unknown kind", func(b *viewBindingBody) { b.Kind = "location" }, "kind"},
		{"refs and filter", func(b *viewBindingBody) { b.Filter = &viewFilterBody{System: "s"} }, "not both"},
		{"empty ref", func(b *viewBindingBody) { b.Refs = []string{" "} }, "ref is empty"},
		{"system_type on components", func(b *viewBindingBody) { b.Refs = nil; b.Filter = &viewFilterBody{SystemType: "room"} }, "system_type"},
		{"product on systems", func(b *viewBindingBody) { b.Kind = "system"; b.Refs = nil; b.Filter = &viewFilterBody{Product: "p"} }, "not systems"},
		{"metric without keys", func(b *viewBindingBody) { b.Keys = nil }, "names its keys"},
		{"empty key", func(b *viewBindingBody) { b.Keys = []string{""} }, "key is empty"},
		{"page on a series", func(b *viewBindingBody) { b.Page = "1.1" }, "limit and page"},
		{"step on events", func(b *viewBindingBody) { b.Primitive = "event"; b.StepSeconds = 60 }, "step_seconds"},
		{"log on a system", func(b *viewBindingBody) { b.Primitive = "log"; b.Kind = "system"; b.Keys = nil }, "belong to components"},
		{"log with keys", func(b *viewBindingBody) { b.Primitive = "log" }, "no keys"},
		{"severity on a metric", func(b *viewBindingBody) { b.Severity = "error" }, "narrow a log"},
		{"unknown severity", func(b *viewBindingBody) { b.Primitive = "log"; b.Keys = nil; b.Severity = "loud" }, "severity"},
		{"no range", func(b *viewBindingBody) { b.WindowSeconds = 0 }, "has a range"},
		{"from and window", func(b *viewBindingBody) { b.From = &from }, "not both"},
		{"to without from", func(b *viewBindingBody) { b.WindowSeconds = 0; b.To = &from }, "to needs from"},
		{"to before from", func(b *viewBindingBody) { b.WindowSeconds = 0; b.From = &from; b.To = &before }, "not after"},
		{"range past two years", func(b *viewBindingBody) { b.WindowSeconds = 0; b.From = &from; b.To = &tooLate }, "two years"},
		{"window past two years", func(b *viewBindingBody) { b.WindowSeconds = 3 * 365 * 86400 }, "two years"},
	}
	for _, c := range cases {
		body := validBinding()
		c.edit(&body)
		_, err := toViewBinding(body)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want a refusal naming %q", c.name, err, c.want)
		}
	}
}

// The tail matcher keeps a row only for the binding it belongs to: its
// primitive and kind, an entity it resolved to, a key it names, after its
// cursor, inside its range and log filter.
func TestViewMatchKeepsOnlyTheBindingsRows(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ents := []storage.ViewEntity{{ID: "c1", Kind: "component"}}
	metric := newViewMatch(storage.ViewBinding{Primitive: tail.LaneMetric, Kind: "component", Keys: []string{"rtt"}, Window: time.Hour}, ents, 10)
	row := tail.Row{Lane: tail.LaneMetric, OwnerKind: "component", OwnerID: "c1", Key: "rtt", TS: now.Add(-time.Minute)}
	if !metric.matches(11, row, now) {
		t.Fatal("the binding's own row did not match")
	}
	ahead := row
	ahead.TS = now.Add(time.Minute)
	if !metric.matches(11, ahead, now) {
		t.Fatal("an open range has no upper bound: an ahead-of-clock row must match, as history keeps it")
	}
	for name, r := range map[string]tail.Row{
		"another entity":   {Lane: tail.LaneMetric, OwnerKind: "component", OwnerID: "c2", Key: "rtt", TS: row.TS},
		"another kind":     {Lane: tail.LaneMetric, OwnerKind: "system", OwnerID: "c1", Key: "rtt", TS: row.TS},
		"another key":      {Lane: tail.LaneMetric, OwnerKind: "component", OwnerID: "c1", Key: "cpu", TS: row.TS},
		"another lane":     {Lane: tail.LaneProperty, OwnerKind: "component", OwnerID: "c1", Key: "rtt", TS: row.TS},
		"before the range": {Lane: tail.LaneMetric, OwnerKind: "component", OwnerID: "c1", Key: "rtt", TS: now.Add(-2 * time.Hour)},
	} {
		if metric.matches(11, r, now) {
			t.Errorf("%s matched", name)
		}
	}
	if metric.matches(10, row, now) {
		t.Error("a row at the binding's cursor matched: it is already in the history")
	}

	closed := newViewMatch(storage.ViewBinding{Primitive: tail.LaneMetric, Kind: "component", Keys: []string{"rtt"},
		From: now.Add(-2 * time.Hour), To: now.Add(-30 * time.Minute)}, ents, 0)
	if closed.matches(1, row, now) {
		t.Error("a row after an absolute range's end matched")
	}

	logs := newViewMatch(storage.ViewBinding{Primitive: tail.LaneLog, Kind: "component", Window: time.Hour,
		Logs: storage.LogFilter{Severity: "warning", Source: "syslog"}}, ents, 0)
	line := tail.Row{Lane: tail.LaneLog, OwnerKind: "component", OwnerID: "c1", Severity: "error", Channel: "syslog",
		TS: now.Add(-3 * time.Hour), ReceivedAt: now.Add(-time.Minute)}
	if !logs.matches(1, line, now) {
		t.Error("a severe enough line on the source, arriving inside the range, did not match (a log's axis is its arrival)")
	}
	quiet := line
	quiet.Severity = "info"
	other := line
	other.Channel = "gelf"
	if logs.matches(1, quiet, now) || logs.matches(1, other, now) {
		t.Error("a line below the severity or on another source matched")
	}
}

// One rows event folds every binding's matching rows across the commits read,
// in commit order, and leaves out a binding nothing matched.
func TestViewTailRowsFoldsEachBinding(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ents := []storage.ViewEntity{{ID: "c1"}}
	matches := []viewMatch{
		newViewMatch(storage.ViewBinding{Primitive: tail.LaneMetric, Kind: "component", Keys: []string{"rtt"}, Window: time.Hour}, ents, 0),
		newViewMatch(storage.ViewBinding{Primitive: tail.LaneEvent, Kind: "component", Window: time.Hour}, nil, 0),
		newViewMatch(storage.ViewBinding{Primitive: tail.LaneProperty, Kind: "component", Keys: []string{"power"}, Window: time.Hour}, ents, 1),
	}
	commits := []tail.Commit{
		{Rows: []tail.Row{
			{Lane: tail.LaneMetric, OwnerKind: "component", OwnerID: "c1", Key: "rtt", Instance: "icmp", TS: now, Number: 1},
			{Lane: tail.LaneProperty, OwnerKind: "component", OwnerID: "c1", Key: "power", TS: now, Text: "on"},
		}},
		{Rows: []tail.Row{
			{Lane: tail.LaneMetric, OwnerKind: "component", OwnerID: "c1", Key: "rtt", Instance: "icmp", TS: now, Number: 2},
			{Lane: tail.LaneProperty, OwnerKind: "component", OwnerID: "c1", Key: "power", TS: now, Text: "off"},
		}},
	}
	out := viewTailRows(matches, commits, []uint64{1, 2}, now)
	if len(out) != 2 || out[0].Binding != 0 || out[1].Binding != 2 {
		t.Fatalf("bindings with rows: %+v", out)
	}
	if m := out[0].Metric; len(m) != 2 || m[0].Value != 1 || m[1].Value != 2 || m[0].Instance != "icmp" || m[0].Entity != "c1" {
		t.Fatalf("metric rows: %+v", m)
	}
	if p := out[1].Property; len(p) != 1 || p[0].Value != "off" {
		t.Fatalf("a binding resumed after the first commit got: %+v", p)
	}
}
