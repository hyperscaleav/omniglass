// Package rbac is the pure capability logic: parsing <resource>:<action>
// permission strings, resolving role inheritance, flattening a principal's
// grants into a permission set, and answering "may this principal perform this
// action on this resource kind" with the :read floor. It has no I/O and no
// storage dependency, so it is unit-testable in isolation. Scope (which entities)
// is the Storage Gateway's job, not this package's; rbac answers capability only.
package rbac

import (
	"fmt"
	"strings"
)

// Role is the capability shape rbac needs: an id, its own permission strings,
// and the ids of roles it inherits.
type Role struct {
	ID          string
	Permissions []string
	Inherits    []string
}

// RoleIndex resolves a role id to its Role. Build it once from the role table.
type RoleIndex map[string]Role

// NewRoleIndex indexes roles by id.
func NewRoleIndex(roles []Role) RoleIndex {
	idx := make(RoleIndex, len(roles))
	for _, r := range roles {
		idx[r.ID] = r
	}
	return idx
}

// Flatten unions the permissions of the named roles (resolving inheritance
// transitively) into one Set. Scope is intentionally ignored: the flattened set
// is the principal's capability anywhere, the fast-reject input; the gateway
// applies scope per query.
func (idx RoleIndex) Flatten(roleIDs []string) Set {
	var perms []string
	seen := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		r, ok := idx[id]
		if !ok {
			return
		}
		perms = append(perms, r.Permissions...)
		for _, parent := range r.Inherits {
			walk(parent)
		}
	}
	for _, id := range roleIDs {
		walk(id)
	}
	return NewSet(perms)
}

// Set is a parsed, queryable permission set.
type Set struct {
	entries []entry
}

type entry struct {
	resource   string
	actions    map[string]struct{}
	allActions bool
}

// NewSet parses permission strings into a Set, skipping any that do not parse
// (a malformed seeded permission should not silently widen access; parse errors
// are dropped, so an unparseable entry simply grants nothing).
func NewSet(perms []string) Set {
	var s Set
	for _, p := range perms {
		if e, err := parse(p); err == nil {
			s.entries = append(s.entries, e)
		}
	}
	return s
}

// Allows reports whether the set permits action on resource. The :read floor
// applies: holding any permission on a resource implies read on it.
func (s Set) Allows(resource, action string) bool {
	for _, e := range s.entries {
		if e.resource != "*" && e.resource != resource {
			continue
		}
		// The resource matches this entry.
		if action == "read" { // :read floor: any permission on the resource implies read
			return true
		}
		if e.allActions {
			return true
		}
		if _, ok := e.actions[action]; ok {
			return true
		}
	}
	return false
}

// Covers reports whether this set grants everything the other set grants: for
// every permission in other, this set permits it too. It is the impersonation
// escalation guard (A may impersonate T only when A.Covers(T)), so it is
// deliberately conservative with wildcards: a set of specific actions never
// covers a "*" (allActions) entry, so a lesser admin cannot impersonate an owner
// to gain the global wildcard. A resource "*" in other requires this set to grant
// the action on every resource (an all-resource entry), which Allows models by
// matching only "*" entries when the queried resource is "*".
func (s Set) Covers(other Set) bool {
	for _, e := range other.entries {
		if e.allActions {
			if !s.grantsAll(e.resource) {
				return false
			}
			continue
		}
		for a := range e.actions {
			if !s.Allows(e.resource, a) {
				return false
			}
		}
	}
	return true
}

// grantsAll reports whether the set grants every action on resource, which only
// an allActions entry (on that resource or the "*" wildcard) can. A resource of
// "*" therefore requires a "*:*" entry.
func (s Set) grantsAll(resource string) bool {
	for _, e := range s.entries {
		if (e.resource == "*" || e.resource == resource) && e.allActions {
			return true
		}
	}
	return false
}

// Strings returns the raw permission strings (wildcard-expanded form is not
// materialized), for the /auth/me hint list. Order is insertion order.
func (s Set) Strings() []string {
	out := make([]string, 0, len(s.entries))
	for _, e := range s.entries {
		if e.allActions {
			out = append(out, e.resource+":*")
			continue
		}
		acts := make([]string, 0, len(e.actions))
		for a := range e.actions {
			acts = append(acts, a)
		}
		out = append(out, e.resource+":"+strings.Join(acts, ","))
	}
	return out
}

func parse(p string) (entry, error) {
	res, acts, ok := strings.Cut(p, ":")
	if !ok || res == "" || acts == "" {
		return entry{}, fmt.Errorf("rbac: bad permission %q", p)
	}
	e := entry{resource: res, actions: map[string]struct{}{}}
	if acts == "*" {
		e.allActions = true
		return e, nil
	}
	for _, a := range strings.Split(acts, ",") {
		a = strings.TrimSpace(a)
		if a == "" {
			return entry{}, fmt.Errorf("rbac: empty action in %q", p)
		}
		e.actions[a] = struct{}{}
	}
	return e, nil
}
