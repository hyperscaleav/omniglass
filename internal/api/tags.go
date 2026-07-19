package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// tagBody is the wire shape of a key in the governed vocabulary.
type tagBody struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	AppliesTo     []string `json:"applies_to" doc:"Entity kinds this key may bind to; empty means universal"`
	Propagates    bool     `json:"propagates" doc:"Whether a bound value cascades to descendants"`
	AllowedValues []string `json:"allowed_values" doc:"The value enum a bound value must belong to; empty means free text"`
}

// tagBindingBody is the wire shape of one bound value.
type tagBindingBody struct {
	Key       string  `json:"key"`
	Value     string  `json:"value"`
	OwnerKind string  `json:"owner_kind"`
	OwnerID   *string `json:"owner_id,omitempty"`
	OwnerName string  `json:"owner_name,omitempty"`
}

// resolvedTagBody is one entry in a component's effective-tags cascade: the tag,
// where in the chain its owner sits, and whether it is the winner or a shadowed
// candidate.
type resolvedTagBody struct {
	Key       string  `json:"key"`
	Value     string  `json:"value"`
	OwnerKind string  `json:"owner_kind"`
	OwnerID   *string `json:"owner_id,omitempty"`
	OwnerName string  `json:"owner_name,omitempty"`
	Band      int     `json:"band" doc:"Cascade tier: 0 global, 1 location, 2 system, 3 component"`
	Depth     int     `json:"depth" doc:"Distance up the tier's tree from the component (0 nearest)"`
	Winner    bool    `json:"winner" doc:"True for the resolved value; false for a shadowed candidate"`
}

func toTagBody(t *storage.Tag) tagBody {
	return tagBody{ID: t.ID, Name: t.Name, AppliesTo: t.AppliesTo, Propagates: t.Propagates, AllowedValues: t.AllowedValues}
}

func toTagBindingBody(b *storage.TagBinding) tagBindingBody {
	return tagBindingBody{
		Key: b.Key, Value: b.Value,
		OwnerKind: b.OwnerKind, OwnerID: b.OwnerID, OwnerName: b.OwnerName,
	}
}

type listTagsOutput struct {
	Body struct {
		Tags []tagBody `json:"tags"`
	}
}

type tagOutput struct {
	Body tagBody
}

type tagValuesOutput struct {
	Body struct {
		Values []string `json:"values"`
	}
}

type createTagInput struct {
	Body struct {
		Name          string   `json:"name" minLength:"1" doc:"The normalized key: a lowercase identifier, unique tenant-wide"`
		AppliesTo     []string `json:"applies_to,omitempty" doc:"Entity kinds this key may bind to (component, system, location); omit for universal"`
		Propagates    *bool    `json:"propagates,omitempty" doc:"Whether bindings cascade to descendants; defaults true"`
		AllowedValues []string `json:"allowed_values,omitempty" doc:"The value enum a bound value must belong to; omit for free text"`
	}
}

type updateTagInput struct {
	Name string `path:"name" doc:"The tag key"`
	Body struct {
		AppliesTo     []string `json:"applies_to,omitempty" doc:"Entity kinds this key may bind to; omit for universal"`
		Propagates    *bool    `json:"propagates,omitempty" doc:"Whether bindings cascade to descendants; defaults true"`
		AllowedValues []string `json:"allowed_values,omitempty" doc:"The value enum a bound value must belong to; omit for free text"`
	}
}

type tagNameInput struct {
	Name string `path:"name" doc:"The tag key"`
}

type globalBindingInput struct {
	Name string `path:"name" doc:"The tag key"`
	Body struct {
		Value string `json:"value" minLength:"1" doc:"The bound value"`
	}
}

type tagBindingOutput struct {
	Body tagBindingBody
}

type entityTagsOutput struct {
	Body struct {
		Tags []tagBindingBody `json:"tags"`
	}
}

type effectiveTagsInput struct {
	Name string `path:"name" doc:"The component's name"`
}

type effectiveTagsOutput struct {
	Body struct {
		Tags []resolvedTagBody `json:"tags"`
	}
}

