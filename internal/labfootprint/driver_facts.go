package labfootprint

import (
	_ "embed"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/seed"
)

// shippedDriverCost is the committed driver cost sweep (#1871), written by
// `omniglass lab perf drivers` on the host it names and reviewed like a
// baseline: it is a measurement, so nothing regenerates it.
//
//go:embed drivercost/shipped.json
var shippedDriverCost []byte

// DriverCostPath is where the sweep writes the shipped table, from the
// repository's root.
const DriverCostPath = "internal/labfootprint/drivercost/shipped.json"

// NotYetMeasured is a planned driver the committed table has no word on:
// before the first sweep is committed, or one added since it ran.
const NotYetMeasured = "not yet measured"

// JSON is the table as it is committed, its lists written as lists even
// when empty.
func (t DriverCostTable) JSON() ([]byte, error) {
	if t.Rows == nil {
		t.Rows = []DriverCost{}
	}
	if t.NotMeasured == nil {
		t.NotMeasured = []DriverSkip{}
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ReadDriverCostTable reads a committed table.
func ReadDriverCostTable(b []byte) (DriverCostTable, error) {
	var t DriverCostTable
	err := json.Unmarshal(b, &t)
	return t, err
}

// DriverCostFact is one shipped driver as the docs list it: its kind (the
// transport it speaks), its measured cost, or why it has none; a measured
// driver with one figure the sweep could not fit carries that figure's
// reason too. MemoryPerEndpointKiB is what one endpoint on the driver holds:
// a task per poll, so its memory per task times its polls.
type DriverCostFact struct {
	Driver                string   `json:"driver"`
	Kind                  string   `json:"kind,omitempty"`
	Product               string   `json:"product,omitempty"`
	Polls                 []string `json:"polls,omitempty"`
	Measured              bool     `json:"measured"`
	StepTasks             int      `json:"step_tasks,omitempty"`
	MemoryPerTaskKiB      *float64 `json:"memory_per_task_kib,omitempty"`
	MemoryPerEndpointKiB  *float64 `json:"memory_per_endpoint_kib,omitempty"`
	MemoryWarmMiB         *float64 `json:"memory_warm_mib,omitempty"`
	MillicoresPerTask     *float64 `json:"millicores_per_task,omitempty"`
	CPUPerRunMillis       *float64 `json:"cpu_per_run_ms,omitempty"`
	ThreadCPUPerRunMillis *float64 `json:"thread_cpu_per_run_ms,omitempty"`
	ThreadShare           *float64 `json:"thread_share,omitempty"`
	Reason                string   `json:"reason,omitempty"`
}

// DriverCostFacts is the docs' driver cost tables: where and on what build
// the sweep ran, every driver the plan knows, and those drivers rolled up by
// kind and interval.
type DriverCostFacts struct {
	Host         *Fingerprint     `json:"host,omitempty"`
	Date         string           `json:"date"`
	Build        string           `json:"build"`
	StepTasks    int              `json:"step_tasks"`
	MaxStepTasks int              `json:"max_step_tasks,omitempty"`
	Kinds        []KindCostFact   `json:"kinds"`
	Drivers      []DriverCostFact `json:"drivers"`
}

// Spread is a figure across a kind's measured drivers: its median (the mean
// of the middle two over an even count), its range, and how many drivers it
// reads from.
type Spread struct {
	Median float64 `json:"median"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	N      int     `json:"n"`
}

// spreadOf is the spread of a non-empty list of figures, nil for none.
func spreadOf(v []float64) *Spread {
	if len(v) == 0 {
		return nil
	}
	v = slices.Sorted(slices.Values(v))
	mid := v[len(v)/2]
	if len(v)%2 == 0 {
		mid = (v[len(v)/2-1] + mid) / 2
	}
	return &Spread{Median: mid, Min: v[0], Max: v[len(v)-1], N: len(v)}
}

// KindCostFact is one kind of work at one interval, the unit a node is sized
// by: the transport, a poll interval its swept drivers ship (none for a kind
// that only listens), the drivers that poll at it, and the cost of the kind's
// measured drivers. A run costs what the kind costs at any interval; the
// interval is how often it runs, so the millicores a task holds there are its
// CPU per run over the interval. Each figure is nil where no driver of the
// kind measured it.
type KindCostFact struct {
	Kind                 string   `json:"kind"`
	Interval             string   `json:"interval,omitempty"`
	PolledBy             []string `json:"polled_by,omitempty"`
	Drivers              []string `json:"drivers,omitempty"`
	MemoryPerTaskKiB     *Spread  `json:"memory_per_task_kib,omitempty"`
	MemoryPerEndpointKiB *Spread  `json:"memory_per_endpoint_kib,omitempty"`
	CPUPerRunMillis      *Spread  `json:"cpu_per_run_ms,omitempty"`
	MillicoresPerTask    *Spread  `json:"millicores_per_task,omitempty"`
}

// kindCostFacts rolls the drivers up by kind: one row per kind and interval
// a planned driver of the kind polls at, and one row with no interval for a
// kind the plan has no target of. Rows come in kind order, then fastest
// interval first.
func kindCostFacts(targets []DriverTarget, drivers []DriverCostFact, kinds map[string]string) []KindCostFact {
	type acc struct {
		measured                  []string
		perTask, perEndpoint, cpu []float64
		intervals                 map[time.Duration][]string
	}
	byKind := map[string]*acc{}
	at := func(kind string) *acc {
		if byKind[kind] == nil {
			byKind[kind] = &acc{intervals: map[time.Duration][]string{}}
		}
		return byKind[kind]
	}
	for _, d := range drivers {
		a := at(d.Kind)
		if d.MemoryPerTaskKiB != nil {
			a.perTask = append(a.perTask, *d.MemoryPerTaskKiB)
			a.perEndpoint = append(a.perEndpoint, *d.MemoryPerEndpointKiB)
		}
		if d.CPUPerRunMillis != nil {
			a.cpu = append(a.cpu, *d.CPUPerRunMillis)
		}
		if d.MemoryPerTaskKiB != nil || d.CPUPerRunMillis != nil {
			a.measured = append(a.measured, d.Driver)
		}
	}
	for _, tg := range targets {
		a := at(kinds[tg.Driver])
		for _, p := range slices.Compact(slices.Sorted(slices.Values(tg.Polls))) {
			a.intervals[p] = append(a.intervals[p], tg.Driver)
		}
	}
	var out []KindCostFact
	for _, kind := range slices.Sorted(maps.Keys(byKind)) {
		a := byKind[kind]
		row := KindCostFact{Kind: kind, Drivers: a.measured,
			MemoryPerTaskKiB: spreadOf(a.perTask), MemoryPerEndpointKiB: spreadOf(a.perEndpoint), CPUPerRunMillis: spreadOf(a.cpu)}
		if len(a.intervals) == 0 {
			out = append(out, row)
			continue
		}
		for _, every := range slices.Sorted(maps.Keys(a.intervals)) {
			r := row
			r.Interval, r.PolledBy = every.String(), slices.Sorted(slices.Values(a.intervals[every]))
			mc := make([]float64, len(a.cpu))
			for i, c := range a.cpu {
				mc[i] = c / every.Seconds()
			}
			r.MillicoresPerTask = spreadOf(mc)
			out = append(out, r)
		}
	}
	return out
}

// driverCostFacts joins the plan with a measured table and each driver's
// kind: each planned driver takes its row, the sweep's reason it has none, or
// NotYetMeasured; each driver the plan skips takes the plan's reason. The
// kind rows roll the result up.
func driverCostFacts(targets []DriverTarget, skips []DriverSkip, kinds map[string]string, t DriverCostTable) DriverCostFacts {
	f := DriverCostFacts{Host: t.Host, Date: t.Date, Build: t.Build, StepTasks: t.StepTasks, MaxStepTasks: t.MaxStepTasks}
	for _, tg := range targets {
		d := DriverCostFact{Driver: tg.Driver, Kind: kinds[tg.Driver], Product: tg.Product}
		for _, p := range tg.Polls {
			d.Polls = append(d.Polls, p.String())
		}
		if i := slices.IndexFunc(t.Rows, func(r DriverCost) bool { return r.Driver == tg.Driver }); i >= 0 {
			r := t.Rows[i]
			d.Measured, d.StepTasks = true, r.StepTasks
			if r.MemoryNotMeasured == "" {
				d.MemoryPerTaskKiB = new(r.MemoryPerTaskBytes / 1024)
				d.MemoryPerEndpointKiB = new(*d.MemoryPerTaskKiB * float64(len(tg.Polls)))
				if r.WarmNotMeasured == "" {
					d.MemoryWarmMiB = new(r.MemoryWarmBytes / (1 << 20))
				}
			}
			if r.CPUNotMeasured == "" {
				d.MillicoresPerTask, d.CPUPerRunMillis = new(r.MillicoresPerTask), new(r.CPUPerRunMillis)
				d.ThreadShare = new(r.ThreadShare)
			}
			d.ThreadCPUPerRunMillis = new(r.ThreadCPUPerRunMillis)
			d.Reason = strings.Join(slices.DeleteFunc([]string{r.MemoryNotMeasured, r.WarmNotMeasured, r.CPUNotMeasured},
				func(s string) bool { return s == "" }), "; ")
		} else if i := slices.IndexFunc(t.NotMeasured, func(s DriverSkip) bool { return s.Driver == tg.Driver }); i >= 0 {
			d.Reason = t.NotMeasured[i].Reason
		} else {
			d.Reason = NotYetMeasured
		}
		f.Drivers = append(f.Drivers, d)
	}
	for _, s := range skips {
		f.Drivers = append(f.Drivers, DriverCostFact{Driver: s.Driver, Kind: kinds[s.Driver], Reason: s.Reason})
	}
	slices.SortFunc(f.Drivers, func(a, b DriverCostFact) int { return strings.Compare(a.Driver, b.Driver) })
	f.Kinds = kindCostFacts(targets, f.Drivers, kinds)
	return f
}

// DriverCostFactsJSON is docs/src/generated/drivercost.json: the shipped
// plan joined with the committed sweep.
func DriverCostFactsJSON() ([]byte, error) {
	t, err := ReadDriverCostTable(shippedDriverCost)
	if err != nil {
		return nil, err
	}
	targets, skips := ShippedDriverSweep()
	b, err := json.MarshalIndent(driverCostFacts(targets, skips, seed.ShippedDriverTransports(), t), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
