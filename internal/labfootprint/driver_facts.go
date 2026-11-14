package labfootprint

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strings"
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

// DriverCostFact is one shipped driver as the docs list it: its measured
// cost, or why it has none; a measured driver with one figure the sweep could
// not fit carries that figure's reason too.
type DriverCostFact struct {
	Driver                string   `json:"driver"`
	Product               string   `json:"product,omitempty"`
	Polls                 []string `json:"polls,omitempty"`
	Measured              bool     `json:"measured"`
	StepTasks             int      `json:"step_tasks,omitempty"`
	MemoryPerTaskKiB      *float64 `json:"memory_per_task_kib,omitempty"`
	MemoryWarmMiB         *float64 `json:"memory_warm_mib,omitempty"`
	MillicoresPerTask     *float64 `json:"millicores_per_task,omitempty"`
	CPUPerRunMillis       *float64 `json:"cpu_per_run_ms,omitempty"`
	ThreadCPUPerRunMillis *float64 `json:"thread_cpu_per_run_ms,omitempty"`
	ThreadShare           *float64 `json:"thread_share,omitempty"`
	Reason                string   `json:"reason,omitempty"`
}

// DriverCostFacts is the docs' driver cost table: where and on what build
// the sweep ran, and every driver the plan knows.
type DriverCostFacts struct {
	Host         *Fingerprint     `json:"host,omitempty"`
	Date         string           `json:"date"`
	Build        string           `json:"build"`
	StepTasks    int              `json:"step_tasks"`
	MaxStepTasks int              `json:"max_step_tasks,omitempty"`
	Drivers      []DriverCostFact `json:"drivers"`
}

// driverCostFacts joins the plan with a measured table: each planned driver
// takes its row, the sweep's reason it has none, or NotYetMeasured; each
// driver the plan skips takes the plan's reason.
func driverCostFacts(targets []DriverTarget, skips []DriverSkip, t DriverCostTable) DriverCostFacts {
	f := DriverCostFacts{Host: t.Host, Date: t.Date, Build: t.Build, StepTasks: t.StepTasks, MaxStepTasks: t.MaxStepTasks}
	for _, tg := range targets {
		d := DriverCostFact{Driver: tg.Driver, Product: tg.Product}
		for _, p := range tg.Polls {
			d.Polls = append(d.Polls, p.String())
		}
		if i := slices.IndexFunc(t.Rows, func(r DriverCost) bool { return r.Driver == tg.Driver }); i >= 0 {
			r := t.Rows[i]
			d.Measured, d.StepTasks = true, r.StepTasks
			if r.MemoryNotMeasured == "" {
				d.MemoryPerTaskKiB = new(r.MemoryPerTaskBytes / 1024)
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
		f.Drivers = append(f.Drivers, DriverCostFact{Driver: s.Driver, Reason: s.Reason})
	}
	slices.SortFunc(f.Drivers, func(a, b DriverCostFact) int { return strings.Compare(a.Driver, b.Driver) })
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
	b, err := json.MarshalIndent(driverCostFacts(targets, skips, t), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
