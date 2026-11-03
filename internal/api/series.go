package api

import (
	"context"
	"errors"
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
	// seriesMaxResolution is the widest bucket a read takes, 30 days in
	// seconds: the whole widest window in one bucket. Past it the request is a
	// 422 from the schema, never an overflowed duration.
	seriesMaxResolution = 30 * 24 * 60 * 60
)

type seriesInput struct {
	Name       string `path:"name" doc:"The owner's name or uuid"`
	Metric     string `path:"metric" doc:"The metric_type name of the series"`
	Hours      int    `query:"hours" minimum:"1" maximum:"720" doc:"The window in hours, counted back from now; 24 when unset"`
	Limit      int    `query:"limit" minimum:"1" maximum:"2000" doc:"The row cap, newest kept; 500 when unset. Ignored when a resolution is set"`
	Resolution int    `query:"resolution" minimum:"1" maximum:"2592000" doc:"The bucket width in seconds, at most 30 days. Set, the read answers with every regular bucket of the window (buckets) instead of raw rows; unset, the raw samples. A window of more than 5000 buckets is refused"`
	Instance   string `query:"instance" doc:"The instance a bucketed read aggregates. Unset, the one instance with measured samples in the window; when more than one has them the read is refused with a 422 listing them. Ignored without a resolution"`

	instanceSet bool
}

type propertySeriesInput struct {
	Name       string `path:"name" doc:"The owner's name or uuid"`
	Property   string `path:"property" doc:"The property_type name of the series"`
	Hours      int    `query:"hours" minimum:"1" maximum:"720" doc:"The window in hours, counted back from now; 24 when unset"`
	Limit      int    `query:"limit" minimum:"1" maximum:"2000" doc:"The row cap, newest kept; 500 when unset. Ignored when a resolution is set"`
	Resolution int    `query:"resolution" minimum:"1" maximum:"2592000" doc:"The bucket width in seconds, at most 30 days. Set, the read answers with every regular bucket of the window (buckets) instead of raw rows; unset, the raw samples. A window of more than 5000 buckets is refused"`
	Instance   string `query:"instance" doc:"The instance a bucketed read aggregates. Unset, the one instance with measured samples in the window; when more than one has them the read is refused with a 422 listing them. Ignored without a resolution"`

	instanceSet bool
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
		Instance          *string            `json:"instance,omitempty" required:"false" doc:"The one instance the buckets aggregate, measured samples only (observed and calculated); present only on a bucketed read"`
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
		Instance          *string              `json:"instance,omitempty" required:"false" doc:"The one instance the buckets aggregate, measured changes only (observed and calculated); present only on a bucketed read"`
		CoveredFrom       *time.Time           `json:"covered_from,omitempty" required:"false" doc:"The earliest instant the read could have data for: the start of the window or the series' effective retention, whichever is later"`
		Buckets           []propertyBucketBody `json:"buckets,omitempty" required:"false" doc:"Every regular bucket of the window, oldest first"`
	}
}

// Resolve notes whether instance was sent at all, so an asked-for empty
// instance (a series with no discriminator) is told apart from none asked for.
func (in *seriesInput) Resolve(ctx huma.Context) []error {
	in.instanceSet = ctx.URL().Query().Has("instance")
	return nil
}

func (in *propertySeriesInput) Resolve(ctx huma.Context) []error {
	in.instanceSet = ctx.URL().Query().Has("instance")
	return nil
}

// askedInstance is the instance a bucketed read was asked for, nil when the
// request named none.
func askedInstance(set bool, instance string) *string {
	if !set {
		return nil
	}
	return &instance
}

// bucketReadErr maps a bucketed read's failure: an ambiguous instance is the
// caller's to resolve, so it is a 422 listing every instance it can pick from,
// one error detail each at query.instance.
func bucketReadErr(err error, what string) error {
	var amb *storage.AmbiguousInstanceError
	if errors.As(err, &amb) {
		details := make([]error, 0, len(amb.Instances))
		for _, inst := range amb.Instances {
			details = append(details, &huma.ErrorDetail{Message: "an instance with measured samples in the window", Location: "query.instance", Value: inst})
		}
		return huma.Error422UnprocessableEntity(fmt.Sprintf("%s has %d instances with measured samples in the window; name one with instance, the buckets of one series never blend another's", amb.Key, len(amb.Instances)), details...)
	}
	return huma.Error500InternalServerError(what)
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
	if resolutionSeconds < 1 || resolutionSeconds > seriesMaxResolution {
		return 0, 0, 0, fmt.Errorf("a resolution of %d seconds is outside 1 to %d (30 days)", resolutionSeconds, seriesMaxResolution)
	}
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
			Description: "The samples behind the effective read's latest value for one series, newest first, windowed (hours) and capped (limit, newest kept). With a resolution (seconds) it answers instead with every regular bucket of the window, oldest first, each with count, min, max, avg and last (null when empty), and covered_from, the earliest instant retention leaves data for. A bucket aggregates one instance's measured samples (observed and calculated, never declared or intended): instance names it, and unset it is the one instance with measured samples in the window, or a 422 listing them when there are several. Nothing is read beyond the series' effective retention until trends exist (#1276). Gated by " + oc.resource + ":read; an out-of-scope owner is a non-disclosing 404.",
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
				got, err := gw.ListMetricBuckets(ctx, oc.kind, id, in.Metric, askedInstance(in.instanceSet, in.Instance), window, res)
				if err != nil {
					return nil, bucketReadErr(err, "read metric series buckets")
				}
				out.Body.ResolutionSeconds = in.Resolution
				out.Body.Instance = &got.Instance
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
			Description: "The change history behind the effective value for one property series, newest first, windowed (hours) and capped (limit, newest kept). With a resolution (seconds) it answers instead with every regular bucket of the window, oldest first, each with the change count and the value in force at its end (null when empty), and covered_from, the earliest instant retention leaves data for. A bucket aggregates one instance's measured changes (observed and calculated, never declared or intended): instance names it, and unset it is the one instance with measured changes in the window, or a 422 listing them when there are several. Gated by " + oc.resource + ":read; an out-of-scope owner is a non-disclosing 404.",
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
				got, err := gw.ListPropertyBuckets(ctx, oc.kind, id, in.Property, askedInstance(in.instanceSet, in.Instance), window, res)
				if err != nil {
					return nil, bucketReadErr(err, "read property series buckets")
				}
				out.Body.ResolutionSeconds = in.Resolution
				out.Body.Instance = &got.Instance
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
