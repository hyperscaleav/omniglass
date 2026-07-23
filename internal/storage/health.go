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
// Transition-only recording. Health lands in state_datapoint, the same
// transition-only substrate the reachability strip reads: a row is written only
// when the value differs from the last one recorded for that owner. The history
// is therefore edges and only edges, which is what makes "when did this break"
// answerable weeks later. Writing a row per recompute would bury the edges in
// samples and answer a different question.
//
// Recompute at the write, inside the caller's transaction. Every mutation that
// can change health recomputes the affected chain before it commits: raising or
// clearing an alarm, staffing or unstaffing a role, declaring or withdrawing one,
// changing a component's capabilities or its product, creating a system, and
// changing the standard it conforms to or the location it sits in. The
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

// healthKey is the state datapoint key carrying a rolled-up verdict. There is one
// series per owner and no instance dimension: an entity has exactly one health.
const healthKey = "health"

// healthRule names the producer in the recorded row's lineage. provenance
// 'calculated' requires a non-null source_rule (and null event_id/audit_id), so
// this constant is what satisfies state_datapoint's lineage CHECK.
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
// impaired. Degraded and Alarms are what turn a verdict into something an
// operator can act on: the role, the capability it lost, and the alarm that took
// it. A satisfied role carries neither.
type HealthRole struct {
	Name        string
	DisplayName string
	Impact      string
	Required    []string
	Quorum      int
	Satisfying  int
	Impaired    bool
	AssignedTo  []string
	Degraded    []string // required capabilities an active alarm has taken away
	Alarms      []Alarm  // the active alarms that degraded them
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
// need: the declaration, who fills it, and what an alarm has taken from each of
// them.
type resolvedRole struct {
	ID          string
	Name        string
	DisplayName string
	Quorum      int
	Impact      string
	Required    []string
	Assigned    []health.Component
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
		if err := recordHealth(ctx, q, "system", s, health.SystemVerdict(healthRoles(roles))); err != nil {
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

// recomputeProductComponents is the trigger shape for a catalog edit: the product
// changed, so every component built to it may now provide a different capability
// set, and every system staffed by one of those components may have moved. The
// components are named and recomputeChain walks the rest of the way up.
//
// A product with no components in use recomputes nothing, which is the common
// case for a catalog edit and costs one query.
func (p *PG) recomputeProductComponents(ctx context.Context, q txQuerier, productID string) error {
	rows, err := q.Query(ctx, `select name from component where product_id = (select id from product where `+registryRefCol(productID)+` = $1) order by name`, productID)
	if err != nil {
		return fmt.Errorf("storage: components of product %q: %w", productID, err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return fmt.Errorf("storage: scan component of product %q: %w", productID, err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: iterate components of product %q: %w", productID, err)
	}
	if len(names) == 0 {
		return nil
	}
	return p.recomputeChain(ctx, q, names, nil, nil)
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
	// event_id and audit_id stay null. The CHECK enforces exactly that shape.
	// The WHERE is the transition rule: no previous row (is distinct from null) or
	// a different one writes; the same value writes nothing.
	owner := ownerArcExpr(ownerKind)
	sql := fmt.Sprintf(`insert into state_datapoint (ts, owner_kind, %[1]s, property_id, instance, value, provenance, source_rule)
		select clock_timestamp(), $1::text, %[3]s, (select id from property where name = $3::text), '', $4::text, 'calculated', $5::text
		where $4::text is distinct from (
			select value from state_datapoint
			where %[1]s = %[3]s and property_id = (select id from property where name = $3::text) and instance = ''
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

// degradedCapabilities is the union of capabilities named by a component's active
// alarms: what the component can no longer be trusted to do. It is the single
// mechanism by which an alarm reaches a system.
func (p *PG) degradedCapabilities(ctx context.Context, q txQuerier, componentName string) ([]string, error) {
	var caps []string
	if err := q.QueryRow(ctx, `
		select coalesce(array_agg(distinct cap.name), '{}')
		from alarm a
		join alarm_capability ac on ac.alarm_id = a.id
		join capability cap on cap.id = ac.capability_id
		where a.component_id = (select id from component where name = $1) and a.cleared_at is null`,
		componentName).Scan(&caps); err != nil {
		return nil, fmt.Errorf("storage: degraded capabilities %q: %w", componentName, err)
	}
	return caps, nil
}

// systemsStaffedBy lists the systems this component fills a role in, the systems
// whose verdict its condition can move.
func (p *PG) systemsStaffedBy(ctx context.Context, q txQuerier, componentName string) ([]string, error) {
	rows, err := q.Query(ctx, `
		select distinct s.name from role_assignment ra join system s on s.id = ra.system_id
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
		select distinct on (sd.system_id) sd.value
		from state_datapoint sd
		where sd.property_id = (select id from property where name = $2)
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
// standard, declared on the system) with what each assigned component provides
// and what an alarm has taken away. It is the resolution EffectiveRoles does,
// carried far enough for the verdict: the required set, the quorum, the impact,
// and each component's effective minus degraded capabilities.
func (p *PG) resolveHealthRoles(ctx context.Context, q txQuerier, systemName string) ([]resolvedRole, error) {
	rows, err := q.Query(ctx, `
		with sys as (
			select id, name, standard_id from system where name = $1
		),
		roles as (
			select r.id, r.name, r.display_name, r.quorum, r.impact
			from sys join system_role r on r.owner_kind = 'standard' and r.standard_id = sys.standard_id
			union all
			select r.id, r.name, r.display_name, r.quorum, r.impact
			from sys join system_role r on r.owner_kind = 'system' and r.system_id = sys.id
		)
		select roles.id, roles.name, roles.display_name, roles.quorum, roles.impact,
		       coalesce(array_agg(distinct cap.name) filter (where cap.name is not null), '{}'),
		       -- NAMES, not ids: the rollup looks each assignee's capabilities and
		       -- alarms up by name, and the report displays them.
		       coalesce(array_agg(distinct ac.name) filter (where ac.name is not null), '{}')
		from roles
		left join role_capability rc on rc.role_id = roles.id
		left join capability cap on cap.id = rc.capability_id
		left join role_assignment ra on ra.role_id = roles.id
		     and ra.system_id = (select id from system where name = $1)
		left join component ac on ac.id = ra.component_id
		group by roles.id, roles.name, roles.display_name, roles.quorum, roles.impact
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
			&r.Required, &r.assignedTo); err != nil {
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

	// One component can fill several roles in a system, so its capability sets are
	// resolved once and reused.
	resolved := map[string]health.Component{}
	out := make([]resolvedRole, 0, len(raw))
	for _, r := range raw {
		role := r.resolvedRole
		role.Assigned = make([]health.Component, 0, len(r.assignedTo))
		for _, name := range r.assignedTo {
			c, ok := resolved[name]
			if !ok {
				provides, err := p.EffectiveCapabilities(ctx, q, name)
				if err != nil {
					return nil, err
				}
				degraded, err := p.degradedCapabilities(ctx, q, name)
				if err != nil {
					return nil, err
				}
				c = health.Component{Name: name, Provides: provides, Degraded: degraded}
				resolved[name] = c
			}
			role.Assigned = append(role.Assigned, c)
		}
		out = append(out, role)
	}
	return out, nil
}

// healthRoles projects the resolved roles onto the pure rollup's input.
func healthRoles(rs []resolvedRole) []health.Role {
	out := make([]health.Role, 0, len(rs))
	for _, r := range rs {
		out = append(out, health.Role{
			Name:     r.Name,
			Required: r.Required,
			Quorum:   r.Quorum,
			Impact:   r.Impact,
			Assigned: r.Assigned,
		})
	}
	return out
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
	rep.Verdict = health.SystemVerdict(healthRoles(roles)).String()
	for i := range roles {
		row, err := p.explainRole(ctx, p.pool, roles[i])
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
// names the role, the capability, and the alarm.
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
// when the role is impaired: which required capabilities its components lost, and
// which active alarms took them. A satisfied role needs no explanation, so it
// costs no alarm read.
func (p *PG) explainRole(ctx context.Context, q txQuerier, r resolvedRole) (HealthRole, error) {
	role := health.Role{Name: r.Name, Required: r.Required, Quorum: r.Quorum, Impact: r.Impact, Assigned: r.Assigned}
	row := HealthRole{
		Name:        r.Name,
		DisplayName: r.DisplayName,
		Impact:      r.Impact,
		Required:    r.Required,
		Quorum:      r.Quorum,
		Satisfying:  role.Satisfying(),
		Impaired:    role.Impaired(),
		AssignedTo:  make([]string, 0, len(r.Assigned)),
		Degraded:    []string{},
		Alarms:      []Alarm{},
	}
	for _, c := range r.Assigned {
		row.AssignedTo = append(row.AssignedTo, c.Name)
	}
	if !row.Impaired {
		return row, nil
	}

	required := newNameSet(r.Required)
	lost := newNameSet(nil)
	for _, c := range r.Assigned {
		for _, d := range c.Degraded {
			if required.has(d) {
				lost.add(d)
			}
		}
	}
	row.Degraded = lost.sorted()
	if len(row.Degraded) == 0 {
		// The role is short-staffed rather than broken: nobody was assigned, or the
		// assignments never provided what it requires. There is no alarm to name.
		return row, nil
	}
	for _, c := range r.Assigned {
		alarms, err := p.activeAlarms(ctx, q, c.Name)
		if err != nil {
			return row, err
		}
		for _, a := range alarms {
			if namesAny(a.Capabilities, lost) {
				row.Alarms = append(row.Alarms, a)
			}
		}
	}
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
			select sd.value from state_datapoint sd
			where sd.system_id = s.id and sd.property_id = (select id from property where name = $2) and sd.instance = ''
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
	sql := fmt.Sprintf(`select ts, value from state_datapoint
		where %s = %s and property_id = (select id from property where name = $2) and instance = '' and ts >= $3
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

func (s nameSet) has(name string) bool { return s[name] }

func (s nameSet) sorted() []string {
	out := make([]string, 0, len(s))
	for n := range s {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// namesAny reports whether any of these names is in the set.
func namesAny(names []string, s nameSet) bool {
	for _, n := range names {
		if s.has(n) {
			return true
		}
	}
	return false
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
