package storage

import (
	"context"
	"fmt"
	"strings"
)

// A dotted address is a lookup (#627 Task 12): `boi.17c.415a.$comp.display-1`
// descends the location tree from a root (`boi`, `17c`, `415a`) and then, via
// the `$comp` accessor, switches into the component plane and descends that
// tree instead. This file is the PARSER only: pure syntax, no database, no
// scope. It turns a ref string into an Address or a reason it cannot. The
// walk that turns an Address into a uuid lives beside scopedByName in
// scopedcrud.go, because it has to run inside a transaction and needs a
// querier; nothing here does.
//
// A bare name (no "." and no "$") is deliberately NOT an address: ParseAddress
// returns (nil, nil) for one, and the caller falls through to the existing
// bare-name resolution (loadByRef / resolveMatches), which is what keeps a
// bare name's ambiguity behavior (#627 Task 10/11) exactly as it was. A uuid
// is the same: never dotted, never accessor-bearing, always (nil, nil).

// AddressKind is the plane an address's terminal segment names.
type AddressKind int

const (
	// AddressLocation is the default plane: an address with no accessor
	// names a location, root-first down the location tree.
	AddressLocation AddressKind = iota
	// AddressComponent is reached via the $comp accessor.
	AddressComponent
	// AddressSystem is reached via the $sys accessor.
	AddressSystem
	// AddressRole is reached via $role, valid only within a system path.
	// No scopedConfig resolves a role today (roles are not one of the three
	// scoped-CRUD tree entities), so ParseAddress accepts the grammar and
	// resolvePath's kind-mismatch check refuses it the same way it would
	// refuse any other plane asked of the wrong table: as ErrPathNotFound,
	// not a parse failure. The accessor is reserved now so a future caller
	// that does resolve roles does not have to renegotiate the grammar.
	AddressRole
)

func (k AddressKind) String() string {
	switch k {
	case AddressLocation:
		return "location"
	case AddressComponent:
		return "component"
	case AddressSystem:
		return "system"
	case AddressRole:
		return "role"
	default:
		return "unknown"
	}
}

// Address is a parsed dotted reference.
//
//	boi.17c.415a.$comp.display-1           Root=[boi 17c 415a] Kind=component Tail=[display-1]
//	boi.17c.415a.$comp.dsp-1.dante-card-1  Root=[boi 17c 415a] Kind=component Tail=[dsp-1 dante-card-1]
//	boi.17c.$sys.av                        Root=[boi 17c]      Kind=system    Tail=[av]
//	boi.17c.$sys.av.$role.primary-dsp      Root=[boi 17c]      Kind=role      Tail=[av]     Role=primary-dsp
//	$comp.display-1                        Root=[]             Kind=component Tail=[display-1] (unplaced)
//
// Root is always the location-tree prefix, root segment first, and may be
// empty (an address that opens directly on an accessor addresses an unplaced
// row: component_orphan_name_key / system_orphan_name_key). Tail is the
// target-plane descent after the accessor: its first element resolves
// against the plane's location bucket (or orphan bucket, when Root is
// empty), each element after that against the plane's parent bucket. Every
// segment, in Root and in Tail and in Role, has already passed the entity
// name rule by the time ParseAddress returns one: a percent-encoded slash
// arrives at the handler already decoded (the #627 preflight probe), so
// validating here is what keeps it from ever reaching a query.
type Address struct {
	Root []string
	Kind AddressKind
	Tail []string
	Role string
}

const (
	accessorComp = "$comp"
	accessorSys  = "$sys"
	accessorRole = "$role"
)

// isAccessor reports whether seg is one of the three reserved plane switches.
// entityNameRe already excludes "$" from every legal name (name.go), so any
// segment starting with "$" is either one of these three or malformed;
// isAccessorLike below is what tells the two apart.
func isAccessor(seg string) bool {
	return seg == accessorComp || seg == accessorSys || seg == accessorRole
}

// isAccessorLike reports whether seg was clearly MEANT as an accessor (it
// starts with "$") even if it is not one of the three recognized ones, so
// `$loc` reports "not a valid accessor" rather than failing the entity name
// rule with a message about lowercase letters that would misdirect the
// caller entirely.
func isAccessorLike(seg string) bool {
	return strings.HasPrefix(seg, "$")
}

