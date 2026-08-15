package storage

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/hyperscaleav/omniglass/internal/health"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Health, resolved and recorded. This file is the I/O half of the rollup: it
// resolves the inputs the pure verdict function needs (internal/health), then
// records the answer as a TRANSITION.
//
// Two rules carry the whole design.
//
// Transition-only recording. Health lands in property, the same
// transition-only substrate the reachability strip reads: a row is written only
// when the value differs from the last one recorded for that owner. The history
// is therefore edges and only edges, which is what makes "when did this break"
// answerable weeks later. Writing a row per recompute would bury the edges in
// samples and answer a different question.
//
// Recompute at the write, inside the caller's transaction. Every mutation that
// can change health recomputes the affected chain before it commits: raising or
// clearing an alarm, staffing or unstaffing a role, declaring or withdrawing one,
// creating a system, and changing the standard it conforms to or the location it
// sits in. A component's own condition is now purely its active alarms (#626),
// so a product swap or a capability fact no longer belongs to that list. The
// alternative, RECORDING on read, would stamp every transition at the moment
// somebody opened a page, which is precisely the inaccuracy the record exists to
// avoid.
//
// A missing trigger is therefore a hole in the history, and it used to be a hole
// in the answer too: the reads served the last recorded value, so an entity no
// write had touched yet read healthy no matter what its roles said. The reads now
// COMPUTE the verdict they serve from the same rows they show (see SystemHealth
// and LocationHealth) without recording anything, so an incomplete trigger set can
// cost an edge in the history but can never make a report contradict itself.
//
// One owner at a time. Both rules above are compare-then-act: read what the roles
// say, compare it with the last recorded value, write only on a difference. Two
// transactions doing that at once for the same owner each read a state the other
// was about to change, so both could conclude they were recording an edge (two
// consecutive identical rows, which is not an edge) or neither could (a real
// transition, silently missing). Two alarms in one room is an ordinary minute in
// an fleet, so this is not a corner. Every recompute therefore takes a
// transaction-scoped advisory lock on the owner BEFORE it resolves that owner's
// inputs, and holds it to commit: the whole resolve-compare-write sequence is
// serialized per owner, and the loser recomputes over the winner's committed
// state instead of over a snapshot that predates it. See lockHealthOwner.

// healthKey is the property sample key carrying a rolled-up verdict. There is
// one series per owner and no instance dimension: an entity has exactly one
// health.
const healthKey = "health"

// healthRule names the producer in the recorded row's lineage. provenance
// 'calculated' requires a non-null source_rule (and a null event_id), so
// this constant is what satisfies property's lineage CHECK.
const healthRule = "health-rollup"

// txQuerier is the surface the recompute needs from its caller's transaction.
// Both pgx.Tx and the pool satisfy it, so the recompute runs inside the write
// that triggered it and, in a read path, straight on the pool.
type txQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// HealthReport answers "what is this entity's health, and why". Verdict is
// derived from the very evidence the report carries (the roles for a system, the
// systems for a location), so the two halves can never contradict each other: a
// report that says healthy beside an impaired outage role is worse than no report.
// Deriving it is a pure computation over rows the read already loaded, so it
// writes nothing and cannot invent a transition. Transitions stay the recorded
// edges, which is a different question ("when did this change") and the one the
// record exists to answer.
type HealthReport struct {
	OwnerKind   string
	OwnerID     string
	Verdict     string
	Roles       []HealthRole   // system reports: the roles that contributed
	Systems     []HealthSystem // location reports: the systems beneath it
	Transitions []HealthTransition
}

// HealthRole is one contributing role, with the causing chain when it is
// impaired. Down and Alarms are what turn a verdict into something an
// operator can act on: which assigned components are not occupying their
// slot, and the alarms that took them down. A satisfied role carries
// neither, and a role that is merely short-staffed (nobody down, just nobody
// assigned) carries Down empty too.
//
// Choice, Alternate and Active are the #626 grouping, carried onto the
// report so a consumer can group what the verdict already groups: Choice
// and Alternate are empty for an unconditional role (it always counts), and
// Active is true for every unconditional role but only for the roles of a
// choice's currently-winning alternate (internal/health.Choice.Active).
// Without this, the verdict and the role list can read as contradicting
// each other: a satisfied all-in-one alternate reads healthy while the
// same report lists its unbuilt component-system alternate's roles as
// impaired, with nothing to say those roles are not the reason.
type HealthRole struct {
	Name       string
	Label      string
	Impact     string
	Quorum     int
	Satisfying int
	Short      int // how many more occupants the role needs to reach quorum
	Spare      int // how many occupants the role has beyond quorum
	Impaired   bool
	AssignedTo []string
	Down       []string // assigned components whose own verdict is outage
	Alarms     []Alarm  // the active alarms on those down components
	Choice     string   // the choice this role belongs to; empty when unconditional
	Alternate  string   // the alternate within Choice; empty when unconditional
	Active     bool     // true for an unconditional role, or a role in its choice's active alternate
}

// HealthSystem is one system under a location, with its recorded verdict. It is
// the location report's drill-down: the system health read explains the rest.
type HealthSystem struct {
	Name    string
	Verdict string
}

// HealthTransition is one recorded edge: the moment the verdict changed and what
// it changed to.
type HealthTransition struct {
	TS    time.Time
	Value string
}

// resolvedRole is a system's role with everything both the verdict and the report
// need: the declaration and who fills it, each with its own current verdict.
type resolvedRole struct {
	ID       string
	Name     string
	Label    string
	Quorum   int
	Impact   string
	Assigned []health.Component
	// AssignedIDs is Assigned's own component id, index for index: the pure
	// health.Component the rollup and the report both consume carries only a
	// Name (display), so a caller that needs to look a specific assignee back
	// up unambiguously (explainRole's active-alarm read) takes the id from
	// here rather than the name Assigned carries, which #627 no longer
	// guarantees is unique.
	AssignedIDs []string
	// ChoiceID, ChoiceName, AlternateID, AlternateName and AlternatePos are
	// set when this role joins one alternate of a choice (#626); nil means
	// unconditional (system_role.alternate_id is NULL), always folded
	// directly rather than grouped. splitHealthRoles is what turns these
	// back into health.SystemVerdictWith's shape.
	ChoiceID      *string
	ChoiceName    *string
	AlternateID   *string
	AlternateName *string
	AlternatePos  *int
}