// registerTagRoutes wires the tag surface: the governed key vocabulary (minting
// gated by tag:create, an admin action), the per-entity value bindings (gated by
// the owner's own update permission), the global binding (tag:update), and the
// per-component effective-tags cascade. Reading the vocabulary and an entity's
// tags rides the viewer floor.
func registerTagRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-tags",
		Method:      http.MethodGet,
		Path:        "/tags",
		Summary:     "List tag keys",
		Description: "Lists the governed key vocabulary. Rides the tag:read floor.",
	}, "tag", "read"), func(ctx context.Context, _ *struct{}) (*listTagsOutput, error) {
		tags, err := gw.ListTags(ctx)
		if err != nil {
			return nil, mapTagErr(err)
		}
		out := &listTagsOutput{}
		out.Body.Tags = make([]tagBody, 0, len(tags))
		for i := range tags {
			out.Body.Tags = append(out.Body.Tags, toTagBody(&tags[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-tag-values",
		Method:      http.MethodGet,
		Path:        "/tags/{name}:values",
		Summary:     "List the distinct values bound for a key",
		Description: "Returns the distinct values already bound for a key across the estate, for value autocomplete on a free-text key (an enum key carries its allowed set on the key itself). Rides the tag:read floor.",
	}, "tag", "read"), func(ctx context.Context, in *tagNameInput) (*tagValuesOutput, error) {
		vals, err := gw.DistinctTagValues(ctx, in.Name)
		if err != nil {
			return nil, mapTagErr(err)
		}
		out := &tagValuesOutput{}
		out.Body.Values = vals
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-tag",
		Method:        http.MethodPost,
		Path:          "/tags",
		DefaultStatus: http.StatusCreated,
		Summary:       "Mint a tag key",
		Description:   "Adds a key to the governed vocabulary. The name is normalized (a lowercase identifier). Gated by tag:create (all-scope, an admin action).",
	}, "tag", "create"), func(ctx context.Context, in *createTagInput) (*tagOutput, error) {
		t, err := gw.CreateTag(ctx, actorID(ctx), storage.TagSpec{
			Name:          in.Body.Name,
			AppliesTo:     in.Body.AppliesTo,
			Propagates:    propagatesOr(in.Body.Propagates),
			AllowedValues: in.Body.AllowedValues,
		}, a.scopeFor(ctx, "tag", "create"))
		if err != nil {
			return nil, mapTagErr(err)
		}
		return &tagOutput{Body: toTagBody(t)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-tag",
		Method:      http.MethodPatch,
		Path:        "/tags/{name}",
		Summary:     "Update a tag key",
		Description: "Replaces a key's governance fields (applies_to, propagates); the name is fixed. Gated by tag:update (all-scope).",
	}, "tag", "update"), func(ctx context.Context, in *updateTagInput) (*tagOutput, error) {
		t, err := gw.UpdateTag(ctx, actorID(ctx), in.Name, storage.TagSpec{
			AppliesTo:     in.Body.AppliesTo,
			Propagates:    propagatesOr(in.Body.Propagates),
			AllowedValues: in.Body.AllowedValues,
		}, a.scopeFor(ctx, "tag", "update"))
		if err != nil {
			return nil, mapTagErr(err)
		}
		return &tagOutput{Body: toTagBody(t)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-tag",
		Method:        http.MethodDelete,
		Path:          "/tags/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a tag key",
		Description:   "Removes a key from the vocabulary, cascading its bindings. Gated by tag:delete (all-scope).",
	}, "tag", "delete"), func(ctx context.Context, in *tagNameInput) (*struct{}, error) {
		if err := gw.DeleteTag(ctx, actorID(ctx), in.Name, a.scopeFor(ctx, "tag", "delete")); err != nil {
			return nil, mapTagErr(err)
		}
		return nil, nil
	})

	// Global binding: a tenant-wide default value for a key, gated by tag:update
	// (there is no owning entity to defer to). Modeled as custom methods on the
	// key so the generated CLI reads `tag set-global <key>` / `tag clear-global`.
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-global-tag",
		Method:      http.MethodPost,
		Path:        "/tags/{name}:setGlobal",
		Summary:     "Set a global tag value",
		Description: "Binds a tenant-wide default value for a key at the global scope. Gated by tag:update (all-scope).",
	}, "tag", "update"), func(ctx context.Context, in *globalBindingInput) (*tagBindingOutput, error) {
		b, err := gw.SetTagBinding(ctx, actorID(ctx), in.Name, "global", nil, in.Body.Value,
			a.scopeFor(ctx, "tag", "update"), a.scopeFor(ctx, "tag", "update"))
		if err != nil {
			return nil, mapTagErr(err)
		}
		return &tagBindingOutput{Body: toTagBindingBody(b)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "clear-global-tag",
		Method:        http.MethodPost,
		Path:          "/tags/{name}:clearGlobal",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Clear a global tag value",
		Description:   "Removes the global binding for a key. Gated by tag:update (all-scope).",
	}, "tag", "update"), func(ctx context.Context, in *tagNameInput) (*struct{}, error) {
		if err := gw.DeleteTagBinding(ctx, actorID(ctx), in.Name, "global", nil,
			a.scopeFor(ctx, "tag", "update"), a.scopeFor(ctx, "tag", "update")); err != nil {
			return nil, mapTagErr(err)
		}
		return nil, nil
	})

	// Per-entity bindings for each scoped kind, gated by the entity's own update.
	registerEntityTagRoutes(api, a, gw, "component")
	registerEntityTagRoutes(api, a, gw, "system")
	registerEntityTagRoutes(api, a, gw, "location")
	registerEntityTagRoutes(api, a, gw, "node")

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "effective-tags",
		Method:      http.MethodGet,
		Path:        "/components/{name}/effective-tags",
		Summary:     "Effective tags for a component",
		Description: "Resolves the tags that cascade onto a component (global -> location -> system -> component): keys union, values override most-specific-wins, with the winner and shadowed candidates. A non-propagating key resolves only from a binding on the component itself. Gated by component:read; the component must be in the caller's component read scope.",
	}, "component", "read"), func(ctx context.Context, in *effectiveTagsInput) (*effectiveTagsOutput, error) {
		comp, err := gw.GetComponent(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		resolved, err := gw.ResolveTags(ctx, comp.ID, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapTagErr(err)
		}
		out := &effectiveTagsOutput{}
		out.Body.Tags = make([]resolvedTagBody, 0, len(resolved))
		for _, r := range resolved {
			out.Body.Tags = append(out.Body.Tags, resolvedTagBody{
				Key: r.Key, Value: r.Value,
				OwnerKind: r.OwnerKind, OwnerID: r.OwnerID, OwnerName: r.OwnerName,
				Band: r.Band, Depth: r.Depth, Winner: r.Winner,
			})
		}
		return out, nil
	})
}

