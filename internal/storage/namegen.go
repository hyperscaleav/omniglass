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

// ErrSystemTypeNoStem is the system-side ErrComponentTypeNoStem: the resolved
// system_type chain carries no stem anywhere, so there is nothing to mint a
// name from. CreateSystemType refuses a root type with no stem
// (ErrRootSystemTypeNeedsStem), so this is reachable only through data that
// predates that guard or through the trusted boot-seed upsert. A refusal by
// policy for the same reason the component one is: a room called "1" says
// nothing about what kind of space it is.
var ErrSystemTypeNoStem = errors.New("storage: system_type chain has no stem to generate a name from")

// ErrSystemTypeRequiredForName is a system asking the platform to name it while
// carrying no system_type at all. system_type_id is nullable (the floor waits
// for the shipped tree to prove out), and the stem a name is minted from lives
// on that registry, so an unclassified system has no source for one.
//
// It is also what an operator meets when they un-classify a system whose name
// the platform owns: that path re-mints the name (the stem may have changed),
// asks for a stem, and lands here. The refusal is the honest answer rather than
// a silent hand-back of the pen, and it names the way out, which is to claim
// the name with :rename first.
var ErrSystemTypeRequiredForName = errors.New("storage: a system with no system_type has no stem to generate a name from; name it explicitly, or :rename it before un-classifying it")

// The name generator (#627 Task 14): when an operator leaves name empty on
// create, or hands the pen back with :resetName, or a move or a reclassify
// leaves a still-platform-owned name no longer fitting its stem, the platform
// mints one instead. One rule, no branches: a nameMint's "<resolved-stem>-<n>",
// n the smallest ordinal whose minted name no sibling in the same placement
// scope already holds. The number that was picked is then STORED (#681) rather
// than left to be read back out of the name by whoever needs it. Roles,
// positions, and staffing feed nothing into it; :swap, assign, and unassign
// never touch a name.
//
// #686 spreads it from component to system, and the two facts a generation site
// has to get right are each ONE VALUE rather than a pair of loose arguments: a
// nameMint (what the name looks like) and a nameScope (which sibling bucket it
// has to be unique in). Both exist because a second entity kind made the loose
// form dangerous: the mint has to be the same one allocation tests against, and
// a system's placement buckets are not a location's.

// nameMint is the SHAPE of a generated name: the resolved stem, plus whether
// the first of that stem in its bucket carries an ordinal at all. It is the one
// place a generated name's shape lives. Every generation site funnels through
// it, and so does allocation itself (pickOrdinal below asks the mint for the
// name it would produce rather than picking a sibling apart), so the two can
// never disagree about what a given ordinal is called.
//
// The stem-less form is not a degenerate case, it is the point (#681): a
// positional type whose ordinal genuinely IS its name (a floor called "1")
// needs a mint that produces "1", not the "-1" a bare format string would
// give, which validateEntityName refuses and which the old prefix-scan
// allocator could never have counted anyway. A stem-less mint therefore ignores
// bareFirst: suppressing the ordinal there would leave no name at all.
type nameMint struct {
	stem string
	// bareFirst suppresses the ordinal on ordinal 1, so the only boardroom in a
	// room is "boardroom" and the second is "boardroom-2" (epic #657's D3,
	// ruled in ADR-0101). It is a field on the mint rather than a branch inside
	// name() because "does this kind of thing carry a number when there is only
	// one of it" is a property of the thing being named, not of the naming
	// rule.
	bareFirst bool
}

// name is the mint: "<stem>-<n>", the stem alone when this mint suppresses the
// first ordinal and n is 1, or the ordinal alone when there is no stem.
func (m nameMint) name(n int) string {
	if m.stem == "" {
		return strconv.Itoa(n)
	}
	if m.bareFirst && n == 1 {
		return m.stem
	}
	return m.stem + "-" + strconv.Itoa(n)
}

