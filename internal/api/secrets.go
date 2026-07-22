package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// secretFieldBody is one field as displayed: its value is the plaintext for a
// non-secret field and the fixed mask for a secret one. The plaintext of a
// secret field never crosses this boundary in slice 1 (no reveal endpoint yet).
type secretFieldBody struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Secret bool   `json:"secret" doc:"Whether the field is encrypted at rest and masked here"`
}

// secretTypeFieldBody is one member of a secret_type shape.
type secretTypeFieldBody struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Secret bool   `json:"secret"`
	Origin string `json:"origin" doc:"operator (set at creation) or lifecycle (filled by the secret's own machinery)"`
}

type secretTypeBody struct {
	ID                    string                `json:"id"`
	DisplayName           string                `json:"display_name"`
	Official              bool                  `json:"official"`
	DefaultAdminSensitive bool                  `json:"default_admin_sensitive" doc:"The admin_sensitive value the create form seeds for this type"`
	Fields                []secretTypeFieldBody `json:"fields"`
}

// secretBody is the wire shape of a stored secret, fields masked.
type secretBody struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	SecretType     string            `json:"secret_type"`
	OwnerKind      string            `json:"owner_kind"`
	OwnerID        *string           `json:"owner_id,omitempty" doc:"The owning entity's id, the canonical handle; absent for a global owner"`
	OwnerName      string            `json:"owner_name,omitempty"`
	AdminSensitive bool              `json:"admin_sensitive" doc:"When true, only the admin tier may see or reveal this secret, regardless of placement"`
	Fields         []secretFieldBody `json:"fields"`
}

func toSecretFieldBodies(fs []storage.ResolvedField) []secretFieldBody {
	out := make([]secretFieldBody, 0, len(fs))
	for _, f := range fs {
		out = append(out, secretFieldBody{Name: f.Name, Value: f.Value, Secret: f.Secret})
	}
	return out
}

func toSecretBody(s *storage.Secret) secretBody {
	return secretBody{
		ID: s.ID, Name: s.Name, SecretType: s.SecretType,
		OwnerKind: s.OwnerKind, OwnerID: s.OwnerID, OwnerName: s.OwnerName,
		AdminSensitive: s.AdminSensitive,
		Fields:         toSecretFieldBodies(s.Fields),
	}
}

type listSecretTypesOutput struct {
	Body struct {
		SecretTypes []secretTypeBody `json:"secret_types"`
	}
}

type listSecretsOutput struct {
	Body struct {
		Secrets []secretBody `json:"secrets"`
	}
}

type secretOutput struct {
	Body secretBody
}

type createSecretInput struct {
	Body struct {
		Name           string            `json:"name" minLength:"1" doc:"The cascade key; unique per owner"`
		SecretType     string            `json:"secret_type" minLength:"1" doc:"A secret_type id"`
		OwnerKind      string            `json:"owner_kind" enum:"platform,location,system,component" doc:"Which tier owns this secret"`
		Owner          *string           `json:"owner,omitempty" doc:"The owning entity's name; omit for a platform secret"`
		AdminSensitive *bool             `json:"admin_sensitive,omitempty" doc:"Admin-only visibility; omit to use the type default. Setting true requires the admin tier"`
		Fields         map[string]string `json:"fields" doc:"The operator field map, validated against the type shape"`
	}
}

type secretIDInput struct {
	ID string `path:"id" doc:"The secret's id"`
}

type updateSecretInput struct {
	ID   string `path:"id" doc:"The secret's id"`
	Body struct {
		Fields map[string]string `json:"fields" doc:"The field values to replace; an omitted field keeps its value"`
	}
}

type revealSecretOutput struct {
	Body struct {
		Fields map[string]string `json:"fields" doc:"The decrypted field values, keyed by field name"`
	}
}

// registerSecretRoutes wires the secret surface: the shape registry, the
// all-scope admin directory, and scoped create/delete. Read (masked) rides the
// viewer floor; create and delete are gated by the sensitive secret:create /
// secret:delete.
// canSecretAdmin reports whether the caller holds the secret action at the admin
// tier (secret:<action>:admin), which admin and owner reach via secret:> / >. It
// gates admin-sensitive secrets: only such a caller may see, reveal, update,
// delete, or create one.
func (a *authenticator) canSecretAdmin(ctx context.Context, action string) bool {
	perms, ok := permsFrom(ctx)
	return ok && perms.Allows("secret", action, "admin")
}

// resolvedSecretBody is one entry in a component's effective-secrets cascade. Its
// fields are MASKED exactly as the directory masks them: this surface answers
// which secret applies to a device and where it comes from, never what it
// contains. Plaintext stays behind the audited reveal.
type resolvedSecretBody struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	SecretType string            `json:"secret_type"`
	OwnerKind  string            `json:"owner_kind"`
	OwnerID    *string           `json:"owner_id,omitempty" doc:"The owning entity's id, the canonical handle; absent for a platform owner"`
	OwnerName  string            `json:"owner_name,omitempty"`
	Band       int               `json:"band" doc:"Cascade tier: 0 platform, 1 location, 3 component"`
	Depth      int               `json:"depth" doc:"Distance up the tier's tree from the component (0 nearest)"`
	Winner     bool              `json:"winner" doc:"True for the resolved secret; false for a shadowed candidate"`
	Fields     []secretFieldBody `json:"fields"`
}

