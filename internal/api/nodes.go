package api

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type nodeBody struct {
	Name            string            `json:"name"`
	DisplayName     string            `json:"display_name,omitempty"`
	Description     string            `json:"description,omitempty"`
	Location        *string           `json:"location,omitempty" doc:"The location the node sits in (descriptive placement, not scope)"`
	Enrolled        bool              `json:"enrolled"`
	LastHeartbeatAt *time.Time        `json:"last_heartbeat_at,omitempty"`
	EnrolledAt      *time.Time        `json:"enrolled_at,omitempty"`
	EffectiveTags   map[string]string `json:"effective_tags,omitempty" doc:"The resolved effective tags (key -> winning value) on this node: its direct bindings plus propagating global tags. For the Tags column and the blade pills."`
}

func toNodeBody(n *storage.Node) nodeBody {
	return nodeBody{
		Name: n.Name, DisplayName: n.DisplayName, Description: n.Description, Location: n.LocationName,
		Enrolled: n.Enrolled, LastHeartbeatAt: n.LastHeartbeatAt, EnrolledAt: n.EnrolledAt,
	}
}

type listNodesOutput struct {
	Body struct {
		Nodes []nodeBody `json:"nodes"`
	}
}

type nodeOutput struct {
	Body nodeBody
}

type nodePathInput struct {
	Name string `path:"name" doc:"The node's unique name"`
}

type createNodeInput struct {
	Body struct {
		Name        string  `json:"name" minLength:"1" doc:"Globally unique node name (also its NATS subject token, so no dots or whitespace)"`
		DisplayName string  `json:"display_name,omitempty" doc:"Operator label; falls back to the name when empty"`
		Description string  `json:"description,omitempty"`
		Location    *string `json:"location,omitempty" doc:"Optional location the node sits in (descriptive placement, not scope)"`
	}
}

// updateNodeInput is the PATCH body: a nil field is left unchanged. Name is not
// patchable (the immutable estate address). A Location of "" clears the placement.
type updateNodeInput struct {
	Name string `path:"name"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty"`
		Description *string `json:"description,omitempty"`
		Location    *string `json:"location,omitempty" doc:"Set the node's location, or \"\" to clear it"`
	}
}

type enrollOutput struct {
	Body struct {
		Name  string `json:"name"`
		Token string `json:"token" doc:"The enrollment token, shown once. Hand it to the node deployment; the node presents it to claim its NATS credential."`
	}
}

type claimNodeInput struct {
	Body struct {
		Name  string `json:"name" minLength:"1"`
		Token string `json:"token" minLength:"1"`
	}
}

type claimOutput struct {
	Body struct {
		NatsURL  string `json:"nats_url" doc:"The NATS URL the node dials"`
		Username string `json:"username" doc:"The node's NATS username (its node name)"`
		Password string `json:"password" doc:"The node's NATS password (its enrollment token)"`
	}
}

