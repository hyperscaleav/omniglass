package lab

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// SoakGetter reads one API path (absolute, from /api/v1) and decodes the JSON
// body into into. It is the verdict's only way to the platform: the soak's
// judge never opens the database and never asks the harness.
type SoakGetter func(ctx context.Context, path string, into any) error

const (
	// SoakAlarmWindow is how far back the system alarms read reaches: a soak
	// that began earlier cannot be judged from it, and the verdict says so
	// rather than judging a part. It is the API's own bound (pinned to
	// api.systemAlarmWindow by TestSoakAlarmReadBoundsMatchTheAPI).
	SoakAlarmWindow = 30 * 24 * time.Hour
	// SoakAlarmCap is the most alarms that read serves (api.systemAlarmLimit,
	// pinned the same way); a read that returns that many may have been cut.
	SoakAlarmCap = 500
)

// The wire shapes the read decodes: only the fields it judges, so the rest of
// the API's bodies can grow without touching it.
type soakFigure struct {
	Loss *float64 `json:"loss"`
}

type soakSpan struct {
	Newest   soakFigure `json:"newest"`
	LastHour soakFigure `json:"last_hour"`
}

type soakKind struct {
	Kind string `json:"kind"`
	soakSpan
}

type soakEnd struct {
	Newest   *float64 `json:"newest"`
	LastHour *float64 `json:"last_hour"`
}

type soakPlatformView struct {
	FiguresRead bool `json:"figures_read"`
	System      *struct {
		Name string `json:"name"`
	} `json:"system"`
	Servers []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"servers"`
	Nodes []struct {
		Name    string   `json:"name"`
		Loss    *float64 `json:"loss"`
		Figures map[string]struct {
			Value float64 `json:"value"`
		} `json:"figures"`
	} `json:"nodes"`
	Pipeline *struct {
		Hops []struct {
			ID          string     `json:"id"`
			Total       soakSpan   `json:"total"`
			Kinds       []soakKind `json:"kinds"`
			ByComponent []struct {
				Name  string     `json:"name"`
				Total soakSpan   `json:"total"`
				Kinds []soakKind `json:"kinds"`
			} `json:"by_component"`
		} `json:"hops"`
		EndToEnd []struct {
			Kind string `json:"kind"`
			soakEnd
		} `json:"end_to_end"`
		EndToEndTotal soakEnd `json:"end_to_end_total"`
	} `json:"pipeline"`
}

type soakSystemAlarms struct {
	Alarms []struct {
		Component string     `json:"component"`
		RaisedBy  string     `json:"raised_by_rule"`
		Severity  string     `json:"severity"`
		Message   string     `json:"message"`
		RaisedAt  time.Time  `json:"raised_at"`
		ClearedAt *time.Time `json:"cleared_at"`
	} `json:"alarms"`
}

// ReadSoak gathers a soak's reading through the API: the platform health view
// (the build the servers run, and every loss ratio on the page as it stands
// now) and the platform system's alarm history, which carries every alarm on
// every platform component, each with when it was raised and cleared. until
// is the end of the window, the moment the loss ratios are read at.
func ReadSoak(ctx context.Context, get SoakGetter, since, until time.Time) (SoakReading, error) {
	if age := until.Sub(since); age > SoakAlarmWindow {
		return SoakReading{}, fmt.Errorf("--since is %s back: the API serves a system's alarm history for %s, so a longer soak cannot be judged from it",
			age.Round(time.Hour), SoakAlarmWindow)
	}
	var view soakPlatformView
	if err := get(ctx, "/api/v1/views/platform", &view); err != nil {
		return SoakReading{}, fmt.Errorf("read the platform health view: %w", err)
	}
	if !view.FiguresRead || view.System == nil || view.Pipeline == nil {
		return SoakReading{}, errors.New("the token cannot read the whole platform health page (it needs node, system and component read over everything): a verdict from part of the page would pass what it could not see")
	}
	build, err := soakBuild(view)
	if err != nil {
		return SoakReading{}, err
	}
	var alarms soakSystemAlarms
	if err := get(ctx, "/api/v1/systems/"+url.PathEscape(view.System.Name)+"/alarms", &alarms); err != nil {
		return SoakReading{}, fmt.Errorf("read the platform system's alarms: %w", err)
	}
	r := SoakReading{Build: build, Since: since, Until: until, Losses: soakLosses(view), AlarmsTruncated: len(alarms.Alarms) >= SoakAlarmCap}
	for _, a := range alarms.Alarms {
		r.Alarms = append(r.Alarms, SoakAlarm{
			Component: a.Component, Rule: a.RaisedBy, Severity: a.Severity, Message: a.Message,
			RaisedAt: a.RaisedAt, ClearedAt: a.ClearedAt,
		})
	}
	return r, nil
}

// soakBuild is the one build every server runs. Two builds in one soak is not
// a soak of either; no build at all is a platform that has not written its
// identity.
func soakBuild(view soakPlatformView) (string, error) {
	builds := map[string]bool{}
	for _, s := range view.Servers {
		if s.Version != "" {
			builds[s.Version] = true
		}
	}
	var names []string
	for b := range builds {
		names = append(names, b)
	}
	sort.Strings(names)
	switch len(names) {
	case 0:
		return "", errors.New("no server on the platform health page reports its build")
	case 1:
		return names[0], nil
	}
	return "", fmt.Errorf("the servers run more than one build (%s): a soak measures one", strings.Join(names, ", "))
}

// soakLosses is every loss ratio the platform health page carries: each
// delivery hop (its total, each kind, each component's share) over the newest
// window and the last hour, the end to end, each node's loss, and each node's
// worklist pull loss. A ratio the page leaves absent (nothing went in) is
// not read as one.
func soakLosses(view soakPlatformView) []SoakLoss {
	var out []SoakLoss
	add := func(where string, loss *float64) {
		if loss != nil {
			out = append(out, SoakLoss{Where: where, Ratio: *loss})
		}
	}
	span := func(where string, s soakSpan) {
		add(where+", newest window", s.Newest.Loss)
		add(where+", last hour", s.LastHour.Loss)
	}
	for _, h := range view.Pipeline.Hops {
		span(h.ID+" hop", h.Total)
		for _, k := range h.Kinds {
			span(h.ID+" hop, "+k.Kind, k.soakSpan)
		}
		for _, c := range h.ByComponent {
			span(h.ID+" hop on "+c.Name, c.Total)
			for _, k := range c.Kinds {
				span(h.ID+" hop on "+c.Name+", "+k.Kind, k.soakSpan)
			}
		}
	}
	for _, e := range view.Pipeline.EndToEnd {
		add("end to end, "+e.Kind+", newest window", e.Newest)
		add("end to end, "+e.Kind+", last hour", e.LastHour)
	}
	add("end to end, newest window", view.Pipeline.EndToEndTotal.Newest)
	add("end to end, last hour", view.Pipeline.EndToEndTotal.LastHour)
	for _, n := range view.Nodes {
		add("node "+n.Name+", last hour", n.Loss)
		if f, ok := n.Figures[writerclass.WorklistPullLossRatio]; ok {
			v := f.Value
			add("node "+n.Name+", worklist pull", &v)
		}
	}
	return out
}
