package labfootprint

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab"
)

// sweepPingGroupRange is the lab's own range for its nodes, so the sweep's
// node runs the pinger as the lab's do.
const sweepPingGroupRange = "0 65535"

// parseLabLabels reads the LAB_LABELS a ready file holds: space-separated
// key=value pairs, the labels the lab put on every container it owns.
func parseLabLabels(s string) (map[string]string, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil, errors.New("the ready file has no LAB_LABELS")
	}
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("label %q is not key=value", f)
		}
		out[k] = v
	}
	return out, nil
}

// sweepVars reads the ready variables a sweep's containers need.
type sweepVars struct {
	project, network, server, image string
	labels                          map[string]string
}

func readSweepVars(env map[string]string) (sweepVars, error) {
	v := sweepVars{project: env["LAB_PROJECT"], network: env["LAB_NETWORK"],
		server: env["LAB_SERVER_URL"], image: env["LAB_IMAGE"]}
	for k, val := range map[string]string{"LAB_PROJECT": v.project, "LAB_NETWORK": v.network,
		"LAB_SERVER_URL": v.server, "LAB_IMAGE": v.image} {
		if val == "" {
			return sweepVars{}, fmt.Errorf("the ready file has no %s", k)
		}
	}
	labels, err := parseLabLabels(env["LAB_LABELS"])
	if err != nil {
		return sweepVars{}, err
	}
	v.labels = labels
	return v, nil
}

// labels are the lab's own plus the service's, so the lab's teardown and its
// leftover check find the sweep's containers as they do its own.
func (v sweepVars) serviceLabels(service string) map[string]string {
	l := maps.Clone(v.labels)
	l[lab.LabelComposeService] = service
	l[lab.LabelComposeNumber] = "1"
	l[lab.LabelComposeOneOff] = "False"
	return l
}

func (v sweepVars) nodeEnv(node, token string) []string {
	return []string{"OMNIGLASS_SERVER=" + v.server, "OMNIGLASS_NODE_NAME=" + node, "OMNIGLASS_NODE_TOKEN=" + token}
}

// sweepCadence is the product's own cadence, the one a deployed node runs, so
// the node's background work (heartbeat, probe, worklist pull) weighs on its
// cgroup as it would in the field rather than at the lab's fast clock.
func sweepCadence() (lab.Cadence, error) { return lab.LookupCadence(lab.CadenceDefault, false) }

// sweepNodeService is the node a driver is measured on: the lab's node, with
// no simulator and no impairments in its process, so its cgroup's CPU and
// memory are the node's own work.
func sweepNodeService(env map[string]string, node, token string) (lab.Service, error) {
	v, err := readSweepVars(env)
	if err != nil {
		return lab.Service{}, err
	}
	cad, err := sweepCadence()
	if err != nil {
		return lab.Service{}, err
	}
	return lab.Service{
		Name: node, Container: v.project + "-" + node + "-1", Image: v.image,
		Cmd: []string{"node", "run", "--heartbeat", cad.Heartbeat,
			"--probe-every", cad.ProbeEvery, "--worklist-every", cad.WorklistEvery},
		Env: v.nodeEnv(node, token),
		// As the lab's nodes: root inside its own container, which the pinger
		// and NET_ADMIN need under distroless.
		User:    "0:0",
		CapAdd:  []string{"NET_ADMIN"},
		Sysctls: map[string]string{"net.ipv4.ping_group_range": sweepPingGroupRange},
		Labels:  v.serviceLabels(node),
		Role:    "The node a driver's cost is measured on, with no simulator in its process.",
	}, nil
}

// sweepSimService is the simulator beside a sweep node, in the node's network
// namespace: it serves the node's simulated components on the loopback the
// node polls, and its CPU lands in a cgroup of its own.
func sweepSimService(env map[string]string, node, token, nodeID string) (lab.Service, error) {
	v, err := readSweepVars(env)
	if err != nil {
		return lab.Service{}, err
	}
	cad, err := sweepCadence()
	if err != nil {
		return lab.Service{}, err
	}
	service := node + "-sim"
	return lab.Service{
		Name: service, Container: v.project + "-" + service + "-1", Image: v.image,
		Cmd: []string{"sim", "run", "--poll", cad.SimPoll},
		Env: v.nodeEnv(node, token),
		// Root, to bind the simulated components' ports below 1024.
		User:      "0:0",
		NetworkOf: nodeID,
		Labels:    v.serviceLabels(service),
		Role:      "The simulator serving a sweep node's components on that node's loopback.",
	}, nil
}

// sweepNodeName is a driver's node at a step: a driver swept again at a
// bigger step takes a fresh node, its first one's rows still in the lab.
func sweepNodeName(driver string, step int) string { return fmt.Sprintf("sweep-%s-%d", driver, step) }

// componentsPerStep is how many components move a driver's task count by
// about target: each component brings one task per poll.
func componentsPerStep(target int, polls []time.Duration) int {
	n := max(len(polls), 1)
	return max((target+n-1)/n, 1)
}

// sweepWindow is the measured window: at least the configured one, and long
// enough for the driver's slowest poll to run in it.
func sweepWindow(window time.Duration, polls []time.Duration) time.Duration {
	if len(polls) == 0 {
		return window
	}
	return max(window, slices.Max(polls))
}

// heldTasksQuery counts every enabled task the server holds for a node, the
// driver's polls and the components' probes: the worklist it sends.
func heldTasksQuery(node string) string {
	return "select count(*) from task t join endpoint i on i.id = t.endpoint_id " +
		"join node n on n.principal_id = i.node_name " +
		"where t.enabled and n.name = " + sqlLiteral(node)
}

// installedWorklistQuery reads the size of the worklist a node last
// installed, which it reports from its platform component, named for it; -1
// before its first.
func installedWorklistQuery(node string) string {
	return "select coalesce((select m.value from metric m " +
		"join metric_series s on s.id = m.series_id " +
		"join metric_type mt on mt.id = s.metric_type_id " +
		"join component c on c.id = s.component_id " +
		"where mt.name = 'worklist-task-count' and c.name = " + sqlLiteral(node) +
		" order by m.ts desc limit 1), -1)"
}

// driverTasksQuery counts the enabled driver polls a node runs.
func driverTasksQuery(node string) string {
	return "select count(*) from task t join endpoint i on i.id = t.endpoint_id " +
		"join node n on n.principal_id = i.node_name " +
		"where t.enabled and t.mode = 'poll' and coalesce(t.spec->>'driver','') <> '' " +
		"and n.name = " + sqlLiteral(node)
}

// pollCPUQuery is the mean poll-cpu-time of the reports wholly inside [from,
// to), in milliseconds per run. A report stamped T covers [T-report, T), so
// the first one inside is stamped from+report and the last one to.
func pollCPUQuery(node string, from, to time.Time, report time.Duration) string {
	return "select coalesce(avg(m.value),0) from metric m " +
		"join metric_series s on s.id = m.series_id " +
		"join metric_type mt on mt.id = s.metric_type_id " +
		"join node n on n.principal_id = s.collector_node_id " +
		"where mt.name = 'poll-cpu-time' and n.name = " + sqlLiteral(node) +
		" and m.ts >= to_timestamp(" + epochLiteral(from.Add(report)) + ") and m.ts <= to_timestamp(" + epochLiteral(to) + ")"
}
