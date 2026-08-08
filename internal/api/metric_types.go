package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// metricTypeBody is the wire shape of a metric type: the numeric lane of the
// signal catalog (#587), carrying the numeric facts (unit, precision) the
// property lane never holds. official marks a seed-owned, read-only type.
type metricTypeBody struct {
	ID          string  `json:"id" doc:"The metric type's uuid, the stable form the contract and telemetry keys store"`
	Name        string  `json:"name"`
	DataType    string  `json:"data_type"`
	DisplayName string  `json:"display_name,omitempty"`
	Description string  `json:"description,omitempty"`
	Unit        *string `json:"unit,omitempty" doc:"The display unit of the series (ms, dB, percent)"`
	Precision   *int    `json:"precision,omitempty" doc:"Decimal places a rendered value keeps"`
	Official    bool    `json:"official"`
}

func toMetricTypeBody(mt *storage.MetricType) metricTypeBody {
	return metricTypeBody{
		ID: mt.ID, Name: mt.Name, DataType: mt.DataType, DisplayName: mt.DisplayName,
		Description: mt.Description, Unit: mt.Unit, Precision: mt.Precision, Official: mt.Official,
	}
}

type listMetricTypesOutput struct {
	Body struct {
		MetricTypes []metricTypeBody `json:"metric_types"`
	}
}

type metricTypeOutput struct {
	Body metricTypeBody
}

type createMetricTypeInput struct {
	Body struct {
		Name        string  `json:"name" minLength:"1" doc:"The metric type name (lowercase kebab)"`
		DataType    string  `json:"data_type" enum:"int,float" doc:"The value type; a metric is always a number"`
		DisplayName string  `json:"display_name,omitempty" doc:"A human label"`
		Description string  `json:"description,omitempty" doc:"What the series measures"`
		Unit        *string `json:"unit,omitempty" doc:"The display unit of the series (ms, dB, percent)"`
		Precision   *int    `json:"precision,omitempty" doc:"Decimal places a rendered value keeps"`
	}
}

type metricTypeNameInput struct {
	Name string `path:"name" doc:"The metric type's name"`
}

type updateMetricTypeInput struct {
	Name string `path:"name" doc:"The metric type's name"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty" doc:"A human label"`
		Description *string `json:"description,omitempty" doc:"What the series measures"`
		Unit        *string `json:"unit,omitempty" doc:"The display unit of the series"`
		Precision   *int    `json:"precision,omitempty" doc:"Decimal places a rendered value keeps"`
	}
}

// registerMetricTypeRoutes wires the metric_type catalog: the estate-wide numeric
// signal directory (no scope injection, it is reference data) and its custom-type
// CRUD. Read rides the viewer floor; create/update/delete are gated by
// metric_type:create / metric_type:update / metric_type:delete. Official
// (seed-owned) metric types are read-only. Mirrors the property_type catalog.
func registerMetricTypeRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-metric-type",
		Method:      http.MethodGet,
		Path:        "/metric-types",
		Summary:     "List metric types",
		Description: "Lists every registered metric type (official and custom). The catalog is estate-wide reference data. Gated by metric_type:read.",
	}, "metric_type", "read"), func(ctx context.Context, _ *struct{}) (*listMetricTypesOutput, error) {
		mts, err := gw.ListMetricTypes(ctx)
		if err != nil {
			return nil, mapMetricTypeErr(err)
		}
		out := &listMetricTypesOutput{}
		out.Body.MetricTypes = make([]metricTypeBody, 0, len(mts))
		for i := range mts {
			out.Body.MetricTypes = append(out.Body.MetricTypes, toMetricTypeBody(&mts[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-metric-type",
		Method:      http.MethodGet,
		Path:        "/metric-types/{name}",
		Summary:     "Get a metric type",
		Description: "Returns one metric type by name. Gated by metric_type:read.",
	}, "metric_type", "read"), func(ctx context.Context, in *metricTypeNameInput) (*metricTypeOutput, error) {
		mt, err := gw.GetMetricType(ctx, in.Name)
		if err != nil {
			return nil, mapMetricTypeErr(err)
		}
		return &metricTypeOutput{Body: toMetricTypeBody(mt)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-metric-type",
		Method:        http.MethodPost,
		Path:          "/metric-types",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a metric type",
		Description:   "Registers a custom metric type (official=false). The name must be a valid metric key. Gated by metric_type:create.",
	}, "metric_type", "create"), func(ctx context.Context, in *createMetricTypeInput) (*metricTypeOutput, error) {
		mt, err := gw.CreateMetricType(ctx, actorID(ctx), storage.MetricTypeSpec{
			Name:        in.Body.Name,
			DataType:    in.Body.DataType,
			DisplayName: in.Body.DisplayName,
			Description: in.Body.Description,
			Unit:        in.Body.Unit,
			Precision:   in.Body.Precision,
		})
		if err != nil {
			return nil, mapMetricTypeErr(err)
		}
		return &metricTypeOutput{Body: toMetricTypeBody(mt)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-metric-type",
		Method:      http.MethodPatch,
		Path:        "/metric-types/{name}",
		Summary:     "Update a metric type",
		Description: "Patches a custom metric type's label, description, unit, or precision (a nil field is unchanged). Data type is fixed at creation. Official metric types are read-only. Gated by metric_type:update.",
	}, "metric_type", "update"), func(ctx context.Context, in *updateMetricTypeInput) (*metricTypeOutput, error) {
		mt, err := gw.UpdateMetricType(ctx, actorID(ctx), in.Name, storage.MetricTypePatch{
			DisplayName: in.Body.DisplayName,
			Description: in.Body.Description,
			Unit:        in.Body.Unit,
			Precision:   in.Body.Precision,
		})
		if err != nil {
			return nil, mapMetricTypeErr(err)
		}
		return &metricTypeOutput{Body: toMetricTypeBody(mt)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-metric-type",
		Method:        http.MethodDelete,
		Path:          "/metric-types/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a metric type",
		Description:   "Removes a custom metric type by name. Official metric types are read-only. Gated by metric_type:delete.",
	}, "metric_type", "delete"), func(ctx context.Context, in *metricTypeNameInput) (*struct{}, error) {
		if err := gw.DeleteMetricType(ctx, actorID(ctx), in.Name); err != nil {
			return nil, mapMetricTypeErr(err)
		}
		return nil, nil
	})
}

// mapMetricTypeErr translates the gateway's metric-type sentinels into HTTP
// status. mapRefErr runs first: a metric type's name stays a single global
// token (#627 Task 12), so a dotted ref is storage.ErrAddressNotAccepted, a
// 422 naming the kind, rather than falling through to the plain not-found
// below.
func mapMetricTypeErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrMetricTypeNotFound):
		return huma.Error404NotFound("metric type not found")
	case errors.Is(err, storage.ErrMetricTypeExists):
		return huma.Error409Conflict("a metric type with this name already exists")
	case errors.Is(err, storage.ErrMetricTypeOfficial):
		return huma.Error409Conflict("an official metric type is read-only")
	case errors.Is(err, storage.ErrMetricTypeInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		return huma.Error500InternalServerError("metric type operation failed")
	}
}