type effectiveSecretsInput struct {
	Name string `path:"name" doc:"The component's name"`
}

type effectiveSecretsOutput struct {
	Body struct {
		Secrets []resolvedSecretBody `json:"secrets"`
	}
}

func registerSecretRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "effective-secrets",
		Method:      http.MethodGet,
		Path:        "/components/{name}/effective-secrets",
		Summary:     "Effective secrets for a component",
		Description: "Resolves the secrets that cascade onto a component (platform -> location -> component), with the winner and the shadowed candidates it overrode. There is NO system band: a secret is device-facing, and the room a component happens to serve is the wrong owner for a credential the device itself answers with. Fields are masked, as in the directory; plaintext is only ever the audited reveal. Gated by secret:read, which the viewer floor does not carry, and admin-sensitive secrets appear only to the admin tier.",
	}, "secret", "read"), func(ctx context.Context, in *effectiveSecretsInput) (*effectiveSecretsOutput, error) {
		comp, err := gw.GetComponent(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		resolved, err := gw.ResolveSecrets(ctx, comp.ID, a.scopeFor(ctx, "secret", "read"), a.canSecretAdmin(ctx, "read"))
		if err != nil {
			return nil, mapSecretErr(err)
		}
		out := &effectiveSecretsOutput{}
		out.Body.Secrets = make([]resolvedSecretBody, 0, len(resolved))
		for i := range resolved {
			r := &resolved[i]
			fields := make([]secretFieldBody, 0, len(r.Fields))
			for _, f := range r.Fields {
				fields = append(fields, secretFieldBody{Name: f.Name, Value: f.Value, Secret: f.Secret})
			}
			out.Body.Secrets = append(out.Body.Secrets, resolvedSecretBody{
				ID: r.ID, Name: r.Name, SecretType: r.SecretType,
				OwnerKind: r.OwnerKind, OwnerID: r.OwnerID, OwnerName: r.OwnerName,
				Band: r.Band, Depth: r.Depth, Winner: r.Winner, Fields: fields,
			})
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-secret-types",
		Method:      http.MethodGet,
		Path:        "/secret-types",
		Summary:     "List secret types",
		Description: "Lists the secret_type shapes a secret can take, for the create form. Gated by secret:read.",
	}, "secret", "read"), func(ctx context.Context, _ *struct{}) (*listSecretTypesOutput, error) {
		types, err := gw.ListSecretTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list secret types")
		}
		out := &listSecretTypesOutput{}
		out.Body.SecretTypes = make([]secretTypeBody, 0, len(types))
		for _, st := range types {
			b := secretTypeBody{ID: st.ID, DisplayName: st.DisplayName, Official: st.Official, DefaultAdminSensitive: st.DefaultAdminSensitive}
			for _, f := range st.Fields {
				b.Fields = append(b.Fields, secretTypeFieldBody{Name: f.Name, Type: f.Type, Secret: f.Secret, Origin: string(f.Origin)})
			}
			out.Body.SecretTypes = append(out.Body.SecretTypes, b)
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-secrets",
		Method:      http.MethodGet,
		Path:        "/secrets",
		Summary:     "List secrets",
		Description: "Lists the secrets the caller may see, with masked fields, filtered to the read scope; admin-sensitive secrets appear only to the admin tier. Gated by secret:read, which the viewer floor does not carry (secret is a sensitive resource).",
	}, "secret", "read"), func(ctx context.Context, _ *struct{}) (*listSecretsOutput, error) {
		secrets, err := gw.ListSecrets(ctx, a.scopeFor(ctx, "secret", "read"), a.canSecretAdmin(ctx, "read"))
		if err != nil {
			return nil, mapSecretErr(err)
		}
		out := &listSecretsOutput{}
		out.Body.Secrets = make([]secretBody, 0, len(secrets))
		for i := range secrets {
			out.Body.Secrets = append(out.Body.Secrets, toSecretBody(&secrets[i]))
		}
		return out, nil
	})

	huma.Register(api, a.platformGated(a.gated(huma.Operation{
		OperationID:   "create-secret",
		Method:        http.MethodPost,
		Path:          "/secrets",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a secret",
		Description:   "Seals a secret at an owner scope. Fields are validated and encrypted against the type shape. Gated by secret:create, plus platform:create when owner_kind is platform (the install-wide tier).",
	}, "secret", "create"), "create"), func(ctx context.Context, in *createSecretInput) (*secretOutput, error) {
		// The body says which tier the write lands at, so the tier gate runs here.
		if in.Body.OwnerKind == platformTier {
			if err := a.requirePlatform(ctx, "create"); err != nil {
				return nil, err
			}
		}
		s, err := gw.CreateSecret(ctx, actorID(ctx), storage.SecretSpec{
			Name:           in.Body.Name,
			SecretType:     in.Body.SecretType,
			OwnerKind:      in.Body.OwnerKind,
			OwnerName:      in.Body.Owner,
			AdminSensitive: in.Body.AdminSensitive,
			Fields:         in.Body.Fields,
		}, a.scopeFor(ctx, "secret", "create"), a.canSecretAdmin(ctx, "create"))
		if err != nil {
			return nil, mapSecretErr(err)
		}
		return &secretOutput{Body: toSecretBody(s)}, nil
	})

	huma.Register(api, a.platformGated(a.gated(huma.Operation{
		OperationID: "update-secret",
		Method:      http.MethodPatch,
		Path:        "/secrets/{id}",
		Summary:     "Update a secret's field values",
		Description: "Replaces the given field values on a secret, re-sealing secret fields. Only values change; name, type, and owner are fixed at creation. An omitted field keeps its value. Gated by secret:update, plus platform:update when the secret sits at the platform tier.",
	}, "secret", "update"), "update"), func(ctx context.Context, in *updateSecretInput) (*secretOutput, error) {
		// Only the stored row knows its tier, so the resolved permission rides with
		// the call and the Gateway applies it beside the scope split.
		s, err := gw.UpdateSecret(ctx, actorID(ctx), in.ID, in.Body.Fields,
			a.scopeFor(ctx, "secret", "read"), a.scopeFor(ctx, "secret", "update"),
			a.canSecretAdmin(ctx, "update"), a.canPlatform(ctx, "update"))
		if err != nil {
			return nil, mapSecretErr(err)
		}
		return &secretOutput{Body: toSecretBody(s)}, nil
	})

	huma.Register(api, a.platformGated(a.gated(huma.Operation{
		OperationID:   "delete-secret",
		Method:        http.MethodDelete,
		Path:          "/secrets/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a secret",
		Description:   "Removes a secret by id. Gated by secret:delete, plus platform:delete when the secret sits at the platform tier; read and delete scopes on the owner drive the 404 versus 403 split.",
	}, "secret", "delete"), "delete"), func(ctx context.Context, in *secretIDInput) (*struct{}, error) {
		if err := gw.DeleteSecret(ctx, actorID(ctx), in.ID,
			a.scopeFor(ctx, "secret", "read"), a.scopeFor(ctx, "secret", "delete"),
			a.canSecretAdmin(ctx, "delete"), a.canPlatform(ctx, "delete")); err != nil {
			return nil, mapSecretErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "reveal-secret",
		Method:      http.MethodPost,
		Path:        "/secrets/{id}:reveal",
		Summary:     "Reveal a secret's plaintext",
		Description: "Decrypts and returns a secret's field values, auditing the decrypt. Gated by secret:reveal at the caller's scope; an admin-sensitive secret additionally needs the admin tier (secret:reveal:admin), so a scoped operator reveals device secrets but never a platform credential.",
	}, "secret", "reveal"), func(ctx context.Context, in *secretIDInput) (*revealSecretOutput, error) {
		fields, err := gw.RevealSecret(ctx, actorID(ctx), in.ID,
			a.scopeFor(ctx, "secret", "read"), a.scopeFor(ctx, "secret", "reveal"), a.canSecretAdmin(ctx, "reveal"))
		if err != nil {
			return nil, mapSecretErr(err)
		}
		out := &revealSecretOutput{}
		out.Body.Fields = fields
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "copy-secret",
		Method:      http.MethodPost,
		Path:        "/secrets/{id}:copy",
		Summary:     "Decrypt a secret for clipboard copy",
		Description: "Decrypts and returns a secret's field values for a clipboard copy, audited under the copy verb (distinct from an on-screen reveal). Same exposure and the same secret:reveal gate as reveal.",
	}, "secret", "reveal"), func(ctx context.Context, in *secretIDInput) (*revealSecretOutput, error) {
		fields, err := gw.CopySecret(ctx, actorID(ctx), in.ID,
			a.scopeFor(ctx, "secret", "read"), a.scopeFor(ctx, "secret", "reveal"), a.canSecretAdmin(ctx, "reveal"))
		if err != nil {
			return nil, mapSecretErr(err)
		}
		out := &revealSecretOutput{}
		out.Body.Fields = fields
		return out, nil
	})

}

// mapSecretErr translates the gateway's secret sentinels into HTTP status.
func mapSecretErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrSecretNotFound):
		return huma.Error404NotFound("secret not found")
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	case errors.Is(err, storage.ErrSecretForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrSecretExists):
		return huma.Error409Conflict("a secret with this name already exists at this scope")
	case errors.Is(err, storage.ErrUnknownSecretType):
		return huma.Error422UnprocessableEntity("unknown secret_type")
	case errors.Is(err, storage.ErrSecretOwnerNotFound):
		return huma.Error422UnprocessableEntity("secret owner not found")
	case errors.Is(err, storage.ErrSecretFieldInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, storage.ErrNoSecretProvider):
		return huma.Error500InternalServerError("no secret key provider configured")
	default:
		return huma.Error500InternalServerError("secret operation failed")
	}
}
