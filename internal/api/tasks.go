package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The task read surface: DERIVED collection work, viewable but never operator-
// authored. A task is derived when an interface is created (the node's unit of
// work over that connection) and carries no node column of its own (its placement
// projects from the interface). Both authz layers apply on every route: a
// task:read permission AND scope injected by the gateway, cascading through the
// task's interface's owning component (an out-of-scope component's task is a
// non-disclosing 404). There is no create/update/delete: authoring lives at the
// interface (create an interface, its task derives).

type taskBody struct {
	ID          string          `json:"id"`
	DisplayName string          `json:"display_name,omitempty"`
	Mode        string          `json:"mode"`
	InterfaceID string          `json:"interface_id" doc:"The interface's surrogate id this task runs over"`
	Node        *string         `json:"node,omitempty" doc:"The node placement, projected from the interface"`
	Spec        json.RawMessage `json:"spec,omitempty" doc:"The inline probe settings (jsonb)"`
	Enabled     bool            `json:"enabled"`
}

func toTaskBody(t *storage.Task) taskBody {
	b := taskBody{
		ID: t.ID, DisplayName: t.DisplayName, Mode: t.Mode,
		InterfaceID: t.InterfaceID, Node: t.Node, Enabled: t.Enabled,
	}
	if len(t.Spec) > 0 {
		b.Spec = json.RawMessage(t.Spec)
	}
	return b
}

type listTasksOutput struct {
	Body struct {
		Tasks []taskBody `json:"tasks"`
	}
}

type taskOutput struct {
	Body taskBody
}

type taskPathInput struct {
	ID string `path:"id" doc:"The task's content-addressed id"`
}

// registerTaskRoutes wires the read-only task surface, gated by task:read and
// scope-injected through the task's interface's owning component. Tasks are
// derived (from creating an interface), so there is no write surface here.
func registerTaskRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-tasks",
		Method:      http.MethodGet,
		Path:        "/tasks",
		Summary:     "List tasks in scope",
		Description: "Lists the tasks whose interface's owning component the caller may read (the component cascade). Tasks are derived from interfaces, not authored. Gated by task:read.",
	}, "task", "read"), func(ctx context.Context, _ *struct{}) (*listTasksOutput, error) {
		tasks, err := gw.ListTasks(ctx, a.scopeFor(ctx, "task", "read"))
		if err != nil {
			return nil, mapTaskErr(err)
		}
		out := &listTasksOutput{}
		out.Body.Tasks = make([]taskBody, 0, len(tasks))
		for i := range tasks {
			out.Body.Tasks = append(out.Body.Tasks, toTaskBody(&tasks[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-task",
		Method:      http.MethodGet,
		Path:        "/tasks/{id}",
		Summary:     "Get a task",
		Description: "Fetches a task by id. A task whose component is out of the caller's read scope is a non-disclosing 404. Gated by task:read.",
	}, "task", "read"), func(ctx context.Context, in *taskPathInput) (*taskOutput, error) {
		t, err := gw.GetTask(ctx, in.ID, a.scopeFor(ctx, "task", "read"))
		if err != nil {
			return nil, mapTaskErr(err)
		}
		return &taskOutput{Body: toTaskBody(t)}, nil
	})
}

func mapTaskErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrTaskNotFound):
		return huma.Error404NotFound("task not found")
	case errors.Is(err, storage.ErrTaskForbidden):
		return huma.Error403Forbidden("forbidden")
	default:
		return huma.Error500InternalServerError("task operation failed")
	}
}
