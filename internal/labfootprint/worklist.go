package labfootprint

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The worklist read (#1868): each node reports, every report window, the
// share of its worklist pulls the bus refused, on its own platform
// component. A refused pull leaves the node running its last worklist while
// every delivery hop reads clean, so the read takes each node's worst window
// and the gate fails any node above 0. The read starts from the node table,
// so a node that reported nothing in the window reads as unreported, never
// as absent.

// WorklistQuery reads each node's worst worklist pull loss reported in
// (from, to], one `node|ratio` a line, and `node|` for a node with no
// observed reading in the window.
func WorklistQuery(from, to time.Time) string {
	return fmt.Sprintf(`select n.name || '|' || coalesce(w.loss::text, '')
		from node n
		left join (
			select s.component_id, max(m.value) as loss
			from metric m
			join metric_series s on s.id = m.series_id join metric_type mt on mt.id = s.metric_type_id
			where mt.name = '%s'
				and s.provenance in ('observed', 'calculated')
				and m.ts > to_timestamp(%s) and m.ts <= to_timestamp(%s)
			group by s.component_id
		) w on w.component_id = n.platform_component_id
		order by n.name`,
		writerclass.WorklistPullLossRatio, epoch(from), epoch(to))
}

// ParseWorklist reads what WorklistQuery writes: a ratio a reporting node,
// and the nodes that reported nothing, in order.
func ParseWorklist(out string) (map[string]float64, []string, error) {
	loss := map[string]float64{}
	var unreported []string
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) != 2 || parts[0] == "" {
			continue
		}
		if parts[1] == "" {
			unreported = append(unreported, parts[0])
			continue
		}
		f, err := strconv.ParseFloat(parts[1], 64)
		if err != nil || f < 0 || f > 1 {
			return nil, nil, fmt.Errorf("read a worklist pull loss %q: not a ratio", line)
		}
		loss[parts[0]] = f
	}
	return loss, unreported, nil
}