// RecomputeHealth recomputes and records the health chain a component sits in:
// the component itself, every system it staffs, and every location over those
// systems. It runs inside the caller's transaction, so the verdict commits with
// the write that caused it or not at all. componentRef is a reference (name or
// uuid, ADR-0062): resolved once here, before it ever reaches recomputeChain, so
// nothing downstream re-derives the id from a name that might no longer be
// unique (#627).
func (p *PG) RecomputeHealth(ctx context.Context, q txQuerier, componentRef string) error {
	c, err := scopedByName(ctx, q, componentConfig, componentRef)
	if err != nil {
		return err
	}
	return p.recomputeChain(ctx, q, []ownerRef{{ID: c.ID, Name: c.Name}}, nil, nil)
}

// ownerRef pairs a health owner's id with its name. Every internal query in
// this file binds the id, including recordHealth and healthTransitions:
// ownerArcExprN's component/system/location branch binds directly ($N::uuid,
// fixed to do so in the round that closed the collection ingest path), so
// nothing downstream re-derives an id from a name that #627 no longer
// guarantees is unique. Name is not currently read anywhere; it survives on
// the type because every construction site already has the row in hand
// (resolving the id pulls the name along for free) except one that used to
// pay for a lookup solely to populate it (systemConfig.afterDelete, since
// fixed to leave it unset). Kept rather than dropped so a future health-report
// read that wants a label without a second lookup has it; if that
// stays untrue, cut the field.
type ownerRef struct {
	ID   string
	Name string
}

// recomputeChain is the shape every trigger reduces to: some components whose own
// verdict may have moved, plus some systems and some locations named explicitly.
// The explicit lists matter for the writes that REMOVE a link (unassigning a
// component, withdrawing a role, moving a system out of a location), where walking
// the current rows would no longer find the entity that just changed.
//
// Every owner is locked before its inputs are resolved, and the lock is held to
// commit, so a concurrent recompute of the same owner resolves over this one's
// committed result rather than over the state it is replacing. Owners are visited
// in a fixed order (components, then systems, then locations, each by id), which
// is what keeps two recomputes over overlapping chains from deadlocking on each
// other's locks.
func (p *PG) recomputeChain(ctx context.Context, q txQuerier, components, systems, locations []ownerRef) error {
	affected := newRefSet(systems)

	for _, c := range newRefSet(components).sorted() {
		if err := lockHealthOwner(ctx, q, "component", c.ID); err != nil {
			return err
		}
		severities, err := p.activeAlarmSeverities(ctx, q, c.ID)
		if err != nil {
			return err
		}
		if err := recordHealth(ctx, q, "component", c.ID, health.ComponentVerdict(severities)); err != nil {
			return err
		}
		staffed, err := p.systemsStaffedBy(ctx, q, c.ID)
		if err != nil {
			return err
		}
		affected.add(staffed...)
	}

	systemRefs := affected.sorted()
	for _, s := range systemRefs {
		if err := lockHealthOwner(ctx, q, "system", s.ID); err != nil {
			return err
		}
		roles, err := p.resolveHealthRoles(ctx, q, s.ID)
		if err != nil {
			return err
		}
		unconditional, choices, _ := splitHealthRoles(roles)
		if err := recordHealth(ctx, q, "system", s.ID, health.SystemVerdictWith(unconditional, choices)); err != nil {
			return err
		}
	}

	// A location's verdict reads the systems' RECORDED values, which the loop
	// above has just refreshed inside this transaction.
	affectedLocations, err := p.locationsOver(ctx, q, systemRefs, locations)
	if err != nil {
		return err
	}
	for _, l := range affectedLocations {
		if err := lockHealthOwner(ctx, q, "location", l.ID); err != nil {
			return err
		}
		v, err := p.locationVerdict(ctx, q, l.ID)
		if err != nil {
			return err
		}
		if err := recordHealth(ctx, q, "location", l.ID, v); err != nil {
			return err
		}
	}
	return nil
}

// lockHealthOwner takes the owner's health lock for the rest of the caller's
// transaction. It is what makes "resolve the inputs, compare with the last
// recorded value, write on a difference" atomic per owner: a second transaction
// recomputing the same owner waits here, and its statements then read the
// winner's committed rows rather than a snapshot that predates them.
//
// The lock is an advisory one keyed on a hash of the owner, not a row lock,
// because the thing being serialized is a computation over many tables rather
// than one row. Owners are locked in a single global order (components, then
// systems, then locations, each by ID), which is what keeps two recomputes over
// overlapping chains from deadlocking. Stated identically in recomputeChain and
// implemented identically in refSet.sorted() and locationsOver's `order by id`:
// one fact in three places rather than three claims (#670). An id is the only
// key that can carry it, because it is the only one guaranteed unique, and a
// tied comparison is not an order. A hash collision costs two unrelated owners a
// wait and nothing else.
//
// It is transaction-scoped, so it releases on commit or rollback with no
// unlocking to forget. The recompute always runs inside the caller's transaction
// (that is the point of taking txQuerier), so there is always a transaction to
// scope it to.
func lockHealthOwner(ctx context.Context, q txQuerier, ownerKind, ownerID string) error {
	if err := lockAdvisory(ctx, q, healthLockKey(ownerKind, ownerID)); err != nil {
		return fmt.Errorf("storage: lock health %s/%s: %w", ownerKind, ownerID, err)
	}
	return nil
}

// healthLockKey is the string one owner's health lock hashes: `health/<kind>/<id>`.
//
// It is a function rather than an expression inline above because the CURRENCY
// is the invariant, and it is one a test can hold (#717). The key was keyed on
// the NAME until the identity epic (#627) moved it, and a name-keyed lock stopped
// being safe at exactly the moment that epic scoped a location's name uniqueness
// to its placement: two rooms under different buildings may both be `415a`, so
// one key would serialize two unrelated owners, and a mixed currency (a name on
// one path, an id on another) would hash two keys for one owner and silently stop
// serializing the compare-then-act sequence that needs it.
func healthLockKey(ownerKind, ownerID string) string {
	return healthKey + "/" + ownerKind + "/" + ownerID
}

