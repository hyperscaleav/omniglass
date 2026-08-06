package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The standard declared-metric contract: the system-side mirror of the product
// metric contract, and the metric twin of the standard property routes. A
// contract line only names a catalog metric and says how the standard presents
// it (an optional default, whether a conforming system must carry it);
// data_type, unit, and precision stay in the metric catalog. The line is
// addressed by name, which makes the write a PUT upsert rather than a
// create/update pair.

type standardMetricBody struct {
	MetricTypeName string          `json:"metric_type_name" doc:"The catalog metric this standard declares"`
	MetricTypeID   string          `json:"metric_type_id" doc:"The catalog metric's uuid, the stable form of metric_type_name"`
	DefaultValue   json.RawMessage `json:"default_value,omitempty" doc:"The contract default, a number shaped by the metric's data_type; omitted when the contract sets none"`
	Required       bool            `json:"required" doc:"Whether every system conforming to this standard must carry the metric"`
}

func toStandardMetricBody(sm *storage.StandardMetric) standardMetricBody {
	return standardMetricBody{
		MetricTypeName: sm.MetricTypeName,
		MetricTypeID:   sm.MetricTypeID,
		DefaultValue:   json.RawMessage(sm.DefaultValue),
		Required:       sm.Required,
	}
}

type listStandardMetricsOutput struct {
	Body struct {
		Metrics []standardMetricBody `json:"metrics"`
	}
}

type standardMetricOutput struct {
	Body standardMetricBody
}

// standardMetricPathInput addresses one contract line by the standard and the
// metric name (the pair the contract is unique on).
type standardMetricPathInput struct {
	ID     string `path:"id" doc:"The standard id"`
	Metric string `path:"metric" doc:"The metric name"`
}

type setStandardMetricInput struct {
	ID     string `path:"id" doc:"The standard id"`
	Metric string `path:"metric" doc:"The metric name"`
	Body   struct {
		DefaultValue any  `json:"default_value,omitempty" doc:"The contract default, validated against the metric's data_type; omit for no default"`
		Required     bool `json:"required,omitempty" doc:"Whether every system conforming to this standard must carry the metric; defaults to false"`
	}
}

// registerStandardMetricRoutes wires the standard's declared-metric contract, a
// sub-collection of the standard catalog and gated with it: the read rides the
// standard:read viewer floor, the writes sit at standard:update /
// standard:delete. Official standards ship their contract with the release, so
// an operator write against one is refused (422) the same way its catalog row is.
func registerStandardMetricRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-standard-metrics",
		Method:      http.MethodGet,
		Path:        "/standards/{id}/metrics",
		Summary:     "List a standard's declared metrics",
		Description: "Lists the standard's declared-metric contract (which metrics every system conforming to it carries), ordered by metric name, each with its optional default and required flag. Gated by standard:read.",
	}, "standard", "read"), func(ctx context.Context, in *standardPathInput) (*listStandardMetricsOutput, error) {
		items, err := gw.ListStandardMetrics(ctx, in.ID)
		if err != nil {
			return nil, mapStandardMetricErr(err)
		}
		out := &listStandardMetricsOutput{}
		out.Body.Metrics = make([]standardMetricBody, 0, len(items))
		for i := range items {
			out.Body.Metrics = append(out.Body.Metrics, toStandardMetricBody(&items[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-standard-metric",
		Method:      http.MethodPut,
		Path:        "/standards/{id}/metrics/{metric}",
		Summary:     "Declare a metric on a standard",
		Description: "Declares a catalog metric on a custom standard, or revises the declaration in place (the line is addressed by name, so the write is idempotent). Official standards are read-only (422); an unknown standard is a 404 and a metric the catalog does not know is a 422. Gated by standard:update.",
	}, "standard", "update"), func(ctx context.Context, in *setStandardMetricInput) (*standardMetricOutput, error) {
		def, err := encodePropertyJSON(in.Body.DefaultValue, "default_value")
		if err != nil {
			return nil, err
		}
		sm, err := gw.SetStandardMetric(ctx, actorID(ctx), in.ID, storage.StandardMetricSpec{
			MetricTypeName: in.Metric,
			DefaultValue:   def,
			Required:       in.Body.Required,
		})
		if err != nil {
			return nil, mapStandardMetricErr(err)
		}
		return &standardMetricOutput{Body: toStandardMetricBody(sm)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-standard-metric",
		Method:        http.MethodDelete,
		Path:          "/standards/{id}/metrics/{metric}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Withdraw a metric from a standard",
		Description:   "Removes one line from a custom standard's contract; conforming systems keep any samples the series already holds, now off-contract. A metric the standard does not declare is a 404, and an official standard is read-only (422). Gated by standard:delete.",
	}, "standard", "delete"), func(ctx context.Context, in *standardMetricPathInput) (*struct{}, error) {
		if err := gw.DeleteStandardMetric(ctx, actorID(ctx), in.ID, in.Metric); err != nil {
			return nil, mapStandardMetricErr(err)
		}
		return nil, nil
	})
}

// mapStandardMetricErr translates the contract storage sentinels into HTTP
// status, mirroring the product metric contract: a metric the catalog does not
// know is a 422 (the request names a metric that does not exist), everything
// else falls through to the shared type-registry mapping, where the standard is
// the type: not-found 404, official read-only 422.
func mapStandardMetricErr(err error) error {
	if errors.Is(err, storage.ErrMetricTypeNotFound) {
		return huma.Error422UnprocessableEntity("unknown metric")
	}
	return mapTypeErr(err, "standard metric")
}
