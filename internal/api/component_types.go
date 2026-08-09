package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// componentTypeBody is the wire shape of a component_type registry row: the
// tree above product (mic, camera, wireless-mic under mic). Stem, Icon, and
// Abbrev are the product's console-facing name/glyph facts, inherited from
// the nearest ancestor that sets them (null in storage; rendered as "" here,
// same as the other optional scalar fields). ParentID/Parent are the tree
// link, absent for a root type. The registry lists alphabetically by
// display_name.
type componentTypeBody struct {
	ID          string   `json:"id" doc:"The component_type's uuid, the stable handle that survives a rename"`
	Name        string   `json:"name" doc:"The name an operator reads and types; renameable"`
	DisplayName string   `json:"display_name"`
	Stem        string   `json:"stem,omitempty" doc:"The auto-generated component name's prefix; empty inherits the nearest ancestor's"`
	Icon        string   `json:"icon,omitempty" doc:"A glyph key; empty inherits the nearest ancestor's"`
	Abbrev      string   `json:"abbrev,omitempty" doc:"A compact form of display_name; empty inherits the nearest ancestor's"`
	DefaultTags []string `json:"default_tags" doc:"Tags every instance of this type (or a descendant that does not override) starts with"`
	Official    bool     `json:"official"`
	ParentID    *string  `json:"parent_id,omitempty" doc:"The parent component_type's id, the canonical handle; absent for a root type"`
	Parent      *string  `json:"parent,omitempty" doc:"The parent component_type's name, for display; absent for a root type"`
}

func toComponentTypeBody(ct *storage.ComponentType, parentName *string) componentTypeBody {
	b := componentTypeBody{
		ID: ct.ID.String(), Name: ct.Name, DisplayName: ct.DisplayName,
		Stem: derefStr(ct.Stem), Icon: derefStr(ct.Icon), Abbrev: derefStr(ct.Abbrev),
		DefaultTags: ct.DefaultTags, Official: ct.Official,
	}
	if ct.ParentID != nil {
		pid := ct.ParentID.String()
		b.ParentID = &pid
		b.Parent = parentName
	}
	return b
}

type listComponentTypesOutput struct {
	Body struct {
		ComponentTypes []componentTypeBody `json:"component_types"`
	}
}

type componentTypePathInput struct {
	ID string `path:"id" doc:"The component_type id"`
}

type createComponentTypeInput struct {
	Body struct {
		Name        string `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The globally unique name"`
		DisplayName string `json:"display_name" minLength:"1" doc:"What an operator reads in pickers and lists"`
		// A stem is a name prefix (#627 Task 14 mints it straight into
		// "<stem>-<n>" component names), so it follows the same rule a name
		// does.
		Stem        string   `json:"stem,omitempty" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The auto-generated component name's prefix; omit to inherit the parent's. Lowercase letters, digits, and hyphens."`
		Icon        string   `json:"icon,omitempty" doc:"A glyph key; omit to inherit the parent's"`
		Abbrev      string   `json:"abbrev,omitempty" doc:"A compact form of display_name; omit to inherit the parent's"`
		DefaultTags []string `json:"default_tags,omitempty" doc:"Tags every instance of this type starts with"`
		ParentID    string   `json:"parent_id,omitempty" doc:"The parent component_type, by name or uuid; omit for a root type"`
	}
}

type updateComponentTypeInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty" doc:"A new operator-facing label"`
		// Same rule as create's Stem: a name prefix follows the name rule.
		Stem        *string   `json:"stem,omitempty" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"A new name prefix. Lowercase letters, digits, and hyphens."`
		Icon        *string   `json:"icon,omitempty" doc:"A new glyph key"`
		Abbrev      *string   `json:"abbrev,omitempty" doc:"A new compact form"`
		DefaultTags *[]string `json:"default_tags,omitempty" doc:"Replaces the default-tag set; omit to leave unchanged"`
	}
}

type componentTypeOutput struct {
	Body componentTypeBody
}

// mapComponentTypeErr translates the component_type storage sentinels into
// HTTP status. ErrParentTypeNotFound (a create/update naming a parent that
// does not resolve) is a 422 naming the offense; everything else falls
// through to the shared type-registry mapping (not-found 404, duplicate 409,
// official read-only 422, in-use 409).
func mapComponentTypeErr(err error) error {
	if errors.Is(err, storage.ErrParentTypeNotFound) {
		return huma.Error422UnprocessableEntity("parent component_type not found")
	}
	if errors.Is(err, storage.ErrRootComponentTypeNeedsStem) {
		return huma.Error422UnprocessableEntity("a root component_type (no parent) must have a stem; there is no ancestor to inherit one from")
	}
	return mapTypeErr(err, "component_type")
}

