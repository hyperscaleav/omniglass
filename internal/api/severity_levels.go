package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/health"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type severityLevelBody struct {
	ID       string `json:"id" doc:"The level's uuid, the stable form of name"`
	Name     string `json:"name" doc:"The level's name; alarms and rules name it"`
	Label    string `json:"label,omitempty"`
	Color    string `json:"color" enum:"gray,blue,green,yellow,orange,red,darkred" doc:"The hue the console renders the level in"`
	Rank     int    `json:"rank" doc:"Compares levels: higher is more severe"`
	Official bool   `json:"official" doc:"Shipped with the platform and read-only"`
	Outage   bool   `json:"outage" doc:"Whether an active alarm at this level puts its component in outage (at or above the high level's rank); below it, an alarm degrades"`
}

func toSeverityLevelBody(l *storage.SeverityLevel) severityLevelBody {
	return severityLevelBody{ID: l.ID, Name: l.Name, Label: l.Label, Color: l.Color, Rank: l.Rank,
		Official: l.Official, Outage: l.Rank >= health.OutageRank}
}

type listSeverityLevelsOutput struct {
	Body struct {
		SeverityLevels []severityLevelBody `json:"severity_levels"`
	}
}

type severityLevelOutput struct{ Body severityLevelBody }

type severityLevelNameInput struct {
	Name string `path:"name" doc:"The level's name"`
}

type createSeverityLevelInput struct {
	Body struct {
		Name  string `json:"name" minLength:"1" doc:"The level's name (lowercase kebab)"`
		Label string `json:"label,omitempty" required:"false" doc:"A human label"`
		Color string `json:"color" doc:"One of gray, blue, green, yellow, orange, red, darkred"`
		Rank  int    `json:"rank" doc:"Its place among the levels: higher is more severe; unique"`
	}
}

type updateSeverityLevelInput struct {
	Name string `path:"name" doc:"The level's name"`
	Body struct {
		Name  *string `json:"name,omitempty" required:"false" doc:"A new name; every alarm at this level follows"`
		Label *string `json:"label,omitempty" required:"false" doc:"A human label"`
		Color *string `json:"color,omitempty" required:"false" doc:"One of gray, blue, green, yellow, orange, red, darkred"`
		Rank  *int    `json:"rank,omitempty" required:"false" doc:"Its place among the levels; unique"`
	}
}

// registerSeverityLevelRoutes wires the severity registry (#917): fleet-wide
// reference data every reader lists, and operator levels an admin adds,
// renames, recolors, re-ranks and deletes. Official levels are read-only.
func registerSeverityLevelRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-severity-level",
		Method:      http.MethodGet,
		Path:        "/severity-levels",
		Summary:     "List severity levels",
		Description: "Lists the severity levels, least to most severe, each with whether an active alarm at it puts a component in outage. Gated by severity_level:read.",
	}, "severity_level", "read"), func(ctx context.Context, _ *struct{}) (*listSeverityLevelsOutput, error) {
		levels, err := gw.ListSeverityLevels(ctx)
		if err != nil {
			return nil, mapSeverityLevelErr(err)
		}
		out := &listSeverityLevelsOutput{}
		out.Body.SeverityLevels = make([]severityLevelBody, 0, len(levels))
		for i := range levels {
			out.Body.SeverityLevels = append(out.Body.SeverityLevels, toSeverityLevelBody(&levels[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-severity-level",
		Method:      http.MethodGet,
		Path:        "/severity-levels/{name}",
		Summary:     "Get a severity level",
		Description: "Returns one severity level by name. Gated by severity_level:read.",
	}, "severity_level", "read"), func(ctx context.Context, in *severityLevelNameInput) (*severityLevelOutput, error) {
		l, err := gw.GetSeverityLevel(ctx, in.Name)
		if err != nil {
			return nil, mapSeverityLevelErr(err)
		}
		return &severityLevelOutput{Body: toSeverityLevelBody(l)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-severity-level",
		Method:        http.MethodPost,
		Path:          "/severity-levels",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a severity level",
		Description:   "Adds an operator severity level at a rank of its own, between two others without renumbering. A name or rank already taken is a 409; a color outside the palette is a 422. Audited. Gated by severity_level:create.",
	}, "severity_level", "create"), func(ctx context.Context, in *createSeverityLevelInput) (*severityLevelOutput, error) {
		l, err := gw.CreateSeverityLevel(ctx, actorID(ctx), storage.SeverityLevel{
			Name: in.Body.Name, Label: in.Body.Label, Color: in.Body.Color, Rank: in.Body.Rank})
		if err != nil {
			return nil, mapSeverityLevelErr(err)
		}
		return &severityLevelOutput{Body: toSeverityLevelBody(l)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-severity-level",
		Method:      http.MethodPatch,
		Path:        "/severity-levels/{name}",
		Summary:     "Update a severity level",
		Description: "Patches an operator level's name, label, color or rank (an absent field is unchanged); a rename carries every alarm at the level with it. Official levels are read-only (409). Audited. Gated by severity_level:update.",
	}, "severity_level", "update"), func(ctx context.Context, in *updateSeverityLevelInput) (*severityLevelOutput, error) {
		l, err := gw.UpdateSeverityLevel(ctx, actorID(ctx), in.Name, storage.SeverityLevelPatch{
			Name: in.Body.Name, Label: in.Body.Label, Color: in.Body.Color, Rank: in.Body.Rank})
		if err != nil {
			return nil, mapSeverityLevelErr(err)
		}
		return &severityLevelOutput{Body: toSeverityLevelBody(l)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-severity-level",
		Method:        http.MethodDelete,
		Path:          "/severity-levels/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a severity level",
		Description:   "Removes an operator level no alarm or event rule names; an official level or one in use is a 409. Audited. Gated by severity_level:delete.",
	}, "severity_level", "delete"), func(ctx context.Context, in *severityLevelNameInput) (*struct{}, error) {
		if err := gw.DeleteSeverityLevel(ctx, actorID(ctx), in.Name); err != nil {
			return nil, mapSeverityLevelErr(err)
		}
		return nil, nil
	})
}

func mapSeverityLevelErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrSeverityLevelNotFound):
		return huma.Error404NotFound("severity level not found")
	case errors.Is(err, storage.ErrSeverityLevelOfficial):
		return huma.Error409Conflict("an official severity level is read-only")
	case errors.Is(err, storage.ErrSeverityLevelExists):
		return huma.Error409Conflict("a severity level already has that name or rank")
	case errors.Is(err, storage.ErrSeverityLevelInUse):
		return huma.Error409Conflict("an alarm or an event rule still names this severity level")
	case errors.Is(err, storage.ErrSeverityLevelInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	}
	return huma.Error500InternalServerError("severity level operation failed")
}
