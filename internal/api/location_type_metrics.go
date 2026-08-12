package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The location type declared-metric contract: the place-side mirror of the
// product and standard metric contracts, and the metric twin of the location
// type property routes. A contract line only names a catalog metric and says
// how the type presents it (an optional default, whether a location of the type
// must carry it); data_type, unit, and precision stay in the metric catalog.
// The line is addressed by name, which makes the write a PUT upsert rather than
// a create/update pair.

type locationTypeMetricBody struct {
	MetricTypeName string          `json:"metric_type_name" doc:"The catalog metric this location type declares"`
	MetricTypeID   string          `json:"metric_type_id" doc:"The catalog metric's uuid, the stable form of metric_type_name"`
	DefaultValue   json.RawMessage `json:"default_value,omitempty" doc:"The contract default, a number shaped by the metric's data_type; omitted when the contract sets none"`
	Required       bool            `json:"required" doc:"Whether every location of this type must carry the metric"`
}

func toLocationTypeMetricBody(lm *storage.LocationTypeMetric) locationTypeMetricBody {
	return locationTypeMetricBody{
		MetricTypeName: lm.MetricTypeName,
		MetricTypeID:   lm.MetricTypeID,
		DefaultValue:   json.RawMessage(lm.DefaultValue),
		Required:       lm.Required,
	}
}

type listLocationTypeMetricsOutput struct {
	Body struct {
		Metrics []locationTypeMetricBody `json:"metrics"`
	}
}

type locationTypeMetricOutput struct {
	Body locationTypeMetricBody
}

// locationTypeMetricPathInput addresses one contract line by the location type
// and the metric name (the pair the contract is unique on).
type locationTypeMetricPathInput struct {
	ID     string `path:"id" doc:"The location type id"`
	Metric string `path:"metric" doc:"The metric name"`
}

type setLocationTypeMetricInput struct {
	ID     string `path:"id" doc:"The location type id"`
	Metric string `path:"metric" doc:"The metric name"`
	Body   struct {
		DefaultValue any  `json:"default_value,omitempty" doc:"The contract default, validated against the metric's data_type; omit for no default"`
		Required     bool `json:"required,omitempty" doc:"Whether every location of this type must carry the metric; defaults to false"`
	}
}

// registerLocationTypeMetricRoutes wires the location type's declared-metric
// contract. The registry it hangs off is gated by location_type:*, so the
// contract keeps that permission story: the read rides the location_type:read
// viewer floor, the writes sit at location_type:update / location_type:delete.
// The writes do NOT consult the official flag (#703, ADR-0106): a contract line
// is a row in its own table rather than a column of the registry row a fork
// covers, and nothing seeds one, so every line in this table is an operator's
// already. Refusing them on the four shipped types would have withdrawn the
// contract editor for the types an estate actually uses.
func registerLocationTypeMetricRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-location-type-metrics",
		Method:      http.MethodGet,
		Path:        "/location-types/{id}/metrics",
		Summary:     "List a location type's declared metrics",
		Description: "Lists the location type's declared-metric contract (which metrics every location of the type carries), ordered by metric name, each with its optional default and required flag. Gated by location_type:read.",
	}, "location_type", "read"), func(ctx context.Context, in *locationTypePathInput) (*listLocationTypeMetricsOutput, error) {
		items, err := gw.ListLocationTypeMetrics(ctx, in.ID)
		if err != nil {
			return nil, mapLocationTypeMetricErr(err)
		}
		out := &listLocationTypeMetricsOutput{}
		out.Body.Metrics = make([]locationTypeMetricBody, 0, len(items))
		for i := range items {
			out.Body.Metrics = append(out.Body.Metrics, toLocationTypeMetricBody(&items[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-location-type-metric",
		Method:      http.MethodPut,
		Path:        "/location-types/{id}/metrics/{metric}",
		Summary:     "Declare a metric on a location type",
		Description: "Declares a catalog metric on a location type, or revises the declaration in place (the line is addressed by name, so the write is idempotent). A shipped (official) type's contract is writable too: a contract line is a row in its own table and nothing seeds one, so every line is an operator's. An unknown type is a 404 and a metric the catalog does not know is a 422. Gated by location_type:update.",
	}, "location_type", "update"), func(ctx context.Context, in *setLocationTypeMetricInput) (*locationTypeMetricOutput, error) {
		def, err := encodePropertyJSON(in.Body.DefaultValue, "default_value")
		if err != nil {
			return nil, err
		}
		lm, err := gw.SetLocationTypeMetric(ctx, actorID(ctx), in.ID, storage.LocationTypeMetricSpec{
			MetricTypeName: in.Metric,
			DefaultValue:   def,
			Required:       in.Body.Required,
		})
		if err != nil {
			return nil, mapLocationTypeMetricErr(err)
		}
		return &locationTypeMetricOutput{Body: toLocationTypeMetricBody(lm)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-location-type-metric",
		Method:        http.MethodDelete,
		Path:          "/location-types/{id}/metrics/{metric}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Withdraw a metric from a location type",
		Description:   "Removes one line from a location type's contract, a shipped (official) type's included, since nothing seeds a contract line; locations of the type keep any samples the series already holds, now off-contract. A metric the type does not declare is a 404, and so is an unknown type. Gated by location_type:delete.",
	}, "location_type", "delete"), func(ctx context.Context, in *locationTypeMetricPathInput) (*struct{}, error) {
		if err := gw.DeleteLocationTypeMetric(ctx, actorID(ctx), in.ID, in.Metric); err != nil {
			return nil, mapLocationTypeMetricErr(err)
		}
		return nil, nil
	})
}

// mapLocationTypeMetricErr translates the contract storage sentinels into HTTP
// status, mirroring the product metric contract: a metric the catalog does not
// know is a 422 (the request names a metric that does not exist), everything
// else falls through to the shared type-registry mapping, of which these paths
// reach the not-found 404. They cannot reach its official read-only 422: the
// guard here is requireRegistryRow, which asks whether the type EXISTS and never
// who owns it (#703, ADR-0106).
func mapLocationTypeMetricErr(err error) error {
	if errors.Is(err, storage.ErrMetricTypeNotFound) {
		return huma.Error422UnprocessableEntity("unknown metric")
	}
	return mapTypeErr(err, "location_type metric")
}
