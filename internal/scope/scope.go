// Package scope is the pure ABAC scope resolver: given a principal's grants and
// the role index, it computes visible_set(P, action) for a resource, the union
// (over only the grants whose role carries that action) of each grant's scope.
// It has no I/O: it answers "which scope roots", and the Storage Gateway expands
// those roots to a bound id set and injects the row filter. Keeping the
// resolution pure makes the over-permit fix (per-action, not a global union)
// unit-testable without a database.
package scope

import "github.com/hyperscaleav/omniglass/internal/rbac"

// Grant is the scope-relevant view of a principal's grant: the role it carries
// and the scope it confers. It mirrors storage.Grant without the storage
// dependency, so this package stays pure.
type Grant struct {
	Role      string
	ScopeKind string
	ScopeID   string // empty for the "all" scope
	// ExcludeRoot narrows the grant's WRITE scope to the descendants of its root:
	// the holder can create under, and update/delete within, the subtree, but not
	// update or delete the root entity itself (a deploy/integrator grant that must
	// not modify the boundary of its own scope). Read and create-placement still
	// include the root. Ignored for the "all" scope.
	ExcludeRoot bool
}

// Set is a resolved scope. All true means every entity of the resource is in
// scope. Otherwise IDs are the scope roots: an entity is in scope when it is one
// of these ids or structurally beneath one (the gateway expands the subtree).
// All false with no IDs means nothing is in scope. ExcludeRootIDs is the subset
// of IDs whose own row is excluded from this action (its descendants stay in
// scope): it is populated only for the modify actions of an ExcludeRoot grant,
// and only when no other grant covers that root inclusively (inclusive wins).
type Set struct {
	All            bool
	IDs            []string
	ExcludeRootIDs []string
}

// Empty reports whether the set admits nothing (no all flag, no roots).
func (s Set) Empty() bool { return !s.All && len(s.IDs) == 0 }

// Resolve computes visible_set(P, action) for resource. A grant contributes only
// when its role carries resource:action (per the role index, including the :read
// floor) and its scope_kind can contain the resource. A scope_kind of "all"
// yields Set{All: true}; a kind naming the resource's own tier or an ancestor
// tier yields that scope id. A grant scoped to a tier below the resource (a
// system scope does not confer location access) does not apply.
func Resolve(grants []Grant, idx rbac.RoleIndex, resource, action string) Set {
	kinds := applicableKinds(resource)
	// ExcludeRoot narrows only the modify actions; read and create-placement keep
	// the root so the holder can see the scope boundary and place children under it.
	excludes := action != "read" && action != "create"
	var set Set
	seen := map[string]bool{}
	inclusive := map[string]bool{} // a root granted without exclusion for this action
	excluded := map[string]bool{}  // a root granted with exclusion for this action
	for _, g := range grants {
		if !idx.Flatten([]string{g.Role}).Allows(resource, action) {
			continue // the role does not carry this action: the over-permit fix
		}
		if g.ScopeKind == "all" {
			set.All = true
			continue
		}
		if !kinds[g.ScopeKind] || g.ScopeID == "" {
			continue // scope kind cannot contain this resource, or malformed
		}
		if !seen[g.ScopeID] {
			seen[g.ScopeID] = true
			set.IDs = append(set.IDs, g.ScopeID)
		}
		if excludes && g.ExcludeRoot {
			excluded[g.ScopeID] = true
		} else {
			inclusive[g.ScopeID] = true
		}
	}
	// A root is excluded from this action only when every grant naming it excludes
	// it: an inclusive grant on the same root wins (a broader parent grant is
	// handled by the gateway's subtree expansion, not here).
	for _, id := range set.IDs {
		if excluded[id] && !inclusive[id] {
			set.ExcludeRootIDs = append(set.ExcludeRootIDs, id)
		}
	}
	return set
}

// applicableKinds returns the scope kinds that can contain a resource: "all"
// always, plus the resource's own tier. The cross-tier cascade (a location scope
// also covering its systems and components) is a later slice; today each entity
// is scoped by its own kind. component and group join as those entities land.
func applicableKinds(resource string) map[string]bool {
	switch resource {
	case "location":
		return map[string]bool{"location": true}
	case "system":
		return map[string]bool{"system": true}
	case "component":
		return map[string]bool{"component": true}
	default:
		return map[string]bool{}
	}
}
