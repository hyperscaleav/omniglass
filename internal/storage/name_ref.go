package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
)

// Every operator resource answers to its name (#1133, ADR-1133). The three tree
// entities already did (a uuid, a bare name, or a dotted address, through
// resolveRef); this file is the same contract for the resources whose name is
// unique only inside something else, or not unique at all:
//
//   - an endpoint is `<component>/<endpoint name>`, its name unique within its
//     component (endpoint_component_name_key);
//   - a secret or a variable is `<name>` or `<owner>/<name>`, its name unique
//     within its owner on every tier (the four *_key_uuid / *_platform_name
//     partial indexes per table), the owner a component, system or location
//     reference or `$platform`, the tier that has no name of its own;
//   - a file is its filename, unique nowhere;
//   - a principal group is its name, unique outright.
//
// One rule runs through all of them, the one resolveRef already states: scope
// decides before ambiguity does. Every candidate a name matches is narrowed to
// what the caller may read FIRST, so a name an out-of-scope row shares never
// makes an in-scope one ambiguous, and a refusal never names a row the caller
// could not have listed. A uuid is tried first and passed straight through, so
// the caller's existing load-then-check path (and its non-disclosing miss) runs
// for it unchanged; a name resolves to the uuid and then runs the very same
// path, which is what keeps every permission and scope check on the resolved
// resource rather than on the spelling that found it.

// ErrUnresolvedRef is a name reference that resolves to no single row the caller
// may see: nothing visible answers to it, or more than one thing does. Either
// way the request names no one resource, so the API answers 404 naming what was
// looked up (#1133). It unwraps to the resource's own not-found sentinel, so a
// caller that already folds that sentinel keeps working.
//
// Candidates is set only when the reference is ambiguous, and holds each match's
// own unambiguous spelling (a full address, an owner-qualified name, or a uuid
// where the resource has no better one). Every candidate is a row the caller may
// read, because the list is built after the scope filter, never before: an
// absent row and an out-of-scope one produce the identical refusal.
//
// Hint says how the kind is addressed, for a reference whose shape could not
// name one at all (an endpoint reference with no `/`).
type ErrUnresolvedRef struct {
	Kind       string
	Ref        string
	Candidates []string
	Hint       string
	notFound   error
}

func (e *ErrUnresolvedRef) Error() string {
	msg := fmt.Sprintf("storage: %s %q not found", e.Kind, e.Ref)
	if len(e.Candidates) > 0 {
		msg += fmt.Sprintf(": it names %d you can see (%s)", len(e.Candidates), strings.Join(e.Candidates, ", "))
	}
	if e.Hint != "" {
		msg += ": " + e.Hint
	}
	return msg
}

// Unwrap returns the resource's not-found sentinel.
func (e *ErrUnresolvedRef) Unwrap() error { return e.notFound }

func unresolved(kind, ref string, notFound error) *ErrUnresolvedRef {
	return &ErrUnresolvedRef{Kind: kind, Ref: ref, notFound: notFound}
}

// addressesOf renders the dotted address of every id in ids, keyed by id, the
// spelling a candidate list names a tree row by: PathsOf joined, the exact
// inverse of resolvePath, in a constant number of queries however many
// candidates an ambiguous name matched.
func addressesOf(ctx context.Context, q querier, tbl scopeTable, ids []string) (map[string]string, error) {
	paths, err := PathsOf(ctx, q, tbl, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(paths))
	for id, segs := range paths {
		out[id] = strings.Join(segs, ".")
	}
	return out, nil
}

// --- endpoint ----------------------------------------------------------------

// EndpointRefHint is how an endpoint is addressed, said whenever a reference's
// shape could not name one.
const EndpointRefHint = "address an endpoint as <component>/<endpoint> (the component by name, dotted address or uuid) or by its uuid"