// lockAdvisory serializes a named critical section for the rest of the caller's
// transaction. It is the answer to compare-then-act: any sequence that reads
// state, decides from it, and writes, is wrong under READ COMMITTED unless the
// whole sequence is serialized, because a second transaction takes its snapshot
// before the first commits and both decide from a state neither will end in.
//
// Transaction-scoped, so it releases on commit or rollback with no unlocking to
// forget. The key is hashed to the bigint the advisory-lock functions take; a
// collision costs two unrelated keys a wait and nothing else.
func lockAdvisory(ctx context.Context, q txQuerier, key string) error {
	if _, err := q.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
		return fmt.Errorf("storage: advisory lock %q: %w", key, err)
	}
	return nil
}

// resolveSystemRefs resolves each system reference (name or uuid, ADR-0062) to
// its id and canonical name once, so recomputeChain never re-derives an id
// from a name that might no longer be unique (#627).
func (p *PG) resolveSystemRefs(ctx context.Context, q txQuerier, refs []string) ([]ownerRef, error) {
	out := make([]ownerRef, 0, len(refs))
	for _, ref := range refs {
		sys, err := scopedByName(ctx, q, systemConfig, ref)
		if err != nil {
			return nil, err
		}
		out = append(out, ownerRef{ID: sys.ID, Name: sys.Name})
	}
	return out, nil
}

// resolveLocationRefs mirrors resolveSystemRefs for locations.
func (p *PG) resolveLocationRefs(ctx context.Context, q txQuerier, refs []string) ([]ownerRef, error) {
	out := make([]ownerRef, 0, len(refs))
	for _, ref := range refs {
		loc, err := scopedByName(ctx, q, locationConfig, ref)
		if err != nil {
			return nil, err
		}
		out = append(out, ownerRef{ID: loc.ID, Name: loc.Name})
	}
	return out, nil
}

// recomputeSystems is the trigger shape for a declaration change, which moves a
// system's roles without touching any component.
func (p *PG) recomputeSystems(ctx context.Context, q txQuerier, systems ...string) error {
	refs, err := p.resolveSystemRefs(ctx, q, systems)
	if err != nil {
		return err
	}
	return p.recomputeChain(ctx, q, nil, refs, nil)
}

// recomputeMovedSystem is the trigger shape for a system that changed location:
// the location it arrived at is reachable from its row, but the one it LEFT is
// not, so that one is named. The old location may have improved (its worst system
// just walked out), which is an edge as real as any failure.
func (p *PG) recomputeMovedSystem(ctx context.Context, q txQuerier, system string, leftLocations ...string) error {
	sys, err := scopedByName(ctx, q, systemConfig, system)
	if err != nil {
		return err
	}
	left, err := p.resolveLocationRefs(ctx, q, leftLocations)
	if err != nil {
		return err
	}
	return p.recomputeChain(ctx, q, nil, []ownerRef{{ID: sys.ID, Name: sys.Name}}, left)
}

// recomputeMovedLocation is the trigger shape for a location that changed
// parent (#642). A location's rollup folds every system in its own subtree, so
// a move carries that whole contribution from one ancestor chain to another and
// BOTH chains move: the one it joined is reachable from its row, the one it
// LEFT is not, so that parent is named, exactly as recomputeMovedSystem names
// the location a system walked out of.
//
// One row per side is the whole input, not a walk. locationsOver takes its
// named locations as the SEED of a recursive ancestry CTE, so naming the moved
// location covers its new parent and every ancestor above it, and naming the
// old parent covers the old chain the same way. Walking either chain in Go
// first would reimplement, in a second place, the walk the query already
// performs. Resolving the old chain AFTER the write is equally safe: the only
// parent edge this write touches is the moved row's own, so the old parent's
// own ancestry reads the same before and after.
//
// The moved location itself is included and costs nothing. Its own verdict
// cannot move (the subtree it folds travelled with it), and recordHealth writes
// nothing when the value has not changed, so it contributes the new chain's
// seed without adding an edge of its own.
func (p *PG) recomputeMovedLocation(ctx context.Context, q txQuerier, moved ownerRef, leftParentID *string) error {
	named := []ownerRef{moved}
	if leftParentID != nil {
		// The id is already in hand from the pre-write image; no lookup, and
		// no name needed (locations resolve their own names inside
		// locationsOver).
		named = append(named, ownerRef{ID: *leftParentID})
	}
	return p.recomputeChain(ctx, q, nil, nil, named)
}

// recordHealth is the transition-only write, and the reason this slice adds no
// history table of its own. It writes NOTHING when the computed verdict already
// matches the last one recorded for this owner.
//
// The comparison is INSIDE the insert rather than a read the caller acts on. One
// statement cannot record a value it did not just compare against, so no future
// trigger, and no reordering of this one, can reintroduce a gap between deciding
// and writing. That is only half the guarantee: the lock below is what stops a
// concurrent transaction from deciding the same thing at the same time. Taking it
// here as well as in the recompute is deliberate. A trigger that reaches straight
// for recordHealth still cannot record a duplicate, and re-taking a lock the
// caller already holds costs nothing.
//
// ts is the moment of the write (clock_timestamp), not the transaction's start
// (now()). Two rows written in one transaction would otherwise share a timestamp,
// and a slow transaction would stamp its edge before edges that were recorded
// while it ran, which is exactly backwards for a record whose whole job is saying
// WHEN something changed. With that, ts and the identity id agree on the order,
// and the reads below take the id: it is the true write sequence.
//
// The first value for an owner is always recorded, even Healthy. An owner whose
// history starts at its first health-relevant write has a defined beginning; the
// alternative (recording only once something goes wrong) leaves a reader unable
// to tell "healthy since we started watching" from "never evaluated".
// ownerID is an id for a component/system/location owner (recomputeChain
// always has one, via ownerRef; SystemHealth and LocationHealth resolve their
// own before calling healthTransitions below), a name for a node (unchanged;
// node names stay globally unique). See ownerArcExprN.
func recordHealth(ctx context.Context, q txQuerier, ownerKind, ownerID string, v health.Verdict) error {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return fmt.Errorf("storage: record health %s/%s: %w", ownerKind, ownerID, err)
	}
	if err := lockHealthOwner(ctx, q, ownerKind, ownerID); err != nil {
		return err
	}
	// provenance 'calculated' pins the lineage: source_rule names the producer,
	// event_id stays null. The CHECK enforces exactly that shape.
	// The WHERE is the transition rule: no previous row (is distinct from null) or
	// a different one writes; the same value writes nothing.
	owner := ownerArcExpr(ownerKind)
	sql := fmt.Sprintf(`insert into property (ts, owner_kind, %[1]s, property_type_id, instance, value, provenance, source_rule)
		select clock_timestamp(), $1::text, %[3]s, (select id from property_type where name = $3::text), '', to_jsonb($4::text), 'calculated', $5::text
		where $4::text is distinct from (
			select value #>> '{}' from property
			where %[1]s = %[3]s and property_type_id = (select id from property_type where name = $3::text) and instance = ''
			order by id desc
			limit 1)`, col, col, owner)
	if _, err := q.Exec(ctx, sql, ownerKind, ownerID, healthKey, v.String(), healthRule); err != nil {
		return fmt.Errorf("storage: record health %s/%s: %w", ownerKind, ownerID, err)
	}
	return nil
}

