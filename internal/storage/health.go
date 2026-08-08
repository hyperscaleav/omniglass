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
// an estate, so this is not a corner. Every recompute therefore takes a
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
	Name        string
	DisplayName string
	Impact      string
	Quorum      int
	Satisfying  int
	Short       int // how many more occupants the role needs to reach quorum
	Spare       int // how many occupants the role has beyond quorum
	Impaired    bool
	AssignedTo  []string
	Down        []string // assigned components whose own verdict is outage
	Alarms      []Alarm  // the active alarms on those down components
	Choice      string   // the choice this role belongs to; empty when unconditional
	Alternate   string   // the alternate within Choice; empty when unconditional
	Active      bool     // true for an unconditional role, or a role in its choice's active alternate
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
	ID          string
	Name        string
	DisplayName string
	Quorum      int
	Impact      string
	Assigned    []health.Component
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
// the write that caused it or not at all.
func (p *PG) RecomputeHealth(ctx context.Context, q txQuerier, componentName string) error {
	return p.recomputeChain(ctx, q, []string{componentName}, nil, nil)
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
// in a fixed order (components, then systems, then locations, each by name), which
// is what keeps two recomputes over overlapping chains from deadlocking on each
// other's locks.
func (p *PG) recomputeChain(ctx context.Context, q txQuerier, components, systems, locations []string) error {
	affected := newNameSet(systems)

	for _, c := range newNameSet(components).sorted() {
		if err := lockHealthOwner(ctx, q, "component", c); err != nil {
			return err
		}
		severities, err := p.activeAlarmSeverities(ctx, q, c)
		if err != nil {
			return err
		}
		if err := recordHealth(ctx, q, "component", c, health.ComponentVerdict(severities)); err != nil {
			return err
		}
		staffed, err := p.systemsStaffedBy(ctx, q, c)
		if err != nil {
			return err
		}
		affected.add(staffed...)
	}

	systemNames := affected.sorted()
	for _, s := range systemNames {
		if err := lockHealthOwner(ctx, q, "system", s); err != nil {
			return err
		}
		roles, err := p.resolveHealthRoles(ctx, q, s)
		if err != nil {
			return err
		}
		unconditional, choices := splitHealthRoles(roles)
		if err := recordHealth(ctx, q, "system", s, health.SystemVerdictWith(unconditional, choices)); err != nil {
			return err
		}
	}

	// A location's verdict reads the systems' RECORDED values, which the loop
	// above has just refreshed inside this transaction.
	affectedLocations, err := p.locationsOver(ctx, q, systemNames, locations)
	if err != nil {
		return err
	}
	for _, l := range affectedLocations {
		if err := lockHealthOwner(ctx, q, "location", l); err != nil {
			return err
		}
		v, err := p.locationVerdict(ctx, q, l)
		if err != nil {
			return err
		}
		if err := recordHealth(ctx, q, "location", l, v); err != nil {
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
// systems, then locations, each by name), which is what keeps two recomputes over
// overlapping chains from deadlocking. A hash collision costs two unrelated
// owners a wait and nothing else.
//
// It is transaction-scoped, so it releases on commit or rollback with no
// unlocking to forget. The recompute always runs inside the caller's transaction
// (that is the point of taking txQuerier), so there is always a transaction to
// scope it to.
func lockHealthOwner(ctx context.Context, q txQuerier, ownerKind, ownerID string) error {
	if err := lockAdvisory(ctx, q, healthKey+"/"+ownerKind+"/"+ownerID); err != nil {
		return fmt.Errorf("storage: lock health %s/%s: %w", ownerKind, ownerID, err)
	}
	return nil
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

// recomputeSystems is the trigger shape for a declaration change, which moves a
// system's roles without touching any component.
func (p *PG) recomputeSystems(ctx context.Context, q txQuerier, systems ...string) error {
	return p.recomputeChain(ctx, q, nil, systems, nil)
}

// recomputeMovedSystem is the trigger shape for a system that changed location:
// the location it arrived at is reachable from its row, but the one it LEFT is
// not, so that one is named. The old location may have improved (its worst system
// just walked out), which is an edge as real as any failure.
func (p *PG) recomputeMovedSystem(ctx context.Context, q txQuerier, system string, leftLocations ...string) error {
	return p.recomputeChain(ctx, q, nil, []string{system}, leftLocations)
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
// reference the recompute passes around. The recompute speaks NAMES throughout
// (recomputeChain takes them, newNameSet dedupes them, the advisory lock keys on
// one), and that is deliberately left alone: it is the part that took two
// attempts to get right. So the resolution happens here, at the write, rather
// than by threading ids through all of it.
//
// A node resolves to its principal_id, which is its primary key and its
// enrollment identity.
func ownerArcExpr(ownerKind string) string { return ownerArcExprN(ownerKind, 2) }

// ownerArcExprN is the same expression at an arbitrary parameter position, since
// the reads and the write do not agree on where the owner sits.
func ownerArcExprN(ownerKind string, n int) string {
	p := fmt.Sprintf("$%d::text", n)
	switch ownerKind {
	case "component", "system", "location":
		return `(select id from ` + ownerKind + ` where name = ` + p + `)`
	case "node":
		return `(select principal_id from node where name = ` + p + `)`
	}
	return p
}

// activeAlarmSeverities lists the severities of a component's active alarms, the
// only input its own verdict takes.
func (p *PG) activeAlarmSeverities(ctx context.Context, q txQuerier, componentName string) ([]string, error) {
	var severities []string
	if err := q.QueryRow(ctx, `
		select coalesce(array_agg(severity), '{}')
		from alarm where component_id = (select id from component where name = $1) and cleared_at is null`,
		componentName).Scan(&severities); err != nil {
		return nil, fmt.Errorf("storage: active alarm severities %q: %w", componentName, err)
	}
	return severities, nil
}

// componentVerdict resolves one component's own current verdict from its
// active alarms: not the recorded row (a read must not trust a trigger it did
// not just run), but the same live computation the recompute makes. It is the
// only input a slot it occupies takes now: an alarm impairs a component
// wholesale, not by name capability, so there is nothing else to resolve.
func (p *PG) componentVerdict(ctx context.Context, q txQuerier, componentName string) (health.Verdict, error) {
	severities, err := p.activeAlarmSeverities(ctx, q, componentName)
	if err != nil {
		return health.Healthy, err
	}
	return health.ComponentVerdict(severities), nil
}

// systemsStaffedBy lists the systems this component fills a role in, the systems
// whose verdict its condition can move.
func (p *PG) systemsStaffedBy(ctx context.Context, q txQuerier, componentName string) ([]string, error) {
	rows, err := q.Query(ctx, `
		select distinct s.name from system_role_assignment ra join system s on s.id = ra.system_id
		where ra.component_id = (select id from component where name = $1) order by 1`,
		componentName)
	if err != nil {
		return nil, fmt.Errorf("storage: systems staffed by %q: %w", componentName, err)
	}
	defer rows.Close()
	return scanNames(rows, "systems staffed by")
}

// conformingSystems lists the systems a standard's declaration reaches. Declaring
// a role on a standard moves every conforming system at once, which is the arc a
// per-system recompute would miss.
func (p *PG) conformingSystems(ctx context.Context, q txQuerier, standardID string) ([]string, error) {
	rows, err := q.Query(ctx, `select name from system where standard_id = (select id from standard where name = $1 or id::text = $1) order by name`, standardID)
	if err != nil {
		return nil, fmt.Errorf("storage: conforming systems %q: %w", standardID, err)
	}
	defer rows.Close()
	return scanNames(rows, "conforming systems")
}

// systemsForRoleOwner resolves the systems one role declaration reaches: the
// single system for an ad-hoc declaration, every conforming system for a
// standard's, since those inherit it live.
func (p *PG) systemsForRoleOwner(ctx context.Context, q txQuerier, ownerKind, ownerID string) ([]string, error) {
	if ownerKind == "standard" {
		return p.conformingSystems(ctx, q, ownerID)
	}
	return []string{ownerID}, nil
}

// locationsOver returns the locations holding these systems, the locations named
// explicitly, and every ancestor above either: the full set whose rollup the
// change can move. The explicit arm carries the location a system has just LEFT,
// which its row no longer points at.
func (p *PG) locationsOver(ctx context.Context, q txQuerier, systems, named []string) ([]string, error) {
	if len(systems) == 0 && len(named) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `
		with recursive placed as (
			select l.id, l.name, l.parent_id
			from system s join location l on l.id = s.location_id
			where s.name = any($1)
			union
			select l.id, l.name, l.parent_id
			from location l where l.name = any($2)
		),
		ancestry as (
			select id, name, parent_id from placed
			union
			select p.id, p.name, p.parent_id
			from location p join ancestry a on a.parent_id = p.id
		)
		select distinct name from ancestry order by name`, systems, named)
	if err != nil {
		return nil, fmt.Errorf("storage: locations over systems: %w", err)
	}
	defer rows.Close()
	return scanNames(rows, "locations over systems")
}

// locationVerdict rolls up a location from the RECORDED health of every system
// placed anywhere in its subtree.
//
// Folding the subtree's systems directly, rather than the child locations'
// verdicts, is what makes the recompute order-independent: the recursive
// definition and this one agree (a location's only inputs are systems, however
// deep), but this one never depends on a child having been recomputed first.
func (p *PG) locationVerdict(ctx context.Context, q txQuerier, locationName string) (health.Verdict, error) {
	rows, err := q.Query(ctx, `
		with recursive subtree as (
			select id from location where name = $1
			union
			select c.id from location c join subtree s on c.parent_id = s.id
		)
		select distinct on (sd.system_id) sd.value #>> '{}'
		from property sd
		where sd.property_type_id = (select id from property_type where name = $2)
		  and sd.system_id in (select id from system where location_id in (select id from subtree))
		order by sd.system_id, sd.id desc`, locationName, healthKey)
	if err != nil {
		return health.Healthy, fmt.Errorf("storage: location verdict %q: %w", locationName, err)
	}
	defer rows.Close()

	var children []health.Verdict
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return health.Healthy, fmt.Errorf("storage: scan location verdict %q: %w", locationName, err)
		}
		children = append(children, health.ParseVerdict(value))
	}
	if err := rows.Err(); err != nil {
		return health.Healthy, fmt.Errorf("storage: iterate location verdict %q: %w", locationName, err)
	}
	return health.RollUp(children), nil
}

// resolveHealthRoles resolves a system's roles from both arcs (inherited from its
// standard, declared on the system) with each assigned component's own current
// verdict. It is the resolution EffectiveRoles does, carried far enough for the
// verdict: the quorum, the impact, and who occupies the slot today.
func (p *PG) resolveHealthRoles(ctx context.Context, q txQuerier, systemName string) ([]resolvedRole, error) {
	// A correlated subquery for assigned, not a GROUP BY: array_agg(distinct
	// ... order by ra.position) is rejected by Postgres outright (the ORDER
	// BY expression must appear in the DISTINCT argument list), and this
	// query joins nothing else that would fan assignment rows out, so the
	// subquery form EffectiveRoles needs for its extra joins works here too,
	// ordered by the occupant's position (#626) rather than alphabetically.
	rows, err := q.Query(ctx, `
		with sys as (
			select id, name, standard_id from system where name = $1
		),
		roles as (
			select r.id, r.name, r.display_name, r.quorum, r.impact, r.alternate_id
			from sys join system_role r on r.owner_kind = 'standard' and r.standard_id = sys.standard_id
			union all
			select r.id, r.name, r.display_name, r.quorum, r.impact, r.alternate_id
			from sys join system_role r on r.owner_kind = 'system' and r.system_id = sys.id
		)
		select roles.id, roles.name, roles.display_name, roles.quorum, roles.impact,
		       -- NAMES, not ids: the rollup looks each assignee's alarms up by
		       -- name, and the report displays them.
		       coalesce((select array_agg(ac.name order by ra.position)
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
		order by roles.name`, systemName)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve health roles %q: %w", systemName, err)
	}

	type rawRole struct {
		resolvedRole
		assignedTo []string
	}
	var raw []rawRole
	for rows.Next() {
		var r rawRole
		if err := rows.Scan(&r.ID, &r.Name, &r.DisplayName, &r.Quorum, &r.Impact,
			&r.assignedTo, &r.AlternateID, &r.AlternateName, &r.AlternatePos, &r.ChoiceID, &r.ChoiceName); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: scan health role %q: %w", systemName, err)
		}
		raw = append(raw, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("storage: iterate health roles %q: %w", systemName, err)
	}
	rows.Close()

	// A component fills at most one role per system (#626), so a name can no
	// longer repeat across this system's role list: the cache below costs
	// nothing to keep even so, since a miss is just one extra query, not a
	// wrong answer.
	resolved := map[string]health.Component{}
	out := make([]resolvedRole, 0, len(raw))
	for _, r := range raw {
		role := r.resolvedRole
		role.Assigned = make([]health.Component, 0, len(r.assignedTo))
		for _, name := range r.assignedTo {
			c, ok := resolved[name]
			if !ok {
				v, err := p.componentVerdict(ctx, q, name)
				if err != nil {
					return nil, err
				}
				c = health.Component{Name: name, Verdict: v}
				resolved[name] = c
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
func splitHealthRoles(rs []resolvedRole) (unconditional []health.Role, choices []health.Choice) {
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
	}
	return unconditional, choices
}

// SystemHealth reports a system's current verdict, the roles that produced it,
// and its recorded transitions at or after since (a zero since returns the whole
// history). The system must be in the read scope; out of scope is the
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
func (p *PG) SystemHealth(ctx context.Context, systemName string, since time.Time, read scope.Set) (*HealthReport, error) {
	inScope, err := p.ownerInScope(ctx, p.pool, "system", systemName, read)
	if err != nil {
		return nil, err
	}
	if !inScope {
		return nil, ErrSystemNotFound
	}
	rep := &HealthReport{OwnerKind: "system", OwnerID: systemName, Roles: []HealthRole{}}
	roles, err := p.resolveHealthRoles(ctx, p.pool, systemName)
	if err != nil {
		return nil, err
	}
	unconditional, choices := splitHealthRoles(roles)
	rep.Verdict = health.SystemVerdictWith(unconditional, choices).String()
	// The same choices splitHealthRoles just grouped for the verdict name
	// which alternate is active in each, so explainRole can mark every role
	// with whether it is the reason the verdict reads what it does or a
	// roads-not-taken alternate's role: the report would otherwise repeat
	// the pre-#626 contradiction the fold itself was built to fix, just one
	// level lower, listing an unbuilt alternate's roles as impaired beside a
	// verdict that already knows to ignore them.
	activeAlt := map[string]string{} // choice name -> its active alternate's name
	for _, c := range choices {
		if active, ok := c.Active(); ok {
			activeAlt[c.Name] = active.Name
		}
	}
	for i := range roles {
		row, err := p.explainRole(ctx, p.pool, roles[i], activeAlt)
		if err != nil {
			return nil, err
		}
		rep.Roles = append(rep.Roles, row)
	}
	if rep.Transitions, err = healthTransitions(ctx, p.pool, "system", systemName, since); err != nil {
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
func (p *PG) LocationHealth(ctx context.Context, locationName string, since time.Time, read scope.Set) (*HealthReport, error) {
	inScope, err := p.ownerInScope(ctx, p.pool, "location", locationName, read)
	if err != nil {
		return nil, err
	}
	if !inScope {
		return nil, ErrLocationNotFound
	}
	rep := &HealthReport{OwnerKind: "location", OwnerID: locationName, Systems: []HealthSystem{}}
	systems, err := p.subtreeSystemHealth(ctx, p.pool, locationName)
	if err != nil {
		return nil, err
	}
	rep.Systems = systems
	rep.Verdict = health.RollUp(systemVerdicts(systems)).String()
	if rep.Transitions, err = healthTransitions(ctx, p.pool, "location", locationName, since); err != nil {
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
// no explanation, so it costs no alarm read. activeAlt is the choice-name to
// active-alternate-name lookup SystemHealth built from the same
// splitHealthRoles grouping the verdict itself folds, so a role's Active flag
// can never disagree with what actually decided the verdict.
func (p *PG) explainRole(ctx context.Context, q txQuerier, r resolvedRole, activeAlt map[string]string) (HealthRole, error) {
	role := health.Role{Name: r.Name, Quorum: r.Quorum, Impact: r.Impact, Assigned: r.Assigned}
	var choiceName, altName string
	active := true // unconditional: always counts
	if r.ChoiceID != nil {
		choiceName, altName = *r.ChoiceName, *r.AlternateName
		active = activeAlt[choiceName] == altName
	}
	row := HealthRole{
		Name:        r.Name,
		DisplayName: r.DisplayName,
		Impact:      r.Impact,
		Quorum:      r.Quorum,
		Satisfying:  role.Satisfying(),
		Short:       role.Short(),
		Spare:       role.Spare(),
		Impaired:    role.Impaired(),
		AssignedTo:  make([]string, 0, len(r.Assigned)),
		Down:        []string{},
		Alarms:      []Alarm{},
		Choice:      choiceName,
		Alternate:   altName,
		Active:      active,
	}
	for _, c := range r.Assigned {
		row.AssignedTo = append(row.AssignedTo, c.Name)
	}
	if !row.Impaired {
		return row, nil
	}

	for _, c := range r.Assigned {
		if c.Occupies() {
			continue
		}
		row.Down = append(row.Down, c.Name)
		alarms, err := p.activeAlarms(ctx, q, c.Name)
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
// their recorded verdicts, ordered by name.
func (p *PG) subtreeSystemHealth(ctx context.Context, q txQuerier, locationName string) ([]HealthSystem, error) {
	rows, err := q.Query(ctx, `
		with recursive subtree as (
			select id from location where name = $1
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
		order by s.name`, locationName, healthKey)
	if err != nil {
		return nil, fmt.Errorf("storage: subtree system health %q: %w", locationName, err)
	}
	defer rows.Close()

	out := []HealthSystem{}
	for rows.Next() {
		var s HealthSystem
		if err := rows.Scan(&s.Name, &s.Verdict); err != nil {
			return nil, fmt.Errorf("storage: scan subtree system health %q: %w", locationName, err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// healthTransitions reads an owner's recorded edges at or after since,
// oldest-first: the same ordered flip sequence the reachability strip reads, on
// the owner arc rather than the component-and-instance one.
func healthTransitions(ctx context.Context, q txQuerier, ownerKind, ownerID string, since time.Time) ([]HealthTransition, error) {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	sql := fmt.Sprintf(`select ts, value #>> '{}' from property
		where %s = %s and property_type_id = (select id from property_type where name = $2) and instance = '' and ts >= $3
		order by ts asc, id asc`, col, ownerArcExprN(ownerKind, 1))
	rows, err := q.Query(ctx, sql, ownerID, healthKey, since)
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

// nameSet is the small dedupe-and-order helper the recompute leans on: the
// affected sets are unions from several queries, and the visit order must be
// deterministic.
type nameSet map[string]bool

func newNameSet(names []string) nameSet {
	s := make(nameSet, len(names))
	s.add(names...)
	return s
}

func (s nameSet) add(names ...string) {
	for _, n := range names {
		if n != "" {
			s[n] = true
		}
	}
}

func (s nameSet) sorted() []string {
	out := make([]string, 0, len(s))
	for n := range s {
		out = append(out, n)
	}
	sort.Strings(out)
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
