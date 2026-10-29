package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The conduit's inbox of unplaced identities (#1282, ADR-1282 point 7): what
// a component that fans its values out heard that no identity binding places.
// Read under component:read on the conduit and scope-injected there, so it
// discloses only what that conduit heard.

type unboundIdentitiesInput struct {
	Name string `path:"name" example:"hq.$comp.meetings-cloud" doc:"The conduit component's name, or a dotted address (e.g. hq.$comp.meetings-cloud)"`
}

type unboundIdentityBody struct {
	Key          string    `json:"key" doc:"The match key: the property type the driver's identity names (e.g. ucc-room-id)"`
	Value        string    `json:"value" doc:"The identity's value as the conduit reported it"`
	Count        int64     `json:"count" doc:"How many samples named it, across the window it has been heard in"`
	FirstHeardAt time.Time `json:"first_heard_at" doc:"When the conduit first reported it unbound"`
	LastHeardAt  time.Time `json:"last_heard_at" doc:"When the conduit last reported it"`
	KeysCarried  []string  `json:"keys_carried" doc:"The metric, property and event keys its samples carried, which a binding would land on its owner"`
}

type unboundIdentitiesOutput struct {
	Body struct {
		Component  string                `json:"component" doc:"The conduit component"`
		Identities []unboundIdentityBody `json:"identities" doc:"The identities it reported that no binding places, most recently heard first"`
	}
}

func registerUnboundIdentityRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-unbound-identities",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/components/{name}/unbound-identities",
		Summary:     "List the identities a conduit reported that no binding places",
		Description: "A component that speaks for many owners (a conduit) lands each value its driver identifies on the owner an identity binding names; a value naming an identity it has no binding for lands on no owner and is kept here, one row per match key and value (a count, first and last heard, the keys it carried), bounded at 1024 per conduit with the least recently heard evicted, and aged out after seven days. Binding one (PUT .../identity-bindings/{key}/{value}) makes the next value naming it land on its owner. Gated by component:read; an out-of-scope conduit is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *unboundIdentitiesInput) (*unboundIdentitiesOutput, error) {
		read := a.scopeFor(ctx, "component", "read")
		comp, err := gw.GetComponent(ctx, in.Name, read)
		if err != nil {
			return nil, mapComponentErr(err)
		}
		rows, err := gw.ListUnboundIdentities(ctx, comp.ID, read)
		if err != nil {
			if refErr, ok := mapRefErr(err); ok {
				return nil, refErr
			}
			return nil, mapComponentErr(err)
		}
		out := &unboundIdentitiesOutput{}
		out.Body.Component = comp.Name
		out.Body.Identities = make([]unboundIdentityBody, 0, len(rows))
		for _, u := range rows {
			keys := u.Keys
			if keys == nil {
				keys = []string{}
			}
			out.Body.Identities = append(out.Body.Identities, unboundIdentityBody{Key: u.Key, Value: u.Value, Count: u.Count,
				FirstHeardAt: u.FirstHeardAt, LastHeardAt: u.LastHeardAt, KeysCarried: keys})
		}
		return out, nil
	})
}
