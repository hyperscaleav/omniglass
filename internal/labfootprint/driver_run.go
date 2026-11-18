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

// sweepPingGroupRange is the lab's own range for its outposts, so the sweep's
// outpost runs the pinger as the lab's do.
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

func (v sweepVars) outpostEnv(outpost, token string) []string {
	return []string{"OMNIGLASS_SERVER=" + v.server, "OMNIGLASS_OUTPOST_NAME=" + outpost, "OMNIGLASS_OUTPOST_TOKEN=" + token}
}

// sweepCadence is the product's own cadence, the one a deployed outpost runs, so
// the outpost's background work (heartbeat, probe, worklist pull) weighs on its
// cgroup as it would in the field rather than at the lab's fast clock.
func sweepCadence() (lab.Cadence, error) { return lab.LookupCadence(lab.CadenceDefault, false) }

// sweepOutpostService is the outpost a driver is measured on: the lab's outpost, with
// no simulator and no impairments in its process, so its cgroup's CPU and
// memory are the outpost's own work.
func sweepOutpostService(env map[string]string, outpost, token string) (lab.Service, error) {
	v, err := readSweepVars(env)
	if err != nil {
		return lab.Service{}, err
	}
	cad, err := sweepCadence()
	if err != nil {
		return lab.Service{}, err
	}
	return lab.Service{
		Name: outpost, Container: v.project + "-" + outpost + "-1", Image: v.image,
		Cmd: []string{"outpost", "run", "--heartbeat", cad.Heartbeat,
			"--probe-every", cad.ProbeEvery, "--worklist-every", cad.WorklistEvery},
		Env: v.outpostEnv(outpost, token),
		// As the lab's outposts: root inside its own container, which the pinger
		// and NET_ADMIN need under distroless.
		User:    "0:0",
		CapAdd:  []string{"NET_ADMIN"},
		Sysctls: map[string]string{"net.ipv4.ping_group_range": sweepPingGroupRange},
		Labels:  v.serviceLabels(outpost),
		Role:    "The outpost a driver's cost is measured on, with no simulator in its process.",
	}, nil
}

// sweepSimService is the simulator beside a sweep outpost, in the outpost's network
// namespace: it serves the outpost's simulated components on the loopback the
// outpost polls, and its CPU lands in a cgroup of its own.
func sweepSimService(env map[string]string, outpost, token, outpostID string) (lab.Service, error) {
	v, err := readSweepVars(env)
	if err != nil {
		return lab.Service{}, err
	}
	cad, err := sweepCadence()
	if err != nil {
		return lab.Service{}, err
	}
	service := outpost + "-sim"
	return lab.Service{
		Name: service, Container: v.project + "-" + service + "-1", Image: v.image,
		Cmd: []string{"sim", "run", "--poll", cad.SimPoll},
		Env: v.outpostEnv(outpost, token),
		// Root, to bind the simulated components' ports below 1024.
		User:      "0:0",
		NetworkOf: outpostID,
		Labels:    v.serviceLabels(service),
		Role:      "The simulator serving a sweep outpost's components on that outpost's loopback.",
	}, nil
}

// sweepOutpostName is a driver's outpost at a step: a driver swept again at a
// bigger step takes a fresh outpost, its first one's rows still in the lab.
func sweepOutpostName(driver string, step int) string {
	return fmt.Sprintf("sweep-%s-%d", driver, step)
}

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

// heldTasksQuery counts every enabled task the server holds for an outpost, the
// driver's polls and the components' probes: the worklist it sends.
func heldTasksQuery(outpost string) string {
	return "select count(*) from task t join endpoint i on i.id = t.endpoint_id " +
		"join outpost n on n.user_id = i.outpost_id " +
		"where t.enabled and n.name = " + sqlLiteral(outpost)
}

// installedWorklistQuery reads the size of the worklist an outpost last
// installed, which it reports from its platform component, named for it; -1
// before its first.
func installedWorklistQuery(outpost string) string {
	return "select coalesce((select m.value from metric m " +
		"join metric_series s on s.id = m.series_id " +
		"join metric_type mt on mt.id = s.metric_type_id " +
		"join component c on c.id = s.component_id " +
		"where mt.name = 'worklist-task-count' and c.name = " + sqlLiteral(outpost) +
		" order by m.ts desc limit 1), -1)"
}

// driverTasksQuery counts the enabled driver polls an outpost runs.
func driverTasksQuery(outpost string) string {
	return "select count(*) from task t join endpoint i on i.id = t.endpoint_id " +
		"join outpost n on n.user_id = i.outpost_id " +
		"where t.enabled and t.mode = 'poll' and coalesce(t.spec->>'driver','') <> '' " +
		"and n.name = " + sqlLiteral(outpost)
}

// pollCPUQuery is the mean poll-cpu-time of the reports wholly inside [from,
// to), in milliseconds per run. A report stamped T covers [T-report, T), so
// the first one inside is stamped from+report and the last one to.
func pollCPUQuery(outpost string, from, to time.Time, report time.Duration) string {
	return "select coalesce(avg(m.value),0) from metric m " +
		"join metric_series s on s.id = m.series_id " +
		"join metric_type mt on mt.id = s.metric_type_id " +
		"join outpost n on n.user_id = s.collector_outpost_id " +
		"where mt.name = 'poll-cpu-time' and n.name = " + sqlLiteral(outpost) +
		" and m.ts >= to_timestamp(" + epochLiteral(from.Add(report)) + ") and m.ts <= to_timestamp(" + epochLiteral(to) + ")"
}
