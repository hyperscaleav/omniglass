// Package rbac is the pure capability logic: parsing permission strings into
// topic patterns, resolving role inheritance, flattening a principal's grants
// into a permission set, and answering "may this principal perform this action on
// this resource" with the :read floor. It has no I/O and no storage dependency,
// so it is unit-testable in isolation. Scope (which entities) is the Storage
// Gateway's job, not this package's; rbac answers capability only.
//
// Permissions are colon-delimited token paths, matched like NATS subjects (which
// the node path already uses, so the whole stack shares one wildcard convention):
//
//   - a literal token matches itself;
//   - `*` matches exactly one token;
//   - `>` matches one or more tokens and must be last.
//
// A normal permission is `resource:action` (two tokens); an admin-sensitive one is
// `resource:action:admin` (three tokens). Because `*` is a single token, a
// two-token pattern like `*:read` (viewer) or `*:*` structurally cannot match a
// three-token `:admin` permission: sensitivity is a deeper token, not a special
// case. The whole-estate superuser is `>`.
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

// pattern is a permission topic pattern: a token path where the last token may be
// the tail wildcard `>` and any token may be the single-token wildcard `*`.
type pattern []string

// Set is a parsed, queryable permission set: a list of grant patterns.
type Set struct {
	patterns []pattern
}

// NewSet parses permission strings into a Set, skipping any that do not parse (a
// malformed seeded permission should not silently widen access; parse errors are
// dropped, so an unparseable entry simply grants nothing). A comma-separated
// action list (`component:create,update`) expands to one pattern per action.
func NewSet(perms []string) Set {
	var s Set
	for _, p := range perms {
		pats, err := parse(p)
		if err != nil {
			continue
		}
		s.patterns = append(s.patterns, pats...)
	}
	return s
}

// parse turns a permission string into one or more patterns. Grammar: colon-
// delimited tokens; the action token (index 1) may be a comma list, which expands;
// `>` is valid only as the final token; no token may be empty.
func parse(p string) ([]pattern, error) {
	if p == "" {
		return nil, fmt.Errorf("rbac: empty permission")
	}
	segs := strings.Split(p, ":")
	for i, s := range segs {
		if s == "" {
			return nil, fmt.Errorf("rbac: empty token in %q", p)
		}
		if s == ">" && i != len(segs)-1 {
			return nil, fmt.Errorf("rbac: tail wildcard %q not last in %q", ">", p)
		}
	}
	if len(segs) == 1 {
		if segs[0] == ">" {
			return []pattern{{">"}}, nil // the whole-estate superuser pattern
		}
		return nil, fmt.Errorf("rbac: permission %q needs an action", p)
	}
	// segs[0] = resource, segs[1] = action(s), segs[2:] = tier and beyond.
	var out []pattern
	for _, a := range strings.Split(segs[1], ",") {
		a = strings.TrimSpace(a)
		if a == "" {
			return nil, fmt.Errorf("rbac: empty action in %q", p)
		}
		pat := pattern{segs[0], a}
		pat = append(pat, segs[2:]...)
		// The tail wildcard is valid only as the final token. Re-check on the built
		// pattern, not just the colon-segments, so a `>` inside the comma action list
		// (`audit:read,>:admin` -> [audit, >, admin]) cannot smuggle a non-final tail.
		for i, tok := range pat {
			if tok == ">" && i != len(pat)-1 {
				return nil, fmt.Errorf("rbac: tail wildcard not last in %q", p)
			}
		}
		out = append(out, pat)
	}
	return out, nil
}

// sensitiveResources are resources a bare single-token `*` wildcard does not
// reach: a principal holds them only through an explicit (scoped) grant, never
// through the `*:read` floor, so a viewer cannot enumerate them. The tail wildcard
// `>` (owner) and a literal grant still name them. Secrets are sensitive so a
// field tech does not see the platform-credential directory; per-secret
// admin-sensitivity (the `:admin` tier, enforced in the gateway) then fences
// individual rows. The IAM directories use the `:admin` tier instead (they have no
// legitimate sub-admin reader), so they are not in this set.
var sensitiveResources = map[string]bool{
	"secret": true,
	// settings is platform configuration, admin-only to read: a bare `*:read`
	// (viewer) must not reach it, so an ordinary user sees settings only through
	// the authn-only /settings/me (client-visible namespaces), never the admin
	// read-with-provenance. admin/owner hold it via the explicit settings grant.
	"settings": true,
}

