package storage

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrComponentTypeNoStem is generateNameForProduct's refusal when the
// resolved component_type chain (the type itself and every ancestor,
// ResolveTypeFacts's walk) has no stem anywhere. CreateComponentType now
// requires a root component_type to carry one (there is no ancestor left to
// inherit from), so this should only be reachable through data that
// predates that guard or bypasses it (UpsertComponentType, the trusted
// boot-seed path). It is a data defect, not a normal "nothing to generate
// from" case: minting "-1" from an empty stem would silently violate the
// entity name rule and the address grammar every downstream reader trusts,
// so this is refused instead, naming the offending component_type.
var ErrComponentTypeNoStem = errors.New("storage: component_type chain has no stem to generate a name from")

// The name generator (#627 Task 14): when an operator leaves name empty on
// create, or hands the pen back with :resetName, or a move or a product
// reclassify leaves a still-platform-owned name no longer fitting its stem,
// the platform mints one instead. One rule, no branches: <resolved-stem>-<n>,
// the ordinal always present (so a lone component still reads "display-1",
// never bare "display"), n the smallest positive integer not already claimed
// by a sibling name matching "<stem>-<digits>" in the same placement scope.
// Roles, positions, and staffing feed nothing into it; :swap, assign, and
// unassign never touch a name.

// ordinalSuffixRe matches the "-<digits>" tail generateComponentName always
// mints. Anchored at both ends so it only matches the whole remainder after
// the stem prefix is stripped, not a digit run buried inside a longer
// operator-typed suffix.
var ordinalSuffixRe = regexp.MustCompile(`^-([0-9]+)$`)

// pickOrdinal returns the smallest positive integer not already claimed by a
// sibling name of the exact form "<stem>-<n>". A name with a different stem
// (a foreign prefix, or a prefix that merely starts with stem's characters,
// e.g. "display-panel-1" against stem "display") is ignored, and so is an
// operator's bare "display" with no ordinal suffix at all: neither occupies
// an ordinal, so neither can block one. Pure: no I/O, no clock, so the whole
// allocation rule is exhaustively unit-testable without a database.
func pickOrdinal(existing []string, stem string) int {
	prefix := stem + "-"
	used := make(map[int]bool, len(existing))
	for _, name := range existing {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		// Keep the leading "-" so the anchored regex only accepts an all-digit
		// remainder; "display-panel-1" strips to "-panel-1", which the regex
		// rejects, leaving the foreign stem's ordinal space untouched.
		suffix := name[len(prefix)-1:]
		m := ordinalSuffixRe.FindStringSubmatch(suffix)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue // unreachable for a regexp-matched digit run, but never trust a parse blindly
		}
		used[n] = true
	}
	n := 1
	for used[n] {
		n++
	}
	return n
}

// nameGenScopeKey composes the placement half of the ordinal generator's
// advisory-lock key and its sibling-scan filter: the same three buckets the
// scoped-name unique indexes enforce (component_parent_name_key,
// component_location_name_key, component_orphan_name_key,
// db/migrations/20260808090000_names_scope_to_placement.sql), a parent
// winning over a location winning over the unplaced/root bucket, mirroring
// ComponentNameTaken's own branch order.
func nameGenScopeKey(parentID, locationID *string) string {
	switch {
	case parentID != nil:
		return "parent/" + *parentID
	case locationID != nil:
		return "location/" + *locationID
	default:
		return "orphan"
	}
}

