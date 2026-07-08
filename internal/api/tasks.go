package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The task CRUD surface: operator authoring of content-addressed collection work.
// Both authz layers apply on every route: a task:<action> permission AND scope
// injected by the gateway, cascading through the task's interface's owning
// component (an out-of-scope component's task is a non-disclosing 404). Spec is
// the inline probe jsonb, passed through as raw JSON. The id is content-addressed
// server-side (interface + mode + spec), so a create is idempotent on identical
// work.

type taskBody struct {
	ID          string          `json:"id"`
	DisplayName string          `json:"display_name,omitempty"`
	Mode        string          `json:"mode"`
	InterfaceID string          `json:"interface_id" doc:"The interface's surrogate id this task runs over"`
	Node        *string         `json:"node,omitempty" doc:"The node placement name, if assigned"`
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

type createTaskInput struct {
	Body struct {
		DisplayName string          `json:"display_name,omitempty"`
		Mode        string          `json:"mode" enum:"poll,listen" doc:"The poll/listen axis"`
		InterfaceID string          `json:"interface_id" format:"uuid" doc:"The interface id this task runs over"`
		Node        *string         `json:"node,omitempty" doc:"Node placement name"`
		Spec        json.RawMessage `json:"spec,omitempty" doc:"Inline probe settings (jsonb)"`
		Enabled     *bool           `json:"enabled,omitempty" doc:"Whether the task is on the worklist; defaults to true"`
	}
}

type updateTaskInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName *string         `json:"display_name,omitempty"`
		Enabled     *bool           `json:"enabled,omitempty" doc:"Toggle the task on/off the worklist"`
		Node        *string         `json:"node,omitempty" doc:"Reassign the node placement"`
		Spec        json.RawMessage `json:"spec,omitempty" doc:"Replace the inline probe settings (jsonb)"`
	}
}

// registerTaskRoutes wires the task CRUD surface, gated by task:<action> and
// scope-injected through the task's interface's owning component.
func registerTaskRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, huma.Operation{
		OperationID: "list-tasks",
		Method:      http.MethodGet,
		Path:        "/tasks",
		Summary:     "List tasks in scope",
		Description: "Lists the tasks whose interface's owning component the caller may read (the component cascade). Gated by task:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("task", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listTasksOutput, error) {
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

	huma.Register(api, huma.Operation{
		OperationID: "get-task",
		Method:      http.MethodGet,
		Path:        "/tasks/{id}",
		Summary:     "Get a task",
		Description: "Fetches a task by id. A task whose component is out of the caller's read scope is a non-disclosing 404. Gated by task:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("task", "read")},
	}, func(ctx context.Context, in *taskPathInput) (*taskOutput, error) {
		t, err := gw.GetTask(ctx, in.ID, a.scopeFor(ctx, "task", "read"))
		if err != nil {
			return nil, mapTaskErr(err)
		}
		return &taskOutput{Body: toTaskBody(t)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-task",
		Method:        http.MethodPost,
		Path:          "/tasks",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a task",
		Description:   "Creates a content-addressed task over an interface. The create scope cascades through the interface's owning component. A duplicate (identical) task is a 409. Gated by task:create.",
		Middlewares:   huma.Middlewares{a.authn, a.require("task", "create")},
	}, func(ctx context.Context, in *createTaskInput) (*taskOutput, error) {
		t, err := gw.CreateTask(ctx, actorID(ctx), storage.TaskSpec{
			DisplayName: in.Body.DisplayName,
			Mode:        in.Body.Mode,
			InterfaceID: in.Body.InterfaceID,
			Node:        in.Body.Node,
			Spec:        []byte(in.Body.Spec),
			Enabled:     in.Body.Enabled,
		}, a.scopeFor(ctx, "task", "create"))
		if err != nil {
			return nil, mapTaskErr(err)
		}
		return &taskOutput{Body: toTaskBody(t)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-task",
		Method:      http.MethodPatch,
		Path:        "/tasks/{id}",
		Summary:     "Update a task",
		Description: "Patches a task's display name, enabled toggle, node placement, or spec. Gated by task:update; read and update scopes (through the component) drive the 404 versus 403 split.",
		Middlewares: huma.Middlewares{a.authn, a.require("task", "update")},
	}, func(ctx context.Context, in *updateTaskInput) (*taskOutput, error) {
		t, err := gw.UpdateTask(ctx, actorID(ctx), in.ID, storage.TaskPatch{
			DisplayName: in.Body.DisplayName,
			Enabled:     in.Body.Enabled,
			Node:        in.Body.Node,
			Spec:        []byte(in.Body.Spec),
		}, a.scopeFor(ctx, "task", "read"), a.scopeFor(ctx, "task", "update"))
		if err != nil {
			return nil, mapTaskErr(err)
		}
		return &taskOutput{Body: toTaskBody(t)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-task",
		Method:        http.MethodDelete,
		Path:          "/tasks/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a task",
		Description:   "Deletes a task. Gated by task:delete; read and delete scopes (through the component) drive the 404 versus 403 split.",
		Middlewares:   huma.Middlewares{a.authn, a.require("task", "delete")},
	}, func(ctx context.Context, in *taskPathInput) (*struct{}, error) {
		if err := gw.DeleteTask(ctx, actorID(ctx), in.ID,
			a.scopeFor(ctx, "task", "read"), a.scopeFor(ctx, "task", "delete")); err != nil {
			return nil, mapTaskErr(err)
		}
		return nil, nil
	})
}

func mapTaskErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrTaskNotFound):
		return huma.Error404NotFound("task not found")
	case errors.Is(err, storage.ErrTaskForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrTaskExists):
		return huma.Error409Conflict("task already exists")
	case errors.Is(err, storage.ErrTaskInterfaceNotFound):
		return huma.Error422UnprocessableEntity("interface not found")
	case errors.Is(err, storage.ErrTaskNodeNotFound):
		return huma.Error422UnprocessableEntity("node not found")
	case errors.Is(err, storage.ErrInvalidTaskMode):
		return huma.Error422UnprocessableEntity("mode must be poll or listen")
	default:
		return huma.Error500InternalServerError("task operation failed")
	}
}
