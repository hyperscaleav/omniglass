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
// and the gate fails any node above 0.

// WorklistQuery reads each node's worst worklist pull loss reported in
// (from, to], one `node|ratio` a line.
func WorklistQuery(from, to time.Time) string {
	return fmt.Sprintf(`select concat_ws('|', n.name, max(m.value))
		from metric m
		join metric_series s on s.id = m.series_id join metric_type mt on mt.id = s.metric_type_id
		join node n on n.platform_component_id = s.component_id
		where mt.name = '%s'
			and m.ts > to_timestamp(%s) and m.ts <= to_timestamp(%s)
		group by n.name order by n.name`,
		writerclass.WorklistPullLossRatio, epoch(from), epoch(to))
}

// ParseWorklist reads what WorklistQuery writes, a ratio a node.
func ParseWorklist(out string) (map[string]float64, error) {
	loss := map[string]float64{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) != 2 {
			continue
		}
		f, err := strconv.ParseFloat(parts[1], 64)
		if err != nil || f < 0 || f > 1 {
			return nil, fmt.Errorf("read a worklist pull loss %q: not a ratio", line)
		}
		loss[parts[0]] = f
	}
	return loss, nil
}