// componentMint and systemMint are the mints this slice resolves. The
// difference between them is the whole of D3: a system suppresses its first
// ordinal, a component does not.
//
// Not converging them is deliberate. A component is a counted thing in a rack,
// where "display-1" beside "display-2" is what an operator writes on the label,
// and #681 has already minted names in that shape; flipping the shared mint to
// suppress would rename every generated component that exists. A system is a
// room, and a room with one boardroom in it does not call it "boardroom-1".
//
// Where the choice COMES FROM is the seam #687 needs. It is a field on the mint
// resolved per type, and until a type carries the fact the per-kind default
// below is where that fact lives. #687 gives location_type a name rule and
// fills this same field from it, replacing nothing here.
func componentMint(stem string) nameMint { return nameMint{stem: stem} }

func systemMint(stem string) nameMint { return nameMint{stem: stem, bareFirst: true} }

// pickOrdinal returns the smallest positive integer whose MINTED name is not
// already held by a sibling in the bucket. The loop runs forward from 1 over
// candidates rather than backward from sibling names to the ordinals they
// encode, and that inversion is the whole of #681's allocator change.
//
// The answers are identical to the prefix-scan it replaces, because that scan
// was already computing this: "the smallest n with no sibling named
// <stem>-<n>". Reading it forward buys three things the parse could not give.
// A stem-less type allocates at all (the mint's "1", "2", where a "-" prefix
// matched nothing and returned 1 forever). A name shape that is not
// "<stem>-<n>" needs no second parser written to match it, only a different
// mint, which is exactly what a suppressed first ordinal is (#686): "boardroom"
// and "boardroom-2" are allocated by the same loop, because the loop never
// knows the shape, it only asks for it. And an operator-typed name occupying
// the shape the generator would mint still blocks it, which is not a nicety:
// the scoped-name unique index is on the NAME, so an allocator that consulted
// only the ordinals the platform recorded would hand back a name a hand-typed
// row already holds and turn an ordinary create into a 23505 the transaction
// cannot recover from.
//
// Taking the MINT rather than the stem is what keeps that guarantee true across
// two shapes. If this tested "<stem>-<n>" while the caller minted a suppressed
// "<stem>", the two would disagree on ordinal 1 and every second create in the
// bucket would 23505.
//
// A sibling in a different stem's space blocks nothing, because no candidate
// this mint produces is ever equal to it: "mic-1" and "display-panel-1" are
// both simply not names stem "display" can mint.
//
// It terminates: candidates are distinct per n, so at most len(existing) of
// them can be taken and the answer is never larger than len(existing)+1. Pure
// (no I/O, no clock), so the whole allocation rule stays exhaustively
// unit-testable without a database.
func pickOrdinal(existing []string, m nameMint) int {
	taken := make(map[string]bool, len(existing))
	for _, name := range existing {
		taken[name] = true
	}
	for n := 1; ; n++ {
		if !taken[m.name(n)] {
			return n
		}
	}
}

// nameScope is one placement bucket of one entity kind: the advisory-lock key
// and the sibling filter TOGETHER, in the shape that kind's own scoped-name
// unique indexes enforce (db/migrations/20260808090000_names_scope_to_placement.sql).
//
// One value rather than a (parentID, locationID) pair passed to two functions,
// because the kinds do not agree on how many buckets there are. component and
// system have THREE (a parent, else a location, else unplaced) and location has
// TWO (a parent, else root), since a location has no located-at column to be
// bucketed by. A caller holding two loose pointers can pair a location with a
// three-way key that reads a column its table does not have; a caller holding
// one of these cannot, because the constructor for a location never accepts a
// location id in the first place.
type nameScope struct {
	// table is a compile-time scopeTable constant, never user input, so
	// interpolating it into the sibling read below is injection-safe (the same
	// allow-list scopetree.go's recursive CTEs rely on).
	table scopeTable
	// bucket identifies the bucket within the table, for the lock key.
	bucket string
	// where and arg are the same bucket as a filter: the predicate that selects
	// exactly the rows the unique index groups together, and the one id it
	// binds (nil for the parentless buckets, which bind nothing).
	where string
	arg   *string
}

