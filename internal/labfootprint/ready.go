package labfootprint

import (
	"fmt"
	"strings"
)

// ParseReadyEnv reads the lab's ready file (lab.FormatReadyEnv): one
// NAME='value' a line, a quote inside a value written as '\”.
func ParseReadyEnv(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		if line == "" {
			continue
		}
		name, val, ok := strings.Cut(line, "=")
		if !ok || len(val) < 2 || val[0] != '\'' || val[len(val)-1] != '\'' {
			return nil, fmt.Errorf("ready file: %q is not NAME='value'", line)
		}
		out[name] = strings.ReplaceAll(val[1:len(val)-1], `'\''`, "'")
	}
	return out, nil
}

// ComponentMetrics is a component and its sampled metrics, as the API lists
// them.
type ComponentMetrics struct {
	Component string
	Metrics   []string
}

// Series is one metric series of one component.
type Series struct {
	Component string
	Metric    string
}

// PickSeries picks up to n series to time, at most perComponent from each
// component in order, skipping components with nothing sampled.
func PickSeries(comps []ComponentMetrics, n, perComponent int) []Series {
	var out []Series
	for _, c := range comps {
		for i, m := range c.Metrics {
			if i >= perComponent || len(out) >= n {
				break
			}
			out = append(out, Series{Component: c.Component, Metric: m})
		}
		if len(out) >= n {
			break
		}
	}
	return out
}