// ownerArcExpr is the SQL for the value a health owner column stores, given the
// reference the recompute passes around. It is the shared, owner-kind-generic
// primitive: current_values.go, property_samples.go, and settlement.go build
// their own SQL from it too, so ownerID here follows the same contract theirs
// does. For a component, system, or location it binds an id directly ($N::uuid,
// no lookup): #627 scopes name uniqueness to placement, so a name-based
// subquery here would raise SQLSTATE 21000 the moment two rows share a name.
// Every caller across all four files resolves the reference to an id once,
// before it ever reaches this expression (recomputeChain's ownerRef in this
// file; ownerArcValue in the others), so ownerID is always already an id for
// these three kinds, never a bare name. A node stays name-resolved: node names
// remain globally unique (they ride NATS subjects), so there is nothing to
// convert.
func ownerArcExpr(ownerKind string) string { return ownerArcExprN(ownerKind, 2) }

// ownerArcExprN is the same expression at an arbitrary parameter position, since
// the reads and the write do not agree on where the owner sits.
func ownerArcExprN(ownerKind string, n int) string {
	switch ownerKind {
	case "component", "system", "location":
		return fmt.Sprintf("$%d::uuid", n)
	case "node":
		return fmt.Sprintf("(select principal_id from node where name = $%d::text)", n)
	}
	return fmt.Sprintf("$%d::text", n)
}

// activeAlarmSeverities lists the severities of a component's active alarms, the
// only input its own verdict takes. Takes the component's id directly (every
// caller already has it, recomputeChain's ownerRef included): binding it
// avoids re-deriving an id from a name that #627 no longer guarantees is
// unique.
func (p *PG) activeAlarmSeverities(ctx context.Context, q txQuerier, componentID string) ([]string, error) {
	var severities []string
	if err := q.QueryRow(ctx, `
		select coalesce(array_agg(severity), '{}')
		from alarm where component_id = $1::uuid and cleared_at is null`,
		componentID).Scan(&severities); err != nil {
		return nil, fmt.Errorf("storage: active alarm severities %q: %w", componentID, err)
	}
	return severities, nil
}

// componentVerdict resolves one component's own current verdict from its
// active alarms: not the recorded row (a read must not trust a trigger it did
// not just run), but the same live computation the recompute makes. It is the
// only input a slot it occupies takes now: an alarm impairs a component
// wholesale, not by name capability, so there is nothing else to resolve.
// Takes the component's id (see activeAlarmSeverities).
func (p *PG) componentVerdict(ctx context.Context, q txQuerier, componentID string) (health.Verdict, error) {
	severities, err := p.activeAlarmSeverities(ctx, q, componentID)
	if err != nil {
		return health.Healthy, err
	}
	return health.ComponentVerdict(severities), nil
}

// systemsStaffedBy lists the systems this component fills a role in, the systems
// whose verdict its condition can move, each as an ownerRef: the query already
// joins by id (ra.component_id = ra.system_id -> s.id), so returning the id
// alongside the name costs nothing and is what lets recomputeChain's caller
// avoid a second, ambiguity-prone name lookup for every system it just found.
func (p *PG) systemsStaffedBy(ctx context.Context, q txQuerier, componentID string) ([]ownerRef, error) {
	rows, err := q.Query(ctx, `
		select distinct s.id, s.name from system_role_assignment ra join system s on s.id = ra.system_id
		where ra.component_id = $1::uuid order by 2`,
		componentID)
	if err != nil {
		return nil, fmt.Errorf("storage: systems staffed by %q: %w", componentID, err)
	}
	defer rows.Close()
	return scanOwnerRefs(rows, "systems staffed by")
}

// conformingSystems lists the systems a standard's declaration reaches, each as
// an ownerRef. Declaring a role on a standard moves every conforming system at
// once, which is the arc a per-system recompute would miss. id is selected
// alongside name (both are right there in the same row, keyed on
// standard_id, an unambiguous FK): the caller hands these straight to
// recomputeChain, which needs the id to lock and record each system without
// re-deriving one from a name #627 no longer guarantees is unique.
func (p *PG) conformingSystems(ctx context.Context, q txQuerier, standardID string) ([]ownerRef, error) {
	rows, err := q.Query(ctx, `select id, name from system where standard_id = (select id from standard where name = $1 or id::text = $1) order by name`, standardID)
	if err != nil {
		return nil, fmt.Errorf("storage: conforming systems %q: %w", standardID, err)
	}
	defer rows.Close()
	return scanOwnerRefs(rows, "conforming systems")
}

// systemsForRoleOwner resolves the systems one role declaration reaches, each
// as an ownerRef: the single system for an ad-hoc declaration (resolved once,
// ambiguity-safe), every conforming system for a standard's, since those
// inherit it live.
func (p *PG) systemsForRoleOwner(ctx context.Context, q txQuerier, ownerKind, ownerID string) ([]ownerRef, error) {
	if ownerKind == "standard" {
		return p.conformingSystems(ctx, q, ownerID)
	}
	sys, err := scopedByName(ctx, q, systemConfig, ownerID)
	if err != nil {
		return nil, err
	}
	return []ownerRef{{ID: sys.ID, Name: sys.Name}}, nil
}

