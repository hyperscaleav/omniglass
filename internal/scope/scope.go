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
}

// Set is a resolved scope. All true means every entity of the resource is in
// scope. Otherwise IDs are the scope roots: an entity is in scope when it is one
// of these ids or structurally beneath one (the gateway expands the subtree).
// All false with no IDs means nothing is in scope.
type Set struct {
	All bool
	IDs []string
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
	var set Set
	seen := map[string]bool{}
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
	}
	return set
}

// applicableKinds returns the scope kinds that can contain a resource: "all"
// always, plus the resource's own tier and any ancestor tier. Only location
// exists today; system, component, and group join as those entities land.
func applicableKinds(resource string) map[string]bool {
	switch resource {
	case "location":
		return map[string]bool{"location": true}
	default:
		return map[string]bool{}
	}
}
