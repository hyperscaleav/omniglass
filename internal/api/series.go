package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The first timeseries reads (#794): the raw samples behind the effective
// reads' latest value, per series, windowed and capped. Raw rows with a hard
// cap rather than server-side downsampling: the cap bounds the payload, and
// aggregation waits for a measured need (the epic's thin cut). The route
// grammar extends the effective reads' own: the collection at
// /{owner}/{name}/metrics, one series' history at .../metrics/{metric}/samples.
const (
	seriesDefaultWindow = 24 * time.Hour
	seriesMaxLimit      = 2000
	seriesDefaultLimit  = 500
)

type seriesInput struct {
	Name   string `path:"name" doc:"The owner's name or uuid"`
	Metric string `path:"metric" doc:"The metric_type name of the series"`
	Hours  int    `query:"hours" minimum:"1" maximum:"720" doc:"The window in hours, counted back from now; 24 when unset"`
	Limit  int    `query:"limit" minimum:"1" maximum:"2000" doc:"The row cap, newest kept; 500 when unset"`
}

type propertySeriesInput struct {
	Name     string `path:"name" doc:"The owner's name or uuid"`
	Property string `path:"property" doc:"The property_type name of the series"`
	Hours    int    `query:"hours" minimum:"1" maximum:"720" doc:"The window in hours, counted back from now; 24 when unset"`
	Limit    int    `query:"limit" minimum:"1" maximum:"2000" doc:"The row cap, newest kept; 500 when unset"`
}

type metricSampleBody struct {
	TS         time.Time `json:"ts" doc:"When the sample was observed"`
	Value      float64   `json:"value"`
	Instance   string    `json:"instance,omitempty" doc:"The series discriminator, when set"`
	Provenance string    `json:"provenance" doc:"observed, or calculated for a derived row"`
	Source     string    `json:"source,omitempty"`
}

type metricSeriesOutput struct {
	Body struct {
		Owner   string             `json:"owner" doc:"The owner's name"`
		Metric  string             `json:"metric" doc:"The series' metric_type name"`
		Samples []metricSampleBody `json:"samples" doc:"Newest first"`
	}
}

type propertySampleBody struct {
	TS         time.Time `json:"ts" doc:"When the value was observed"`
	Value      string    `json:"value"`
	Instance   string    `json:"instance,omitempty" doc:"The series discriminator, when set"`
	Provenance string    `json:"provenance" doc:"observed, declared, or calculated"`
	Source     string    `json:"source,omitempty"`
}

type propertySeriesOutput struct {
	Body struct {
		Owner    string               `json:"owner" doc:"The owner's name"`
		Property string               `json:"property" doc:"The series' property_type name"`
		Samples  []propertySampleBody `json:"samples" doc:"Newest first: the change history behind the effective value"`
	}
}

func seriesWindow(hours int) time.Duration {
	if hours <= 0 {
		return seriesDefaultWindow
	}
	return time.Duration(hours) * time.Hour
}

func seriesLimit(limit int) int {
	if limit <= 0 {
		return seriesDefaultLimit
	}
	if limit > seriesMaxLimit {
		return seriesMaxLimit
	}
	return limit
}

func registerSeriesRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	type ownerCfg struct {
		kind     string
		resource string
		resolve  func(ctx context.Context, name string) (id, display string, err error)
	}
	owners := []ownerCfg{
		{kind: "component", resource: "component", resolve: func(ctx context.Context, name string) (string, string, error) {
			c, err := gw.GetComponent(ctx, name, a.scopeFor(ctx, "component", "read"))
			if err != nil {
				return "", "", mapComponentErr(err)
			}
			return c.ID, c.Name, nil
		}},
		{kind: "system", resource: "system", resolve: func(ctx context.Context, name string) (string, string, error) {
			s, err := gw.GetSystem(ctx, name, a.scopeFor(ctx, "system", "read"))
			if err != nil {
				return "", "", mapSystemErr(err)
			}
			return s.ID, s.Name, nil
		}},
	}
	for _, oc := range owners {
		oc := oc
		huma.Register(api, a.gated(huma.Operation{
			OperationID: "list-" + oc.kind + "-metric-series",
			Method:      http.MethodGet,
			Path:        "/" + oc.kind + "s/{name}/metrics/{metric}/samples",
			Summary:     "Read one metric series' raw samples",
			Description: "The samples behind the effective read's latest value for one series, newest first, windowed (hours) and capped (limit, newest kept). Gated by " + oc.resource + ":read; an out-of-scope owner is a non-disclosing 404.",
		}, oc.resource, "read"), func(ctx context.Context, in *seriesInput) (*metricSeriesOutput, error) {
			id, display, err := oc.resolve(ctx, in.Name)
			if err != nil {
				return nil, err
			}
			rows, err := gw.ListMetricSeries(ctx, oc.kind, id, in.Metric, seriesWindow(in.Hours), seriesLimit(in.Limit))
			if err != nil {
				return nil, huma.Error500InternalServerError("read metric series")
			}
			out := &metricSeriesOutput{}
			out.Body.Owner = display
			out.Body.Metric = in.Metric
			out.Body.Samples = make([]metricSampleBody, 0, len(rows))
			for _, r := range rows {
				out.Body.Samples = append(out.Body.Samples, metricSampleBody{TS: r.TS, Value: r.Value, Instance: r.Instance, Provenance: r.Provenance, Source: r.Source})
			}
			return out, nil
		})
		huma.Register(api, a.gated(huma.Operation{
			OperationID: "list-" + oc.kind + "-property-series",
			Method:      http.MethodGet,
			Path:        "/" + oc.kind + "s/{name}/properties/{property}/samples",
			Summary:     "Read one property series' change history",
			Description: "The change history behind the effective value for one property series, newest first, windowed (hours) and capped (limit, newest kept). Gated by " + oc.resource + ":read; an out-of-scope owner is a non-disclosing 404.",
		}, oc.resource, "read"), func(ctx context.Context, in *propertySeriesInput) (*propertySeriesOutput, error) {
			id, display, err := oc.resolve(ctx, in.Name)
			if err != nil {
				return nil, err
			}
			rows, err := gw.ListPropertySeries(ctx, oc.kind, id, in.Property, seriesWindow(in.Hours), seriesLimit(in.Limit))
			if err != nil {
				return nil, huma.Error500InternalServerError("read property series")
			}
			out := &propertySeriesOutput{}
			out.Body.Owner = display
			out.Body.Property = in.Property
			out.Body.Samples = make([]propertySampleBody, 0, len(rows))
			for _, r := range rows {
				out.Body.Samples = append(out.Body.Samples, propertySampleBody{TS: r.TS, Value: r.Value, Instance: r.Instance, Provenance: r.Provenance, Source: r.Source})
			}
			return out, nil
		})
	}
}
