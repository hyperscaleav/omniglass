package labfootprint

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The schedule hop's read (#1693): each node's dispatcher counts, every
// report window, the runs that fell due and the runs it made, per kind the
// run produces, on its own platform component under the instance
// schedule/{kind}. The read sums them over the measured window. Its in and
// out are runs, not items: a run is one due whatever it emits.

// ScheduleCount is one kind's schedule hop over the window: the runs due and
// the runs made, summed across the nodes.
type ScheduleCount struct {
	Kind delivery.Kind
	Due  uint64
	Made uint64
}

// ScheduleQuery sums every node's schedule hop counts reported in (from, to],
// one `instance|due|made` a line.
func ScheduleQuery(from, to time.Time) string {
	return fmt.Sprintf(`select concat_ws('|', s.instance,
			sum(m.value) filter (where mt.name = '%[1]s'), sum(m.value) filter (where mt.name = '%[2]s'))
		from metric m
		join metric_series s on s.id = m.series_id join metric_type mt on mt.id = s.metric_type_id
		where mt.name in ('%[1]s', '%[2]s') and s.instance like 'schedule/%%'
			and s.component_id in (select platform_component_id from node)
			and m.ts > to_timestamp(%[3]s) and m.ts <= to_timestamp(%[4]s)
		group by s.instance order by s.instance`,
		writerclass.DeliveryInCount, writerclass.DeliveryOutCount, epoch(from), epoch(to))
}

// ParseSchedule reads what ScheduleQuery writes, in kind order, keeping the
// schedule hop's instances alone.
func ParseSchedule(out string) ([]ScheduleCount, error) {
	byKind := map[delivery.Kind]ScheduleCount{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) != 3 {
			continue
		}
		h, k, ok := delivery.ParseHopInstance(parts[0])
		if !ok || h != delivery.Schedule {
			continue
		}
		var n [2]uint64
		for i, v := range parts[1:] {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f < 0 {
				return nil, fmt.Errorf("read a schedule count %q: not a count", line)
			}
			n[i] = uint64(math.Round(f))
		}
		c := byKind[k]
		c.Kind, c.Due, c.Made = k, c.Due+n[0], c.Made+n[1]
		byKind[k] = c
	}
	var cs []ScheduleCount
	for _, k := range delivery.Kinds {
		if c, ok := byKind[k]; ok {
			cs = append(cs, c)
		}
	}
	return cs, nil
}

// ScheduleRowsOf is one schedule row per kind with a due in the window, in
// kind order: its dues in, its runs made out, and its skipped dues the loss.
// A kind nothing fell due for has no row.
func ScheduleRowsOf(counts []ScheduleCount) []DeliveryRow {
	var rows []DeliveryRow
	for _, k := range delivery.Kinds {
		i := slices.IndexFunc(counts, func(c ScheduleCount) bool { return c.Kind == k })
		if i < 0 || counts[i].Due == 0 {
			continue
		}
		made := min(counts[i].Made, counts[i].Due)
		row := DeliveryRow{Hop: delivery.Schedule, Kind: k, In: counts[i].Due, Out: made,
			Outcomes: map[delivery.Outcome]uint64{delivery.Made: made, delivery.Skipped: counts[i].Due - made}}
		if made < row.In {
			row.Cause = delivery.Skipped
		}
		if l, ok := delivery.Loss(row.In, row.Out); ok {
			row.Loss = &l
		}
		rows = append(rows, row)
	}
	return rows
}

// WithSchedule is rows with each kind's schedule row ahead of that kind's
// other hops, as an item crosses them: kind order, then hop order.
func WithSchedule(schedule, rows []DeliveryRow) []DeliveryRow {
	if len(schedule) == 0 {
		return rows
	}
	var out []DeliveryRow
	for _, k := range delivery.Kinds {
		for _, set := range [][]DeliveryRow{schedule, rows} {
			for _, r := range set {
				if r.Kind == k {
					out = append(out, r)
				}
			}
		}
	}
	return out
}