// resolveComponentTypeParent resolves a parent ref (name or uuid, "" for a
// root type) to the full row, so the caller has both the id CreateComponentType
// wants and the name a create/update response echoes back. An unknown ref
// normalizes to ErrParentTypeNotFound, the same sentinel the FK-backed create
// path raises, so mapComponentTypeErr has one branch for both.
func resolveComponentTypeParent(ctx context.Context, gw storage.Gateway, ref string) (*storage.ComponentType, error) {
	if ref == "" {
		return nil, nil
	}
	parent, err := gw.GetComponentType(ctx, ref)
	if err != nil {
		return nil, storage.ErrParentTypeNotFound
	}
	return parent, nil
}

// registerComponentTypeRoutes wires the component_type registry surface: list
// (the tree, flattened with parent links), create/update/delete of custom
// (non-official) nodes. Gated by component_type:read|create|update|delete;
// read sits in the viewer floor (*:read), the mutations at the admin tier,
// the same split as location_type.
func registerComponentTypeRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-component-types",
		Method:      http.MethodGet,
		Path:        "/component-types",
		Summary:     "List component types",
		Description: "Lists the component_type registry (the taxonomy a product is classified under: mic, camera, wireless-mic under mic), ordered alphabetically by display name. Each row carries its parent link, so the console reconstructs the tree client-side. Gated by component_type:read.",
	}, "component_type", "read"), func(ctx context.Context, _ *struct{}) (*listComponentTypesOutput, error) {
		types, err := gw.ListComponentTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list component types")
		}
		byID := make(map[uuid.UUID]string, len(types))
		for i := range types {
			byID[types[i].ID] = types[i].Name
		}
		out := &listComponentTypesOutput{}
		out.Body.ComponentTypes = make([]componentTypeBody, 0, len(types))
		for i := range types {
			var parentName *string
			if types[i].ParentID != nil {
				if name, ok := byID[*types[i].ParentID]; ok {
					parentName = &name
				}
			}
			out.Body.ComponentTypes = append(out.Body.ComponentTypes, toComponentTypeBody(&types[i], parentName))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-component-type",
		Method:        http.MethodPost,
		Path:          "/component-types",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a component type",
		Description:   "Creates a custom (non-official) component_type, optionally under a parent. Gated by component_type:create.",
	}, "component_type", "create"), func(ctx context.Context, in *createComponentTypeInput) (*componentTypeOutput, error) {
		parent, err := resolveComponentTypeParent(ctx, gw, in.Body.ParentID)
		if err != nil {
			return nil, mapComponentTypeErr(err)
		}
		var parentID *uuid.UUID
		var parentName *string
		if parent != nil {
			parentID = &parent.ID
			parentName = &parent.Name
		}
		ct, err := gw.CreateComponentType(ctx, actorID(ctx), storage.ComponentType{
			Name: in.Body.Name, DisplayName: in.Body.DisplayName,
			Stem: ptrOrNil(in.Body.Stem), Icon: ptrOrNil(in.Body.Icon), Abbrev: ptrOrNil(in.Body.Abbrev),
			DefaultTags: in.Body.DefaultTags, ParentID: parentID,
		})
		if err != nil {
			return nil, mapComponentTypeErr(err)
		}
		return &componentTypeOutput{Body: toComponentTypeBody(ct, parentName)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-component-type",
		Method:      http.MethodPatch,
		Path:        "/component-types/{id}",
		Summary:     "Update a component type",
		Description: "Patches a custom component_type's display_name, stem, icon, abbrev, or default_tags. Official types are read-only (422). Gated by component_type:update.",
	}, "component_type", "update"), func(ctx context.Context, in *updateComponentTypeInput) (*componentTypeOutput, error) {
		ct, err := gw.UpdateComponentType(ctx, actorID(ctx), in.ID, storage.ComponentTypePatch{
			DisplayName: in.Body.DisplayName, Stem: in.Body.Stem, Icon: in.Body.Icon, Abbrev: in.Body.Abbrev,
			DefaultTags: in.Body.DefaultTags,
		})
		if err != nil {
			return nil, mapComponentTypeErr(err)
		}
		var parentName *string
		if ct.ParentID != nil {
			if parent, err := gw.GetComponentType(ctx, ct.ParentID.String()); err == nil {
				parentName = &parent.Name
			}
		}
		return &componentTypeOutput{Body: toComponentTypeBody(ct, parentName)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-component-type",
		Method:        http.MethodDelete,
		Path:          "/component-types/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a component type",
		Description:   "Deletes a custom component_type, refused if official (422) or still a parent of another component_type (409). Gated by component_type:delete.",
	}, "component_type", "delete"), func(ctx context.Context, in *componentTypePathInput) (*struct{}, error) {
		if err := gw.DeleteComponentType(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapComponentTypeErr(err)
		}
		return nil, nil
	})
}