// ErrInvalidAddress is a dotted-address syntax failure caught before any
// query runs: an empty segment, an accessor in the wrong place or an
// unrecognized one, or a segment that fails the entity name rule (the
// decoded-percent-slash case the #627 preflight probe found). Msg is the
// caller-facing reason; mapRefErr surfaces it verbatim in a 422.
type ErrInvalidAddress struct {
	Msg string
}

func (e *ErrInvalidAddress) Error() string { return "storage: " + e.Msg }

func invalidAddress(format string, args ...any) error {
	return &ErrInvalidAddress{Msg: fmt.Sprintf(format, args...)}
}

// ParseAddress parses ref into an Address, or reports (nil, nil) when ref is
// not an address at all (a uuid, or a bare name with no "." and no "$"): both
// fall through to the existing bare-name/uuid resolution untouched by #627's
// address grammar. A non-nil error is always an *ErrInvalidAddress.
func ParseAddress(ref string) (*Address, error) {
	if ref == "" || isUUID(ref) {
		return nil, nil
	}
	if !strings.ContainsAny(ref, ".$") {
		return nil, nil
	}

	segs := strings.Split(ref, ".")
	for _, s := range segs {
		if s == "" {
			return nil, invalidAddress(
				"empty segment in address %q: an accessor like $comp must be quoted in a shell", ref)
		}
	}

	addr := &Address{}
	i := 0
	for ; i < len(segs); i++ {
		if isAccessorLike(segs[i]) {
			break
		}
		if err := validateEntityName(segs[i]); err != nil {
			return nil, invalidAddress("invalid segment %d (%q) in address %q: %s", i, segs[i], ref, err)
		}
		addr.Root = append(addr.Root, segs[i])
	}
	if i == len(segs) {
		// No accessor: a pure location-tree path, possibly nested. Still a
		// complete address (deterministic: location_root_name_key and
		// location_parent_name_key each guarantee at most one row per hop).
		addr.Kind = AddressLocation
		return addr, nil
	}

	switch segs[i] {
	case accessorComp:
		addr.Kind = AddressComponent
	case accessorSys:
		addr.Kind = AddressSystem
	default:
		return nil, invalidAddress(
			"%q is not a valid accessor in address %q: expected $comp or $sys", segs[i], ref)
	}
	i++
	if i == len(segs) {
		return nil, invalidAddress("address %q ends with %s and no name after it", ref, segs[i-1])
	}

	for ; i < len(segs); i++ {
		if segs[i] == accessorRole {
			if addr.Kind != AddressSystem {
				return nil, invalidAddress(
					"%q is only valid within a system path, in address %q", accessorRole, ref)
			}
			if i != len(segs)-2 {
				return nil, invalidAddress(
					"%q must be followed by exactly one role name, in address %q", accessorRole, ref)
			}
			role := segs[i+1]
			if err := validateEntityName(role); err != nil {
				return nil, invalidAddress("invalid role name %q in address %q: %s", role, ref, err)
			}
			addr.Kind = AddressRole
			addr.Role = role
			return addr, nil
		}
		if isAccessorLike(segs[i]) {
			return nil, invalidAddress("%q is not valid mid-path, in address %q", segs[i], ref)
		}
		if err := validateEntityName(segs[i]); err != nil {
			return nil, invalidAddress("invalid segment %d (%q) in address %q: %s", i, segs[i], ref, err)
		}
		addr.Tail = append(addr.Tail, segs[i])
	}
	return addr, nil
}

// ErrAddressNotAccepted is returned for a ref that looks like #627's address
// grammar (dot-joined, or opening on an accessor) sent to a registry whose
// names stay a single global kebab token: node, tag, and the four telemetry
// lane types (metric_type, property_type, event_type, command_type). Those
// tables sit outside the three scoped tree entities the grammar addresses,
// so a dotted ref here is refused up front rather than silently missing
// every row and falling through to a plain 404 that never says why.
type ErrAddressNotAccepted struct {
	Kind string
	Ref  string
}

func (e *ErrAddressNotAccepted) Error() string {
	return fmt.Sprintf("storage: a %s name is a single token, not an address (got %q)", e.Kind, e.Ref)
}

