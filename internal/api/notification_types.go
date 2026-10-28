package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The notification_type registry (#618, #1319): what a message about an alarm
// says. Fleet-wide reference data mirroring the command_type catalog (no scope
// injected, read on the viewer floor); official types are read-only. A type's
// two templates render through the shared template sandbox over the alarm
// fact map, and :preview shows exactly what a delivery would carry.

type notificationTypeBody struct {
	ID          string    `json:"id" doc:"The type's uuid, the stable form of name"`
	Name        string    `json:"name"`
	Label       string    `json:"label,omitempty"`
	Description string    `json:"description,omitempty"`
	Subject     string    `json:"subject" doc:"The one-line subject template"`
	Text        string    `json:"text" doc:"The body text template"`
	Official    bool      `json:"official" doc:"Shipped with the platform and read-only"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toNotificationTypeBody(t *storage.NotificationType) notificationTypeBody {
	return notificationTypeBody{ID: t.ID, Name: t.Name, Label: t.Label, Description: t.Description,
		Subject: t.Subject, Text: t.Text, Official: t.Official, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
}

type listNotificationTypesOutput struct {
	Body struct {
		NotificationTypes []notificationTypeBody `json:"notification_types"`
	}
}

type notificationTypeOutput struct{ Body notificationTypeBody }

type notificationTypeRefInput struct {
	Name string `path:"name" doc:"The notification type's name or uuid"`
}

// notificationTemplateDoc is the field help both templates carry: what a
// template may read, generated from the one declaration of the fact map.
var notificationTemplateDoc = "A template in the sandboxed template language a label rule uses (title, upper, lower, slug, words; if, and, or, not, eq and the comparisons), reading the alarm facts " +
	strings.Join(storage.AlarmFactKeys(), ", ") + "; a fact outside them renders empty, and a refused construct (printf, call, index, range, a variable) is a 422"

type createNotificationTypeInput struct {
	Body struct {
		Name        string `json:"name" minLength:"1" doc:"The type's name (lowercase kebab)"`
		Label       string `json:"label,omitempty" required:"false" doc:"A human label"`
		Description string `json:"description,omitempty" required:"false" doc:"What the message is for"`
		Subject     string `json:"subject" doc:"The one-line subject template"`
		Text        string `json:"text" doc:"The body text template"`
	}
}

type updateNotificationTypeInput struct {
	Name string `path:"name" doc:"The notification type's name or uuid"`
	Body struct {
		Label       *string `json:"label,omitempty" required:"false" doc:"A human label (empty clears it)"`
		Description *string `json:"description,omitempty" required:"false" doc:"What the message is for"`
		Subject     *string `json:"subject,omitempty" required:"false" doc:"The one-line subject template"`
		Text        *string `json:"text,omitempty" required:"false" doc:"The body text template"`
	}
}

type previewNotificationTypeInput struct {
	Name string `path:"name" doc:"The notification type's name or uuid"`
	Body struct {
		Component  string  `json:"component" minLength:"1" doc:"The component the alarm is on, by name or dotted address"`
		Alarm      string  `json:"alarm,omitempty" required:"false" doc:"The alarm's id; absent previews the component's most recently raised alarm"`
		Transition string  `json:"transition,omitempty" required:"false" enum:"raised,cleared" doc:"The edge to render for; absent reads the alarm's own state (cleared when it is, raised otherwise)"`
		Subject    *string `json:"subject,omitempty" required:"false" doc:"A draft subject template to render instead of the stored one, so an editor previews before it saves"`
		Text       *string `json:"text,omitempty" required:"false" doc:"A draft text template to render instead of the stored one"`
	}
}

type previewNotificationTypeOutput struct {
	Body struct {
		Subject    string          `json:"subject" doc:"The rendered subject, as a delivery would carry it"`
		Text       string          `json:"text" doc:"The rendered text, as a delivery would carry it"`
		Transition string          `json:"transition" enum:"raised,cleared" doc:"The edge it was rendered for"`
		Facts      []alarmFactBody `json:"facts" doc:"The alarm facts the templates read, every key in its declared order (an absent fact is empty)"`
	}
}

// alarmFactBody is one fact a notification template reads, by its key.
type alarmFactBody struct {
	Name  string `json:"name" doc:"The key a template reads it by ({{.Name}})"`
	Value string `json:"value"`
}

// orderedFacts lists a fact map in the order the map is declared.
func orderedFacts(m map[string]string) []alarmFactBody {
	out := make([]alarmFactBody, 0, len(m))
	for _, k := range storage.AlarmFactKeys() {
		out = append(out, alarmFactBody{Name: k, Value: m[k]})
	}
	return out
}

// registerNotificationTypeRoutes wires the registry's CRUD, gated by
// notification_type:read / :create / :update / :delete, and its :preview
// (read). The :rename rides the shared registry rename (registry_rename.go).
func registerNotificationTypeRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-notification-type",
		Method:      http.MethodGet,
		Path:        "/notification-types",
		Summary:     "List notification types",
		Description: "Lists every notification type (official and operator), by name: what a message about an alarm says. Fleet-wide reference data. Gated by notification_type:read.",
	}, "notification_type", "read"), func(ctx context.Context, _ *struct{}) (*listNotificationTypesOutput, error) {
		ts, err := gw.ListNotificationTypes(ctx)
		if err != nil {
			return nil, mapNotificationTypeErr(err)
		}
		out := &listNotificationTypesOutput{}
		out.Body.NotificationTypes = make([]notificationTypeBody, 0, len(ts))
		for i := range ts {
			out.Body.NotificationTypes = append(out.Body.NotificationTypes, toNotificationTypeBody(&ts[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-notification-type",
		Method:      http.MethodGet,
		Path:        "/notification-types/{name}",
		Summary:     "Get a notification type",
		Description: "Returns one notification type by name or uuid. Gated by notification_type:read.",
	}, "notification_type", "read"), func(ctx context.Context, in *notificationTypeRefInput) (*notificationTypeOutput, error) {
		t, err := gw.GetNotificationType(ctx, in.Name)
		if err != nil {
			return nil, mapNotificationTypeErr(err)
		}
		return &notificationTypeOutput{Body: toNotificationTypeBody(t)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-notification-type",
		Method:        http.MethodPost,
		Path:          "/notification-types",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a notification type",
		Description:   "Registers an operator notification type (official=false). " + notificationTemplateDoc + ", and is never stored. A taken name is a 409. Audited. Gated by notification_type:create.",
	}, "notification_type", "create"), func(ctx context.Context, in *createNotificationTypeInput) (*notificationTypeOutput, error) {
		t, err := gw.CreateNotificationType(ctx, actorID(ctx), storage.NotificationTypeSpec{
			Name: in.Body.Name, Label: in.Body.Label, Description: in.Body.Description,
			Subject: in.Body.Subject, Text: in.Body.Text,
		})
		if err != nil {
			return nil, mapNotificationTypeErr(err)
		}
		return &notificationTypeOutput{Body: toNotificationTypeBody(t)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-notification-type",
		Method:      http.MethodPatch,
		Path:        "/notification-types/{name}",
		Summary:     "Update a notification type",
		Description: "Patches an operator type's label, description or templates (an absent field is unchanged). " + notificationTemplateDoc + ". An official type is read-only (409). The name moves only through :rename. Audited. Gated by notification_type:update.",
	}, "notification_type", "update"), func(ctx context.Context, in *updateNotificationTypeInput) (*notificationTypeOutput, error) {
		t, err := gw.UpdateNotificationType(ctx, actorID(ctx), in.Name, storage.NotificationTypePatch{
			Label: in.Body.Label, Description: in.Body.Description, Subject: in.Body.Subject, Text: in.Body.Text,
		})
		if err != nil {
			return nil, mapNotificationTypeErr(err)
		}
		return &notificationTypeOutput{Body: toNotificationTypeBody(t)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-notification-type",
		Method:        http.MethodDelete,
		Path:          "/notification-types/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a notification type",
		Description:   "Removes an operator notification type. An official type, or one an action rule still sends, is a 409. Audited. Gated by notification_type:delete.",
	}, "notification_type", "delete"), func(ctx context.Context, in *notificationTypeRefInput) (*struct{}, error) {
		if err := gw.DeleteNotificationType(ctx, actorID(ctx), in.Name); err != nil {
			return nil, mapNotificationTypeErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "preview-notification-type",
		Extensions:  mcpExposed(),
		Method:      http.MethodPost,
		Path:        "/notification-types/{name}:preview",
		Summary:     "Preview a notification type against an alarm",
		Description: "Renders the type's subject and text (or a draft of either, sent in the body) against one real alarm, exactly as a delivery of that alarm's transition would carry them, with the facts they read. Writes nothing. The alarm is named by its component and id; without an id, the component's most recently raised alarm. A draft template the sandbox refuses is a 422, one that fails to render (its output past the cap) a 422 naming it. Gated by notification_type:read; the component is resolved in the caller's component read scope, so a component outside it is a non-disclosing 404.",
	}, "notification_type", "read"), func(ctx context.Context, in *previewNotificationTypeInput) (*previewNotificationTypeOutput, error) {
		compID, err := requireComponentInScope(ctx, a, gw, in.Body.Component, "read")
		if err != nil {
			return nil, err
		}
		p, err := gw.PreviewNotificationType(ctx, in.Name, compID, in.Body.Alarm, in.Body.Transition, in.Body.Subject, in.Body.Text)
		if err != nil {
			return nil, mapNotificationTypeErr(err)
		}
		out := &previewNotificationTypeOutput{}
		out.Body.Subject, out.Body.Text, out.Body.Transition, out.Body.Facts = p.Subject, p.Text, p.Transition, orderedFacts(p.Facts)
		return out, nil
	})
}

func mapNotificationTypeErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrNotificationTypeNotFound), errors.Is(err, storage.ErrTypeNotFound):
		return huma.Error404NotFound("notification type not found")
	case errors.Is(err, storage.ErrAlarmNotFound):
		return huma.Error404NotFound("alarm not found")
	case errors.Is(err, storage.ErrNotificationTypeExists):
		return huma.Error409Conflict("a notification type with this name already exists")
	case errors.Is(err, storage.ErrNotificationTypeOfficial):
		return huma.Error409Conflict("an official notification type is read-only")
	case errors.Is(err, storage.ErrNotificationTypeInUse):
		return huma.Error409Conflict("an action rule still sends this notification type")
	case errors.Is(err, storage.ErrNotificationTypeInvalid):
		return huma.Error422UnprocessableEntity(storage.TemplateRefusal(err))
	case errors.Is(err, storage.ErrNotificationRender):
		return huma.Error422UnprocessableEntity(storage.TemplateRefusal(err))
	}
	return huma.Error500InternalServerError("notification type operation failed")
}
