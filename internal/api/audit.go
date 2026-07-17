package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The audit read surface: GET /audit-log lists recent audit-trail events, each
// with the actor and, for an impersonated action, the real actor behind it. The
// write side lives in the gateway (writeAuditRes for estate mutations,
// WriteAuthEvent for auth events); this is read-only and gated by audit:read.

type auditListInput struct {
	Limit    int    `query:"limit" doc:"Max rows to return, newest first (default 100, capped at 500)"`
	Resource string `query:"resource" doc:"Filter to one resource kind (e.g. auth, principal_grant)"`
	Verb     string `query:"verb" doc:"Filter to one verb (e.g. login, create)"`
	Before   string `query:"before" doc:"Only events strictly older than this RFC3339 timestamp (paging backward)"`
}

type auditEventBody struct {
	ID            string `json:"id"`
	TS            string `json:"ts"`
	Actor         string `json:"actor,omitempty"`
	ActorName     string `json:"actor_name,omitempty"`
	RealActor     string `json:"real_actor,omitempty"`
	RealActorName string `json:"real_actor_name,omitempty"`
	Verb          string `json:"verb"`
	Resource      string `json:"resource"`
	ResourceID    string `json:"resource_id,omitempty"`
}

type auditListOutput struct {
	Body struct {
		Events []auditEventBody `json:"events"`
	}
}

func registerAuditRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-audit-log",
		Method:      http.MethodGet,
		Path:        "/audit-log",
		Summary:     "List audit-trail events",
		Description: "Recent audit-trail events, newest first, each with the actor and, for an impersonated action, the real actor behind it. Read-only; gated by audit:read:admin (admin/owner only, since the audit trail is admin-sensitive).",
	}, "audit", "read", "admin"), func(ctx context.Context, in *auditListInput) (*auditListOutput, error) {
		entries, err := gw.ListAuditLog(ctx, storage.AuditFilter{Limit: in.Limit, Resource: in.Resource, Verb: in.Verb, Before: in.Before})
		if err != nil {
			return nil, huma.Error500InternalServerError("list audit log")
		}
		out := &auditListOutput{}
		out.Body.Events = make([]auditEventBody, 0, len(entries))
		for _, e := range entries {
			out.Body.Events = append(out.Body.Events, auditEventBody{
				ID: e.ID, TS: e.TS,
				Actor: e.ActorID, ActorName: e.ActorName,
				RealActor: e.RealActorID, RealActorName: e.RealActorName,
				Verb: e.Verb, Resource: e.Resource, ResourceID: e.ResourceID,
			})
		}
		return out, nil
	})
}