// registerNodeRoutes wires the node surface: AIP CRUD-ish create/list/get plus
// the :enroll custom method (mint a token) and the public :claim exchange (a node
// presents its token, gets its NATS credential). natsURL is the address the claim
// reply hands back so the node needs only the server URL.
func registerNodeRoutes(api huma.API, a *authenticator, gw storage.Gateway, natsURL string) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-nodes",
		Method:      http.MethodGet,
		Path:        "/nodes",
		Summary:     "List nodes",
		Description: "Lists the edge nodes. A node is estate-wide, so listing requires an all-scope read. Gated by node:read.",
	}, "node", "read"), func(ctx context.Context, _ *struct{}) (*listNodesOutput, error) {
		nodes, err := gw.ListNodes(ctx, a.scopeFor(ctx, "node", "read"))
		if err != nil {
			return nil, mapNodeErr(err)
		}
		ids := make([]string, len(nodes))
		for i := range nodes {
			ids[i] = nodes[i].PrincipalID
		}
		effTags, err := gw.EffectiveTags(ctx, "node", ids)
		if err != nil {
			return nil, huma.Error500InternalServerError("list nodes")
		}
		out := &listNodesOutput{}
		out.Body.Nodes = make([]nodeBody, 0, len(nodes))
		for i := range nodes {
			b := toNodeBody(&nodes[i])
			b.EffectiveTags = effTags[nodes[i].PrincipalID]
			out.Body.Nodes = append(out.Body.Nodes, b)
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-node",
		Method:      http.MethodGet,
		Path:        "/nodes/{name}",
		Summary:     "Get a node",
		Description: "Fetches a node by name. Requires an all-scope read. Gated by node:read.",
	}, "node", "read"), func(ctx context.Context, in *nodePathInput) (*nodeOutput, error) {
		n, err := gw.GetNode(ctx, in.Name, a.scopeFor(ctx, "node", "read"))
		if err != nil {
			return nil, mapNodeErr(err)
		}
		b := toNodeBody(n)
		effTags, err := gw.EffectiveTags(ctx, "node", []string{n.PrincipalID})
		if err != nil {
			return nil, huma.Error500InternalServerError("get node")
		}
		b.EffectiveTags = effTags[n.PrincipalID]
		return &nodeOutput{Body: b}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-node",
		Method:        http.MethodPost,
		Path:          "/nodes",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a node",
		Description:   "Registers an edge node server-side (day-one enrollment: create, then :enroll to mint its token). Gated by node:create.",
	}, "node", "create"), func(ctx context.Context, in *createNodeInput) (*nodeOutput, error) {
		if !validNodeName(in.Body.Name) {
			return nil, huma.Error422UnprocessableEntity("node name must be a single subject token (no dots, whitespace, or wildcards)")
		}
		n, err := gw.CreateNode(ctx, actorID(ctx), storage.NodeSpec{
			Name: in.Body.Name, DisplayName: in.Body.DisplayName, Description: in.Body.Description, LocationName: in.Body.Location,
		}, a.scopeFor(ctx, "node", "create"))
		if err != nil {
			return nil, mapNodeErr(err)
		}
		return &nodeOutput{Body: toNodeBody(n)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-node",
		Method:      http.MethodPatch,
		Path:        "/nodes/{name}",
		Summary:     "Update a node",
		Description: "Patches a node's display name, description, and location (a nil field is unchanged; a location of \"\" clears it). The name is immutable. Requires an all-scope action. Gated by node:update.",
	}, "node", "update"), func(ctx context.Context, in *updateNodeInput) (*nodeOutput, error) {
		n, err := gw.UpdateNode(ctx, actorID(ctx), in.Name, storage.NodePatch{
			DisplayName: in.Body.DisplayName, Description: in.Body.Description, LocationName: in.Body.Location,
		}, a.scopeFor(ctx, "node", "read"), a.scopeFor(ctx, "node", "update"))
		if err != nil {
			return nil, mapNodeErr(err)
		}
		return &nodeOutput{Body: toNodeBody(n)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "enroll-node",
		Method:      http.MethodPost,
		Path:        "/nodes/{name}:enroll",
		Summary:     "Mint a node's enrollment token",
		Description: "Mints (or re-mints) the node's enrollment token and returns it once. The token is stored only as a hash; it is never logged. Gated by node:enroll.",
	}, "node", "enroll"), func(ctx context.Context, in *nodePathInput) (*enrollOutput, error) {
		token, hash, _, err := auth.NewBearerToken()
		if err != nil {
			return nil, huma.Error500InternalServerError("mint enrollment token")
		}
		if _, err := gw.SetEnrollmentToken(ctx, actorID(ctx), in.Name, hex.EncodeToString(hash), a.scopeFor(ctx, "node", "enroll")); err != nil {
			return nil, mapNodeErr(err)
		}
		out := &enrollOutput{}
		out.Body.Name = in.Name
		out.Body.Token = token
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "claim-node",
		Method:      http.MethodPost,
		Path:        "/nodes:claim",
		Summary:     "Claim a node identity in exchange for its NATS credential",
		Description: "The node-facing exchange: a node presents its enrollment token and receives its NATS credential (url, username, password). Public (the token is the authentication); an invalid token is a 401.",
		Errors:      []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *claimNodeInput) (*claimOutput, error) {
		n, err := gw.ClaimNode(ctx, in.Body.Name, hex.EncodeToString(auth.HashToken(in.Body.Token)))
		if err != nil {
			return nil, mapNodeErr(err)
		}
		out := &claimOutput{}
		out.Body.NatsURL = natsURL
		out.Body.Username = n.Name
		out.Body.Password = in.Body.Token
		return out, nil
	})
}

// validNodeName rejects names that would break the subject model (a node name is
// the last token of its NATS subjects, matched by a single-token wildcard).
func validNodeName(name string) bool {
	if name == "" {
		return false
	}
	return !strings.ContainsAny(name, ". \t\n\r*>")
}

func mapNodeErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrNodeNotFound):
		return huma.Error404NotFound("node not found")
	case errors.Is(err, storage.ErrNodeExists):
		return huma.Error409Conflict("node name already exists")
	case errors.Is(err, storage.ErrNodeForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrEnrollmentInvalid):
		return huma.Error401Unauthorized("invalid enrollment token")
	case errors.Is(err, storage.ErrInvalidNodeName):
		return huma.Error422UnprocessableEntity("node name must be a single subject token (no dots, whitespace, or wildcards)")
	case errors.Is(err, storage.ErrLocationNotFound):
		return huma.Error422UnprocessableEntity("location not found")
	default:
		return huma.Error500InternalServerError("node operation failed")
	}
}
