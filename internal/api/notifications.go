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
	Target        string     `json:"target" doc:"The target it goes to, by name: a webhook, or the group or person target it reached its recipient through"`
	Channel       string     `json:"channel" enum:"webhook,inbox,email" doc:"How it travels: a webhook post, a person's console inbox (delivered by its own commit), or email"`
	Recipient     string     `json:"recipient,omitempty" doc:"The person it is addressed to, by username: shown to that person and to holders of principal:read:admin, withheld from everyone else (see recipient_withheld)"`
	Withheld      bool       `json:"recipient_withheld,omitempty" doc:"True when the row is addressed to a person whose name the caller may not see"`
	ReadAt        *time.Time `json:"read_at,omitempty" doc:"When its recipient read it, for an inbox entry"`
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
		Transition: n.Transition, ActionRule: n.Rule, Type: n.Type, Target: n.Target, Channel: n.Channel,
		Recipient: n.Recipient, Withheld: n.Personal && n.Recipient == "", ReadAt: n.ReadAt, Subject: n.Subject, Text: n.Text,
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
		Description: "The messages sent, on their way, or failed about alarms, newest first, filtered by alarm, target or status. Only notifications about components in the caller's component read scope are listed. A row addressed to a person names them only to that person and to holders of principal:read:admin. Gated by notification:read.",
	}, "notification", "read"), func(ctx context.Context, in *listNotificationsInput) (*listNotificationsOutput, error) {
		ns, err := gw.ListNotifications(ctx, storage.NotificationFilter{AlarmID: in.Alarm, Target: in.Target, Status: in.Status, Limit: in.Limit},
			a.scopeFor(ctx, "component", "read"), recipientView(ctx, a))
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
		n, err := gw.GetNotification(ctx, in.ID, a.scopeFor(ctx, "component", "read"), recipientView(ctx, a))
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
		n, err := gw.RetryNotification(ctx, actorID(ctx), in.ID, a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "notification", "retry"), recipientView(ctx, a))
		if err != nil {
			return nil, mapNotificationErr(err)
		}
		return &notificationOutput{Body: toNotificationBody(n)}, nil
	})
}

// recipientView is who the caller may see a person-addressed row's recipient
// as (#1341): themselves always, everyone when they hold the principal
// directory's admin-tier read.
func recipientView(ctx context.Context, a *authenticator) storage.RecipientView {
	v := storage.RecipientView{All: a.allows(ctx, "principal", "read", "admin")}
	if pr, ok := principalFrom(ctx); ok {
		v.Self = pr.ID
	}
	return v
}

type listInboxInput struct {
	Unread bool `query:"unread" doc:"Only the entries not yet read"`
	Limit  int  `query:"limit" minimum:"0" maximum:"500" doc:"At most this many, newest first (default 100)"`
}

type listInboxOutput struct {
	Body struct {
		Notifications []notificationBody `json:"notifications"`
		Unread        int                `json:"unread" doc:"How many entries the caller has not read, over the same scope"`
	}
}

type markAllReadOutput struct {
	Body struct {
		Marked int `json:"marked" doc:"How many entries were marked read"`
	}
}

// registerInboxRoutes wires the caller's own console inbox (#1341): their
// person-addressed inbox rows, each about a component their component read
// scope admits at the moment they read it.
func registerInboxRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-auth-me-notifications",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/auth/me/notifications",
		Summary:     "List your inbox",
		Description: "The caller's own console inbox: the notifications addressed to them on the inbox channel, newest first, with the unread count. Self-scoped (never another principal's), and scoped again by the caller's component read scope as it stands now, so an entry about a component they can no longer read is gone. Gated by notification:read.",
	}, "notification", "read"), func(ctx context.Context, in *listInboxInput) (*listInboxOutput, error) {
		inbox, err := gw.ListInbox(ctx, callerID(ctx), storage.InboxFilter{Unread: in.Unread, Limit: in.Limit}, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapNotificationErr(err)
		}
		out := &listInboxOutput{}
		out.Body.Unread = inbox.Unread
		out.Body.Notifications = make([]notificationBody, 0, len(inbox.Notifications))
		for i := range inbox.Notifications {
			out.Body.Notifications = append(out.Body.Notifications, toNotificationBody(&inbox.Notifications[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "mark-read-auth-me-notification",
		Method:      http.MethodPost,
		Path:        "/auth/me/notifications/{id}:markRead",
		Summary:     "Mark an inbox entry read",
		Description: "Marks one of the caller's own inbox entries read (one already read keeps its first read time). An entry that is not the caller's, or about a component outside their component read scope, is a non-disclosing 404. Audited. Gated by notification:read.",
	}, "notification", "read"), func(ctx context.Context, in *notificationIDInput) (*notificationOutput, error) {
		n, err := gw.MarkInboxRead(ctx, actorID(ctx), callerID(ctx), in.ID, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapNotificationErr(err)
		}
		return &notificationOutput{Body: toNotificationBody(n)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "mark-all-read-auth-me-notifications",
		Method:      http.MethodPost,
		Path:        "/auth/me/notifications:markAllRead",
		Summary:     "Mark every inbox entry read",
		Description: "Marks every unread entry in the caller's own inbox read, over the same scope the inbox reads, and says how many. Audited as one row. Gated by notification:read.",
	}, "notification", "read"), func(ctx context.Context, _ *struct{}) (*markAllReadOutput, error) {
		n, err := gw.MarkInboxAllRead(ctx, actorID(ctx), callerID(ctx), a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapNotificationErr(err)
		}
		out := &markAllReadOutput{}
		out.Body.Marked = n
		return out, nil
	})
}

// callerID is the request principal's uuid; empty when unauthenticated (a
// gated route never runs then).
func callerID(ctx context.Context) string {
	if pr, ok := principalFrom(ctx); ok {
		return pr.ID
	}
	return ""
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
