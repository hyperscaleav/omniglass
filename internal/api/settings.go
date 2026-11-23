package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type settingsReadOutput struct {
	Body struct {
		Values  settings.Settings `json:"values"`
		Sources map[string]string `json:"sources" doc:"key 'namespace.key' to the winning level (default|file|env|platform)"`
		Locks   map[string]string `json:"locks" doc:"key 'namespace.key' to the locking level, when locked"`
	}
}

type settingsMeOutput struct {
	Body struct {
		Values settings.Settings `json:"values"`
	}
}

type settingsPatchInput struct {
	Namespace string         `path:"namespace" doc:"the settings namespace, e.g. ui" example:"notification"`
	Body      map[string]any `doc:"an RFC 7386 JSON Merge Patch for the namespace; null on a key restores it" example:"{\"egress_allow\":[\"smtp.example.com\"]}"`
}

// settingsMePatchInput is a person's merge patch for one of their own
// profile-domain namespaces (#2450).
type settingsMePatchInput struct {
	Namespace string         `path:"namespace" doc:"a profile-domain settings namespace: ui, keybindings or learn" example:"learn"`
	Body      map[string]any `doc:"an RFC 7386 JSON Merge Patch for the namespace; null on a key restores it to the platform's value" example:"{\"done\":[\"device-to-samples\"]}"`
}

type settingsNamespaceInput struct {
	Namespace string `path:"namespace" doc:"the settings namespace"`
}

// registerSettingsRoutes wires the settings engine: an admin read with provenance,
// a client-safe /settings/me any authenticated user may read, and admin writes
// (merge-patch a namespace, restore a namespace, factory reset). Every write acts
// on the platform scope, the same scope the resolver reads, so all three carry
// platform:update on top of settings:update: a settings binding is install-wide
// by definition, never scoped to part of the fleet.
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
		Description: "The current user's resolved settings, client-visible namespaces only, no provenance: the platform level with the caller's own user-level bindings on top. Feeds the SPA at boot. Requires authentication.",
		Middlewares: huma.Middlewares{a.authn},
	}, func(ctx context.Context, _ *struct{}) (*settingsMeOutput, error) {
		return settingsMe(ctx, gw, svc)
	})

	huma.Register(api, huma.Operation{
		OperationID: "patch-settings-me-namespace",
		Method:      http.MethodPatch,
		Path:        "/settings/me/{namespace}",
		Summary:     "Update the caller's own settings namespace",
		Description: "Applies an RFC 7386 JSON Merge Patch to the caller's own user-level binding for a profile-domain namespace (ui, keybindings, learn); null on a key restores it to the platform's value. A platform-domain namespace is the admin's (403), an unknown one a 404, a bad key or value a 422, and a key an admin locked at the platform level a 409. Answers the caller's effective settings. Requires authentication; self-scoped (writes only your own binding).",
		Middlewares: huma.Middlewares{a.authn},
	}, func(ctx context.Context, in *settingsMePatchInput) (*settingsMeOutput, error) {
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
		if !settings.ProfileNamespaces()[in.Namespace] {
			return nil, huma.Error403Forbidden("a platform-domain namespace is set at the platform level only, by an admin")
		}
		r, err := svc.Resolve(ctx)
		if err != nil {
			return nil, err
		}
		for key := range in.Body {
			if lvl, locked := r.Locks[in.Namespace+"."+key]; locked {
				return nil, huma.Error409Conflict(in.Namespace + "." + key + " is locked at the " + lvl + " level")
			}
		}
		if _, err := gw.MergePatchUserSettingBinding(ctx, actorID(ctx), in.Namespace, in.Body); err != nil {
			return nil, err
		}
		return settingsMe(ctx, gw, svc)
	})

	huma.Register(api, a.conditionalGated(a.platformGated(a.gated(huma.Operation{
		OperationID: "patch-settings-namespace",
		Method:      http.MethodPatch,
		Path:        "/settings/{namespace}",
		Summary:     "Update a settings namespace",
		Description: "Applies an RFC 7386 JSON Merge Patch to the namespace's platform binding; null on a key restores it (for example {\"egress_allow\":null}). Gated by settings:update and platform:update.",
	}, "settings", "update"), "update"), "secret", "bind"), func(ctx context.Context, in *settingsPatchInput) (*settingsReadOutput, error) {
		if err := a.requirePlatform(ctx, "update"); err != nil {
			return nil, err
		}
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
		if in.Namespace == "agent" {
			if err := a.checkAgentOverrides(ctx, in.Body); err != nil {
				return nil, err
			}
		}
		// The merge is a single atomic read-modify-write in the Gateway, serialized
		// against concurrent patches to the same namespace so no update is lost.
		// The notification namespace's mail relay binds a secret, so its patch
		// is judged on the settings it results in (#1317), as is the assistant
		// namespace's model key (#2109).
		switch in.Namespace {
		case "notification":
			if _, err := gw.MergePatchNotificationSettings(ctx, actorID(ctx), in.Body, a.secretBind(ctx)); err != nil {
				return nil, mapMailSecretErr(err)
			}
		case "assistant":
			if _, err := gw.MergePatchAssistantSettings(ctx, actorID(ctx), in.Body, a.secretBind(ctx)); err != nil {
				return nil, mapModelKeyErr(err)
			}
		case "sso":
			if _, err := gw.MergePatchSSOSettings(ctx, actorID(ctx), in.Body, a.secretBind(ctx)); err != nil {
				return nil, mapSSOSecretErr(err)
			}
		default:
			if _, err := gw.MergePatchSettingBinding(ctx, actorID(ctx), "platform", in.Namespace, in.Body); err != nil {
				return nil, err
			}
		}
		return resolveOutput(ctx, svc)
	})

	huma.Register(api, a.platformGated(a.gated(huma.Operation{
		OperationID:   "delete-settings-namespace",
		Method:        http.MethodDelete,
		Path:          "/settings/{namespace}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Restore a settings namespace to defaults",
		Description:   "Drops the namespace's platform binding, restoring the env layer, the file layer and the declared defaults. Gated by settings:update and platform:update.",
	}, "settings", "update"), "update"), func(ctx context.Context, in *settingsNamespaceInput) (*struct{}, error) {
		if err := a.requirePlatform(ctx, "update"); err != nil {
			return nil, err
		}
		if err := gw.DeleteSettingBinding(ctx, actorID(ctx), "platform", in.Namespace); err != nil {
			return nil, err
		}
		return nil, nil
	})

	huma.Register(api, a.platformGated(a.gated(huma.Operation{
		OperationID:   "restore-settings-defaults",
		Method:        http.MethodPost,
		Path:          "/settings:restoreDefaults",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Restore all settings to defaults",
		Description:   "Removes every platform binding (a factory reset), restoring the env layer, the file layer and the declared defaults. Gated by settings:update and platform:update.",
	}, "settings", "update"), "update"), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		if err := a.requirePlatform(ctx, "update"); err != nil {
			return nil, err
		}
		if err := gw.DeleteAllSettingBindings(ctx, actorID(ctx), "platform"); err != nil {
			return nil, err
		}
		return nil, nil
	})
}