// locationsOver returns the locations holding these systems, the locations named
// explicitly, and every ancestor above either: the full set whose rollup the
// change can move. The explicit arm carries the location a system has just LEFT,
// which its row no longer points at. Both inputs and the result are ownerRefs,
// resolved by id (systems.location_id, location.parent_id are both ids
// already), never by re-joining on a name.
//
// Ordered by ID, which is the recompute's lock order (see recomputeChain) and
// the same key refSet.sorted() gives the component and system tiers. It used to
// order by name, which was the same order back when a location name was unique
// fleet-wide and is not an order at all now that #627 scoped uniqueness to
// placement: two rooms under different buildings can both be 415a, and a tied
// comparison leaves their relative order to the plan. That is not academic. The
// two arms of the union below feed the sort in different orders, so the two
// production trigger shapes (recomputeMovedLocation naming both rooms,
// recomputeMovedSystem reaching one through a system and naming the other)
// really did visit the same pair in opposite orders, which is the deadlock
// precondition the per-owner advisory locks depend on being impossible.
func (p *PG) locationsOver(ctx context.Context, q txQuerier, systems, named []ownerRef) ([]ownerRef, error) {
	if len(systems) == 0 && len(named) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `
		with recursive placed as (
			select l.id, l.name, l.parent_id
			from system s join location l on l.id = s.location_id
			where s.id = any($1::uuid[])
			union
			select l.id, l.name, l.parent_id
			from location l where l.id = any($2::uuid[])
		),
		ancestry as (
			select id, name, parent_id from placed
			union
			select p.id, p.name, p.parent_id
			from location p join ancestry a on a.parent_id = p.id
		)
		select distinct id, name from ancestry order by id`, refIDs(systems), refIDs(named))
	if err != nil {
		return nil, fmt.Errorf("storage: locations over systems: %w", err)
	}
	defer rows.Close()
	return scanOwnerRefs(rows, "locations over systems")
}

// locationVerdict rolls up a location from the RECORDED health of every system
// placed anywhere in its subtree.
//
// Folding the subtree's systems directly, rather than the child locations'
// verdicts, is what makes the recompute order-independent: the recursive
// definition and this one agree (a location's only inputs are systems, however
// deep), but this one never depends on a child having been recomputed first.
// Takes the location's id directly (see activeAlarmSeverities).
func (p *PG) locationVerdict(ctx context.Context, q txQuerier, locationID string) (health.Verdict, error) {
	rows, err := q.Query(ctx, `
		with recursive subtree as (
			select id from location where id = $1::uuid
			union
			select c.id from location c join subtree s on c.parent_id = s.id
		)
		select distinct on (sd.system_id) sd.value #>> '{}'
		from property sd
		where sd.property_type_id = (select id from property_type where name = $2)
		  and sd.system_id in (select id from system where location_id in (select id from subtree))
		order by sd.system_id, sd.id desc`, locationID, healthKey)
	if err != nil {
		return health.Healthy, fmt.Errorf("storage: location verdict %q: %w", locationID, err)
	}
	defer rows.Close()

	var children []health.Verdict
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return health.Healthy, fmt.Errorf("storage: scan location verdict %q: %w", locationID, err)
		}
		children = append(children, health.ParseVerdict(value))
	}
	if err := rows.Err(); err != nil {
		return health.Healthy, fmt.Errorf("storage: iterate location verdict %q: %w", locationID, err)
	}
	return health.RollUp(children), nil
}

// SystemVerdict is one system's recorded health verdict, the single fact a list
// row's badge renders. Deliberately not a HealthReport: the report resolves every
// role, its occupants, their alarms and thirty days of transitions, which is the
// right read for one system's panel and the wrong one to pay N times to colour a
// column (#653).
type SystemVerdict struct {
	SystemID string
	Verdict  string
}