// siblingNamesInScope reads every component name sharing parentID/locationID's
// placement bucket, the exact candidate set pickOrdinal scans. excludeID
// omits one row (a move or a reclassify recomputing its OWN row's name,
// which already occupies this bucket under its old name at read time); nil
// excludes nothing (a create, whose row does not exist yet).
func siblingNamesInScope(ctx context.Context, q querier, parentID, locationID, excludeID *string) ([]string, error) {
	var (
		rows pgx.Rows
		err  error
	)
	switch {
	case parentID != nil:
		rows, err = q.Query(ctx, `select name from component where parent_id = $1 and ($2::uuid is null or id <> $2::uuid)`, *parentID, excludeID)
	case locationID != nil:
		rows, err = q.Query(ctx, `select name from component where parent_id is null and location_id = $1 and ($2::uuid is null or id <> $2::uuid)`, *locationID, excludeID)
	default:
		rows, err = q.Query(ctx, `select name from component where parent_id is null and location_id is null and ($1::uuid is null or id <> $1::uuid)`, excludeID)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: scan sibling names for name generation: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("storage: scan sibling name for name generation: %w", err)
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// generateComponentName mints "<stem>-<n>" inside the caller's transaction.
//
// The advisory lock it takes first is transaction-scoped
// (pg_advisory_xact_lock, released at commit or rollback, the same primitive
// lockMemberComponent already uses to serialize a component's default
// membership) and keyed on stem PLUS placement scope, not either alone: two
// concurrent creates under different parents never contend for the same
// ordinal even with the same stem, and two concurrent creates under the same
// parent with different stems never contend either, since ordinals are
// counted per (stem, scope) pair, not per scope or per stem. Only two
// creates that would actually collide on the same ordinal serialize, and
// they do so with no retry: a 23505 would abort the whole transaction, and
// this repo's other race of this class (ADR-0075's guarded conditional
// insert) is a different shape (a single conditional write, not an
// allocate-then-write sequence), so a lock taken before the read-then-decide
// is the only one of the two established patterns that fits.
//
// excludeID is threaded straight through to siblingNamesInScope; see its
// doc comment.
//
// The mint never returns without passing validateEntityName first: an
// enforced postcondition of this primitive, not an assumed one a caller's
// comment claims on its behalf. Every caller (create, :resetName, a move, a
// reclassify) trusts this return value with no re-check of its own, so the
// guarantee has to live here, the one place all of them funnel through, or
// it is not a guarantee at all.
func generateComponentName(ctx context.Context, tx pgx.Tx, stem string, parentID, locationID, excludeID *string) (string, error) {
	if err := lockAdvisory(ctx, tx, "component_name/"+stem+"/"+nameGenScopeKey(parentID, locationID)); err != nil {
		return "", err
	}
	existing, err := siblingNamesInScope(ctx, tx, parentID, locationID, excludeID)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%d", stem, pickOrdinal(existing, stem))
	if err := validateEntityName(name); err != nil {
		return "", fmt.Errorf("storage: generated name %q is invalid: %w", name, err)
	}
	return name, nil
}

// genericDeviceProductID resolves generic-device's id, the same fallback
// CreateComponent's insert applies at the SQL level (COALESCE) for a caller
// that skips product entirely. The name generator needs the id as a plain Go
// value, not just a SQL-side default, because it has to know which
// component_type it is minting a stem from.
func genericDeviceProductID(ctx context.Context, q querier) (string, error) {
	var id string
	if err := q.QueryRow(ctx, `select id from product where name = 'generic-device'`).Scan(&id); err != nil {
		return "", fmt.Errorf("storage: resolve generic-device product: %w", err)
	}
	return id, nil
}

// componentTypeIDForProduct resolves the component_type a product is
// classified under, the input resolveTypeFacts walks to find a stem. Takes a
// querier so it reads inside the caller's transaction rather than
// committed-only state (the same reason resolveTypeFacts itself takes one).
func componentTypeIDForProduct(ctx context.Context, q querier, productID string) (uuid.UUID, error) {
	var raw string
	if err := q.QueryRow(ctx, `select component_type_id from product where id = $1`, productID).Scan(&raw); err != nil {
		return uuid.UUID{}, fmt.Errorf("storage: resolve component_type for product %q: %w", productID, err)
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("storage: product %q component_type_id %q is not a uuid: %w", productID, raw, err)
	}
	return id, nil
}

// generateNameForProduct is the two-step stem-then-ordinal path every
// generation site (create, :resetName, a move, a product reclassify) shares:
// resolve the product's component_type stem, then mint the ordinal within
// the given placement scope. One function so "generate a name" has exactly
// one implementation, never a per-call-site variant.
func generateNameForProduct(ctx context.Context, tx pgx.Tx, productID string, parentID, locationID, excludeID *string) (string, error) {
	typeID, err := componentTypeIDForProduct(ctx, tx, productID)
	if err != nil {
		return "", err
	}
	stem, _, _, _, err := resolveTypeFacts(ctx, tx, typeID)
	if err != nil {
		return "", fmt.Errorf("storage: resolve type facts for product %q: %w", productID, err)
	}
	// A resolved stem of "" means the walk reached the root of this
	// component_type's ancestry with no stem set anywhere on it: refused by
	// name rather than minted into "-1", which would pass straight through
	// as a real component name and fail every downstream reader that
	// assumes the entity name grammar.
	if stem == "" {
		return "", fmt.Errorf("%w: component_type %s (product %q)", ErrComponentTypeNoStem, typeID, productID)
	}
	return generateComponentName(ctx, tx, stem, parentID, locationID, excludeID)
}