// placedNameScope is the three-bucket shape component and system share: a
// parent wins over a location, which wins over the unplaced bucket, mirroring
// both tables' identical unique indexes and ComponentNameTaken's own branch
// order.
func placedNameScope(table scopeTable, parentID, locationID *string) nameScope {
	switch {
	case parentID != nil:
		return nameScope{table: table, bucket: "parent/" + *parentID, where: "parent_id = $1", arg: parentID}
	case locationID != nil:
		return nameScope{table: table, bucket: "location/" + *locationID, where: "parent_id is null and location_id = $1", arg: locationID}
	default:
		return nameScope{table: table, bucket: "orphan", where: "parent_id is null and location_id is null"}
	}
}

// treeNameScope is the two-bucket shape a self-referencing tree with no
// located-at column has: under a parent, else at the root. It takes no location
// id, which is the point: location HAS no location_id, so the three-way shape
// is not a mistake to avoid at each call site, it is a value that cannot be
// constructed.
func treeNameScope(table scopeTable, parentID *string) nameScope {
	if parentID != nil {
		return nameScope{table: table, bucket: "parent/" + *parentID, where: "parent_id = $1", arg: parentID}
	}
	return nameScope{table: table, bucket: "root", where: "parent_id is null"}
}

func componentNameScope(parentID, locationID *string) nameScope {
	return placedNameScope(componentTable, parentID, locationID)
}

func systemNameScope(parentID, locationID *string) nameScope {
	return placedNameScope(systemTable, parentID, locationID)
}

func locationNameScope(parentID *string) nameScope {
	return treeNameScope(locationTable, parentID)
}

// lockKey is the advisory-lock key for allocating stem's ordinals in this
// bucket: the table, the stem and the bucket, all three. Two concurrent creates
// under different parents never contend even with the same stem, two under the
// same parent with different stems never contend either, and a system never
// contends with a component that happens to share a parent id.
func (s nameScope) lockKey(stem string) string {
	return string(s.table) + "_name/" + stem + "/" + s.bucket
}

