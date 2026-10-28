package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The effective-metric reads: the metric twin of the per-owner effective
// property lists, one GET per owner kind over the owner-generic
// EffectiveMetrics resolver. Only the read exists on this surface: a metric's
// values are telemetry (the series), not something a detail page sets, so the
// property side's PUT/DELETE have no metric twin here. Each read is gated and
// scope-injected exactly like its property sibling, so an out-of-scope owner is
// a non-disclosing 404.

type effectiveMetricBody struct {
	MetricTypeName string               `json:"metric_type_name" doc:"The catalog metric name"`
	MetricTypeID   string               `json:"metric_type_id" doc:"The catalog metric's uuid, the stable form of metric_type_name"`
	Label          string               `json:"label,omitempty" doc:"The metric's human label; omitted when unset"`
	DataType       string               `json:"data_type" doc:"The numeric value type, from the metric catalog"`
	Required       bool                 `json:"required" doc:"Whether the contract requires the metric; always false off-contract"`
	IsSampled      bool                 `json:"is_sampled" doc:"True when the series holds an observed or calculated sample"`
	FromContract   bool                 `json:"from_contract" doc:"True when the owner's classifier declares the metric; false for a series landing directly on the owner"`
	DefaultValue   json.RawMessage      `json:"default_value,omitempty" doc:"The contract default; omitted when the contract sets none"`
	SampleValue    json.RawMessage      `json:"sample_value,omitempty" doc:"The series' latest sample; omitted when none has arrived"`
	Value          json.RawMessage      `json:"value,omitempty" doc:"The effective value: the latest sample, or the contract default until one arrives"`
	SourceRule     string               `json:"source_rule,omitempty" required:"false" doc:"The calc rule the latest sample was derived by, by the name it was written under; absent for any other sample"`
	SourceRuleID   string               `json:"source_rule_id,omitempty" required:"false" doc:"That calc rule's uuid, the stable form of source_rule"`
	Instance       string               `json:"instance" doc:"The instance the shown sample came from (ADR-0456): for an observed sample the endpoint that collected it, empty for the owner-level series (a calculated sample, or a push naming no endpoint) and when nothing is sampled"`
	InstanceCount  int                  `json:"instance_count" doc:"How many live instances of the metric hold a sample on this owner (a deleted endpoint's frozen reading is not counted); the shown sample is the newest of them. 0 when nothing is sampled"`
	Instances      []metricInstanceBody `json:"instances,omitempty" required:"false" doc:"Each live instance's latest sample, newest first; present only when instance_count is above 1, so a single-instance metric carries none"`
}

// metricInstanceBody is one live instance's latest sample of a metric.
type metricInstanceBody struct {
	Instance string          `json:"instance" doc:"The instance: the endpoint that collected the sample, or empty for the owner-level series"`
	Value    json.RawMessage `json:"value" doc:"That instance's latest sample"`
	TS       time.Time       `json:"ts" doc:"When that sample was taken"`
	Endpoint bool            `json:"endpoint" doc:"True when the instance names an endpoint the owner has; false for the owner-level series and an instance a push named"`
}

func toEffectiveMetricBody(e *storage.EffectiveMetric) effectiveMetricBody {
	return effectiveMetricBody{
		MetricTypeName: e.MetricTypeName,
		MetricTypeID:   e.MetricTypeID,
		Label:          e.Label,
		DataType:       e.DataType,
		Required:       e.Required,
		IsSampled:      e.IsSampled,
		FromContract:   e.FromContract,
		DefaultValue:   json.RawMessage(e.DefaultValue),
		SampleValue:    json.RawMessage(e.SampleValue),
		Value:          json.RawMessage(e.Value),
		SourceRule:     e.SourceRule,
		SourceRuleID:   e.SourceRuleID,
		Instance:       e.Instance,
		InstanceCount:  e.InstanceCount,
		Instances:      toMetricInstanceBodies(e.Instances),
	}
}

func toMetricInstanceBodies(in []storage.MetricInstanceSample) []metricInstanceBody {
	if len(in) == 0 {
		return nil
	}
	out := make([]metricInstanceBody, len(in))
	for i, s := range in {
		out[i] = metricInstanceBody{Instance: s.Instance, Value: s.Value, TS: s.TS, Endpoint: s.Endpoint}
	}
	return out
}

type componentMetricsOutput struct {
	Body struct {
		Component string                `json:"component"`
		Metrics   []effectiveMetricBody `json:"metrics"`
	}
}

type systemMetricsOutput struct {
	Body struct {
		System  string                `json:"system"`
		Metrics []effectiveMetricBody `json:"metrics"`
	}
}

type locationMetricsOutput struct {
	Body struct {
		Location string                `json:"location"`
		Metrics  []effectiveMetricBody `json:"metrics"`
	}
}

// registerEffectiveMetricRoutes wires the three per-owner effective-metric
// reads, each gated by its owner's :read and carrying the matching scope so the
// gateway resolves the owner within it.
func registerEffectiveMetricRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-component-metrics",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/components/{name}/metrics",
		Summary:     "List a component's effective metrics",
		Description: "Every metric the component's product declares, resolved to the newest observed or calculated sample across its live instances (instance names the one shown, instance_count counts them) or the contract default until one arrives (is_sampled marks a live series), plus any metric sampled directly on the component (from_contract false). Gated by component:read; an out-of-scope component is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *componentPathInput) (*componentMetricsOutput, error) {
		eff, err := gw.EffectiveMetrics(ctx, "component", in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		out := &componentMetricsOutput{}
		out.Body.Component = in.Name
		out.Body.Metrics = toEffectiveMetricBodies(eff)
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-system-metrics",
		Method:      http.MethodGet,
		Path:        "/systems/{name}/metrics",
		Summary:     "List a system's effective metrics",
		Description: "Every metric the system's standard declares, resolved to the newest observed or calculated sample across its live instances (instance names the one shown, instance_count counts them) or the contract default until one arrives (is_sampled marks a live series), plus any metric sampled directly on the system (from_contract false). Gated by system:read; an out-of-scope system is a non-disclosing 404.",
	}, "system", "read"), func(ctx context.Context, in *systemPathInput) (*systemMetricsOutput, error) {
		eff, err := gw.EffectiveMetrics(ctx, "system", in.Name, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		out := &systemMetricsOutput{}
		out.Body.System = in.Name
		out.Body.Metrics = toEffectiveMetricBodies(eff)
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-location-metrics",
		Method:      http.MethodGet,
		Path:        "/locations/{name}/metrics",
		Summary:     "List a location's effective metrics",
		Description: "Every metric the location's type declares, resolved to the newest observed or calculated sample across its live instances (instance names the one shown, instance_count counts them) or the contract default until one arrives (is_sampled marks a live series), plus any metric sampled directly on the location (from_contract false). Gated by location:read; an out-of-scope location is a non-disclosing 404.",
	}, "location", "read"), func(ctx context.Context, in *locationPathInput) (*locationMetricsOutput, error) {
		eff, err := gw.EffectiveMetrics(ctx, "location", in.Name, a.scopeFor(ctx, "location", "read"))
		if err != nil {
			return nil, mapLocationErr(err)
		}
		out := &locationMetricsOutput{}
		out.Body.Location = in.Name
		out.Body.Metrics = toEffectiveMetricBodies(eff)
		return out, nil
	})
}

func toEffectiveMetricBodies(eff []storage.EffectiveMetric) []effectiveMetricBody {
	out := make([]effectiveMetricBody, 0, len(eff))
	for i := range eff {
		out = append(out, toEffectiveMetricBody(&eff[i]))
	}
	return out
}
