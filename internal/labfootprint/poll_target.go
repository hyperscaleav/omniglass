package labfootprint

import (
	"fmt"
	"strings"
)

// pollTargetsQuery lists every enabled poll task's endpoint with a component,
// by the node that holds it: the node's name, the component, the endpoint's
// transport, and the task's schedule. A driver poll's schedule is its baked
// function's every ("0s" when it declares none, so it runs each tick); any
// other spec is a reachability probe (empty), run at the node's probe
// interval. The driver test is driver.IsBaked's, and the join the node's
// worklist read.
const pollTargetsQuery = `select n.name, i.component, i.transport,
		case when coalesce(t.spec->>'driver', '') <> '' then coalesce(t.spec->'schedule'->>'every', '0s') else '' end
	from task t
	join endpoint i on i.id = t.endpoint_id
	join node n on n.principal_id = i.node_name
	where t.enabled and t.mode = 'poll' and i.component is not null
	order by 1, 2, 3`

// parsePollTargets reads pollTargetsQuery's rows into each node's targets,
// each at the interval its node actually runs it. A node with no cadence
// (not one of the lab's node containers) keeps the node's defaults.
func parsePollTargets(out string, cadence map[string]NodeCadence) (map[string][]PollTarget, error) {
	targets := map[string][]PollTarget{}
	for line := range strings.Lines(out) {
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) != 4 || f[0] == "" || f[1] == "" {
			return nil, fmt.Errorf("poll targets: want node|component|transport|every, got %q", line)
		}
		c, ok := cadence[f[0]]
		if !ok {
			c = ParseNodeCadence(nil)
		}
		targets[f[0]] = append(targets[f[0]], PollTarget{ComponentID: f[1], Transport: f[2], Interval: c.Interval(f[3])})
	}
	return targets, nil
}
