// Package scope is the pure ABAC scope resolver: given a principal's grants and
// the role index, it computes visible_set(P, action) for a resource, the union
// (over only the grants whose role carries that action) of each grant's scope.
// It has no I/O: it answers "which scope roots", and the Storage Gateway expands
// those roots to a bound id set and injects the row filter. Keeping the
// resolution pure makes the over-permit fix (per-action, not a global union)
// unit-testable without a database.
package scope

import "github.com/hyperscaleav/omniglass/internal/rbac"

// Scope operators. A grant's ScopeOp says HOW its ScopeID matches the tree.
// Empty is treated as OpSubtree (the default, and the value a row that predates
// the column carries), so every grant that omits an op means the plain subtree.
const (
	OpSubtree         = "subtree"           // root + descendants, every action
	OpSubtreeExclRoot = "subtree_excl_root" // root + descendants for read/create, descendants only for modify
	OpSelf            = "self"              // the root row only (read/update/delete); no descendants, no create-placement
)

// Grant is the scope-relevant view of a principal's grant: the role it carries
// and the scope it confers. It mirrors storage.Grant without the storage
// dependency, so this package stays pure.
type Grant struct {
	Role      string
	ScopeKind string
	ScopeID   string // empty for the "all" scope
	// ScopeOp says how ScopeID matches the tree: OpSubtree (root + descendants),
	// OpSubtreeExclRoot (descendants only for modify, root kept for read/create, so
	// a deploy/integrator grant manages within a subtree without editing its own
	// boundary), or OpSelf (exactly the root row for read/update/delete, no
	// descendants and no create-placement, a leaf-lock on one node). Empty means
	// OpSubtree. Ignored for the "all" scope.
	ScopeOp string
}

// Set is a resolved scope. All true means every entity of the resource is in
// scope. Otherwise a row is in scope when it satisfies any of: it is one of IDs
// or structurally beneath one (the gateway expands the subtree); it is a strict
// descendant of an ExcludeRootIDs root (that root's own row is out for this
// action, descendants stay in); or its id is one of SelfIDs (matched exactly, no
// subtree walk). ExcludeRootIDs is a subset of IDs, populated only for the modify
// actions of a subtree_excl_root grant and only when no other grant covers that
// root inclusively (inclusive wins). SelfIDs carries self-op roots plus any root
// a self grant re-adds after an exclusion stripped it. All false with every list
// empty means nothing is in scope.
type Set struct {
	All            bool
	IDs            []string
	ExcludeRootIDs []string
	SelfIDs        []string
}

// Empty reports whether the set admits nothing (no all flag, no roots of any op).
func (s Set) Empty() bool { return !s.All && len(s.IDs) == 0 && len(s.SelfIDs) == 0 }

// Resolve computes visible_set(P, action) for resource. A grant contributes only
// when its role carries resource:action (per the role index, including the :read
// floor) and its scope_kind can contain the resource. A scope_kind of "all"
// yields Set{All: true}; a kind naming the resource's own tier or an ancestor
// tier yields that scope id. A grant scoped to a tier below the resource (a
// system scope does not confer location access) does not apply.
func Resolve(grants []Grant, idx rbac.RoleIndex, resource, action string) Set {
	kinds := applicableKinds(resource)
	// subtree_excl_root narrows only the modify actions; read and create-placement
	// keep the root so the holder can see the scope boundary and place children
	// under it. self is the root row for read/update/delete but NOT create: a
	// leaf-lock grants no authority to grow the tree under its node.
	excludes := action != "read" && action != "create"
	selfApplies := action != "create"
	var set Set
	seen := map[string]bool{}    // a root already added to IDs (subtree ops)
	selfSeen := map[string]bool{}
	inclusive := map[string]bool{} // a root a subtree op admits in full for this action
	excluded := map[string]bool{}  // a root a subtree_excl_root op strips for this action
	self := map[string]bool{}      // a root a self op admits as its own row only
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
		op := g.ScopeOp
		if op == "" {
			op = OpSubtree
		}
		if op == OpSelf {
			if selfApplies {
				self[g.ScopeID] = true
			}
			continue // self roots never enter IDs (no subtree expansion), and never grant create
		}
		if !seen[g.ScopeID] {
			seen[g.ScopeID] = true
			set.IDs = append(set.IDs, g.ScopeID)
		}
		if excludes && op == OpSubtreeExclRoot {
			excluded[g.ScopeID] = true
		} else {
			inclusive[g.ScopeID] = true
		}
	}
	// A subtree root is excluded from this action only when every subtree grant
	// naming it excludes it: an inclusive grant on the same root wins (a broader
	// parent grant is handled by the gateway's subtree expansion, not here).
	for _, id := range set.IDs {
		if excluded[id] && !inclusive[id] {
			set.ExcludeRootIDs = append(set.ExcludeRootIDs, id)
		}
	}
	// A self root contributes its own row unless a subtree op already admits that
	// row inclusively (then self is redundant). When a subtree_excl_root op stripped
	// the root for this action, the self grant re-adds exactly the row: the root
	// stays in ExcludeRootIDs (its subtree walk skips it) and joins SelfIDs (its row
	// matches by id). Deterministic order: emit in first-seen order over the grants.
	for _, g := range grants {
		id := g.ScopeID
		if g.ScopeOp != OpSelf || !self[id] || selfSeen[id] || inclusive[id] {
			continue
		}
		selfSeen[id] = true
		set.SelfIDs = append(set.SelfIDs, id)
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
