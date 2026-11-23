package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The batch creates (#2368, ADR-2368): POST /locations:batchCreate,
// /systems:batchCreate and /components:batchCreate, the AIP-233 batch create
// of each collection. Each request is exactly the single create's body, run
// as the single create runs it, in order, in one transaction, under the
// caller's own scope; every row writes its own audit row, stamped from_batch
// with the one batch id the response returns. A refused row refuses the
// batch, mapped as its single create maps it and named by its position in the
// request. validate_only runs the same transaction and rolls it back, so a dry
// run refuses exactly what the write would.

// batchRowLimit is the most requests one batch create carries. A batch is one
// transaction, so a bigger import is split by the client (by building), each
// call its own batch and its own batch id; the limit keeps a call's lock span
// bounded and its body under the server's cap.
const batchRowLimit = 1000

type batchCreateLocationsInput struct {
	Body struct {
		Requests     []createLocationInputBody `json:"requests" minItems:"1" doc:"The locations to create, each the body POST /locations takes, in order: a parent before its children, which then name it as their parent"`
		ValidateOnly bool                      `json:"validate_only,omitempty" doc:"Run every request in the transaction and roll it back: the response is what the write would create (names minted, ids empty), or the row that would refuse it, and nothing is written"`
	}
}

type batchCreateSystemsInput struct {
	Body struct {
		Requests     []createSystemInputBody `json:"requests" minItems:"1" doc:"The systems to create, each the body POST /systems takes, in order: a parent before its children"`
		ValidateOnly bool                    `json:"validate_only,omitempty" doc:"Run every request in the transaction and roll it back: the response is what the write would create (names minted, ids empty), or the row that would refuse it, and nothing is written"`
	}
}

type batchCreateComponentsInput struct {
	Body struct {
		Requests     []createComponentInputBody `json:"requests" minItems:"1" doc:"The components to create, each the body POST /components takes, in order: a parent before its children"`
		ValidateOnly bool                       `json:"validate_only,omitempty" doc:"Run every request in the transaction and roll it back: the response is what the write would create (names minted, ids empty), or the row that would refuse it, and nothing is written"`
	}
}

type batchCreateLocationsOutput struct {
	Status int
	Body   struct {
		Locations    []locationBody `json:"locations" doc:"The locations created, each as POST /locations answers it, in request order"`
		BatchID      string         `json:"batch_id,omitempty" doc:"The batch id every audit row of this call carries as from_batch; omitted on a dry run"`
		ValidateOnly bool           `json:"validate_only" doc:"Whether this was a dry run that wrote nothing"`
	}
}

type batchCreateSystemsOutput struct {
	Status int
	Body   struct {
		Systems      []systemBody `json:"systems" doc:"The systems created, each as POST /systems answers it, in request order"`
		BatchID      string       `json:"batch_id,omitempty" doc:"The batch id every audit row of this call carries as from_batch; omitted on a dry run"`
		ValidateOnly bool         `json:"validate_only" doc:"Whether this was a dry run that wrote nothing"`
	}
}

type batchCreateComponentsOutput struct {
	Status int
	Body   struct {
		Components   []componentBody `json:"components" doc:"The components created, each as POST /components answers it, in request order"`
		BatchID      string          `json:"batch_id,omitempty" doc:"The batch id every audit row of this call carries as from_batch; omitted on a dry run"`
		ValidateOnly bool            `json:"validate_only" doc:"Whether this was a dry run that wrote nothing"`
	}
}

// batchStart refuses a batch over the row limit, and returns the context its
// rows are written under (stamped with a new batch id unless it is a dry run),
// the id, and the status the call answers.
func batchStart(ctx context.Context, n int, validateOnly bool) (context.Context, string, int, error) {
	if n > batchRowLimit {
		return ctx, "", 0, huma.NewError(http.StatusRequestEntityTooLarge,
			fmt.Sprintf("the batch carries %d requests, over the limit of %d; split it into batches of at most %d (by building) and send each", n, batchRowLimit, batchRowLimit))
	}
	if validateOnly {
		return ctx, "", http.StatusOK, nil
	}
	id := uuid.Must(uuid.NewV7()).String()
	return storage.WithBatch(ctx, id), id, http.StatusCreated, nil
}

// atRow puts a row's position in the request on a refusal: leading the detail,
// and as the error's location, so a client finds the row that refused.
func atRow(i int, err error) error {
	path := fmt.Sprintf("requests[%d]", i)
	var em *huma.ErrorModel
	if errors.As(err, &em) {
		em.Errors = append(em.Errors, &huma.ErrorDetail{Location: "body." + path, Message: em.Detail})
		em.Detail = path + ": " + em.Detail
	}
	return err
}

// batchErr maps a refused batch as the refusing row's single create maps it,
// at that row.
func batchErr(err error, mapErr func(error) error) error {
	var be *storage.BatchError
	if errors.As(err, &be) {
		return atRow(be.Index, mapErr(be.Err))
	}
	return mapErr(err)
}

// dryIDs is what a dry run blanks: every id the batch minted, which the
// rollback discarded. A reference to a row that was already there keeps its
// id. Pure.
func dryIDs(minted map[string]bool, id *string, parentID **string) {
	*id = ""
	if *parentID != nil && minted[**parentID] {
		*parentID = nil
	}
}

func registerBatchCreateRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "batch-create-locations",
		Extensions:    mcpExposed(),
		Method:        http.MethodPost,
		Path:          "/locations:batchCreate",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create many locations in one write",
		Description: "Creates locations in one transaction: each request is the body POST /locations takes, run as that create runs it, in order, so a location may name as its parent one an earlier request creates. Every row writes its own audit row carrying the one batch_id the response returns, under from_batch. A refused request refuses the whole batch, mapped as POST /locations maps it, with its position in the request (requests[3]) leading the detail; nothing lands. validate_only runs the same transaction and rolls it back, answering 200 with the names the write would mint and no ids. " +
			fmt.Sprintf("A batch carries at most %d requests; more is a 413 naming the limit, so split a bigger import (by building). ", batchRowLimit) +
			"Gated by location:create, and each request is held to that scope as its single create is.",
	}, "location", "create"), func(ctx context.Context, in *batchCreateLocationsInput) (*batchCreateLocationsOutput, error) {
		ctx, id, status, err := batchStart(ctx, len(in.Body.Requests), in.Body.ValidateOnly)
		if err != nil {
			return nil, err
		}
		specs := make([]storage.LocationSpec, len(in.Body.Requests))
		for i, r := range in.Body.Requests {
			specs[i] = storage.LocationSpec{Name: r.Name, Label: r.Label, LocationType: r.LocationType, ParentName: r.Parent, ExpectedName: r.ExpectedName}
		}
		made, err := gw.BatchCreateLocations(ctx, actorID(ctx), specs, a.scopeFor(ctx, "location", "create"), in.Body.ValidateOnly)
		if err != nil {
			return nil, batchErr(err, mapLocationErr)
		}
		out := &batchCreateLocationsOutput{Status: status}
		out.Body.BatchID, out.Body.ValidateOnly = id, in.Body.ValidateOnly
		out.Body.Locations = make([]locationBody, 0, len(made))
		minted := map[string]bool{}
		for _, l := range made {
			minted[l.ID] = true
		}
		for _, l := range made {
			b := toLocationBody(l)
			if in.Body.ValidateOnly {
				dryIDs(minted, &b.ID, &b.ParentID)
			}
			out.Body.Locations = append(out.Body.Locations, b)
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "batch-create-systems",
		Extensions:    mcpExposed(),
		Method:        http.MethodPost,
		Path:          "/systems:batchCreate",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create many systems in one write",
		Description: "Creates systems in one transaction: each request is the body POST /systems takes, run as that create runs it, in order, so a system may name as its parent one an earlier request creates, and its location by name as the single create does. Every row writes its own audit row carrying the one batch_id the response returns, under from_batch. A refused request refuses the whole batch, mapped as POST /systems maps it, with its position in the request (requests[3]) leading the detail; nothing lands. validate_only runs the same transaction and rolls it back, answering 200 with the names the write would mint and no ids. " +
			fmt.Sprintf("A batch carries at most %d requests; more is a 413 naming the limit, so split a bigger import (by building). ", batchRowLimit) +
			"Gated by system:create; each request is held to that scope, and its location resolves within your location:read scope, as its single create is.",
	}, "system", "create"), func(ctx context.Context, in *batchCreateSystemsInput) (*batchCreateSystemsOutput, error) {
		ctx, id, status, err := batchStart(ctx, len(in.Body.Requests), in.Body.ValidateOnly)
		if err != nil {
			return nil, err
		}
		specs := make([]storage.SystemSpec, len(in.Body.Requests))
		for i, r := range in.Body.Requests {
			specs[i] = storage.SystemSpec{Name: r.Name, Label: r.Label, StandardID: ptrOrNil(r.StandardID), SystemTypeID: ptrOrNil(r.SystemTypeID),
				ParentName: r.Parent, LocationName: r.Location, ExpectedName: r.ExpectedName}
		}
		made, err := gw.BatchCreateSystems(ctx, actorID(ctx), specs, a.scopeFor(ctx, "system", "create"), a.scopeFor(ctx, "location", "read"), in.Body.ValidateOnly)
		if err != nil {
			return nil, batchErr(err, mapSystemErr)
		}
		out := &batchCreateSystemsOutput{Status: status}
		out.Body.BatchID, out.Body.ValidateOnly = id, in.Body.ValidateOnly
		out.Body.Systems = make([]systemBody, 0, len(made))
		minted := map[string]bool{}
		for _, s := range made {
			minted[s.ID] = true
		}
		for _, s := range made {
			b := toSystemBody(s)
			if in.Body.ValidateOnly {
				dryIDs(minted, &b.ID, &b.ParentID)
			}
			out.Body.Systems = append(out.Body.Systems, b)
		}
		return out, nil
	})

	huma.Register(api, a.conditionalGated(a.gated(huma.Operation{
		OperationID:   "batch-create-components",
		Extensions:    mcpExposed(),
		Method:        http.MethodPost,
		Path:          "/components:batchCreate",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create many components in one write",
		Description: "Creates components in one transaction: each request is the body POST /components takes, run as that create runs it, in order, so a component may name as its parent one an earlier request creates, and its system and location by name as the single create does. Every row writes its own audit row carrying the one batch_id the response returns, under from_batch. A refused request refuses the whole batch, mapped as POST /components maps it, with its position in the request (requests[3]) leading the detail; nothing lands. validate_only runs the same transaction and rolls it back, answering 200 with the names the write would mint and no ids. " +
			fmt.Sprintf("A batch carries at most %d requests; more is a 413 naming the limit, so split a bigger import (by building). ", batchRowLimit) +
			"Gated by component:create; each request is held to that scope, its location resolves within your location:read scope, and a request naming a system also needs system:update over it, as its single create does.",
	}, "component", "create"), "system", "update"), func(ctx context.Context, in *batchCreateComponentsInput) (*batchCreateComponentsOutput, error) {
		ctx, id, status, err := batchStart(ctx, len(in.Body.Requests), in.Body.ValidateOnly)
		if err != nil {
			return nil, err
		}
		specs := make([]storage.ComponentSpec, len(in.Body.Requests))
		for i, r := range in.Body.Requests {
			if r.Product == nil || *r.Product == "" {
				return nil, atRow(i, huma.Error422UnprocessableEntity(errProductRequired))
			}
			if r.System != nil && *r.System != "" && !a.allows(ctx, "system", "update") {
				return nil, atRow(i, huma.Error403Forbidden(ErrSystemBindNeedsUpdate))
			}
			specs[i] = storage.ComponentSpec{Name: r.Name, Label: r.Label, ParentName: r.Parent, SystemName: r.System,
				LocationName: r.Location, ProductName: r.Product, ExpectedName: r.ExpectedName}
		}
		made, err := gw.BatchCreateComponents(ctx, actorID(ctx), specs, a.scopeFor(ctx, "component", "create"), a.scopeFor(ctx, "location", "read"),
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"), in.Body.ValidateOnly)
		if err != nil {
			return nil, batchErr(err, mapComponentErr)
		}
		out := &batchCreateComponentsOutput{Status: status}
		out.Body.BatchID, out.Body.ValidateOnly = id, in.Body.ValidateOnly
		out.Body.Components = make([]componentBody, 0, len(made))
		minted := map[string]bool{}
		for _, c := range made {
			minted[c.ID] = true
		}
		for _, c := range made {
			b := toComponentBody(c)
			if in.Body.ValidateOnly {
				dryIDs(minted, &b.ID, &b.ParentID)
			}
			out.Body.Components = append(out.Body.Components, b)
		}
		return out, nil
	})
	declareDryRunStatus(api, "/locations:batchCreate", "/systems:batchCreate", "/components:batchCreate")
}

// declareDryRunStatus adds the 200 a validate_only call answers beside the 201
// of a write, with the same body, so the spec and the typed client name both.
func declareDryRunStatus(api huma.API, paths ...string) {
	for _, path := range paths {
		item := api.OpenAPI().Paths[path]
		if item == nil || item.Post == nil || item.Post.Responses["201"] == nil {
			continue
		}
		ok := *item.Post.Responses["201"]
		ok.Description = "The dry run: what the write would create, nothing written"
		item.Post.Responses["200"] = &ok
	}
}
