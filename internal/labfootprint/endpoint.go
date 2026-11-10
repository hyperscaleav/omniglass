package labfootprint

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
	"github.com/hyperscaleav/omniglass/internal/node"
)

// The closed loop's arithmetic is internal/closedloop's; this file is the
// harness's half: each node's cadence, read off its container, and the
// report's lines.

// ParseNodeCadence reads --heartbeat and --probe-every off a node's command;
// a flag missing or unreadable keeps the node's default.
func ParseNodeCadence(cmd []string) closedloop.NodeCadence {
	c := closedloop.NodeCadence{Heartbeat: node.DefaultHeartbeat}
	flag := func(i int, name string) (string, bool) {
		if v, ok := strings.CutPrefix(cmd[i], name+"="); ok {
			return v, true
		}
		if cmd[i] == name && i+1 < len(cmd) {
			return cmd[i+1], true
		}
		return "", false
	}
	for i := range cmd {
		if v, ok := flag(i, "--heartbeat"); ok {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				c.Heartbeat = d
			}
		}
		if v, ok := flag(i, "--probe-every"); ok {
			if d, err := time.ParseDuration(v); err == nil && d >= 0 {
				c.ProbeEvery = d
			}
		}
	}
	return c
}

// endpointTable renders the closed loop's lines.
func endpointTable(r closedloop.EndpointReport, b *strings.Builder) {
	line := func(name, value string) { fmt.Fprintf(b, "  %-28s %s\n", name, value) }
	b.WriteString("\nendpoints: what each simulated endpoint observed, reconciled with what its node sent\n")
	for _, p := range r.Protocols {
		line(p.Protocol+" received", fmt.Sprintf("%s   (%s poll targets; accepted %s, answered %s, refused %s, error %s, dropped %s, withheld %s, streams %s)",
			count(float64(p.Received)), count(float64(p.PollTargets)), count(float64(p.Accepted)), count(float64(p.Answered)), count(float64(p.Refused)),
			count(float64(p.Error)), count(float64(p.Dropped)), count(float64(p.Withheld)), count(float64(p.StreamsOpened))))
		if p.Latency.N > 0 {
			line(p.Protocol+" service time", fmt.Sprintf("p50 %.2f ms, p95 %.2f ms, p99 %.2f ms   (%s settled)",
				p.Latency.P50, p.Latency.P95, p.Latency.P99, count(float64(p.Latency.N))))
		}
		line(p.Protocol+" overlap-ratio", fmt.Sprintf("%.3f   (%s requests found their face busy)", p.OverlapRatio, count(float64(p.Overlapped))))
		var highs []string
		for _, n := range sortedKeys(p.InFlightHigh) {
			highs = append(highs, fmt.Sprintf("%s %d", n, p.InFlightHigh[n]))
		}
		line(p.Protocol+" in flight high", strings.Join(highs, ", "))
	}
	gauge := func(v *int64) string {
		if v == nil {
			return "not wired yet"
		}
		return fmt.Sprint(*v)
	}
	for _, n := range r.Nodes {
		var flags string
		if n.Impaired {
			flags += ", impaired"
		}
		if n.Restarted {
			flags += ", restarted"
		}
		line(n.Node+" arrival high", fmt.Sprintf("%d   (dispatch queue depth %s, busy workers %s%s)",
			n.ArrivalHigh, gauge(n.DispatchQueueDepth), gauge(n.BusyWorkers), flags))
	}
	for _, g := range r.GapTotals {
		v := fmt.Sprintf("%s lost", count(float64(g.Loss)))
		if g.Impaired > 0 {
			v += fmt.Sprintf(", %s impaired", count(float64(g.Impaired)))
		}
		if g.AnsweredLate > 0 {
			v += fmt.Sprintf(", %s answered late", count(float64(g.AnsweredLate)))
		}
		if g.Masked > 0 {
			v += fmt.Sprintf("   (masked %s: %s)", count(float64(g.Masked)), g.MaskedBy)
		}
		line(g.Gap, v)
	}
	// Each node's entries past a masking term alone, so a loss names where.
	for _, g := range r.Gaps {
		if g.Count == 0 && g.Reading == closedloop.ReadingLoss {
			continue
		}
		what := g.Protocol
		if g.ComponentID != "" {
			what += " " + g.ComponentID
		}
		line("  "+g.Node+" "+g.Gap, strings.TrimSpace(fmt.Sprintf("%s %d %s", what, g.Count, g.Reading)))
	}
	if len(r.WorstFaces) > 0 {
		b.WriteString("  worst faces since the sim started (not windowed):\n")
		for _, f := range r.WorstFaces {
			fmt.Fprintf(b, "    %s %s %s %s: %d of %d overlapped\n", f.Node, f.Component, f.Face, f.Protocol, f.Overlapped, f.Received)
		}
	}
}

func sortedKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