// RejectAddressForm refuses ref for kind when it looks like an address
// (contains "." or "$") rather than a plain name or a uuid. Call it before
// any lookup on a registry outside the three tree entities: node, tag, and
// the four lane types. A ref that is not address-shaped, including any
// ordinary kebab name or a uuid, passes through untouched.
func RejectAddressForm(kind, ref string) error {
	if ref == "" || isUUID(ref) {
		return nil
	}
	if !strings.ContainsAny(ref, ".$") {
		return nil
	}
	return &ErrAddressNotAccepted{Kind: kind, Ref: ref}
}

// PathOf computes id's own dotted address, accessor included, as the exact
// inverse of resolvePath (#627 Task 15): parsing the string PathOf returns
// (dot-joined) and resolving it through resolvePath lands back on id, because
// both directions agree on the one rule that makes the round trip lossless:
// an address's Root binds only against a PLANE ROOT's own location_id
// (resolvePlaneRoot requires parent_id IS NULL), never a descendant's own
// location_id column. A nested component or system can carry its own
// location_id (CreateComponent and MoveComponent both allow one; it is a
// descriptive field, projected on the wire as `location`), but that value
// plays no part in its address: only walking up to the row with no parent
// and reading THAT row's location_id reproduces what a forward resolve would
// accept back.
//
// For tbl == locationTable, PathOf returns the location's own ancestor
// chain, root first, with no accessor (AddressLocation's own shape). For
// componentTable/systemTable, it returns the location chain the row's own
// plane-root sits at (empty when that root is unplaced/orphan), the
// accessor ($comp or $sys), and the row's own ancestor chain within its
// tree, root first, ending with its own name.
func PathOf(ctx context.Context, q querier, tbl scopeTable, id string) ([]string, error) {
	own, err := ancestorNames(ctx, q, tbl, id)
	if err != nil {
		return nil, err
	}
	if tbl == locationTable {
		return own, nil
	}
	locID, err := planeRootLocationID(ctx, q, tbl, id)
	if err != nil {
		return nil, err
	}
	var root []string
	if locID != nil {
		root, err = ancestorNames(ctx, q, locationTable, *locID)
		if err != nil {
			return nil, err
		}
	}
	accessor := accessorComp
	if tbl == systemTable {
		accessor = accessorSys
	}
	segs := make([]string, 0, len(root)+1+len(own))
	segs = append(segs, root...)
	segs = append(segs, accessor)
	segs = append(segs, own...)
	return segs, nil
}

// ancestorNames walks id's own parent_id chain within tbl, from id up to the
// row whose parent_id is null, returning the chain's names root first (id's
// own name last). Every one of the three tree tables shares this exact
// self-referencing shape, so one recursive CTE serves all three: PathOf
// above runs it once against the target's own tree and, for a component or
// system, a second time against the location tree its plane root sits in.
func ancestorNames(ctx context.Context, q querier, tbl scopeTable, id string) ([]string, error) {
	rows, err := q.Query(ctx, `
		with recursive anc(id, name, parent_id, depth) as (
			select id, name, parent_id, 0 from `+string(tbl)+` where id = $1
			union all
			select t.id, t.name, t.parent_id, anc.depth + 1
			from `+string(tbl)+` t join anc on t.id = anc.parent_id
		) cycle id set is_cycle using path
		select name from anc order by depth desc`, id)
	if err != nil {
		return nil, fmt.Errorf("storage: walk %s ancestor names for %q: %w", tbl, id, err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("storage: scan %s ancestor name for %q: %w", tbl, id, err)
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// planeRootLocationID resolves id's plane root's own location_id: the row
// reached by walking parent_id up from id to the one row in tbl with no
// parent, then reading THAT row's location_id. tbl must be componentTable or
// systemTable (a location has no location_id column of its own; PathOf never
// calls this for locationTable).
func planeRootLocationID(ctx context.Context, q querier, tbl scopeTable, id string) (*string, error) {
	var locID *string
	err := q.QueryRow(ctx, `
		with recursive anc(id, parent_id, location_id, depth) as (
			select id, parent_id, location_id, 0 from `+string(tbl)+` where id = $1
			union all
			select t.id, t.parent_id, t.location_id, anc.depth + 1
			from `+string(tbl)+` t join anc on t.id = anc.parent_id
		) cycle id set is_cycle using path
		select location_id from anc order by depth desc limit 1`, id).Scan(&locID)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve %s plane-root location for %q: %w", tbl, id, err)
	}
	return locID, nil
}
