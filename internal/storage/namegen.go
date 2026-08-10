package storage

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrComponentTypeNoStem is generateNameForProduct's refusal when the
// resolved component_type chain (the type itself and every ancestor,
// ResolveTypeFacts's walk) has no stem anywhere. CreateComponentType now
// requires a root component_type to carry one (there is no ancestor left to
// inherit from), so this should only be reachable through data that
// predates that guard or bypasses it (UpsertComponentType, the trusted
// boot-seed path).
//
// The refusal survives #681 even though mintName can now produce a legal
// stem-less name ("1" rather than the "-1" that used to make this
// mechanically impossible). A stem-less name is right for a positional type
// whose ordinal genuinely is its name, a floor or a rack unit; it is not
// right for a device, where a bare "1" says nothing about what the thing is
// and a component_type carrying no stem at all is a data defect rather than a
// declaration. So this stays a refusal by policy, naming the offending
// component_type, not a fallback.
var ErrComponentTypeNoStem = errors.New("storage: component_type chain has no stem to generate a name from")

// The name generator (#627 Task 14): when an operator leaves name empty on
// create, or hands the pen back with :resetName, or a move or a product
// reclassify leaves a still-platform-owned name no longer fitting its stem,
// the platform mints one instead. One rule, no branches: mintName's
// "<resolved-stem>-<n>", the ordinal always present (so a lone component
// still reads "display-1", never bare "display"), n the smallest ordinal
// whose minted name no sibling in the same placement scope already holds.
// The number that was picked is then STORED (#681) rather than left to be
// read back out of the name by whoever needs it. Roles, positions, and
// staffing feed nothing into it; :swap, assign, and unassign never touch a
// name.

// mintName is the one place a generated name's SHAPE lives: "<stem>-<n>", or
// the ordinal alone when the type carries no stem. Every generation site
// funnels through it, and so does allocation itself (pickOrdinal below tests
// the name this would mint rather than picking a name apart), so the two can
// never disagree about what a given ordinal is called.
//
// The stem-less form is not a degenerate case, it is the point (#681): a
// positional type whose ordinal genuinely IS its name (a floor called "1")
// needs a mint that produces "1", not the "-1" a bare format string would
// give, which validateEntityName refuses and which the old prefix-scan
// allocator could never have counted anyway.
func mintName(stem string, n int) string {
	if stem == "" {
		return strconv.Itoa(n)
	}
	return stem + "-" + strconv.Itoa(n)
}

// pickOrdinal returns the smallest positive integer whose MINTED name is not
// already held by a sibling in the bucket. The loop runs forward from 1 over
// candidates rather than backward from sibling names to the ordinals they
// encode, and that inversion is the whole of #681's allocator change.
//
// The answers are identical to the prefix-scan it replaces, because that scan
// was already computing this: "the smallest n with no sibling named
// <stem>-<n>". Reading it forward buys three things the parse could not give.
// A stem-less type allocates at all (mintName's "1", "2", where a "-" prefix
// matched nothing and returned 1 forever). A name shape that is not
// "<stem>-<n>" needs no second parser written to match it, only a different
// mint. And an operator-typed name occupying the shape the generator would
// mint still blocks it, which is not a nicety: the scoped-name unique index is
// on the NAME, so an allocator that consulted only the ordinals the platform
// recorded would hand back a name a hand-typed row already holds and turn an
// ordinary create into a 23505 the transaction cannot recover from.
//
// A sibling in a different stem's space blocks nothing, because no candidate
// this mint produces is ever equal to it: "mic-1" and "display-panel-1" are
// both simply not names stem "display" can mint.
//
// It terminates: at most len(existing) candidates can be taken, so the answer
// is never larger than len(existing)+1. Pure (no I/O, no clock), so the whole
// allocation rule stays exhaustively unit-testable without a database.
func pickOrdinal(existing []string, stem string) int {
	taken := make(map[string]bool, len(existing))
	for _, name := range existing {
		taken[name] = true
	}
	for n := 1; ; n++ {
		if !taken[mintName(stem, n)] {
			return n
		}
	}
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
// placement bucket, the exact candidate set pickOrdinal tests against.
// excludeID omits one row (a move or a reclassify recomputing its OWN row's
// name, which already occupies this bucket under its old name at read time);
// nil excludes nothing (a create, whose row does not exist yet).
//
// It reads NAMES, not the ordinals #681 now stores beside them, and that is
// the deliberate half of this slice. The constraint allocation has to satisfy
// is the scoped-name unique index, which is on the name, and the rows holding
// a name are not the same set as the rows holding an ordinal: an operator can
// type "display-1" by hand, taking the name while owning no ordinal at all.
// An allocator reading the ordinal column would not see that row and would
// mint its name a second time. What the stored ordinal buys is everything
// DOWNSTREAM of allocation (the bare render, a label rule reading .Ordinal,
// the recompute-and-compare invariant); what unblocks a stem-less name is
// pickOrdinal minting candidates instead of parsing siblings.
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

// generateComponentName mints a name inside the caller's transaction and
// returns it together with the ORDINAL it was minted from, which the caller
// stores on the row (#681). The number used to be computed here, formatted
// into the name, and dropped on the floor, leaving every downstream reader to
// recover it by string surgery; it is a fact the platform owns, so it is
// returned as one.
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
func generateComponentName(ctx context.Context, tx pgx.Tx, stem string, parentID, locationID, excludeID *string) (string, int, error) {
	if err := lockAdvisory(ctx, tx, "component_name/"+stem+"/"+nameGenScopeKey(parentID, locationID)); err != nil {
		return "", 0, err
	}
	existing, err := siblingNamesInScope(ctx, tx, parentID, locationID, excludeID)
	if err != nil {
		return "", 0, err
	}
	ordinal := pickOrdinal(existing, stem)
	name := mintName(stem, ordinal)
	if err := validateEntityName(name); err != nil {
		return "", 0, fmt.Errorf("storage: generated name %q is invalid: %w", name, err)
	}
	return name, ordinal, nil
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
func generateNameForProduct(ctx context.Context, tx pgx.Tx, productID string, parentID, locationID, excludeID *string) (string, int, error) {
	typeID, err := componentTypeIDForProduct(ctx, tx, productID)
	if err != nil {
		return "", 0, err
	}
	stem, _, _, _, err := resolveTypeFacts(ctx, tx, typeID)
	if err != nil {
		return "", 0, fmt.Errorf("storage: resolve type facts for product %q: %w", productID, err)
	}
	// A resolved stem of "" means the walk reached the root of this
	// component_type's ancestry with no stem set anywhere on it. The mint
	// below would now produce a legal name from it ("1", not the "-1" that
	// used to make this mechanically impossible), which is exactly right for
	// a positional type and exactly wrong for a device: see
	// ErrComponentTypeNoStem for why this stays a refusal.
	if stem == "" {
		return "", 0, fmt.Errorf("%w: component_type %s (product %q)", ErrComponentTypeNoStem, typeID, productID)
	}
	return generateComponentName(ctx, tx, stem, parentID, locationID, excludeID)
}
