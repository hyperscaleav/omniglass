package settings

import "context"

// OverridesFunc reads the override level for a scope: its document and its locks
// (namespace to locked key-paths). It is a function seam so the settings package
// does not import storage (which would cycle); the server passes a closure over
// the Gateway.
type OverridesFunc func(ctx context.Context, scope string) (Doc, map[string][]string, error)

// Service resolves effective settings from the three slice-0 levels: the type's own
// declared defaults, the operator file (captured at boot), and the platform DB
// override (read live per call). It holds no per-request state.
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

// Resolve builds the level stack (default, file, platform) and resolves it. The
// scope passed to the override reader is the value the rows carry, so a rename of
// the tier is a rename here and in the schema together.
func (s *Service) Resolve(ctx context.Context) (Resolved, error) {
	platformDoc, platformLocks, err := s.overrides(ctx, "platform")
	if err != nil {
		return Resolved{}, err
	}
	return s.ResolveOverride(platformDoc, platformLocks), nil
}

// ResolveOverride resolves the effective document from an override level the
// CALLER has already read, for a caller that cannot let this service do the
// reading: the Storage Gateway resolves a setting from inside a transaction it
// is holding, and a read issued anywhere but that transaction would take a
// second connection from the pool it is already occupying.
//
// The level stack is the one Resolve builds, so the two cannot answer
// differently, and the caller supplies only the layer it is better placed to
// fetch.
func (s *Service) ResolveOverride(platformDoc Doc, platformLocks map[string][]string) Resolved {
	return Resolve(
		Level{Name: "default", Doc: s.defaults},
		Level{Name: "file", Doc: s.file},
		Level{Name: "platform", Doc: platformDoc, Locks: platformLocks},
	)
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
