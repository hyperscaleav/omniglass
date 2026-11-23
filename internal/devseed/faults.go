package devseed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim/impair"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Fault is one thing really wrong in the example fleet (#2434). The seed
// never raises its alarm: it sets up the cause and an event rule judges it,
// so the alarm links to its rule, carries the event that raised it, agrees
// with what the component's own page reads, and clears when the cause lifts.
//
// The cause depends on what is there to collect. Beside the simulator (a lab)
// it is Impairment, held by the simulator and shown on the Simulation page,
// where a presenter lifts it and the outpost reads the device recover. With
// no simulator (the docs' screenshots, a bare dev server) nothing collects,
// so it is Reading: the one sample an outpost would have landed when the
// fault began, landed once through the same ingest, which the engine judges.
// Component is a fixture key.
type Fault struct {
	Component  string        `yaml:"component"`
	Why        string        `yaml:"why"`
	Impairment SimImpairment `yaml:"impairment"`
	Reading    Reading       `yaml:"reading"`
}

// SimImpairment is a fault's impairment as the fixture spells it.
type SimImpairment struct {
	Unplugged bool    `yaml:"unplugged"`
	LatencyMs int     `yaml:"latency_ms"`
	JitterMs  int     `yaml:"jitter_ms"`
	LossPct   float64 `yaml:"loss_pct"`
	RateKbit  int     `yaml:"rate_kbit"`
}

// Impairment is the simulator's own form of it.
func (s SimImpairment) Impairment() impair.Impairment {
	return impair.Impairment{Unplugged: s.Unplugged, LatencyMs: s.LatencyMs, JitterMs: s.JitterMs, LossPct: s.LossPct, RateKbit: s.RateKbit}
}

// Reading is one sample under a component's endpoint: a text Value lands on
// the property lane (a state such as endpoint-reachable), a number on the
// metric lane.
type Reading struct {
	Key      string `yaml:"key"`
	Instance string `yaml:"instance"`
	Value    any    `yaml:"value"`
	Source   string `yaml:"source"`
}

// EventRule is one of the fleet's own event rules, in the boot seed's
// spelling (internal/seed/event_rules.yaml). It is an operator's rule, not an
// official one: seeded once by name, so an edit made in the console stands.
type EventRule struct {
	Name           string `yaml:"name"`
	Label          string `yaml:"label"`
	Description    string `yaml:"description"`
	SampleKey      string `yaml:"sample_key"`
	Fire           string `yaml:"fire"`
	FireSustain    string `yaml:"fire_sustain"`
	Clear          string `yaml:"clear"`
	Severity       string `yaml:"severity"`
	FireEventType  string `yaml:"fire_event_type"`
	ClearEventType string `yaml:"clear_event_type"`
}

// seedEventRules creates the fleet's own rules. One that exists is left as
// it stands, so a re-run changes nothing.
func seedEventRules(ctx context.Context, gw storage.Gateway, actorID string, doc Doc) error {
	for _, r := range doc.EventRules {
		spec := storage.EventRuleSpec{Name: r.Name, Label: r.Label, Description: r.Description, SampleKey: r.SampleKey,
			Fire: rules.Criteria{Expr: r.Fire}, Severity: r.Severity, FireEventType: r.FireEventType, ClearEventType: r.ClearEventType}
		if r.Clear != "" {
			spec.Clear = &rules.Criteria{Expr: r.Clear}
		}
		if r.FireSustain != "" {
			d, err := time.ParseDuration(r.FireSustain)
			if err != nil {
				return fmt.Errorf("devseed: event rule %s fire_sustain: %w", r.Name, err)
			}
			spec.FireSustain = d
		}
		if _, err := gw.CreateEventRule(ctx, actorID, spec, scope.Set{All: true}); err != nil && !errors.Is(err, storage.ErrEventRuleExists) {
			return fmt.Errorf("devseed: create event rule %s: %w", r.Name, err)
		}
	}
	return nil
}

// faultImpairments maps each fault's component id to the impairment the
// simulator holds for it.
func faultImpairments(doc Doc, compIDs map[string]string) (map[string]impair.Impairment, error) {
	out := map[string]impair.Impairment{}
	for _, f := range doc.Faults {
		id, ok := compIDs[f.Component]
		if !ok {
			return nil, fmt.Errorf("devseed: fault references unknown component key %q", f.Component)
		}
		out[id] = f.Impairment.Impairment()
	}
	return out, nil
}

// seedFaultReadings lands each fault's reading, as the outpost holding the
// component's endpoint would, for a fleet with no simulator. A series that
// already holds a sample is left alone (samples are append-only), so a
// re-run writes nothing.
func seedFaultReadings(ctx context.Context, gw storage.Gateway, doc Doc, compIDs map[string]string) error {
	now := time.Now().UTC()
	for _, f := range doc.Faults {
		id, ok := compIDs[f.Component]
		if !ok {
			return fmt.Errorf("devseed: fault references unknown component key %q", f.Component)
		}
		r := f.Reading
		w := storage.OutpostWriter(id, r.Instance)
		var err error
		switch v := r.Value.(type) {
		case string:
			var have []storage.PropertySample
			if have, err = gw.ListPropertySeries(ctx, "component", id, r.Key, 0, 1); err == nil && len(have) == 0 {
				err = gw.InsertPropertySamples(ctx, w, []storage.PropertySampleWrite{
					{OwnerKind: "component", OwnerID: id, Key: r.Key, Instance: r.Instance, Value: v, Source: r.Source, TS: now}})
			}
		case int, float64:
			var have []storage.MetricSample
			if have, err = gw.ListMetricSeries(ctx, "component", id, r.Key, 0, 1); err == nil && len(have) == 0 {
				err = gw.InsertMetricSamples(ctx, w, []storage.MetricSampleWrite{
					{OwnerKind: "component", OwnerID: id, Key: r.Key, Instance: r.Instance, Value: number(v), Source: r.Source, TS: now}})
			}
		default:
			err = fmt.Errorf("value %v is neither a state nor a number", r.Value)
		}
		if err != nil {
			return fmt.Errorf("devseed: fault reading %s on %q: %w", r.Key, f.Component, err)
		}
	}
	return nil
}

func number(v any) float64 {
	if i, ok := v.(int); ok {
		return float64(i)
	}
	return v.(float64)
}
