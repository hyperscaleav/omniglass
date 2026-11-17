package api

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type outpostBody struct {
	Name            string            `json:"name"`
	Label           string            `json:"label,omitempty"`
	Description     string            `json:"description,omitempty"`
	Location        *string           `json:"location,omitempty" doc:"The location the outpost sits in (descriptive placement, not scope)"`
	LocationID      *string           `json:"location_id,omitempty" doc:"The location's id; the stable form of location"`
	Enrolled        bool              `json:"enrolled"`
	LastHeartbeatAt *time.Time        `json:"last_heartbeat_at,omitempty"`
	EnrolledAt      *time.Time        `json:"enrolled_at,omitempty"`
	OrphanedBatches int64             `json:"orphaned_batches" doc:"How many of this outpost's telemetry batches the server dropped because their task is not placed on this outpost (moved to another outpost, or unknown): the confinement fence, counted rather than silent. A rising count means the outpost is collecting for a task it no longer holds"`
	LastOrphanedAt  *time.Time        `json:"last_orphaned_at,omitempty" doc:"When the newest orphaned batch arrived"`
	EffectiveTags   map[string]string `json:"effective_tags,omitempty" doc:"The resolved effective tags (key -> winning value) on this outpost: its direct bindings plus propagating platform tags. For the Tags column and the blade pills."`
}

func toOutpostBody(n *storage.Outpost) outpostBody {
	return outpostBody{
		Name: n.Name, Label: n.Label, Description: n.Description, Location: n.LocationName, LocationID: n.LocationID,
		Enrolled: n.Enrolled, LastHeartbeatAt: n.LastHeartbeatAt, EnrolledAt: n.EnrolledAt,
		OrphanedBatches: n.OrphanedBatches, LastOrphanedAt: n.LastOrphanedAt,
	}
}

type listOutpostsOutput struct {
	Body struct {
		Outposts []outpostBody `json:"outposts"`
	}
}

type outpostOutput struct {
	Body outpostBody
}

type outpostPathInput struct {
	Name string `path:"name" doc:"The outpost's unique name"`
}

type createOutpostInput struct {
	Body struct {
		Name        string  `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"Globally unique outpost name (lowercase letters, digits, and hyphens); it is also the outpost's NATS subject token, which is why the rule forbids a dot"`
		Label       string  `json:"label,omitempty" doc:"Operator label; falls back to the name when empty"`
		Description string  `json:"description,omitempty" doc:"Free-form operator notes about the outpost"`
		Location    *string `json:"location,omitempty" doc:"Optional location the outpost sits in, by name or id (descriptive placement, not scope)"`
	}
}

// updateOutpostInput is the PATCH body: a nil field is left unchanged. Name is not
// patchable (the immutable fleet address). A Location of "" clears the placement.
type updateOutpostInput struct {
	Name string `path:"name"`
	Body struct {
		Label       *string `json:"label,omitempty" doc:"A new operator-facing label"`
		Description *string `json:"description,omitempty" doc:"New free-form operator notes"`
		Location    *string `json:"location,omitempty" doc:"Set the outpost's location by name or id, or \"\" to clear it"`
	}
}

type enrollOutput struct {
	Body struct {
		Name  string `json:"name"`
		Token string `json:"token" doc:"The enrollment token, shown once. Hand it to the outpost deployment; the outpost presents it to claim its NATS credential."`
	}
}

type claimOutpostInput struct {
	Body struct {
		Name  string `json:"name" minLength:"1" doc:"The outpost name the enrollment was minted for"`
		Token string `json:"token" minLength:"1" doc:"The one-time enrollment token from outpost create, exchanged here for the outpost's NATS credential"`
	}
}

type claimOutput struct {
	Body struct {
		NatsURL  string `json:"nats_url" doc:"The NATS URL the outpost dials"`
		Username string `json:"username" doc:"The outpost's NATS username (its outpost name)"`
		Password string `json:"password" doc:"The outpost's NATS password (its enrollment token)"`
	}
}

