package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The unclaimed-source inbox (decision 4 of #1146): every source a listener
// heard from that no endpoint claims, aggregated per source and bounded, so
// onboarding a sender reads here rather than in a log. A webhook post to an
// unknown or rotated token lands a row by its sender's address; the token it
// tried is never kept. Claiming one is the existing endpoint create (the
// console opens it pre-filled), and the row is let go once the sender's
// posts land on an endpoint.

type unclaimedSourceBody struct {
	Host         string    `json:"host" doc:"The listener's host: server, for the webhook route"`
	Transport    string    `json:"transport" doc:"The push transport the source sent over"`
	Source       string    `json:"source" doc:"The sender's address"`
	FirstHeardAt time.Time `json:"first_heard_at" doc:"When the listener first heard from it"`
	LastHeardAt  time.Time `json:"last_heard_at" doc:"When the listener last heard from it"`
	Count        int64     `json:"count" doc:"How many messages it sent that nothing claimed"`
	LastMessage  string    `json:"last_message" doc:"Its last message, cut to 512 bytes: a stranger's body is untrusted data, so no more of it is kept"`
}

type listUnclaimedSourcesOutput struct {
	Body struct {
		Sources []unclaimedSourceBody `json:"sources"`
	}
}

func registerUnclaimedRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-unclaimed-sources",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/unclaimed-sources",
		Summary:     "List the sources no endpoint claims",
		Description: "The unclaimed-source inbox: each source a listener heard from that no endpoint claims (a webhook post to an unknown or rotated token), with how often, when first and last, and its last message cut to 512 bytes. Bounded at 256 sources per listener, the least recently heard evicted, and aged out after seven days; a source is let go once its messages land on an endpoint. Claim one by creating its endpoint. Gated by node:read: it is the listener's own observation.",
	}, "node", "read"), func(ctx context.Context, _ *struct{}) (*listUnclaimedSourcesOutput, error) {
		us, err := gw.ListUnclaimedSources(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list unclaimed sources")
		}
		out := &listUnclaimedSourcesOutput{}
		out.Body.Sources = make([]unclaimedSourceBody, 0, len(us))
		for _, u := range us {
			out.Body.Sources = append(out.Body.Sources, unclaimedSourceBody{
				Host: u.Host, Transport: u.Transport, Source: u.Source,
				FirstHeardAt: u.FirstHeardAt, LastHeardAt: u.LastHeardAt, Count: u.Count, LastMessage: u.LastMessage,
			})
		}
		return out, nil
	})
}