// SystemVerdicts is the BULK health read: every system in the caller's read
// scope with its current verdict, in one statement whatever the fleet size.
//
// It exists because the console had no bulk read and paid a full health
// resolution per row to paint one column (#653). Its scope behaviour is
// ListSystems' behaviour, deliberately and by construction: the in-scope id set
// is scopedListSQL over the same table with the same binds scopedList passes, so
// a caller sees a verdict for exactly the systems it sees rows for, and an empty
// scope is an empty answer rather than a refusal.
//
// The shape is locationVerdict's, one `distinct on` pass over the series rather
// than a correlated latest-row subquery per system, so an fleet of fifteen
// thousand systems reads the property series once and not once per row.
//
// A system with nothing recorded is reported HEALTHY rather than omitted, and
// that default is not a guess: recordHealth only writes on a TRANSITION, so a
// system that has never been recomputed has no row, and SystemVerdictWith over no
// impaired active role is Healthy. TestSystemVerdictsAgreeWithTheReport holds
// this read to SystemHealth's own verdict, defaulted rows included, so the two
// cannot drift.
func (p *PG) SystemVerdicts(ctx context.Context, read scope.Set) ([]SystemVerdict, error) {
	if read.Empty() {
		return nil, nil
	}
	args := []any{}
	key := "$1"
	if !read.All {
		roots := uuidRoots(read.IDs)
		selfIDs := uuidRoots(read.SelfIDs)
		if len(roots) == 0 && len(selfIDs) == 0 {
			return nil, nil // every scope root is malformed: nothing is in scope
		}
		args = append(args, roots, selfIDs)
		key = "$3"
	}
	args = append(args, healthKey)
	rows, err := p.pool.Query(ctx, `
		with sys as (`+scopedListSQL(systemConfig.table, "id", read.All)+`),
		latest as (
			select distinct on (v.system_id) v.system_id, v.value #>> '{}' as verdict
			from property v
			where v.property_type_id = (select id from property_type where name = `+key+`)
			  and v.system_id in (select id from sys)
			order by v.system_id, v.id desc
		)
		select s.id, coalesce(l.verdict, '`+health.Healthy.String()+`')
		from sys s left join latest l on l.system_id = s.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: system verdicts: %w", err)
	}
	defer rows.Close()
	out := []SystemVerdict{}
	for rows.Next() {
		var v SystemVerdict
		if err := rows.Scan(&v.SystemID, &v.Verdict); err != nil {
			return nil, fmt.Errorf("storage: scan system verdict: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// resolveHealthRoles resolves a system's roles from both arcs (inherited from its
// standard, declared on the system) with each assigned component's own current
// verdict. It is the resolution EffectiveRoles does, carried far enough for the
// verdict: the quorum, the impact, and who occupies the slot today. Takes the
// system's id directly (see activeAlarmSeverities).
func (p *PG) resolveHealthRoles(ctx context.Context, q txQuerier, systemID string) ([]resolvedRole, error) {
	// A correlated subquery for assigned, not a GROUP BY: array_agg(distinct
	// ... order by ra.position) is rejected by Postgres outright (the ORDER
	// BY expression must appear in the DISTINCT argument list), and this
	// query joins nothing else that would fan assignment rows out, so the
	// subquery form EffectiveRoles needs for its extra joins works here too,
	// ordered by the occupant's position (#626) rather than alphabetically.
	rows, err := q.Query(ctx, `
		with sys as (
			select id, name, standard_id from system where id = $1::uuid
		),
		roles as (
			select r.id, r.name, coalesce(r.label, '') as label, r.quorum, r.impact, r.alternate_id
			from sys join system_role r on r.owner_kind = 'standard' and r.standard_id = sys.standard_id
			union all
			select r.id, r.name, coalesce(r.label, '') as label, r.quorum, r.impact, r.alternate_id
			from sys join system_role r on r.owner_kind = 'system' and r.system_id = sys.id
		)
		select roles.id, roles.name, roles.label, roles.quorum, roles.impact,
		       -- names for display, ids to look an assignee's own verdict up
		       -- unambiguously (#627): both come off the same
		       -- ra.component_id = ac.id join, so carrying the id costs
		       -- nothing extra here and saves every caller from re-resolving
		       -- one from a name that might no longer be unique.
		       coalesce((select array_agg(ac.name order by ra.position)
		                   from system_role_assignment ra join component ac on ac.id = ra.component_id
		                  where ra.role_id = roles.id and ra.system_id = sys.id), '{}'),
		       coalesce((select array_agg(ac.id order by ra.position)
		                   from system_role_assignment ra join component ac on ac.id = ra.component_id
		                  where ra.role_id = roles.id and ra.system_id = sys.id), '{}'),
		       roles.alternate_id, ca.name, ca.position, rc.id, rc.name
		-- sys first, then roles: a bare comma before a JOIN...ON only puts the
		-- item immediately to its right in scope for that ON clause, so
		-- "roles, sys left join choice_alternate" would leave "roles"
		-- invisible to ca's ON clause. roles must be adjacent to the joins
		-- that reference it; sys is only needed by the correlated subquery
		-- above, which (unlike an ON clause) can reach any FROM-list item.
		from sys, roles
		left join choice_alternate ca on ca.id = roles.alternate_id
		left join role_choice rc on rc.id = ca.choice_id
		order by roles.name`, systemID)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve health roles %q: %w", systemID, err)
	}

	type rawRole struct {
		resolvedRole
		assignedTo  []string
		assignedIDs []string
	}
	var raw []rawRole
	for rows.Next() {
		var r rawRole
		if err := rows.Scan(&r.ID, &r.Name, &r.Label, &r.Quorum, &r.Impact,
			&r.assignedTo, &r.assignedIDs, &r.AlternateID, &r.AlternateName, &r.AlternatePos, &r.ChoiceID, &r.ChoiceName); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: scan health role %q: %w", systemID, err)
		}
		raw = append(raw, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("storage: iterate health roles %q: %w", systemID, err)
	}
	rows.Close()

	// A component fills at most one role per system (#626), so an id can no
	// longer repeat across this system's role list: the cache below costs
	// nothing to keep even so, since a miss is just one extra query, not a
	// wrong answer. Keyed by id, not name: two components could share a name
	// once #627 lands, and a name-keyed cache would hand one assignee's
	// verdict to the other.
	resolved := map[string]health.Component{}
	out := make([]resolvedRole, 0, len(raw))
	for _, r := range raw {
		role := r.resolvedRole
		role.Assigned = make([]health.Component, 0, len(r.assignedTo))
		role.AssignedIDs = append([]string(nil), r.assignedIDs...)
		for i, name := range r.assignedTo {
			id := r.assignedIDs[i]
			c, ok := resolved[id]
			if !ok {
				v, err := p.componentVerdict(ctx, q, id)
				if err != nil {
					return nil, err
				}
				c = health.Component{Name: name, Verdict: v}
				resolved[id] = c
			}
			role.Assigned = append(role.Assigned, c)
		}
		out = append(out, role)
	}
	return out, nil
}

// splitHealthRoles partitions a system's resolved roles into the two shapes
// health.SystemVerdictWith takes (#626): the roles that always count
// (system_role.alternate_id is NULL) and the roles grouped by the choice and
// alternate they belong to, each alternate's roles ordered by
// choice_alternate.position (internal/health.Choice.Active's tie-break).
// Flattening every role through a single fold, the way the pre-#626
// healthRoles did, is not merely incomplete once a choice exists: an
// all-in-one room's satisfied video-bar alternate would still take the
// system to outage over its component-system alternate's five unstaffed
// roles, because nothing would tell the fold those five are optional.
//
// Building this from a map rather than relying on the query's ORDER BY
// keeping same-choice rows contiguous is deliberate: the query orders by
// roles.name (for the unrelated per-role report this same read serves), not
// by choice or position, so grouping has to happen here regardless of scan
// order. Choices are then sorted by name and each choice's alternates by
// position for a deterministic, map-iteration-free result.
//
// byID is choices again, keyed by choice_id rather than indexed by name:
// health.Choice carries only a Name (the pure package has no notion of a
// storage id), but two DISTINCT choices can share one, a system inheriting
// a standard-owned "conferencing" while also declaring its own ad-hoc
// "conferencing" (role_choice_name_key is scoped to one owner arc, not
// globally). A caller building a name-keyed lookup from choices alone would
// silently collapse the two; byID is what lets explainRole (SystemHealth)
// resolve a role's active status by the same id resolvedRole already
// carries instead.
func splitHealthRoles(rs []resolvedRole) (unconditional []health.Role, choices []health.Choice, byID map[string]health.Choice) {
	type altGroup struct {
		name  string
		pos   int
		roles []health.Role
	}
	choiceNames := map[string]string{}              // choice id -> name
	choiceAlts := map[string]map[string]*altGroup{} // choice id -> alternate id -> group

	for _, r := range rs {
		role := health.Role{Name: r.Name, Quorum: r.Quorum, Impact: r.Impact, Assigned: r.Assigned}
		if r.ChoiceID == nil {
			unconditional = append(unconditional, role)
			continue
		}
		cid, aid := *r.ChoiceID, *r.AlternateID
		choiceNames[cid] = *r.ChoiceName
		if choiceAlts[cid] == nil {
			choiceAlts[cid] = map[string]*altGroup{}
		}
		g := choiceAlts[cid][aid]
		if g == nil {
			pos := 0
			if r.AlternatePos != nil {
				pos = *r.AlternatePos
			}
			g = &altGroup{name: *r.AlternateName, pos: pos}
			choiceAlts[cid][aid] = g
		}
		g.roles = append(g.roles, role)
	}

	cids := make([]string, 0, len(choiceNames))
	for cid := range choiceNames {
		cids = append(cids, cid)
	}
	sort.Slice(cids, func(i, j int) bool { return choiceNames[cids[i]] < choiceNames[cids[j]] })

	for _, cid := range cids {
		groups := choiceAlts[cid]
		alts := make([]altGroup, 0, len(groups))
		for _, g := range groups {
			alts = append(alts, *g)
		}
		sort.Slice(alts, func(i, j int) bool { return alts[i].pos < alts[j].pos })
		out := health.Choice{Name: choiceNames[cid]}
		for _, g := range alts {
			out.Alternates = append(out.Alternates, health.Alternate{Name: g.name, Roles: g.roles})
		}
		choices = append(choices, out)
		if byID == nil {
			byID = make(map[string]health.Choice, len(cids))
		}
		byID[cid] = out
	}
	return unconditional, choices, byID
}

// SystemHealth reports a system's current verdict, the roles that produced it,
// and its recorded transitions inside window, counted back from the DATABASE's
// clock rather than the server's (#719, tsSince); a zero window returns the
// whole history. The system must be in the read scope; out of scope is the
// non-disclosing ErrSystemNotFound.
//
// The verdict is the rollup of the roles served beside it, not the last recorded
// value. The two agree whenever the trigger set is complete, and where they would
// disagree the resolved roles are the honest answer: a system that conforms to a
// standard whose roles nobody has staffed is broken from the moment it exists.
// "The roles served beside it" is the flat list, not the fold's inputs
// directly: a choice's non-active alternate still serves its roles here
// (each with Active false, HealthRole), so the verdict can be healthy while
// the list beside it shows impaired rows, and Active is what tells the two
// apart rather than that being a disagreement.
func (p *PG) SystemHealth(ctx context.Context, systemName string, window time.Duration, read scope.Set) (*HealthReport, error) {
	// Resolved once, here, rather than through ownerInScope (which does the
	// same lookup but discards the id): every read below, including
	// healthTransitions, binds sys.ID, never re-derived from systemName once
	// it might no longer be unique (#627). rep.OwnerID keeps echoing the
	// caller's own reference, unchanged. scopedByNameInScope, not
	// scopedByName-then-inScopeTree: ruling 2 requires ambiguity judged
	// inside read, not fleet-wide, since a name unique within this caller's
	// own scope must resolve even when an unrelated out-of-scope row shares
	// it.
	sys, err := scopedByNameInScope(ctx, p.pool, systemConfig, systemName, "system", read)
	if err != nil {
		return nil, err
	}
	rep := &HealthReport{OwnerKind: "system", OwnerID: systemName, Roles: []HealthRole{}}
	roles, err := p.resolveHealthRoles(ctx, p.pool, sys.ID)
	if err != nil {
		return nil, err
	}
	unconditional, choices, choicesByID := splitHealthRoles(roles)
	rep.Verdict = health.SystemVerdictWith(unconditional, choices).String()
	// The same choices splitHealthRoles just grouped for the verdict name
	// which alternate is active in each, so explainRole can mark every role
	// with whether it is the reason the verdict reads what it does or a
	// roads-not-taken alternate's role: the report would otherwise repeat
	// the pre-#626 contradiction the fold itself was built to fix, just one
	// level lower, listing an unbuilt alternate's roles as impaired beside a
	// verdict that already knows to ignore them. Keyed by choice id
	// (choicesByID), not name: a system can reach two distinct choices that
	// happen to share a name (its standard's and its own ad-hoc one), and a
	// name-keyed lookup would silently collapse them.
	activeAlt := map[string]string{} // choice id -> its active alternate's name
	for cid, c := range choicesByID {
		if active, ok := c.Active(); ok {
			activeAlt[cid] = active.Name
		}
	}
	for i := range roles {
		row, err := p.explainRole(ctx, p.pool, roles[i], activeAlt)
		if err != nil {
			return nil, err
		}
		rep.Roles = append(rep.Roles, row)
	}
	if rep.Transitions, err = healthTransitions(ctx, p.pool, "system", sys.ID, window); err != nil {
		return nil, err
	}
	return rep, nil
}

// LocationHealth reports a location's current verdict, the systems beneath it
// with theirs, and its recorded transitions. The systems are the drill-down: a
// degraded location names which system is at fault, and the system health read
// names the role, which occupant is down, and the alarm.
//
// The verdict is the rollup of exactly those systems, so the headline and the
// drill-down can never disagree: a location cannot read healthy over a system it
// itself lists as an outage.
func (p *PG) LocationHealth(ctx context.Context, locationName string, window time.Duration, read scope.Set) (*HealthReport, error) {
	// Resolved once via scopedByNameInScope, not scopedByName-then-
	// inScopeTree (see SystemHealth's comment; ruling 2, #627); rep.OwnerID
	// keeps echoing the caller's own reference, unchanged.
	loc, err := scopedByNameInScope(ctx, p.pool, locationConfig, locationName, "location", read)
	if err != nil {
		return nil, err
	}
	rep := &HealthReport{OwnerKind: "location", OwnerID: locationName, Systems: []HealthSystem{}}
	systems, err := p.subtreeSystemHealth(ctx, p.pool, loc.ID)
	if err != nil {
		return nil, err
	}
	rep.Systems = systems
	rep.Verdict = health.RollUp(systemVerdicts(systems)).String()
	if rep.Transitions, err = healthTransitions(ctx, p.pool, "location", loc.ID, window); err != nil {
		return nil, err
	}
	return rep, nil
}

// systemVerdicts projects the reported systems onto the pure rollup's input.
func systemVerdicts(systems []HealthSystem) []health.Verdict {
	out := make([]health.Verdict, 0, len(systems))
	for _, s := range systems {
		out = append(out, health.ParseVerdict(s.Verdict))
	}
	return out
}

// explainRole turns a resolved role into the report row, adding the causing chain
// when the role is impaired: which assigned components are not occupying their
// slot (down), and which active alarms took them down. A satisfied role needs
// no explanation, so it costs no alarm read. activeAlt is the choice-id to
// active-alternate-name lookup SystemHealth built from the same
// splitHealthRoles grouping the verdict itself folds, so a role's Active flag
// can never disagree with what actually decided the verdict. Keyed by id, not
// name: two distinct choices (a system's own and its standard's) can share a
// name, and a name-keyed lookup would answer for the wrong one.
func (p *PG) explainRole(ctx context.Context, q txQuerier, r resolvedRole, activeAlt map[string]string) (HealthRole, error) {
	role := health.Role{Name: r.Name, Quorum: r.Quorum, Impact: r.Impact, Assigned: r.Assigned}
	var choiceName, altName string
	active := true // unconditional: always counts
	if r.ChoiceID != nil {
		choiceName, altName = *r.ChoiceName, *r.AlternateName
		active = activeAlt[*r.ChoiceID] == altName
	}
	row := HealthRole{
		Name:       r.Name,
		Label:      r.Label,
		Impact:     r.Impact,
		Quorum:     r.Quorum,
		Satisfying: role.Satisfying(),
		Short:      role.Short(),
		Spare:      role.Spare(),
		Impaired:   role.Impaired(),
		AssignedTo: make([]string, 0, len(r.Assigned)),
		Down:       []string{},
		Alarms:     []Alarm{},
		Choice:     choiceName,
		Alternate:  altName,
		Active:     active,
	}
	for _, c := range r.Assigned {
		row.AssignedTo = append(row.AssignedTo, c.Name)
	}
	if !row.Impaired {
		return row, nil
	}

	for i, c := range r.Assigned {
		if c.Occupies() {
			continue
		}
		row.Down = append(row.Down, c.Name)
		// r.AssignedIDs[i] is the same assignee: resolvedRole keeps the two
		// slices built together (resolveHealthRoles), so an id-by-name
		// re-lookup here, ambiguous once #627 lands, is never needed.
		alarms, err := p.activeAlarms(ctx, q, r.AssignedIDs[i])
		if err != nil {
			return row, err
		}
		row.Alarms = append(row.Alarms, alarms...)
	}
	// The role may be short-staffed rather than broken: nobody was assigned at
	// all, so nobody is "down" and there is no alarm to name.
	return row, nil
}

// subtreeSystemHealth lists the systems placed anywhere under a location with
// their recorded verdicts, ordered by name. Takes the location's id directly
// (see activeAlarmSeverities).
func (p *PG) subtreeSystemHealth(ctx context.Context, q txQuerier, locationID string) ([]HealthSystem, error) {
	rows, err := q.Query(ctx, `
		with recursive subtree as (
			select id from location where id = $1::uuid
			union
			select c.id from location c join subtree s on c.parent_id = s.id
		)
		select s.name, coalesce((
			select sd.value #>> '{}' from property sd
			where sd.system_id = s.id and sd.property_type_id = (select id from property_type where name = $2) and sd.instance = ''
			order by sd.id desc
			limit 1
		), 'healthy')
		from system s
		where s.location_id in (select id from subtree)
		order by s.name`, locationID, healthKey)
	if err != nil {
		return nil, fmt.Errorf("storage: subtree system health %q: %w", locationID, err)
	}
	defer rows.Close()

	out := []HealthSystem{}
	for rows.Next() {
		var s HealthSystem
		if err := rows.Scan(&s.Name, &s.Verdict); err != nil {
			return nil, fmt.Errorf("storage: scan subtree system health %q: %w", locationID, err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// healthTransitions reads an owner's recorded edges inside window, oldest-first:
// the same ordered flip sequence the reachability strip reads, on the owner arc
// rather than the component-and-instance one. ownerID follows recordHealth's
// contract (see ownerArcExprN): an id for component/system/location, a name for
// node. The window's boundary is the database's own (tsSince, #719).
func healthTransitions(ctx context.Context, q txQuerier, ownerKind, ownerID string, window time.Duration) ([]HealthTransition, error) {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	bound, args := tsSince("ts", window, ownerID, healthKey)
	sql := fmt.Sprintf(`select ts, value #>> '{}' from property
		where %s = %s and property_type_id = (select id from property_type where name = $2) and instance = '' and %s
		order by ts asc, id asc`, col, ownerArcExprN(ownerKind, 1), bound)
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: health transitions %s/%s: %w", ownerKind, ownerID, err)
	}
	defer rows.Close()

	out := []HealthTransition{}
	for rows.Next() {
		var t HealthTransition
		if err := rows.Scan(&t.TS, &t.Value); err != nil {
			return nil, fmt.Errorf("storage: scan health transition %s/%s: %w", ownerKind, ownerID, err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// refSet is the small dedupe-and-order helper the recompute leans on: the
// affected sets are unions from several queries, and the visit order must be
// deterministic. Keyed by id, not name (nameSet's #627 replacement): two
// distinct owners can share a name, but never an id, so keying on the name
// would silently collapse them into one visit.
type refSet map[string]string // id -> name

func newRefSet(refs []ownerRef) refSet {
	s := make(refSet, len(refs))
	s.add(refs...)
	return s
}

func (s refSet) add(refs ...ownerRef) {
	for _, r := range refs {
		if r.ID != "" {
			s[r.ID] = r.Name
		}
	}
}

func (s refSet) sorted() []ownerRef {
	ids := make([]string, 0, len(s))
	for id := range s {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ownerRef, len(ids))
	for i, id := range ids {
		out[i] = ownerRef{ID: id, Name: s[id]}
	}
	return out
}

// refIDs projects a []ownerRef to its ids, for binding into a `= any($1::uuid[])`
// parameter.
func refIDs(refs []ownerRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.ID
	}
	return out
}

// scanNames drains a single-text-column result into a slice.
func scanNames(rows pgx.Rows, what string) ([]string, error) {
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("storage: scan %s: %w", what, err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate %s: %w", what, err)
	}
	return out, nil
}

// scanOwnerRefs drains an (id, name) two-column result into ownerRefs.
func scanOwnerRefs(rows pgx.Rows, what string) ([]ownerRef, error) {
	var out []ownerRef
	for rows.Next() {
		var r ownerRef
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			return nil, fmt.Errorf("storage: scan %s: %w", what, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate %s: %w", what, err)
	}
	return out, nil
}
