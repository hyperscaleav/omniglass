package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The product declared-metric contract, the metric twin of the declared-property
// routes: which catalog metrics every instance of the product carries, so a
// product can ship a default sample rate. A contract line only names a catalog
// metric and says how the product presents it (an optional default, whether an
// instance must carry it); data_type, unit, and precision stay in the metric
// catalog, so they are never restated per product. The line is addressed by
// name, which makes the write a PUT upsert rather than a create/update pair.

type productMetricBody struct {
	MetricTypeName string          `json:"metric_type_name" doc:"The catalog metric this product declares"`
	MetricTypeID   string          `json:"metric_type_id" doc:"The catalog metric's uuid, the stable form of metric_type_name"`
	DefaultValue   json.RawMessage `json:"default_value,omitempty" doc:"The contract default, a number shaped by the metric's data_type; omitted when the contract sets none"`
	Required       bool            `json:"required" doc:"Whether every instance of this product must carry the metric"`
}

func toProductMetricBody(pm *storage.ProductMetric) productMetricBody {
	return productMetricBody{
		MetricTypeName: pm.MetricTypeName,
		MetricTypeID:   pm.MetricTypeID,
		DefaultValue:   json.RawMessage(pm.DefaultValue),
		Required:       pm.Required,
	}
}

type listProductMetricsOutput struct {
	Body struct {
		Metrics []productMetricBody `json:"metrics"`
	}
}

type productMetricOutput struct {
	Body productMetricBody
}

// productMetricPathInput addresses one contract line by the product and the
// metric name (the pair the contract is unique on).
type productMetricPathInput struct {
	ID     string `path:"id" doc:"The product id"`
	Metric string `path:"metric" doc:"The metric name"`
}

type setProductMetricInput struct {
	ID     string `path:"id" doc:"The product id"`
	Metric string `path:"metric" doc:"The metric name"`
	Body   struct {
		DefaultValue any  `json:"default_value,omitempty" doc:"The contract default, validated against the metric's data_type; omit for no default"`
		Required     bool `json:"required,omitempty" doc:"Whether every instance of this product must carry the metric; defaults to false"`
	}
}

// registerProductMetricRoutes wires the product's declared-metric contract, a
// sub-collection of the product registry and gated with it: the read rides the
// product:read viewer floor, the writes sit at product:update / product:delete.
// Official products ship their contract with the release, so an operator write
// against one is refused (422) the same way its registry row is.
func registerProductMetricRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-product-metrics",
		Method:      http.MethodGet,
		Path:        "/products/{id}/metrics",
		Summary:     "List a product's declared metrics",
		Description: "Lists the product's declared-metric contract (which metrics every instance of the product carries), ordered by metric name, each with its optional default and required flag. Gated by product:read.",
	}, "product", "read"), func(ctx context.Context, in *productPathInput) (*listProductMetricsOutput, error) {
		items, err := gw.ListProductMetrics(ctx, in.ID)
		if err != nil {
			return nil, mapProductMetricErr(err)
		}
		out := &listProductMetricsOutput{}
		out.Body.Metrics = make([]productMetricBody, 0, len(items))
		for i := range items {
			out.Body.Metrics = append(out.Body.Metrics, toProductMetricBody(&items[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-product-metric",
		Method:      http.MethodPut,
		Path:        "/products/{id}/metrics/{metric}",
		Summary:     "Declare a metric on a product",
		Description: "Declares a catalog metric on a custom product, or revises the declaration in place (the line is addressed by name, so the write is idempotent). Official products are read-only (422); an unknown product is a 404 and a metric the catalog does not know is a 422. Gated by product:update.",
	}, "product", "update"), func(ctx context.Context, in *setProductMetricInput) (*productMetricOutput, error) {
		def, err := encodePropertyJSON(in.Body.DefaultValue, "default_value")
		if err != nil {
			return nil, err
		}
		pm, err := gw.SetProductMetric(ctx, actorID(ctx), in.ID, storage.ProductMetricSpec{
			MetricTypeName: in.Metric,
			DefaultValue:   def,
			Required:       in.Body.Required,
		})
		if err != nil {
			return nil, mapProductMetricErr(err)
		}
		return &productMetricOutput{Body: toProductMetricBody(pm)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-product-metric",
		Method:        http.MethodDelete,
		Path:          "/products/{id}/metrics/{metric}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Withdraw a metric from a product",
		Description:   "Removes one line from a custom product's contract; instances keep any samples the series already holds, now off-contract. A metric the product does not declare is a 404, and an official product is read-only (422). Gated by product:delete.",
	}, "product", "delete"), func(ctx context.Context, in *productMetricPathInput) (*struct{}, error) {
		if err := gw.DeleteProductMetric(ctx, actorID(ctx), in.ID, in.Metric); err != nil {
			return nil, mapProductMetricErr(err)
		}
		return nil, nil
	})
}

// mapProductMetricErr translates the contract storage sentinels into HTTP
// status. A metric the catalog does not know is a 422 (the request names a
// metric that does not exist), everything else falls through to the shared
// type-registry mapping, where the product is the type: not-found 404, official
// read-only 422.
func mapProductMetricErr(err error) error {
	if errors.Is(err, storage.ErrMetricTypeNotFound) {
		return huma.Error422UnprocessableEntity("unknown metric")
	}
	return mapTypeErr(err, "product metric")
}