// entitySetTagInput carries the entity name and the key + value to bind;
// entityRemoveTagInput carries the name and the key to remove. The entity kind
// is fixed by the route the closure registers.
type entitySetTagInput struct {
	Name string `path:"name" doc:"The entity's name"`
	Body struct {
		Key   string `json:"key" minLength:"1" doc:"The tag key (must exist and apply to this kind)"`
		Value string `json:"value" minLength:"1" doc:"The bound value"`
	}
}

type entityRemoveTagInput struct {
	Name string `path:"name" doc:"The entity's name"`
	Body struct {
		Key string `json:"key" minLength:"1" doc:"The tag key to remove"`
	}
}

type entityNameInput struct {
	Name string `path:"name" doc:"The entity's name"`
}

// registerEntityTagRoutes registers the list, set, and remove binding routes for
// one entity kind. Binding is modeled as custom methods on the entity (like the
// principal lifecycle actions), so the generated CLI reads `component set-tag
// <name>` rather than colliding with the top-level tag resource. Reading an
// entity's tags rides that entity's read floor; setting or removing a value is
// the entity's own update, so binding a tag needs no permission beyond the write
// the operator already holds on the entity.
func registerEntityTagRoutes(api huma.API, a *authenticator, gw storage.Gateway, kind string) {
	base := "/" + kind + "s/{name}"

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-" + kind + "-tags",
		Method:      http.MethodGet,
		Path:        base + ":listTags",
		Summary:     "List tags on a " + kind,
		Description: "Lists the tags bound directly on a " + kind + " (not the resolved cascade). Gated by " + kind + ":read.",
	}, kind, "read"), func(ctx context.Context, in *entityNameInput) (*entityTagsOutput, error) {
		binds, err := gw.ListEntityTags(ctx, kind, &in.Name, a.scopeFor(ctx, kind, "read"))
		if err != nil {
			return nil, mapTagErr(err)
		}
		out := &entityTagsOutput{}
		out.Body.Tags = make([]tagBindingBody, 0, len(binds))
		for i := range binds {
			out.Body.Tags = append(out.Body.Tags, toTagBindingBody(&binds[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-" + kind + "-tag",
		Method:      http.MethodPost,
		Path:        base + ":setTag",
		Summary:     "Set a tag value on a " + kind,
		Description: "Binds a value for a key on a " + kind + ". The key must exist and apply to this entity kind. Setting a value is the ordinary entity write, gated by " + kind + ":update.",
	}, kind, "update"), func(ctx context.Context, in *entitySetTagInput) (*tagBindingOutput, error) {
		b, err := gw.SetTagBinding(ctx, actorID(ctx), in.Body.Key, kind, &in.Name, in.Body.Value,
			a.scopeFor(ctx, kind, "read"), a.scopeFor(ctx, kind, "update"))
		if err != nil {
			return nil, mapTagErr(err)
		}
		return &tagBindingOutput{Body: toTagBindingBody(b)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "remove-" + kind + "-tag",
		Method:        http.MethodPost,
		Path:          base + ":removeTag",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Remove a tag value from a " + kind,
		Description:   "Removes a key's value from a " + kind + ". Gated by " + kind + ":update.",
	}, kind, "update"), func(ctx context.Context, in *entityRemoveTagInput) (*struct{}, error) {
		if err := gw.DeleteTagBinding(ctx, actorID(ctx), in.Body.Key, kind, &in.Name,
			a.scopeFor(ctx, kind, "read"), a.scopeFor(ctx, kind, "update")); err != nil {
			return nil, mapTagErr(err)
		}
		return nil, nil
	})
}

// propagatesOr resolves the optional propagates flag, defaulting to true (a tag
// cascades unless the operator opts out).
func propagatesOr(p *bool) bool {
	if p == nil {
		return true
	}
	return *p
}

// mapTagErr translates the gateway's tag sentinels into HTTP status. The
// component sentinels leak through the binding paths (owner resolution), so they
// are mapped here too.
func mapTagErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrTagNotFound):
		return huma.Error404NotFound("tag key not found")
	case errors.Is(err, storage.ErrTagBindingNotFound):
		return huma.Error404NotFound("tag binding not found")
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	case errors.Is(err, storage.ErrSystemNotFound):
		return huma.Error404NotFound("system not found")
	case errors.Is(err, storage.ErrLocationNotFound):
		return huma.Error404NotFound("location not found")
	case errors.Is(err, storage.ErrNodeNotFound):
		return huma.Error404NotFound("node not found")
	case errors.Is(err, storage.ErrTagForbidden),
		errors.Is(err, storage.ErrComponentForbidden),
		errors.Is(err, storage.ErrSystemForbidden),
		errors.Is(err, storage.ErrLocationForbidden),
		errors.Is(err, storage.ErrNodeForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrTagExists):
		return huma.Error409Conflict("a tag key with this name already exists")
	case errors.Is(err, storage.ErrTagKeyInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, storage.ErrTagAppliesToInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, storage.ErrTagValueInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, storage.ErrTagValueNotAllowed):
		return huma.Error422UnprocessableEntity("value is not in this key's allowed set")
	case errors.Is(err, storage.ErrTagKindNotAllowed):
		return huma.Error422UnprocessableEntity("this tag key does not apply to this entity kind")
	default:
		return huma.Error500InternalServerError("tag operation failed")
	}
}
