package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// systemTypeBody is the wire shape of a system_type registry row: the coarse
// taxonomy of what kind of space a system is (a boardroom, a classroom, a video
// wall). It is not the standard, which stays the blueprint a system conforms
// to. Stem, Icon, and Abbrev are the system's console-facing name and glyph
// facts, inherited from the nearest ancestor that sets them (null in storage;
// rendered as "" here, the same as every other optional scalar). ParentID and
// Parent are the tree link, absent for a root type. The registry lists
// alphabetically by display_name.
type systemTypeBody struct {
	ID          string  `json:"id" doc:"The system_type's uuid, the stable handle that survives a rename"`
	Name        string  `json:"name" doc:"The name an operator reads and types; renameable"`
	DisplayName string  `json:"display_name"`
	Stem        string  `json:"stem,omitempty" doc:"The prefix a generated system name is built from; empty inherits the nearest ancestor's"`
	Icon        string  `json:"icon,omitempty" doc:"A glyph key; empty inherits the nearest ancestor's"`
	Abbrev      string  `json:"abbrev,omitempty" doc:"A compact form of display_name; empty inherits the nearest ancestor's"`
	LabelRule   string  `json:"label_rule,omitempty" doc:"The label template systems of this type get; empty inherits the nearest ancestor's, then the global rule for systems"`
	Official    bool    `json:"official"`
	ParentID    *string `json:"parent_id,omitempty" doc:"The parent system_type's id, the canonical handle; absent for a root type"`
	Parent      *string `json:"parent,omitempty" doc:"The parent system_type's name, for display; absent for a root type"`
}

func toSystemTypeBody(st *storage.SystemType, parentName *string) systemTypeBody {
	b := systemTypeBody{
		ID: st.ID.String(), Name: st.Name, DisplayName: st.DisplayName,
		Stem: derefStr(st.Stem), Icon: derefStr(st.Icon), Abbrev: derefStr(st.Abbrev), LabelRule: derefStr(st.LabelRule),
		Official: st.Official,
	}
	if st.ParentID != nil {
		pid := st.ParentID.String()
		b.ParentID = &pid
		b.Parent = parentName
	}
	return b
}

type listSystemTypesOutput struct {
	Body struct {
		SystemTypes []systemTypeBody `json:"system_types"`
	}
}

type systemTypePathInput struct {
	ID string `path:"id" doc:"The system_type id"`
}

type createSystemTypeInput struct {
	Body struct {
		Name        string `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The globally unique name"`
		DisplayName string `json:"display_name" minLength:"1" doc:"What an operator reads in pickers and lists"`
		// A stem is a name prefix (#657's name rule mints it straight into a
		// generated system name), so it follows the same rule a name does.
		Stem     string `json:"stem,omitempty" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The prefix a generated system name is built from; omit to inherit the parent's. Lowercase letters, digits, and hyphens. Required for a root type, which has no ancestor to inherit one from."`
		Icon     string `json:"icon,omitempty" doc:"A glyph key; omit to inherit the parent's"`
		Abbrev    string `json:"abbrev,omitempty" doc:"A compact form of display_name; omit to inherit the parent's"`
		LabelRule string `json:"label_rule,omitempty" doc:"The label template systems of this type get, a Go text/template over the system data map; omit to inherit the nearest ancestor's. Refused (422) if it does not compile"`
		ParentID  string `json:"parent_id,omitempty" doc:"The parent system_type, by name or uuid; omit for a root type"`
	}
}

type updateSystemTypeInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty" doc:"A new operator-facing label"`
		// Same rule as create's Stem: a name prefix follows the name rule.
		Stem   *string `json:"stem,omitempty" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"A new name prefix. Lowercase letters, digits, and hyphens."`
		Icon      *string `json:"icon,omitempty" doc:"A new glyph key"`
		Abbrev    *string `json:"abbrev,omitempty" doc:"A new compact form"`
		LabelRule *string `json:"label_rule,omitempty" doc:"A new label template for systems of this type; omit to leave unchanged, \"\" to clear back to the inherited one. Refused (422) if it does not compile. Editing it restamps nothing on its own: apply it with /systems:recomputeLabels, having seen the blast radius with :previewLabels"`
	}
}

type systemTypeOutput struct {
	Body systemTypeBody
}

// mapSystemTypeErr translates the system_type storage sentinels into HTTP
// status. The two write-time refusals are 422s naming the offense; everything
// else falls through to the shared type-registry mapping (not-found 404,
// duplicate 409, official read-only 422, in-use 409).
func mapSystemTypeErr(err error) error {
	if errors.Is(err, storage.ErrParentSystemTypeNotFound) {
		return huma.Error422UnprocessableEntity("parent system_type not found")
	}
	if errors.Is(err, storage.ErrRootSystemTypeNeedsStem) {
		return huma.Error422UnprocessableEntity("a root system_type (no parent) must have a stem; there is no ancestor to inherit one from")
	}
	return mapTypeErr(err, "system_type")
}

