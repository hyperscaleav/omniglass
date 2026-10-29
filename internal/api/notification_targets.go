package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/mail"
	"github.com/hyperscaleav/omniglass/internal/notifier"
	"github.com/hyperscaleav/omniglass/internal/notify"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The notification_target surface (#618, #1320): where a message goes.
// Admin-only (notification_target is a sensitive resource, off the viewer
// floor), and every route takes an all-scope grant: a target is install-wide,
// an egress destination holding a credential reference. The URL is sensitive
// (a chat incoming-webhook URL is itself a credential): every read masks it
// and :reveal returns it, audited. :test sends one synthetic message through
// the egress primitive and answers what the receiver said.

type notificationTargetBody struct {
	ID        string    `json:"id" doc:"The target's uuid, the stable form of name"`
	Name      string    `json:"name"`
	Label     string    `json:"label,omitempty"`
	Kind      string    `json:"kind" enum:"webhook,email,principal_group,principal" doc:"The target kind: webhook posts a signed JSON envelope to the URL; email sends to fixed addresses through the install's mail relay; principal_group reaches every human member of a group, read at each alarm transition; principal reaches one person. A person is told only about alarms on components they can read"`
	URL       string    `json:"url,omitempty" doc:"A webhook's destination, masked to its scheme and host (the path and query often carry a credential); :reveal returns it whole"`
	Group     string    `json:"principal_group,omitempty" doc:"The group a principal_group target reaches, by name; shown to a caller who reads the group directory (principal_group:read:admin, all-scope)"`
	GroupID   string    `json:"principal_group_id,omitempty" doc:"The group's uuid"`
	Principal string    `json:"principal,omitempty" doc:"The person a principal target reaches, by username; shown to a caller who reads the principal directory (principal:read:admin, all-scope)"`
	PrincID   string    `json:"principal_id,omitempty" doc:"The person's uuid"`
	Channels  []string  `json:"channels,omitempty" doc:"The channels a group or person target delivers on: inbox (the console inbox) and email (each person's directory email)"`
	Addresses []string  `json:"addresses,omitempty" doc:"An email target's recipients: a shared mailbox, sent through the install's mail relay"`
	Secret    string    `json:"secret,omitempty" doc:"The platform secret the send signs (an hmac-key) or authenticates (a bearer-token) with, by name; absent sends unsigned"`
	SecretID  string    `json:"secret_id,omitempty" doc:"The bound secret's uuid, the stable form of secret"`
	Auth      string    `json:"auth" enum:"none,hmac,bearer" doc:"How a send proves itself: hmac signs each body with the bound hmac-key (X-Omniglass-Signature), bearer presents the bound bearer-token, none sends unsigned"`
	Enabled   bool      `json:"enabled" doc:"Whether deliveries go to it; a disabled target is skipped"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// toNotificationTargetBody renders a target; the group or person a target
// names is shown only to a caller who reads that directory (people), since a
// target read is not a directory read.
func toNotificationTargetBody(t *storage.NotificationTarget, people storage.PeopleUse) notificationTargetBody {
	auth := "none"
	switch t.SecretType {
	case storage.SecretTypeHMACKey:
		auth = "hmac"
	case storage.SecretTypeBearerToken:
		auth = "bearer"
	}
	url := ""
	if t.URL != "" {
		url = storage.MaskURL(t.URL)
	}
	b := notificationTargetBody{ID: t.ID, Name: t.Name, Label: t.Label, Kind: t.Kind, URL: url, Channels: t.Channels, Addresses: t.Addresses,
		Secret: t.Secret, SecretID: t.SecretID, Auth: auth, Enabled: t.Enabled, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	if people.Groups {
		b.Group, b.GroupID = t.PrincipalGroup, t.PrincipalGroupID
	}
	if people.Principals {
		b.Principal, b.PrincID = t.Principal, t.PrincipalID
	}
	return b
}

type listNotificationTargetsOutput struct {
	Body struct {
		NotificationTargets []notificationTargetBody `json:"notification_targets"`
	}
}

type notificationTargetOutput struct{ Body notificationTargetBody }

type notificationTargetRefInput struct {
	Name string `path:"name" doc:"The target's name or uuid"`
}

type createNotificationTargetInput struct {
	Body struct {
		Name    string   `json:"name" minLength:"1" doc:"The target's name (lowercase kebab)"`
		Label   string   `json:"label,omitempty" required:"false" doc:"A human label"`
		Kind    string   `json:"kind" enum:"webhook,email,principal_group,principal" doc:"The target kind"`
		URL     string   `json:"url,omitempty" required:"false" doc:"A webhook's destination URL: https, or plain http to a host or address the notification.egress_allow setting names. A private, loopback or link-local destination is refused at send unless that setting names it; a cloud metadata address always is"`
		Group   string   `json:"principal_group,omitempty" required:"false" doc:"A principal_group target's group, by name or uuid; binding one takes principal_group:read:admin"`
		Princ   string   `json:"principal,omitempty" required:"false" doc:"A principal target's person, by username or uuid; binding one takes principal:read:admin"`
		Chans   []string `json:"channels,omitempty" required:"false" doc:"A group or person target's channels: inbox, email"`
		Addrs   []string `json:"addresses,omitempty" required:"false" doc:"An email target's recipient addresses"`
		Secret  string   `json:"secret,omitempty" required:"false" doc:"A platform secret, by name or uuid, of type hmac-key (signs each body) or bearer-token (sent as Authorization: Bearer)"`
		Enabled *bool    `json:"enabled,omitempty" required:"false" doc:"Whether deliveries go to it (default true)"`
	}
}

type updateNotificationTargetInput struct {
	Name string `path:"name" doc:"The target's name or uuid"`
	Body struct {
		Label   *string   `json:"label,omitempty" required:"false" doc:"A human label (empty clears it)"`
		URL     *string   `json:"url,omitempty" required:"false" doc:"The destination URL: https, or plain http to a host or address the notification.egress_allow setting names. A private, loopback or link-local destination is refused at send unless that setting names it; a cloud metadata address always is"`
		Secret  *string   `json:"secret,omitempty" required:"false" doc:"A platform secret by name or uuid; empty unbinds it"`
		Chans   *[]string `json:"channels,omitempty" required:"false" doc:"A group or person target's channels: inbox, email"`
		Addrs   *[]string `json:"addresses,omitempty" required:"false" doc:"An email target's recipient addresses"`
		Enabled *bool     `json:"enabled,omitempty" required:"false" doc:"Whether deliveries go to it"`
	}
}

type revealNotificationTargetOutput struct {
	Body struct {
		Name string `json:"name"`
		URL  string `json:"url" doc:"The destination URL, whole"`
	}
}

type testNotificationTargetOutput struct {
	Body struct {
		OK     bool   `json:"ok" doc:"Whether the receiver answered 2xx"`
		Status int    `json:"status" doc:"The receiver's HTTP status; 0 when no answer came (refused, unreachable, TLS, timeout)"`
		Reason string `json:"reason,omitempty" doc:"Why it failed, as a delivery would record it; never the URL or a credential"`
	}
}

// registerNotificationTargetRoutes wires the target CRUD, :reveal and :test.
func registerNotificationTargetRoutes(api huma.API, a *authenticator, gw storage.Gateway, eo egress.Options) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-notification-target",
		Method:      http.MethodGet,
		Path:        "/notification-targets",
		Summary:     "List notification targets",
		Description: "Lists every notification target, by name, each URL masked to its scheme and host. An all-scope directory: gated by notification_target:read with an all-scope grant (admin), and off the viewer floor, since a target is an egress destination holding a credential.",
	}, "notification_target", "read"), func(ctx context.Context, _ *struct{}) (*listNotificationTargetsOutput, error) {
		ts, err := gw.ListNotificationTargets(ctx, a.scopeFor(ctx, "notification_target", "read"))
		if err != nil {
			return nil, mapNotificationTargetErr(err)
		}
		out := &listNotificationTargetsOutput{}
		people := peopleUse(ctx, a)
		out.Body.NotificationTargets = make([]notificationTargetBody, 0, len(ts))
		for i := range ts {
			out.Body.NotificationTargets = append(out.Body.NotificationTargets, toNotificationTargetBody(&ts[i], people))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-notification-target",
		Method:      http.MethodGet,
		Path:        "/notification-targets/{name}",
		Summary:     "Get a notification target",
		Description: "Returns one notification target by name or uuid, its URL masked. Gated by notification_target:read with an all-scope grant.",
	}, "notification_target", "read"), func(ctx context.Context, in *notificationTargetRefInput) (*notificationTargetOutput, error) {
		t, err := gw.GetNotificationTarget(ctx, in.Name, a.scopeFor(ctx, "notification_target", "read"))
		if err != nil {
			return nil, mapNotificationTargetErr(err)
		}
		return &notificationTargetOutput{Body: toNotificationTargetBody(t, peopleUse(ctx, a))}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-notification-target",
		Method:        http.MethodPost,
		Path:          "/notification-targets",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a notification target",
		Description:   "Adds a destination a notification can be sent to: a webhook, its URL and, optionally, the platform secret it signs or authenticates with; or people, a principal_group (every human member, read at each alarm transition) or one principal, on the channels named. A person is told only about an alarm on a component their own component read scope admits. Binding a group takes principal_group:read:admin and binding a person principal:read:admin (a 403 without it), since the target reaches whoever they name. Binding a secret takes the right to reveal it (secret:reveal, and the admin tier for an admin-sensitive one), since every send hands it to the URL: a secret the caller cannot read is a 404, one it can read but not reveal a 403. A URL the egress contract refuses under the allow list in force, an unknown kind, or a secret that is not a platform hmac-key or bearer-token is a 422; a taken name a 409. Audited, the URL masked. Gated by notification_target:create with an all-scope grant.",
	}, "notification_target", "create"), func(ctx context.Context, in *createNotificationTargetInput) (*notificationTargetOutput, error) {
		t, err := gw.CreateNotificationTarget(ctx, actorID(ctx), storage.NotificationTargetSpec{
			Name: in.Body.Name, Label: in.Body.Label, Kind: in.Body.Kind, URL: in.Body.URL,
			Secret: in.Body.Secret, PrincipalGroup: in.Body.Group, Principal: in.Body.Princ, Channels: in.Body.Chans, Addresses: in.Body.Addrs, Enabled: in.Body.Enabled,
		}, a.scopeFor(ctx, "notification_target", "create"), secretUse(ctx, a), peopleUse(ctx, a))
		if err != nil {
			return nil, mapNotificationTargetErr(err)
		}
		return &notificationTargetOutput{Body: toNotificationTargetBody(t, peopleUse(ctx, a))}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-notification-target",
		Method:      http.MethodPatch,
		Path:        "/notification-targets/{name}",
		Summary:     "Update a notification target",
		Description: "Patches a target's label, URL, secret or enabled flag (an absent field is unchanged; an empty secret unbinds it). Binding a secret takes the right to reveal it, as on create, and so does moving the URL of a target that keeps a bound secret (a 403 without it), since the next send would hand the secret to the new URL. Audited, the URL masked and a change to it recorded as changed. Gated by notification_target:update with an all-scope grant.",
	}, "notification_target", "update"), func(ctx context.Context, in *updateNotificationTargetInput) (*notificationTargetOutput, error) {
		t, err := gw.UpdateNotificationTarget(ctx, actorID(ctx), in.Name, storage.NotificationTargetPatch{
			Label: in.Body.Label, URL: in.Body.URL, Secret: in.Body.Secret, Channels: in.Body.Chans, Addresses: in.Body.Addrs, Enabled: in.Body.Enabled,
		}, a.scopeFor(ctx, "notification_target", "update"), secretUse(ctx, a))
		if err != nil {
			return nil, mapNotificationTargetErr(err)
		}
		return &notificationTargetOutput{Body: toNotificationTargetBody(t, peopleUse(ctx, a))}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-notification-target",
		Method:        http.MethodDelete,
		Path:          "/notification-targets/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a notification target",
		Description:   "Removes a target. One an action rule still sends to is a 409. Audited. Gated by notification_target:delete with an all-scope grant.",
	}, "notification_target", "delete"), func(ctx context.Context, in *notificationTargetRefInput) (*struct{}, error) {
		if err := gw.DeleteNotificationTarget(ctx, actorID(ctx), in.Name, a.scopeFor(ctx, "notification_target", "delete")); err != nil {
			return nil, mapNotificationTargetErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "reveal-notification-target",
		Method:      http.MethodPost,
		Path:        "/notification-targets/{name}:reveal",
		Summary:     "Reveal a notification target's URL",
		Description: "Returns the target's URL whole. Audited as reveal (the URL is not in the audit row). Gated by notification_target:reveal with an all-scope grant.",
	}, "notification_target", "reveal"), func(ctx context.Context, in *notificationTargetRefInput) (*revealNotificationTargetOutput, error) {
		u, err := gw.RevealNotificationTargetURL(ctx, actorID(ctx), in.Name, a.scopeFor(ctx, "notification_target", "reveal"))
		if err != nil {
			return nil, mapNotificationTargetErr(err)
		}
		out := &revealNotificationTargetOutput{}
		out.Body.Name, out.Body.URL = in.Name, u
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "test-notification-target",
		Method:      http.MethodPost,
		Path:        "/notification-targets/{name}:test",
		Summary:     "Send a test notification to a target",
		Description: "Sends one synthetic message (transition test, about no alarm) to a webhook or email target through the egress contract, signed or authenticated as a delivery would be (an email through the install's mail relay), and answers what the receiver said; a group or person target is a 422, since people are told only about alarms. Records nothing but its audit row (the result, never the URL). A disabled target is still tested. Gated by notification_target:test with an all-scope grant.",
	}, "notification_target", "test"), func(ctx context.Context, in *notificationTargetRefInput) (*testNotificationTargetOutput, error) {
		ts, err := gw.TargetForTest(ctx, in.Name, a.scopeFor(ctx, "notification_target", "test"))
		if err != nil {
			return nil, mapNotificationTargetErr(err)
		}
		res := testSend(ctx, gw, ts, eo)
		if err := gw.AuditTargetTest(ctx, actorID(ctx), ts.TargetID, res.Status, res.OK, res.Reason); err != nil {
			return nil, huma.Error500InternalServerError("recording the test failed")
		}
		out := &testNotificationTargetOutput{}
		out.Body.OK, out.Body.Status, out.Body.Reason = res.OK, res.Status, res.Reason
		return out, nil
	})
}

// peopleUse is the caller's right over the people a target names: reading
// the group or the principal directory, each its admin-tier read on an
// all-scope grant (canReadDirectory).
func peopleUse(ctx context.Context, a *authenticator) storage.PeopleUse {
	return storage.PeopleUse{Groups: a.canReadDirectory(ctx, "principal_group"), Principals: a.canReadDirectory(ctx, "principal")}
}

// secretUse is the caller's right over a secret it would bind: the reveal
// route's own gate (secret:read and secret:reveal scopes, the admin tier for
// an admin-sensitive one), since a bound secret is sent wherever the URL says.
func secretUse(ctx context.Context, a *authenticator) storage.SecretUse {
	return storage.SecretUse{Read: a.scopeFor(ctx, "secret", "read"), Reveal: a.scopeFor(ctx, "secret", "reveal"), CanAdmin: a.canSecretAdmin(ctx, "reveal")}
}

// testSend builds and sends a target's synthetic message.
func testSend(ctx context.Context, gw storage.Gateway, ts *storage.TargetSend, eo egress.Options) egress.Result {
	if ts.CredentialMissing {
		return egress.Result{Reason: "the target's secret no longer unseals; bind it again"}
	}
	pol, err := gw.EgressPolicy(ctx)
	if err != nil {
		return egress.Result{Reason: strings.TrimPrefix(err.Error(), "egress: ")}
	}
	if ts.Kind == storage.TargetEmail {
		relay, refused, err := gw.MailRelay(ctx)
		if err != nil {
			return egress.Result{Reason: "the mail relay could not be read"}
		}
		if refused != "" {
			return egress.Result{Reason: refused}
		}
		res := notifier.SendEmail(ctx, mail.New(pol, eo), uuid.NewString(), "Omniglass test notification",
			"A test from the notification target "+ts.Name+". Deliveries to this target arrive in this shape.",
			&storage.EmailSend{Relay: *relay, To: ts.Addresses, Date: time.Now()})
		return egress.Result{OK: res.OK, Status: res.Status, Reason: res.Reason}
	}
	req, err := notify.Build(ts.URL, notify.Envelope{
		ID: uuid.NewString(), Type: "test", Transition: "test",
		Subject: "Omniglass test notification",
		Text:    "A test from the notification target " + ts.Name + ". Deliveries to this target arrive in this shape.",
	}, notify.Credential{HMACKey: ts.HMACKey, BearerToken: ts.BearerToken}, time.Now())
	if err != nil {
		return egress.Result{Reason: err.Error()}
	}
	return egress.New(pol, eo).Post(ctx, req)
}

func mapNotificationTargetErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrNotificationTargetPeopleForbidden):
		return huma.Error403Forbidden("binding a group takes principal_group:read:admin and binding a person principal:read:admin: the target reaches whoever they name")
	case errors.Is(err, storage.ErrNotificationTargetForbidden):
		return huma.Error403Forbidden("notification targets take an all-scope grant: a target is install-wide")
	case errors.Is(err, storage.ErrNotificationTargetNotFound):
		return huma.Error404NotFound("notification target not found")
	case errors.Is(err, storage.ErrNotificationTargetExists):
		return huma.Error409Conflict("a notification target with this name already exists")
	case errors.Is(err, storage.ErrNotificationTargetInUse):
		return huma.Error409Conflict("an action rule still sends to this target")
	case errors.Is(err, storage.ErrSecretNotFound):
		return huma.Error404NotFound("secret not found")
	case errors.Is(err, storage.ErrSecretForbidden):
		return huma.Error403Forbidden("binding a secret to a target, or moving the URL of a target that binds one, takes the right to reveal that secret (secret:reveal)")
	case errors.Is(err, storage.ErrNotificationTargetInvalid):
		return huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), "storage: notification target is invalid: "))
	}
	return mapTypeErr(err, "notification target")
}