// endpointIDByRef resolves an endpoint reference to the endpoint's uuid within
// the caller's read scope: a uuid passes through untouched (the caller's own
// load-then-check runs for it), and `<component>/<name>` resolves the component
// through the component resolver's own lookup (loadByRef: a uuid, a dotted
// address, or a bare name) narrowed to the components that carry an endpoint of
// that name AND that the caller may read, before ambiguity is judged (the shape
// loadByRefWithin gives the tree resolvers, done here in two set-wide
// statements). A component that carries no such endpoint was never a candidate
// for this reference, so `display-1/ssh` resolves even where two rooms each hold
// a display-1 and only one of them speaks ssh.
//
// A component-less (server-hosted) endpoint has no unique name, since
// endpoint_component_name_key admits any number of null-component rows, so it
// stays addressed by uuid alone.
func endpointIDByRef(ctx context.Context, q querier, ref string, read scope.Set) (string, error) {
	if isUUID(ref) {
		return ref, nil
	}
	compRef, name, ok := strings.Cut(ref, "/")
	if !ok || compRef == "" || name == "" || strings.Contains(name, "/") {
		e := unresolved("endpoint", ref, ErrEndpointNotFound)
		e.Hint = EndpointRefHint
		return "", e
	}
	compIDs, err := treeRefIDs(ctx, q, componentConfig, compRef)
	if err != nil {
		return "", err
	}
	if len(compIDs) == 0 {
		return "", unresolved("endpoint", ref, ErrEndpointNotFound)
	}
	// The relation first (the components that carry an endpoint of this name),
	// then the scope, each one statement over the whole candidate set however
	// many components share the name.
	rows, err := q.Query(ctx, `select id, component from endpoint where component = any($1::uuid[]) and name = $2 order by component`, compIDs, name)
	if err != nil {
		return "", fmt.Errorf("storage: resolve endpoint %q: %w", ref, err)
	}
	type match struct{ endpoint, component string }
	var carrying []match
	for rows.Next() {
		var m match
		if err := rows.Scan(&m.endpoint, &m.component); err != nil {
			rows.Close()
			return "", fmt.Errorf("storage: scan endpoint %q: %w", ref, err)
		}
		carrying = append(carrying, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("storage: resolve endpoint %q: %w", ref, err)
	}
	carrierIDs := make([]string, 0, len(carrying))
	for _, m := range carrying {
		carrierIDs = append(carrierIDs, m.component)
	}
	readable, err := inScopeIDs(ctx, q, componentTable, carrierIDs, read)
	if err != nil {
		return "", err
	}
	var within []match
	for _, m := range carrying {
		if readable[m.component] {
			within = append(within, m)
		}
	}
	switch len(within) {
	case 0:
		return "", unresolved("endpoint", ref, ErrEndpointNotFound)
	case 1:
		return within[0].endpoint, nil
	}
	ids := make([]string, 0, len(within))
	for _, m := range within {
		ids = append(ids, m.component)
	}
	addrs, err := addressesOf(ctx, q, componentTable, ids)
	if err != nil {
		return "", err
	}
	e := unresolved("endpoint", ref, ErrEndpointNotFound)
	for _, m := range within {
		e.Candidates = append(e.Candidates, addrs[m.component]+"/"+name)
	}
	return "", e
}

// --- secret and variable -----------------------------------------------------

// PlatformOwner is the owner spelling of the platform tier, which has no name of
// its own. The `$` sigil is the one the address grammar already reserves for
// words the platform defines (ADR-0089): a name can never contain it, so
// `$platform` can never collide with a location or a component that happens to
// be named platform.
const PlatformOwner = "$platform"

// ownedTable is a table whose rows hang off the exclusive owner arc (platform,
// location, system, component) and carry a name unique within their owner:
// secret and variable.
type ownedTable struct {
	table    string
	kind     string
	notFound error
	// sensitive is the expression over the row (aliased s) that fences it to
	// the admin tier.
	sensitive string
}

var (
	secretOwned   = ownedTable{table: "secret", kind: "secret", notFound: ErrSecretNotFound, sensitive: "s.admin_sensitive"}
	variableOwned = ownedTable{table: "variable", kind: "variable", notFound: ErrVariableNotFound, sensitive: "false"}
)

// ownedCandidate is one visible row a name matched, as much of it as the owner
// match and the refusal's spelling need.
type ownedCandidate struct {
	id        string
	ownerKind string
	ownerID   *string
}

// ownedIDByRef resolves a secret or variable reference to the row's uuid: a uuid
// passes through, `<name>` matches every row so named, and `<owner>/<name>`
// keeps the ones whose owner the owner part names. The matches are narrowed to
// what the caller may read IN the query (the arc-scope primitive over the
// owner, the platform tier only with the all scope, and an admin-sensitive row
// only with canAdmin), one statement however many owners share the name, and
// BEFORE the owner is matched or ambiguity judged, so neither step can be told
// apart from absence for a row the caller cannot see.
//
// The owner part is `$platform`, a uuid, or any reference a tree resolver takes
// (a bare name or a dotted address), matched against each visible candidate's
// own owner: it resolves scope-blind, which is safe because it only ever
// intersects with rows already proven visible, and it reports no outcome of its
// own (a dotted address that misses its plane is simply no match).
func ownedIDByRef(ctx context.Context, q querier, t ownedTable, ref string, read scope.Set, canAdmin bool) (string, error) {
	if isUUID(ref) {
		return ref, nil
	}
	owner, name, qualified := strings.Cut(ref, "/")
	if !qualified {
		owner, name = "", ref
	}
	if name == "" || (qualified && owner == "") || strings.Contains(name, "/") {
		e := unresolved(t.kind, ref, t.notFound)
		e.Hint = "address a " + t.kind + " as <name> or <owner>/<name> (the owner by name, dotted address, uuid, or " + PlatformOwner + ") or by its uuid"
		return "", e
	}
	if read.Empty() {
		return "", unresolved(t.kind, ref, t.notFound)
	}
	sel := `
		select s.id, s.owner_kind, coalesce(s.component_id, s.system_id, s.location_id)
		from ` + t.table + ` s where s.name = $1 and (not ` + t.sensitive + ` or $2)`
	args := []any{name, canAdmin}
	if !read.All {
		inclusive, excluded, selfIDs := arcScopeArgs(read)
		sel = arcScopeCTEs(3, 4) + sel + ` and ` + arcScopePredicate("s", 5)
		args = append(args, inclusive, excluded, selfIDs)
	}
	rows, err := q.Query(ctx, sel+` order by s.id`, args...)
	if err != nil {
		return "", fmt.Errorf("storage: resolve %s %q: %w", t.kind, ref, err)
	}
	var seen []ownedCandidate
	for rows.Next() {
		var c ownedCandidate
		if err := rows.Scan(&c.id, &c.ownerKind, &c.ownerID); err != nil {
			rows.Close()
			return "", fmt.Errorf("storage: scan %s %q: %w", t.kind, ref, err)
		}
		seen = append(seen, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("storage: resolve %s %q: %w", t.kind, ref, err)
	}
	if qualified {
		owned, err := ownerMatches(ctx, q, owner, seen)
		if err != nil {
			return "", err
		}
		seen = owned
	}
	switch len(seen) {
	case 0:
		return "", unresolved(t.kind, ref, t.notFound)
	case 1:
		return seen[0].id, nil
	}
	e := unresolved(t.kind, ref, t.notFound)
	spelled, err := ownerSpellings(ctx, q, seen)
	if err != nil {
		return "", err
	}
	for _, c := range seen {
		e.Candidates = append(e.Candidates, spelled[c.id]+"/"+name)
	}
	return "", e
}

// ownerMatches keeps the candidates whose owner the owner reference names.
func ownerMatches(ctx context.Context, q querier, owner string, cands []ownedCandidate) ([]ownedCandidate, error) {
	if len(cands) == 0 {
		return nil, nil
	}
	ids := map[string]bool{}
	switch {
	case owner == PlatformOwner:
	case isUUID(owner):
		ids[owner] = true
	default:
		kinds := map[string]bool{}
		for _, c := range cands {
			kinds[c.ownerKind] = true
		}
		var (
			found []string
			err   error
		)
		for kind := range kinds {
			switch kind {
			case "component":
				found, err = treeRefIDs(ctx, q, componentConfig, owner)
			case "system":
				found, err = treeRefIDs(ctx, q, systemConfig, owner)
			case "location":
				found, err = treeRefIDs(ctx, q, locationConfig, owner)
			default:
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, id := range found {
				ids[id] = true
			}
		}
	}
	var out []ownedCandidate
	for _, c := range cands {
		if owner == PlatformOwner {
			if c.ownerKind == "platform" {
				out = append(out, c)
			}
			continue
		}
		if c.ownerID != nil && ids[*c.ownerID] {
			out = append(out, c)
		}
	}
	return out, nil
}

// treeRefIDs is every row id a tree reference matches, scope-blind: a uuid, a
// dotted address, or every row carrying a bare name. A dotted address whose
// plane is not cfg's (a location address asked of the component table) is no
// match rather than an error, since the owner part is tried against each tier.
// A syntax error in an address is still returned: it discloses nothing.
func treeRefIDs[T any](ctx context.Context, q querier, cfg scopedConfig[T], ref string) ([]string, error) {
	rows, err := loadByRef(ctx, q, cfg, ref)
	var pathMiss *ErrPathNotFound
	if errors.As(err, &pathMiss) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, cfg.idOf(r))
	}
	return out, nil
}

// ownerSpellings is how each candidate's owner is written in a refusal, keyed
// by the candidate's id: the platform sigil, or the owner's dotted address,
// batched per owner tier.
func ownerSpellings(ctx context.Context, q querier, cands []ownedCandidate) (map[string]string, error) {
	byTier := map[scopeTable][]string{}
	for _, c := range cands {
		if tbl, ok := scopeKindTable(c.ownerKind); ok && c.ownerID != nil {
			byTier[tbl] = append(byTier[tbl], *c.ownerID)
		}
	}
	addrs := map[string]string{}
	for tbl, ids := range byTier {
		got, err := addressesOf(ctx, q, tbl, ids)
		if err != nil {
			return nil, err
		}
		for id, a := range got {
			addrs[id] = a
		}
	}
	out := make(map[string]string, len(cands))
	for _, c := range cands {
		switch {
		case c.ownerKind == "platform" || c.ownerID == nil:
			out[c.id] = PlatformOwner
		case addrs[*c.ownerID] != "":
			out[c.id] = addrs[*c.ownerID]
		default:
			out[c.id] = *c.ownerID
		}
	}
	return out, nil
}

// --- file --------------------------------------------------------------------

// fileIDByRef resolves a file reference to its uuid: a uuid passes through, and
// anything else is a filename, matched among the files the caller may see (a
// sensitive file only with the admin tier). A filename is unique nowhere, so two
// visible files of one name refuse with both uuids, the only spelling a file has
// that is unique. A filename that is itself uuid-shaped is reached by its id,
// since the uuid is always tried first.
func fileIDByRef(ctx context.Context, q querier, ref string, canAdmin bool) (string, error) {
	if isUUID(ref) {
		return ref, nil
	}
	rows, err := q.Query(ctx, `select id from file where name = $1 and (not sensitive or $2) order by id`, ref, canAdmin)
	if err != nil {
		return "", fmt.Errorf("storage: resolve file %q: %w", ref, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("storage: scan file %q: %w", ref, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("storage: resolve file %q: %w", ref, err)
	}
	switch len(ids) {
	case 0:
		return "", unresolved("file", ref, ErrFileNotFound)
	case 1:
		return ids[0], nil
	}
	e := unresolved("file", ref, ErrFileNotFound)
	e.Candidates = ids
	return "", e
}

// --- principal group ---------------------------------------------------------

// groupIDByRef resolves a principal group reference to its uuid: a uuid passes
// through (the caller's existing not-found applies to it), and anything else is
// the group's name, unique outright (principal_group_name_key). Every caller
// runs it after the all-scope check, so a non-all caller learns nothing about
// which names exist.
func groupIDByRef(ctx context.Context, q querier, ref string) (string, error) {
	if isUUID(ref) {
		return ref, nil
	}
	var id string
	err := q.QueryRow(ctx, `select id from principal_group where name = $1`, ref).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", unresolved("principal group", ref, ErrGroupNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("storage: resolve principal group %q: %w", ref, err)
	}
	return id, nil
}

// --- principal ---------------------------------------------------------------

// principalIDByRef resolves a principal reference to its uuid: a uuid passes
// through (any kind, and an unknown one too, so the caller's own not-found
// applies unchanged), and anything else is a human's username or a service
// account's name (#1133), each unique among its own kind. A word that is both
// names no one principal and is refused with both uuids. A node is reached by
// its name on /nodes, not here.
func principalIDByRef(ctx context.Context, q querier, ref string) (string, error) {
	if isUUID(ref) {
		return ref, nil
	}
	rows, err := q.Query(ctx, `
		select principal_id from human where username = $1
		union all
		select principal_id from service where name = $1
		order by 1`, ref)
	if err != nil {
		return "", fmt.Errorf("storage: resolve principal ref %q: %w", ref, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("storage: scan principal ref %q: %w", ref, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("storage: resolve principal ref %q: %w", ref, err)
	}
	switch len(ids) {
	case 0:
		return "", unresolved("principal", ref, ErrPrincipalNotFound)
	case 1:
		return ids[0], nil
	}
	e := unresolved("principal", ref, ErrPrincipalNotFound)
	e.Candidates = ids
	return "", e
}

// --- grant scope root --------------------------------------------------------

// resolveScopeRoot resolves a grant's scope root, a location, system or
// component by uuid, bare name or dotted address (#1133), to the uuid the grant
// stores and the scope filter matches. Granting is all-scope IAM work, so the
// root resolves fleet-wide (scopedByName); a reference that names nothing is
// ErrBadScope, as an unknown uuid always was, and one that names more than one
// row is the tree resolver's own ErrAmbiguousName, listing every candidate to a
// caller entitled to see all of them.
func resolveScopeRoot(ctx context.Context, q querier, kind, ref string) (string, error) {
	var (
		id  string
		err error
	)
	switch kind {
	case "location":
		var l *Location
		if l, err = scopedByName(ctx, q, locationConfig, ref); l != nil {
			id = l.ID
		}
	case "system":
		var s *System
		if s, err = scopedByName(ctx, q, systemConfig, ref); s != nil {
			id = s.ID
		}
	case "component":
		var c *Component
		if c, err = scopedByName(ctx, q, componentConfig, ref); c != nil {
			id = c.ID
		}
	default:
		return "", ErrBadScope // "group" and any other non-tree kind are unsupported
	}
	var ambiguous *ErrAmbiguousName
	switch {
	case errors.As(err, &ambiguous):
		return "", err
	case errors.Is(err, ErrLocationNotFound), errors.Is(err, ErrSystemNotFound), errors.Is(err, ErrComponentNotFound):
		return "", ErrBadScope
	case err != nil:
		return "", err
	}
	return id, nil
}

// arcOwnerName is the current name of an owner-arc row's owner, "" for the
// platform tier. A secret or variable write answers with it (#1133), so a name
// reference that resolved to an owner the caller did not expect says where it
// landed in the very answer.
func arcOwnerName(ctx context.Context, q querier, kind string, id *string) (string, error) {
	tbl, ok := scopeKindTable(kind)
	if !ok || id == nil {
		return "", nil
	}
	var name string
	if err := q.QueryRow(ctx, `select name from `+string(tbl)+` where id = $1`, *id).Scan(&name); err != nil {
		return "", fmt.Errorf("storage: owner name of %s %q: %w", kind, *id, err)
	}
	return name, nil
}
