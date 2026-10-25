package ingress

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/logline"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Routed is one pushed message routed through its endpoint's listeners: the
// listener that claimed it ("" when none did), what its emits interpreted to,
// the faults of those that did not, and the message's raw log line when it
// lands one.
type Routed struct {
	Listener string
	Samples  []collection.Sample
	Faults   []error
	Raw      *collection.Sample
}

// Route offers a message to the listeners in order, listeners first
// (ADR-0151): the first whose match claims it interprets it through the one
// extraction primitive. On a push transport the raw message lands as a log
// line when nothing claims it, and beside a claimed one's emits unless the
// listener declares log: false (decision 5 of #1146); a claimed message whose
// emit faulted lands its raw line whatever the listener says, carrying the
// faults, so an operator sees why it mapped to less than the driver says.
func Route(fns []*driver.BakedFunction, msg driver.Message, at time.Time) Routed {
	var r Routed
	var claimer *driver.BakedFunction
	for _, fn := range fns {
		if fn.Match != nil && fn.Match.MatchesMessage(msg) {
			claimer = fn
			break
		}
	}
	attrs := map[string]any{}
	if claimer != nil {
		r.Listener = claimer.Function
		attrs["listener"] = claimer.Function
		emitted, faults := claimer.Interpret(msg.Payload(), at)
		for _, e := range emitted {
			r.Samples = append(r.Samples, collection.FromEmitted(e))
		}
		r.Faults = faults
		if len(faults) > 0 {
			fs := make([]string, 0, len(faults))
			for _, f := range faults {
				fs = append(fs, f.Error())
			}
			attrs["fault"] = strings.Join(fs, "; ")
		}
		if claimer.Log != nil && !*claimer.Log && len(faults) == 0 {
			return r
		}
	}
	raw, _ := json.Marshal(attrs)
	if len(attrs) == 0 {
		raw = nil
	}
	text, a := logline.Cap(logline.Clean(msg.Data), raw)
	r.Raw = &collection.Sample{Lane: driver.LaneLog, Text: text, IsText: true, Attributes: a, TS: at}
	return r
}

// Batch is a routed message as the batch the push lane carries: owned by the
// endpoint's component, every sample and the raw line under the endpoint's
// name, its source the transport. The lane's writer admits only collected
// keys (ADR-0161), whatever a driver emits.
func Batch(componentID, endpoint string, r Routed) *ogv1.TelemetryBatch {
	b := &ogv1.TelemetryBatch{
		Owner:  &ogv1.Owner{Kind: "component", Ref: componentID},
		Source: endpoint,
	}
	samples := r.Samples
	if r.Raw != nil {
		samples = append(append([]collection.Sample(nil), samples...), *r.Raw)
	}
	for _, d := range samples {
		var ts *timestamppb.Timestamp
		if !d.TS.IsZero() {
			ts = timestamppb.New(d.TS)
		}
		switch {
		case d.Lane == driver.LaneEvent:
			b.Events = append(b.Events, &ogv1.EventSample{Name: d.Name, Instance: endpoint, Message: valid(d.Text), Payload: d.Attributes, Ts: ts})
		case d.Lane == driver.LaneLog:
			b.Logs = append(b.Logs, &ogv1.LogLine{Message: valid(d.Text), Severity: d.Severity, Facility: valid(d.Facility),
				Source: valid(d.Source), Attributes: d.Attributes, Instance: endpoint, Ts: ts})
		case d.IsText:
			vj, _ := json.Marshal(d.Text)
			b.Properties = append(b.Properties, &ogv1.PropertySample{Name: d.Name, Instance: endpoint, ValueJson: string(vj), Ts: ts})
		default:
			b.Metrics = append(b.Metrics, &ogv1.MetricSample{Name: d.Name, Instance: endpoint, Value: d.Value, Ts: ts})
		}
	}
	return b
}

func valid(s string) string { return strings.ToValidUTF8(s, "�") }
