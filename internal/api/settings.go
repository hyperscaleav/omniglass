package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type settingsReadOutput struct {
	Body struct {
		Values  settings.Settings `json:"values"`
		Sources map[string]string `json:"sources" doc:"key 'namespace.key' to the winning level (code|file|global)"`
		Locks   map[string]string `json:"locks" doc:"key 'namespace.key' to the locking level, when locked"`
	}
}

type settingsMeOutput struct {
	Body struct {
		Values settings.Settings `json:"values"`
	}
}

type settingsPatchInput struct {
	Namespace string         `path:"namespace" doc:"the settings namespace, e.g. ui"`
	Body      map[string]any `doc:"an RFC 7386 JSON Merge Patch for the namespace; null on a key restores it"`
}

type settingsNamespaceInput struct {
	Namespace string `path:"namespace" doc:"the settings namespace"`
}

// registerSettingsRoutes wires the settings engine: an admin read with provenance,
// a client-safe /settings/me any authenticated user may read, and admin writes
// (merge-patch a namespace, restore a namespace, factory reset). Writes act on the
// global scope in slice-0.
func registerSettingsRoutes(api huma.API, a *authenticator, gw storage.Gateway, svc *settings.Service) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-settings",
		Method:      http.MethodGet,
		Path:        "/settings",
		Summary:     "Get effective settings with provenance",
		Description: "The effective settings document plus per-key provenance (which level won) and lock state. Gated by settings:read (admin).",
	}, "settings", "read"), func(ctx context.Context, _ *struct{}) (*settingsReadOutput, error) {
		return resolveOutput(ctx, svc)
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-settings-me",
		Method:      http.MethodGet,
		Path:        "/settings/me",
		Summary:     "Get the caller's effective settings",
		Description: "The current principal's resolved settings, client-visible namespaces only, no provenance. Feeds the SPA at boot. Requires authentication.",
		Middlewares: huma.Middlewares{a.authn},
	}, func(ctx context.Context, _ *struct{}) (*settingsMeOutput, error) {
		vals, err := svc.ClientEffective(ctx)
		if err != nil {
			return nil, err
		}
		typed, err := settings.Typed(vals)
		if err != nil {
			return nil, err
		}
		out := &settingsMeOutput{}
		out.Body.Values = typed
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "patch-settings-namespace",
		Method:      http.MethodPatch,
		Path:        "/settings/{namespace}",
		Summary:     "Update a settings namespace",
		Description: "Applies an RFC 7386 JSON Merge Patch to the namespace's global override; null on a key restores it. Gated by settings:update.",
	}, "settings", "update"), func(ctx context.Context, in *settingsPatchInput) (*settingsReadOutput, error) {
		// Validate the patch against the namespace's reflected schema before storing:
		// an unknown namespace is a 404, a bad key or value a 422.
		if err := settings.Validate(in.Namespace, in.Body); err != nil {
			if errors.Is(err, settings.ErrUnknownNamespace) {
				return nil, huma.Error404NotFound("unknown settings namespace")
			}
			var fe *settings.FieldError
			if errors.As(err, &fe) {
				return nil, huma.Error422UnprocessableEntity(fe.Error())
			}
			return nil, err
		}
		// The merge is a single atomic read-modify-write in the Gateway, serialized
		// against concurrent patches to the same namespace so no update is lost.
		if _, err := gw.MergePatchSettingOverride(ctx, actorID(ctx), "global", in.Namespace, in.Body); err != nil {
			return nil, err
		}
		return resolveOutput(ctx, svc)
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-settings-namespace",
		Method:        http.MethodDelete,
		Path:          "/settings/{namespace}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Restore a settings namespace to defaults",
		Description:   "Drops the namespace's global override, restoring file and code defaults. Gated by settings:update.",
	}, "settings", "update"), func(ctx context.Context, in *settingsNamespaceInput) (*struct{}, error) {
		if err := gw.DeleteSettingOverride(ctx, actorID(ctx), "global", in.Namespace); err != nil {
			return nil, err
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "restore-settings-defaults",
		Method:        http.MethodPost,
		Path:          "/settings:restoreDefaults",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Restore all settings to defaults",
		Description:   "Removes every global override (a factory reset). Gated by settings:update.",
	}, "settings", "update"), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		if err := gw.DeleteAllSettingOverrides(ctx, actorID(ctx), "global"); err != nil {
			return nil, err
		}
		return nil, nil
	})
}

// resolveOutput resolves the effective settings document and renders it as the
// admin read shape (values plus provenance and lock levels).
func resolveOutput(ctx context.Context, svc *settings.Service) (*settingsReadOutput, error) {
	r, err := svc.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	typed, err := settings.Typed(r.Values)
	if err != nil {
		return nil, err
	}
	out := &settingsReadOutput{}
	out.Body.Values = typed
	out.Body.Sources = r.Sources
	out.Body.Locks = r.Locks
	return out, nil
}