// registerOutpostRoutes wires the outpost surface: AIP CRUD-ish create/list/get plus
// the :enroll custom method (mint a token) and the public :claim exchange (an outpost
// presents its token, gets its NATS credential). natsURL is the address the claim
// reply hands back so the outpost needs only the server URL.
func registerOutpostRoutes(api huma.API, a *authenticator, gw storage.Gateway, natsURL string, hub SimHub) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-outposts",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/outposts",
		Summary:     "List outposts",
		Description: "Lists the edge outposts. An outpost is fleet-wide, so listing requires an all-scope read. Gated by outpost:read.",
	}, "outpost", "read"), func(ctx context.Context, _ *struct{}) (*listOutpostsOutput, error) {
		outposts, err := gw.ListOutposts(ctx, a.scopeFor(ctx, "outpost", "read"))
		if err != nil {
			return nil, mapOutpostErr(err)
		}
		ids := make([]string, len(outposts))
		for i := range outposts {
			ids[i] = outposts[i].PrincipalID
		}
		effTags, err := gw.EffectiveTags(ctx, "outpost", ids)
		if err != nil {
			return nil, huma.Error500InternalServerError("list outposts")
		}
		out := &listOutpostsOutput{}
		out.Body.Outposts = make([]outpostBody, 0, len(outposts))
		for i := range outposts {
			b := toOutpostBody(&outposts[i])
			b.EffectiveTags = effTags[outposts[i].PrincipalID]
			out.Body.Outposts = append(out.Body.Outposts, b)
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-outpost",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/outposts/{name}",
		Summary:     "Get an outpost",
		Description: "Fetches an outpost by name. Requires an all-scope read. Gated by outpost:read.",
	}, "outpost", "read"), func(ctx context.Context, in *outpostPathInput) (*outpostOutput, error) {
		n, err := gw.GetOutpost(ctx, in.Name, a.scopeFor(ctx, "outpost", "read"))
		if err != nil {
			return nil, mapOutpostErr(err)
		}
		b := toOutpostBody(n)
		effTags, err := gw.EffectiveTags(ctx, "outpost", []string{n.PrincipalID})
		if err != nil {
			return nil, huma.Error500InternalServerError("get outpost")
		}
		b.EffectiveTags = effTags[n.PrincipalID]
		return &outpostOutput{Body: b}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-outpost",
		Method:        http.MethodPost,
		Path:          "/outposts",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create an outpost",
		Description:   "Registers an edge outpost server-side (day-one enrollment: create, then :enroll to mint its token). Gated by outpost:create.",
	}, "outpost", "create"), func(ctx context.Context, in *createOutpostInput) (*outpostOutput, error) {
		n, err := gw.CreateOutpost(ctx, actorID(ctx), storage.OutpostSpec{
			Name: in.Body.Name, Label: in.Body.Label, Description: in.Body.Description, LocationName: in.Body.Location,
		}, a.scopeFor(ctx, "outpost", "create"), a.scopeFor(ctx, "location", "read"))
		if err != nil {
			return nil, mapOutpostErr(err)
		}
		return &outpostOutput{Body: toOutpostBody(n)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-outpost",
		Method:      http.MethodPatch,
		Path:        "/outposts/{name}",
		Summary:     "Update an outpost",
		Description: "Patches an outpost's label, description, and location (a nil field is unchanged; a location of \"\" clears it). The name is immutable. Requires an all-scope action. Gated by outpost:update.",
	}, "outpost", "update"), func(ctx context.Context, in *updateOutpostInput) (*outpostOutput, error) {
		n, err := gw.UpdateOutpost(ctx, actorID(ctx), in.Name, storage.OutpostPatch{
			Label: in.Body.Label, Description: in.Body.Description, LocationName: in.Body.Location,
		}, a.scopeFor(ctx, "outpost", "read"), a.scopeFor(ctx, "outpost", "update"), a.scopeFor(ctx, "location", "read"))
		if err != nil {
			return nil, mapOutpostErr(err)
		}
		return &outpostOutput{Body: toOutpostBody(n)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-outpost",
		Method:        http.MethodDelete,
		Path:          "/outposts/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete an outpost",
		Description:   "Decommissions an outpost: a hard delete that cascades its endpoints, their derived tasks, its outpost-owned tags and self-telemetry, its platform component, and its enrollment credential. Component telemetry it collected is unaffected. Requires an all-scope action. Gated by outpost:delete.",
	}, "outpost", "delete"), func(ctx context.Context, in *outpostPathInput) (*struct{}, error) {
		if err := gw.DeleteOutpost(ctx, actorID(ctx), in.Name, a.scopeFor(ctx, "outpost", "read"), a.scopeFor(ctx, "outpost", "delete")); err != nil {
			return nil, mapOutpostErr(err)
		}
		// The deleted outpost's simulator status goes with it; a nil hub (no bus) has none.
		if hub != nil {
			hub.Forget(in.Name)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "enroll-outpost",
		Extensions:  plaintext(),
		Method:      http.MethodPost,
		Path:        "/outposts/{name}:enroll",
		Summary:     "Mint an outpost's enrollment token",
		Description: "Mints (or re-mints) the outpost's enrollment token and returns it once. The token is stored only as a hash; it is never logged. Gated by outpost:enroll.",
	}, "outpost", "enroll"), func(ctx context.Context, in *outpostPathInput) (*enrollOutput, error) {
		token, hash, _, err := auth.NewBearerToken()
		if err != nil {
			return nil, huma.Error500InternalServerError("mint enrollment token")
		}
		if _, err := gw.SetEnrollmentToken(ctx, actorID(ctx), in.Name, hex.EncodeToString(hash), a.scopeFor(ctx, "outpost", "enroll")); err != nil {
			return nil, mapOutpostErr(err)
		}
		out := &enrollOutput{}
		out.Body.Name = in.Name
		out.Body.Token = token
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "claim-outpost",
		Extensions:  plaintext(),
		Method:      http.MethodPost,
		Path:        "/outposts:claim",
		Summary:     "Claim an outpost identity in exchange for its NATS credential",
		Description: "The outpost-facing exchange: an outpost presents its enrollment token and receives its NATS credential (url, username, password). Public (the token is the authentication); an invalid token is a 401.",
		Errors:      []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *claimOutpostInput) (*claimOutput, error) {
		n, err := gw.ClaimOutpost(ctx, in.Body.Name, hex.EncodeToString(auth.HashToken(in.Body.Token)))
		if err != nil {
			return nil, mapOutpostErr(err)
		}
		out := &claimOutput{}
		out.Body.NatsURL = natsURL
		out.Body.Username = n.Name
		out.Body.Password = in.Body.Token
		return out, nil
	})
}

func mapOutpostErr(err error) error {
	if platErr, ok := mapPlatformErr(err); ok {
		return platErr
	}
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	switch {
	case errors.Is(err, storage.ErrOutpostNotFound):
		return huma.Error404NotFound("outpost not found")
	case errors.Is(err, storage.ErrOutpostExists):
		return huma.Error409Conflict("outpost name already exists")
	case errors.Is(err, storage.ErrOutpostForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrEnrollmentInvalid):
		return huma.Error401Unauthorized("invalid enrollment token")
	case errors.Is(err, storage.ErrEntityNameIsUUID):
		return huma.Error422UnprocessableEntity("outpost name may not be a uuid")
	case errors.Is(err, storage.ErrInvalidEntityName):
		return huma.Error422UnprocessableEntity("outpost name must be lowercase letters, digits, and hyphens, starting with a letter or digit")
	case errors.Is(err, storage.ErrLocationNotFound):
		return huma.Error422UnprocessableEntity("location not found")
	default:
		return huma.Error500InternalServerError("outpost operation failed")
	}
}
