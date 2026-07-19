package settings

import "context"

// OverridesFunc reads the override level for a scope: its document and its locks
// (namespace to locked key-paths). It is a function seam so the settings package
// does not import storage (which would cycle); the server passes a closure over
// the Gateway.
type OverridesFunc func(ctx context.Context, scope string) (Doc, map[string][]string, error)

// Service resolves effective settings from the three slice-0 levels: embedded code
// defaults, the operator file (captured at boot), and the global DB override (read
// live per call). It holds no per-request state.
type Service struct {
	defaults  Doc
	file      Doc
	overrides OverridesFunc
}

// NewService captures the parsed operator file and the override reader. Defaults
// come from the embedded asset.
func NewService(file Doc, overrides OverridesFunc) *Service {
	if file == nil {
		file = Doc{}
	}
	return &Service{defaults: Defaults(), file: file, overrides: overrides}
}

// Resolve builds the level stack (code, file, global) and resolves it.
func (s *Service) Resolve(ctx context.Context) (Resolved, error) {
	globalDoc, globalLocks, err := s.overrides(ctx, "global")
	if err != nil {
		return Resolved{}, err
	}
	return Resolve(
		Level{Name: "code", Doc: s.defaults},
		Level{Name: "file", Doc: s.file},
		Level{Name: "global", Doc: globalDoc, Locks: globalLocks},
	), nil
}

// Effective returns the resolved values (all namespaces).
func (s *Service) Effective(ctx context.Context) (Doc, error) {
	r, err := s.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	return r.Values, nil
}

// ClientEffective returns the resolved values filtered to client-visible
// namespaces, for /settings/me (what an unprivileged SPA may read at boot).
func (s *Service) ClientEffective(ctx context.Context) (Doc, error) {
	r, err := s.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	visible := ClientVisibleNamespaces()
	out := Doc{}
	for ns, keys := range r.Values {
		if visible[ns] {
			out[ns] = keys
		}
	}
	return out, nil
}