// siblingNames reads every name in this bucket, the exact candidate set
// pickOrdinal tests against. excludeID omits one row (a move or a reclassify
// recomputing its OWN row's name, which already occupies this bucket under its
// old name at read time); nil excludes nothing (a create, whose row does not
// exist yet).
//
// It reads NAMES, not the ordinals #681 stores beside them, and that is
// deliberate (ADR-0097). The constraint allocation has to satisfy is the
// scoped-name unique index, which is on the name, and the rows holding a name
// are not the same set as the rows holding an ordinal: an operator can type
// "display-1" by hand, taking the name while owning no ordinal at all. An
// allocator reading the ordinal column would not see that row and would mint
// its name a second time. What the stored ordinal buys is everything
// DOWNSTREAM of allocation (the bare render, a label rule reading .Ordinal, the
// recompute-and-compare invariant).
func (s nameScope) siblingNames(ctx context.Context, q querier, excludeID *string) ([]string, error) {
	sql := `select name from ` + string(s.table) + ` where ` + s.where
	var (
		rows pgx.Rows
		err  error
	)
	if s.arg != nil {
		rows, err = q.Query(ctx, sql+` and ($2::uuid is null or id <> $2::uuid)`, *s.arg, excludeID)
	} else {
		rows, err = q.Query(ctx, sql+` and ($1::uuid is null or id <> $1::uuid)`, excludeID)
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

// generateName mints a name inside the caller's transaction and returns it
// together with the ORDINAL it was minted from, which the caller stores on the
// row (#681). The number used to be computed here, formatted into the name, and
// dropped on the floor, leaving every downstream reader to recover it by string
// surgery; it is a fact the platform owns, so it is returned as one.
//
// The advisory lock it takes first is transaction-scoped (pg_advisory_xact_lock,
// released at commit or rollback, the same primitive lockMemberComponent
// already uses to serialize a component's default membership) and keyed on the
// table, the stem AND the placement bucket, not any one alone: only two
// allocations that would actually collide on the same name serialize. They do
// so with no retry: a 23505 would abort the whole transaction, and this repo's
// other race of this class (ADR-0075's guarded conditional insert) is a
// different shape (a single conditional write, not an allocate-then-write
// sequence), so a lock taken before the read-then-decide is the only one of the
// two established patterns that fits.
//
// The mint never returns without passing validateEntityName first: an enforced
// postcondition of this primitive, not an assumed one a caller's comment claims
// on its behalf. Every caller (create, :resetName, a move, a reclassify)
// trusts this return value with no re-check of its own, so the guarantee has to
// live here, the one place all of them funnel through, or it is not a guarantee
// at all. It is also what makes a rule-produced name safe to add later: whatever
// shape a mint grows, it is refused here before it can reach a row.
func generateName(ctx context.Context, tx pgx.Tx, m nameMint, sc nameScope, excludeID *string) (string, int, error) {
	if err := lockAdvisory(ctx, tx, sc.lockKey(m.stem)); err != nil {
		return "", 0, err
	}
	existing, err := sc.siblingNames(ctx, tx, excludeID)
	if err != nil {
		return "", 0, err
	}
	ordinal := pickOrdinal(existing, m)
	name := m.name(ordinal)
	if err := validateEntityName(name); err != nil {
		return "", 0, fmt.Errorf("storage: generated name %q is invalid: %w", name, err)
	}
	return name, ordinal, nil
}

// generateComponentName is generateName for the component tier: the component
// mint (no suppression) in the component's own three placement buckets.
func generateComponentName(ctx context.Context, tx pgx.Tx, stem string, parentID, locationID, excludeID *string) (string, int, error) {
	return generateName(ctx, tx, componentMint(stem), componentNameScope(parentID, locationID), excludeID)
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

// generateNameForProduct is the two-step stem-then-ordinal path every component
// generation site (create, :resetName, a move, a product reclassify) shares:
// resolve the product's component_type stem, then mint the ordinal within
// the given placement scope. One function so "generate a name" has exactly
// one implementation, never a per-call-site variant.
func generateNameForProduct(ctx context.Context, tx pgx.Tx, productID string, parentID, locationID, excludeID *string) (string, int, error) {
	typeID, err := componentTypeIDForProduct(ctx, tx, productID)
	if err != nil {
		return "", 0, err
	}
	stem, _, _, _, _, err := resolveTypeFacts(ctx, tx, typeID)
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

// generateNameForSystemType is generateNameForProduct's system-tier twin
// (#686): resolve the system_type chain's stem, then mint the ordinal within
// the system's own placement bucket. The same two steps in the same order, one
// function, so a system's name has exactly one implementation across the four
// sites that (re)generate it (create, :resetName, a move, a reclassify).
//
// It consumes resolveSystemTypeFacts rather than walking system_type itself:
// the inherited-first-non-null rule is already written once (ADR-0095), and a
// second walker here would be a second answer to "what is this type's stem".
func generateNameForSystemType(ctx context.Context, tx pgx.Tx, systemTypeID *string, parentID, locationID, excludeID *string) (string, int, error) {
	if systemTypeID == nil || *systemTypeID == "" {
		return "", 0, ErrSystemTypeRequiredForName
	}
	typeID, err := uuid.Parse(*systemTypeID)
	if err != nil {
		return "", 0, fmt.Errorf("storage: system_type id %q is not a uuid: %w", *systemTypeID, err)
	}
	stem, _, _, _, err := resolveSystemTypeFacts(ctx, tx, typeID)
	if err != nil {
		return "", 0, fmt.Errorf("storage: resolve system_type facts for %q: %w", typeID, err)
	}
	if stem == "" {
		return "", 0, fmt.Errorf("%w: system_type %s", ErrSystemTypeNoStem, typeID)
	}
	return generateName(ctx, tx, systemMint(stem), systemNameScope(parentID, locationID), excludeID)
}