// match reports whether a grant pattern matches a required permission path (both
// token slices): a literal matches itself, `*` matches one token, `>` matches one
// or more remaining tokens (and is last). No wildcard reaches deeper than it
// spans, so a two-token pattern never matches a three-token `:admin` path, and a
// bare `*` at the resource position never matches a sensitive resource.
func match(pat pattern, path []string) bool {
	i := 0
	for ; i < len(pat); i++ {
		if pat[i] == ">" {
			return len(path)-i >= 1 // tail: at least one remaining token
		}
		if i >= len(path) {
			return false // pattern has a token the path lacks
		}
		if pat[i] == "*" {
			if i == 0 && sensitiveResources[path[0]] {
				return false // a bare resource wildcard does not reach a sensitive resource
			}
			continue
		}
		if pat[i] != path[i] {
			return false
		}
	}
	return i == len(path) // both exhausted exactly
}

// Allows reports whether the set permits the required permission, given as its
// tokens (e.g. Allows("location", "read") or Allows("audit", "read", "admin")).
// The :read floor applies: holding any permission on a resource implies read on
// that resource (the two-token `<resource>:read`), so a verb-only role can read
// what it acts on. The floor never reaches a `:admin` (three-token) read.
func (s Set) Allows(tokens ...string) bool {
	for _, p := range s.patterns {
		if match(p, tokens) {
			return true
		}
	}
	if len(tokens) == 2 && tokens[1] == "read" {
		sensitive := sensitiveResources[tokens[0]]
		for _, p := range s.patterns {
			if len(p) > 0 && (p[0] == ">" || p[0] == tokens[0] || (p[0] == "*" && !sensitive)) {
				return true
			}
		}
	}
	return false
}

// Covers reports whether this set grants everything the other set grants: every
// grant pattern in other is subsumed by some pattern in this set. It is the
// escalation guard for impersonation (A may impersonate T only when A.Covers(T))
// and for grant creation (a granter may grant a role only when it covers it), so
// it is conservative: it checks single-pattern subsumption, so if other's reach
// is only covered by the union of several of this set's patterns it returns false
// (deny), never a false grant. The :read floor need not be handled here: if this
// set covers other's explicit patterns, this set holds a permission on each of
// those resources too, so its floor already covers other's implied reads.
func (s Set) Covers(other Set) bool {
	for _, p := range other.patterns {
		if !s.coversPattern(p) {
			return false
		}
	}
	return true
}

// coversPattern reports whether this set grants everything pattern p grants, by
// direct subsumption or, for a two-token read pattern, via the :read floor (any
// permission on the resource confers read on it). The floor mirrors Allows: a
// `[R, read]` is covered when this set holds a permission on R (or, when R is the
// `*` resource wildcard, a permission on every resource).
func (s Set) coversPattern(p pattern) bool {
	for _, q := range s.patterns {
		if subsumes(q, p) {
			return true
		}
	}
	if len(p) == 2 && p[1] == "read" {
		sensitive := sensitiveResources[p[0]]
		for _, q := range s.patterns {
			if len(q) == 0 {
				continue
			}
			if p[0] == "*" {
				if q[0] == "*" || q[0] == ">" {
					return true
				}
			} else if q[0] == ">" || q[0] == p[0] || (q[0] == "*" && !sensitive) {
				return true
			}
		}
	}
	return false
}

// subsumes reports whether q's language is a superset of p's: every concrete
// permission p matches, q also matches. `>` (q) covers any non-empty remainder;
// `*` (q) covers a single token (literal or `*`); a literal (q) covers only itself.
// A `>` in p is covered only by a `>` in q at the same position.
func subsumes(q, p pattern) bool {
	i := 0
	for ; i < len(q); i++ {
		if q[i] == ">" {
			return len(p)-i >= 1 // q's tail covers p's non-empty remainder
		}
		if i >= len(p) {
			return false // q requires a token p never has
		}
		if p[i] == ">" {
			return false // p is a tail here; a single q token cannot cover it
		}
		if q[i] == "*" {
			if i == 0 && sensitiveResources[p[0]] {
				return false // a bare resource wildcard does not subsume a sensitive resource
			}
			continue // covers any single token, including p's "*"
		}
		if q[i] != p[i] {
			return false
		}
	}
	return i == len(p)
}

// Strings returns the permission strings, one per pattern, for the /auth/me hint
// list and the roles view. Order is insertion order.
func (s Set) Strings() []string {
	out := make([]string, 0, len(s.patterns))
	for _, p := range s.patterns {
		out = append(out, strings.Join(p, ":"))
	}
	return out
}
