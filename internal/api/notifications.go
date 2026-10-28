package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The notification delivery surface (#618, #1321): what was sent about which
// alarm, and the retry of one that failed. notification:read rides the viewer
// floor, and every read is scoped by the notification's alarm's component,
// as the alarm reads are: a principal never sees a message about a component
// outside its component read scope. :retry sits with operator, beside
// alarm:acknowledge, its action scope resolved on the component tier.

type notificationBody struct {
	ID            string     `json:"id" doc:"The notification's uuid, the Idempotency-Key its receiver sees"`
	Alarm         string     `json:"alarm" doc:"The alarm it is about, by uuid"`
	Component     string     `json:"component" doc:"The alarm's component, by name"`
	ComponentID   string     `json:"component_id" doc:"The component's uuid"`
	Transition    string     `json:"transition" enum:"raised,cleared" doc:"The alarm edge it is about"`
	ActionRule    string     `json:"action_rule" doc:"The action rule that queued it, by name (kept after the rule is deleted)"`
	Type          string     `json:"notification_type" doc:"The notification type that rendered it, by name"`
	Target        string     `json:"target" doc:"The target it goes to, by name"`
	Subject       string     `json:"subject" doc:"The rendered subject"`
	Text          string     `json:"text" doc:"The rendered text"`
	Status        string     `json:"status" enum:"queued,sent,failed" doc:"queued: on its way (its next attempt at next_attempt_at); sent: the receiver answered 2xx; failed: its last attempt failed, or it was refused before one"`
	Attempts      int        `json:"attempts" doc:"Attempts made; the 8th failure is final"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty" doc:"When the tick next tries it, while queued"`
	LastStatus    *int       `json:"last_status,omitempty" doc:"The receiver's HTTP status on the last attempt"`
	LastError     string     `json:"last_error,omitempty" doc:"Why the last attempt failed, never the URL or a credential"`
	SentAt        *time.Time `json:"sent_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at" doc:"When it was queued: the transition's own moment"`
}

func toNotificationBody(n *storage.Notification) notificationBody {
	return notificationBody{ID: n.ID, Alarm: n.AlarmID, Component: n.ComponentName, ComponentID: n.ComponentID,
		Transition: n.Transition, ActionRule: n.Rule, Type: n.Type, Target: n.Target, Subject: n.Subject, Text: n.Text,
		Status: n.Status, Attempts: n.Attempts, NextAttemptAt: n.NextAttemptAt, LastStatus: n.LastStatus,
		LastError: n.LastError, SentAt: n.SentAt, CreatedAt: n.CreatedAt}
}

type listNotificationsInput struct {
	Alarm  string `query:"alarm" doc:"Only the notifications about this alarm (its uuid)"`
	Target string `query:"target" doc:"Only those to this target, by name"`
	Status string `query:"status" enum:"queued,sent,failed," doc:"Only those in this status"`
	Limit  int    `query:"limit" minimum:"0" maximum:"500" doc:"At most this many, newest first (default 100)"`
}

type listNotificationsOutput struct {
	Body struct {
		Notifications []notificationBody `json:"notifications"`
	}
}

type notificationOutput struct{ Body notificationBody }

type notificationIDInput struct {
	ID string `path:"id" doc:"The notification's uuid"`
}

func registerNotificationRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-notification",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/notifications",
		Summary:     "List notifications",
		Description: "The messages sent, on their way, or failed about alarms, newest first, filtered by alarm, target or status. Only notifications about components in the caller's component read scope are listed. Gated by notification:read.",
	}, "notification", "read"), func(ctx context.Context, in *listNotificationsInput) (*listNotificationsOutput, error) {
		ns, err := gw.ListNotifications(ctx, storage.NotificationFilter{AlarmID: in.Alarm, Target: in.Target, Status: in.Status, Limit: in.Limit},
			a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapNotificationErr(err)
		}
		out := &listNotificationsOutput{}
		out.Body.Notifications = make([]notificationBody, 0, len(ns))
		for i := range ns {
			out.Body.Notifications = append(out.Body.Notifications, toNotificationBody(&ns[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-notification",
		Method:      http.MethodGet,
		Path:        "/notifications/{id}",
		Summary:     "Get a notification",
		Description: "Returns one notification. One about a component outside the caller's component read scope is a non-disclosing 404. Gated by notification:read.",
	}, "notification", "read"), func(ctx context.Context, in *notificationIDInput) (*notificationOutput, error) {
		n, err := gw.GetNotification(ctx, in.ID, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapNotificationErr(err)
		}
		return &notificationOutput{Body: toNotificationBody(n)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "retry-notification",
		Method:      http.MethodPost,
		Path:        "/notifications/{id}:retry",
		Summary:     "Retry a failed notification",
		Description: "Re-queues a failed notification for a fresh run of attempts, sending the same rendered message with the same Idempotency-Key. One that is queued or sent, or one that failed before any attempt (its rule's when did not run, or its type did not render, so it holds no message), is a 409. Audited as retry. Gated by notification:retry, whose scope resolves on the component tier; a component outside the caller's component read scope is a non-disclosing 404, one it reads but may not retry on a 403.",
	}, "notification", "retry"), func(ctx context.Context, in *notificationIDInput) (*notificationOutput, error) {
		n, err := gw.RetryNotification(ctx, actorID(ctx), in.ID, a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "notification", "retry"))
		if err != nil {
			return nil, mapNotificationErr(err)
		}
		return &notificationOutput{Body: toNotificationBody(n)}, nil
	})
}

func mapNotificationErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrNotificationNotFound):
		return huma.Error404NotFound("notification not found")
	case errors.Is(err, storage.ErrNotificationForbidden):
		return huma.Error403Forbidden("retrying a notification about this component is outside your notification:retry scope")
	case errors.Is(err, storage.ErrNotificationNotRetryable):
		return huma.Error409Conflict("only a failed notification that was attempted is retried: a queued one is on its way, a sent one arrived, and one that failed before any attempt holds no message to send")
	}
	return huma.Error500InternalServerError("notification operation failed")
}
