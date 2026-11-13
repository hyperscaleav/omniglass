package labfootprint

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The publish hop's read (#1746): the server's push lane publisher counts,
// every report window, the calls that reached its turn and the calls whose
// wait ran out (answered 503 before minting anything), on the server's own
// platform component. The read sums them over the measured window. Its in
// and out are calls, not items: a refused call minted nothing, so it is on
// no ledger and is refused here, never missing on the ingest row.

// publishLabel is what the publish row counts, and why its refusals are no
// ingest row's missing.
const publishLabel = "counts pushes, not items: refused-turn is a push answered 503 when its wait for the turn ran out; it minted nothing, so it is never missing at ingest"

// PublishCount is the push lane's turn over the window: the calls that
// reached it and the calls it refused.
type PublishCount struct {
	Calls   uint64
	Refused uint64
}

// PublishQuery sums the server's push lane turn counts reported in
// (from, to], one `calls|refused` line, 0|0 when nothing pushed.
func PublishQuery(from, to time.Time) string {
	return fmt.Sprintf(`select concat_ws('|',
			coalesce(sum(m.value) filter (where mt.name = '%[1]s'), 0), coalesce(sum(m.value) filter (where mt.name = '%[2]s'), 0))
		from metric m
		join metric_series s on s.id = m.series_id join metric_type mt on mt.id = s.metric_type_id
		where mt.name in ('%[1]s', '%[2]s')
			and m.ts > to_timestamp(%[3]s) and m.ts <= to_timestamp(%[4]s)`,
		writerclass.PushLanePublishCount, writerclass.PushLaneRefusalCount, epoch(from), epoch(to))
}

// ParsePublish reads what PublishQuery writes. Refusals past the calls are
// an error: every refusal is a call.
func ParsePublish(out string) (PublishCount, error) {
	line := strings.TrimSpace(out)
	parts := strings.Split(line, "|")
	if len(parts) != 2 {
		return PublishCount{}, fmt.Errorf("read the publish counts %q: not calls|refused", line)
	}
	var n [2]uint64
	for i, v := range parts {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 {
			return PublishCount{}, fmt.Errorf("read the publish counts %q: not a count", line)
		}
		n[i] = uint64(math.Round(f))
	}
	if n[1] > n[0] {
		return PublishCount{}, fmt.Errorf("read the publish counts %q: more refusals than calls", line)
	}
	return PublishCount{Calls: n[0], Refused: n[1]}, nil
}

// PublishRowOf is the push lane's publish row: its calls in, the calls that
// took the turn out, and the refused calls the loss. No call, no row.
func PublishRowOf(c PublishCount) []DeliveryRow {
	if c.Calls == 0 {
		return nil
	}
	taken := c.Calls - min(c.Refused, c.Calls)
	row := DeliveryRow{Lane: LanePush, Hop: delivery.Publish, In: c.Calls, Out: taken,
		Outcomes: map[delivery.Outcome]uint64{delivery.TurnTaken: taken, delivery.RefusedTurn: c.Calls - taken}}
	if l, ok := delivery.Loss(row.In, row.Out); ok {
		row.Loss = &l
	}
	return []DeliveryRow{row}
}

// WithPublish is rows with the publish row ahead of the push lane's rows, as
// a push crosses the turn before it is minted and ingested.
func WithPublish(publish, rows []DeliveryRow) []DeliveryRow {
	if len(publish) == 0 {
		return rows
	}
	i := len(rows)
	for j, r := range rows {
		if r.Lane == LanePush {
			i = j
			break
		}
	}
	out := append(append([]DeliveryRow{}, rows[:i]...), publish...)
	return append(out, rows[i:]...)
}