// settingsMe resolves the caller's client-visible settings: the platform level
// with their own user-level bindings on top (#2450).
func settingsMe(ctx context.Context, gw storage.Gateway, svc *settings.Service) (*settingsMeOutput, error) {
	rows, err := gw.GetUserSettingBindings(ctx, actorID(ctx))
	if err != nil {
		return nil, err
	}
	user, _ := storage.SettingLevel(rows)
	vals, err := svc.ClientEffectiveFor(ctx, user)
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

// mapModelKeyErr maps an assistant settings patch refused for omnibot's model
// key (#2109): binding one, or moving base_url or the provider while one stays
// bound, takes the right to bind it (secret:bind), as the mail relay's does.
func mapModelKeyErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrSecretNotFound):
		return huma.Error404NotFound("secret not found")
	case errors.Is(err, storage.ErrSecretForbidden):
		return huma.Error403Forbidden("binding omnibot's model key, or moving base_url or the provider while one is bound, takes the right to bind that secret (secret:bind)")
	case errors.Is(err, storage.ErrModelKeyInvalid):
		return huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), "storage: omnibot's model key is invalid: "))
	}
	return err
}

// mapMailSecretErr maps a notification settings patch refused for the mail
// relay's secret (#1317): binding one, or moving the relay while one stays
// bound, by any layer, takes the right to bind it (secret:bind), exactly as
// binding a secret to a notification target does.
func mapMailSecretErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrSecretNotFound):
		return huma.Error404NotFound("secret not found")
	case errors.Is(err, storage.ErrSecretForbidden):
		return huma.Error403Forbidden("binding the mail relay's secret, or moving the relay while one is bound, takes the right to bind that secret (secret:bind)")
	case errors.Is(err, storage.ErrMailSecretInvalid):
		return huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), "storage: the mail relay's secret is invalid: "))
	}
	return err
}

// mapSSOSecretErr maps an sso settings patch refused for the client secret
// (#2362): naming one, or moving the issuer while one is bound, takes the
// right to bind it (secret:bind), since every sign-in sends it to the
// issuer's token endpoint.
func mapSSOSecretErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrSecretNotFound):
		return huma.Error404NotFound("secret not found")
	case errors.Is(err, storage.ErrSecretForbidden):
		return huma.Error403Forbidden("binding the single sign-on client secret, or moving the issuer while one is bound, takes the right to bind that secret (secret:bind)")
	case errors.Is(err, storage.ErrSSOSecretInvalid):
		return huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), storage.ErrSSOSecretInvalid.Error()+": "))
	}
	return err
}