// resolveSystemTypeParent resolves a parent ref (name or uuid, "" for a root
// type) to the full row, so the caller has both the id CreateSystemType wants
// and the name a create response echoes back. An unknown ref normalizes to
// ErrParentSystemTypeNotFound, the same sentinel the FK-backed create path
// raises, so mapSystemTypeErr has one branch for both.
func resolveSystemTypeParent(ctx context.Context, gw storage.Gateway, ref string) (*storage.SystemType, error) {
	if ref == "" {
		return nil, nil
	}
	parent, err := gw.GetSystemType(ctx, ref)
	if err != nil {
		return nil, storage.ErrParentSystemTypeNotFound
	}
	return parent, nil
}

// registerSystemTypeRoutes wires the system_type registry surface: list (the
// tree, flattened with parent links), create/update/delete of custom
// (non-official) nodes. Gated by system_type:read|create|update|delete; read
// sits in the viewer floor (*:read), the mutations at the admin tier, the same
// split component_type and location_type use.
func registerSystemTypeRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-system-types",
		Method:      http.MethodGet,
		Path:        "/system-types",
		Summary:     "List system types",
		Description: "Lists the system_type registry (the coarse taxonomy of what kind of space a system is: a boardroom, a classroom, a video wall), ordered alphabetically by display name. Each row carries its parent link, so the console reconstructs the tree client-side. Distinct from standard, which is the blueprint a system conforms to. Gated by system_type:read.",
	}, "system_type", "read"), func(ctx context.Context, _ *struct{}) (*listSystemTypesOutput, error) {
		types, err := gw.ListSystemTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list system types")
		}
		byID := make(map[uuid.UUID]string, len(types))
		for i := range types {
			byID[types[i].ID] = types[i].Name
		}
		out := &listSystemTypesOutput{}
		out.Body.SystemTypes = make([]systemTypeBody, 0, len(types))
		for i := range types {
			var parentName *string
			if types[i].ParentID != nil {
				if name, ok := byID[*types[i].ParentID]; ok {
					parentName = &name
				}
			}
			out.Body.SystemTypes = append(out.Body.SystemTypes, toSystemTypeBody(&types[i], parentName))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-system-type",
		Method:        http.MethodPost,
		Path:          "/system-types",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a system type",
		Description:   "Creates a custom (non-official) system_type, optionally under a parent and optionally with the label_rule systems of that type get. A root type must carry a stem, since it has no ancestor to inherit one from. An unparseable label_rule is a 422. Gated by system_type:create.",
	}, "system_type", "create"), func(ctx context.Context, in *createSystemTypeInput) (*systemTypeOutput, error) {
		parent, err := resolveSystemTypeParent(ctx, gw, in.Body.ParentID)
		if err != nil {
			return nil, mapSystemTypeErr(err)
		}
		var parentID *uuid.UUID
		var parentName *string
		if parent != nil {
			parentID = &parent.ID
			parentName = &parent.Name
		}
		st, err := gw.CreateSystemType(ctx, actorID(ctx), storage.SystemType{
			Name: in.Body.Name, DisplayName: in.Body.DisplayName,
			Stem: ptrOrNil(in.Body.Stem), Icon: ptrOrNil(in.Body.Icon), Abbrev: ptrOrNil(in.Body.Abbrev),
			LabelRule: ptrOrNil(in.Body.LabelRule), ParentID: parentID,
		})
		if err != nil {
			return nil, mapSystemTypeErr(err)
		}
		return &systemTypeOutput{Body: toSystemTypeBody(st, parentName)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-system-type",
		Method:      http.MethodPatch,
		Path:        "/system-types/{id}",
		Summary:     "Update a system type",
		Description: "Patches a custom system_type's display_name, stem, icon, abbrev, or label_rule. An unparseable label_rule is a 422 at rule-edit time, never a broken row at create time, and setting one restamps nothing on its own: apply it with /systems:recomputeLabels after seeing the blast radius with :previewLabels. Official types are read-only (422). Gated by system_type:update.",
	}, "system_type", "update"), func(ctx context.Context, in *updateSystemTypeInput) (*systemTypeOutput, error) {
		st, err := gw.UpdateSystemType(ctx, actorID(ctx), in.ID, storage.SystemTypePatch{
			DisplayName: in.Body.DisplayName, Stem: in.Body.Stem, Icon: in.Body.Icon, Abbrev: in.Body.Abbrev,
			LabelRule: in.Body.LabelRule,
		})
		if err != nil {
			return nil, mapSystemTypeErr(err)
		}
		var parentName *string
		if st.ParentID != nil {
			if parent, err := gw.GetSystemType(ctx, st.ParentID.String()); err == nil {
				parentName = &parent.Name
			}
		}
		return &systemTypeOutput{Body: toSystemTypeBody(st, parentName)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-system-type",
		Method:        http.MethodDelete,
		Path:          "/system-types/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a system type",
		Description:   "Deletes a custom system_type, refused if official (422), still a parent of another system_type (409), or still classifying a system (409). Gated by system_type:delete.",
	}, "system_type", "delete"), func(ctx context.Context, in *systemTypePathInput) (*struct{}, error) {
		if err := gw.DeleteSystemType(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapSystemTypeErr(err)
		}
		return nil, nil
	})
}
