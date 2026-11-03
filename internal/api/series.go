package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The timeseries reads (#794): the samples behind the effective reads' latest
// value, per series. Without a resolution they are the raw rows, windowed and
// capped, newest first: the cap bounds the payload, and a wide window is cut to
// its tail. With a resolution (#1269) the same route answers with every regular
// bucket of the window instead (min, max, avg, count, last), so a 30-day request
// is 720 hourly buckets and never a truncated tail. The route grammar extends
// the effective reads' own: the collection at /{owner}/{name}/metrics, one
// series' history at .../metrics/{metric}/samples.
const (
	seriesDefaultWindow = 24 * time.Hour
	seriesMaxLimit      = 2000
	seriesDefaultLimit  = 500
	// seriesMaxBuckets bounds a bucketed answer's size: 30 days at 10 minutes
	// fits; a finer grid over a wide range is refused, not cut.
	seriesMaxBuckets = 5000
)

type seriesInput struct {
	Name       string `path:"name" doc:"The owner's name or uuid"`
	Metric     string `path:"metric" doc:"The metric_type name of the series"`
	Hours      int    `query:"hours" minimum:"1" maximum:"720" doc:"The window in hours, counted back from now; 24 when unset"`
	Limit      int    `query:"limit" minimum:"1" maximum:"2000" doc:"The row cap, newest kept; 500 when unset. Ignored when a resolution is set"`
	Resolution int    `query:"resolution" minimum:"1" doc:"The bucket width in seconds. Set, the read answers with every regular bucket of the window (buckets) instead of raw rows; unset, the raw samples. A window of more than 5000 buckets is refused"`
}

type propertySeriesInput struct {
	Name       string `path:"name" doc:"The owner's name or uuid"`
	Property   string `path:"property" doc:"The property_type name of the series"`
	Hours      int    `query:"hours" minimum:"1" maximum:"720" doc:"The window in hours, counted back from now; 24 when unset"`
	Limit      int    `query:"limit" minimum:"1" maximum:"2000" doc:"The row cap, newest kept; 500 when unset. Ignored when a resolution is set"`
	Resolution int    `query:"resolution" minimum:"1" doc:"The bucket width in seconds. Set, the read answers with every regular bucket of the window (buckets) instead of raw rows; unset, the raw samples. A window of more than 5000 buckets is refused"`
}

type metricSampleBody struct {
	TS         time.Time `json:"ts" doc:"When the sample was observed"`
	Value      float64   `json:"value"`
	Instance   string    `json:"instance,omitempty" doc:"The series discriminator, when set"`
	Provenance string    `json:"provenance" doc:"observed, or calculated for a derived row"`
	Source     string    `json:"source,omitempty"`
}

// metricBucketBody is one regular bucket. An empty bucket has count 0 and every
// aggregate null: present and null, so a chart tells no data from a zero.
type metricBucketBody struct {
	TS    time.Time `json:"ts" doc:"The bucket's start"`
	Count int64     `json:"count" doc:"Samples in the bucket"`
	Min   *float64  `json:"min" required:"true" nullable:"true"`
	Max   *float64  `json:"max" required:"true" nullable:"true"`
	Avg   *float64  `json:"avg" required:"true" nullable:"true"`
	Last  *float64  `json:"last" required:"true" nullable:"true" doc:"The newest sample's value in the bucket"`
}

type metricSeriesOutput struct {
	Body struct {
		Owner   string             `json:"owner" doc:"The owner's name"`
		Metric  string             `json:"metric" doc:"The series' metric_type name"`
		Samples []metricSampleBody `json:"samples" doc:"Newest first; empty when a resolution was asked for"`
		// The bucketed answer (#1269), absent from a raw read.
		ResolutionSeconds int                `json:"resolution_seconds,omitempty" required:"false" doc:"The bucket width asked for; present only on a bucketed read"`
		CoveredFrom       *time.Time         `json:"covered_from,omitempty" required:"false" doc:"The earliest instant the read could have data for: the start of the window or the series' effective retention, whichever is later. A bucket before it is empty because of retention, not because collection stopped"`
		Buckets           []metricBucketBody `json:"buckets,omitempty" required:"false" doc:"Every regular bucket of the window, oldest first"`
	}
}

type propertySampleBody struct {
	TS         time.Time `json:"ts" doc:"When the value was observed"`
	Value      string    `json:"value"`
	Instance   string    `json:"instance,omitempty" doc:"The series discriminator, when set"`
	Provenance string    `json:"provenance" doc:"observed, declared, or calculated"`
	Source     string    `json:"source,omitempty"`
}

// propertyBucketBody is one regular bucket of a property series: min, max and
// avg mean nothing to a string, so it carries the change count and the value in
// force at the bucket's end. A chart holds last across the empty buckets after it.
type propertyBucketBody struct {
	TS    time.Time `json:"ts" doc:"The bucket's start"`
	Count int64     `json:"count" doc:"Changes in the bucket"`
	Last  *string   `json:"last" required:"true" nullable:"true" doc:"The value in force at the bucket's end, from the newest change in it; null for a bucket with no change"`
}

type propertySeriesOutput struct {
	Body struct {
		Owner    string               `json:"owner" doc:"The owner's name"`
		Property string               `json:"property" doc:"The series' property_type name"`
		Samples  []propertySampleBody `json:"samples" doc:"Newest first: the change history behind the effective value; empty when a resolution was asked for"`
		// The bucketed answer (#1269), absent from a raw read.
		ResolutionSeconds int                  `json:"resolution_seconds,omitempty" required:"false" doc:"The bucket width asked for; present only on a bucketed read"`
		CoveredFrom       *time.Time           `json:"covered_from,omitempty" required:"false" doc:"The earliest instant the read could have data for: the start of the window or the series' effective retention, whichever is later"`
		Buckets           []propertyBucketBody `json:"buckets,omitempty" required:"false" doc:"Every regular bucket of the window, oldest first"`
	}
}

func seriesWindow(hours int) time.Duration {
	if hours <= 0 {
		return seriesDefaultWindow
	}
	return time.Duration(hours) * time.Hour
}

// seriesBucketing validates a bucketed request: the window, the bucket width
// and how many buckets they make, the window rounded up to a whole bucket. A
// grid past seriesMaxBuckets is refused rather than cut.
func seriesBucketing(hours, resolutionSeconds int) (window, resolution time.Duration, n int, err error) {
	window = seriesWindow(hours)
	resolution = time.Duration(resolutionSeconds) * time.Second
	n = int((window + resolution - 1) / resolution)
	if n > seriesMaxBuckets {
		return 0, 0, 0, fmt.Errorf("a %s window at %s makes %d buckets, past the %d a read answers with; widen the resolution", window, resolution, n, seriesMaxBuckets)
	}
	return window, resolution, n, nil
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
			Extensions:  mcpExposed(),
			Method:      http.MethodGet,
			Path:        "/" + oc.kind + "s/{name}/metrics/{metric}/samples",
			Summary:     "Read one metric series' samples",
			Description: "The samples behind the effective read's latest value for one series, newest first, windowed (hours) and capped (limit, newest kept). With a resolution (seconds) it answers instead with every regular bucket of the window, oldest first, each with count, min, max, avg and last (null when empty), and covered_from, the earliest instant retention leaves data for; nothing is read beyond the series' effective retention until trends exist (#1276). Gated by " + oc.resource + ":read; an out-of-scope owner is a non-disclosing 404.",
		}, oc.resource, "read"), func(ctx context.Context, in *seriesInput) (*metricSeriesOutput, error) {
			id, display, err := oc.resolve(ctx, in.Name)
			if err != nil {
				return nil, err
			}
			out := &metricSeriesOutput{}
			out.Body.Owner = display
			out.Body.Metric = in.Metric
			if in.Resolution > 0 {
				window, res, n, err := seriesBucketing(in.Hours, in.Resolution)
				if err != nil {
					return nil, huma.Error422UnprocessableEntity(err.Error())
				}
				got, err := gw.ListMetricBuckets(ctx, oc.kind, id, in.Metric, window, res)
				if err != nil {
					return nil, huma.Error500InternalServerError("read metric series buckets")
				}
				out.Body.ResolutionSeconds = in.Resolution
				out.Body.CoveredFrom = &got.CoveredFrom
				out.Body.Samples = []metricSampleBody{}
				out.Body.Buckets = make([]metricBucketBody, 0, n)
				for _, b := range got.Buckets {
					out.Body.Buckets = append(out.Body.Buckets, metricBucketBody{TS: b.TS, Count: b.Count, Min: b.Min, Max: b.Max, Avg: b.Avg, Last: b.Last})
				}
				return out, nil
			}
			rows, err := gw.ListMetricSeries(ctx, oc.kind, id, in.Metric, seriesWindow(in.Hours), seriesLimit(in.Limit))
			if err != nil {
				return nil, huma.Error500InternalServerError("read metric series")
			}
			out.Body.Samples = make([]metricSampleBody, 0, len(rows))
			for _, r := range rows {
				out.Body.Samples = append(out.Body.Samples, metricSampleBody{TS: r.TS, Value: r.Value, Instance: r.Instance, Provenance: r.Provenance, Source: r.Source})
			}
			return out, nil
		})
		huma.Register(api, a.gated(huma.Operation{
			OperationID: "list-" + oc.kind + "-property-series",
			Extensions:  mcpExposed(),
			Method:      http.MethodGet,
			Path:        "/" + oc.kind + "s/{name}/properties/{property}/samples",
			Summary:     "Read one property series' change history",
			Description: "The change history behind the effective value for one property series, newest first, windowed (hours) and capped (limit, newest kept). With a resolution (seconds) it answers instead with every regular bucket of the window, oldest first, each with the change count and the value in force at its end (null when empty), and covered_from, the earliest instant retention leaves data for. Gated by " + oc.resource + ":read; an out-of-scope owner is a non-disclosing 404.",
		}, oc.resource, "read"), func(ctx context.Context, in *propertySeriesInput) (*propertySeriesOutput, error) {
			id, display, err := oc.resolve(ctx, in.Name)
			if err != nil {
				return nil, err
			}
			out := &propertySeriesOutput{}
			out.Body.Owner = display
			out.Body.Property = in.Property
			if in.Resolution > 0 {
				window, res, n, err := seriesBucketing(in.Hours, in.Resolution)
				if err != nil {
					return nil, huma.Error422UnprocessableEntity(err.Error())
				}
				got, err := gw.ListPropertyBuckets(ctx, oc.kind, id, in.Property, window, res)
				if err != nil {
					return nil, huma.Error500InternalServerError("read property series buckets")
				}
				out.Body.ResolutionSeconds = in.Resolution
				out.Body.CoveredFrom = &got.CoveredFrom
				out.Body.Samples = []propertySampleBody{}
				out.Body.Buckets = make([]propertyBucketBody, 0, n)
				for _, b := range got.Buckets {
					out.Body.Buckets = append(out.Body.Buckets, propertyBucketBody{TS: b.TS, Count: b.Count, Last: b.Last})
				}
				return out, nil
			}
			rows, err := gw.ListPropertySeries(ctx, oc.kind, id, in.Property, seriesWindow(in.Hours), seriesLimit(in.Limit))
			if err != nil {
				return nil, huma.Error500InternalServerError("read property series")
			}
			out.Body.Samples = make([]propertySampleBody, 0, len(rows))
			for _, r := range rows {
				out.Body.Samples = append(out.Body.Samples, propertySampleBody{TS: r.TS, Value: r.Value, Instance: r.Instance, Provenance: r.Provenance, Source: r.Source})
			}
			return out, nil
		})
	}
}
