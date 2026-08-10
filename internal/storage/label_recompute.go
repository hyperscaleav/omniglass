package storage

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hyperscaleav/omniglass/internal/label"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
)

// The bulk label recompute (#685): one engine, two consumers.
//
// # Why one engine and not two
//
// The operator-facing VERB (preview a rule change, then apply it) and the
// internal CASCADE (a location was renamed, so restamp the rows that read it)
// are the same computation over a different narrowing. Building them separately
// would give the estate two answers to "what should this label be", and the
// recompute-and-compare invariant would only ever be checking one of them.
//
// # Why a preview is an apply that rolls back
//
// A preview must list EXACTLY the rows an apply then changes, and the tempting
// implementation (a read-only pass that renders and compares without writing)
// cannot do that here. Recomputing the locations moves their labels, which
// stales the components and systems placed at them, so the honest answer for a
// location recompute includes rows of two other kinds that only exist as a
// consequence of writes the read-only pass never made. A simulation of those
// writes would be a second implementation of the cascade, and the two would
// drift.
//
// So a preview runs the apply, collects its change set, and rolls the
// transaction back. It is exact by construction rather than by an argument, and
// nothing else has to be kept in step with it. What it costs is honest to say:
// a preview is not a pure read. It takes the operation lock, takes FOR UPDATE
// on the rows it visits, and produces WAL that is then discarded, so it is an
// operator gesture rather than something to put behind a keystroke.
//
// What it still does NOT promise is atomicity ACROSS the pair. The estate can
// move between the preview and the apply, and holding a lock across two HTTP
// requests to prevent that would let an operator who opened a preview and
// wandered off block every write on the tier. The apply's own returned set is
// what closes that gap: it is the record of what actually happened, derived and
// applied in one transaction.
//
// # Blast radius, and why only half of the write paths cascade
//
// Every fact a rule reads belongs to some row, and a change to that row stales
// every label that reads it. The line this file draws is not "the row's own
// facts versus everyone else's" (slice 2's line, which placement erased) but
// how many rows one edit can stale:
//
//   - Bounded by placement (the rows AT one location, the members of one
//     system, one component's own membership): cascaded eagerly, inside the
//     act's transaction, so the estate is never observably stale.
//   - Bounded only by the estate (a rule at any tier, a component_type's or
//     product's or vendor's display_name, the acronym list): left to the verb.
//     This is the epic's own argument, applied consistently: editing a shared
//     classification must not silently rewrite 15,000 rows any more than
//     editing a rule may.
//
// # Locking
//
// A recompute is compare-then-act (read the row, render, compare, write), which
// is wrong under READ COMMITTED unless serialized. Two locks do it:
//
//   - One advisory lock for the whole operation, so two bulk recomputes never
//     interleave. It is deliberately coarse: a bulk recompute is an operator
//     gesture or the tail of a rename, not a hot path.
//   - FOR UPDATE on the rows an apply selects, so a single-row stamp running
//     concurrently in another transaction either lands before this read or
//     waits for this transaction to commit. The single-row stamps need no lock
//     of their own: each runs inside the write that already holds that row's
//     lock from its own UPDATE ... RETURNING.
//
// The order is fixed and stated so it cannot drift: a path that takes both the
// label lock and a health lock takes the LABEL one first (UpdateSystem is the
// only such path today). Nothing takes them the other way round, so there is no
// cycle to deadlock on.

// labelRecomputeKey is the advisory-lock key every bulk recompute serializes
// on. One key, not one per row: 15,000 advisory locks in a transaction is a
// lock table, not a lock.
const labelRecomputeKey = "label/recompute"

// LabelChange is one row a recompute would alter: what it reads now, and what
// its rules produce. It is the preview's whole content and the apply's receipt.
//
// From and To are the read values, so an unset label reads as the empty string
// rather than distinguishing NULL from blank; the pen is not on this type at
// all because a row whose label the operator owns is never a candidate.
type LabelChange struct {
	Kind string
	ID   string
	Name string
	From string
	To   string
}

// ErrLabelKindUnknown is a recompute naming an entity kind that has no label
// rule. Refused by name rather than quietly recomputing nothing, which is what
// a typo in a generated client would otherwise look like.
var ErrLabelKindUnknown = errors.New("storage: unknown label entity kind")

// labelNarrow restricts a recompute to the rows a CASCADE knows can have moved.
// Every field nil is the verb's own case: every row of the kind, inside the
// caller's scope.
type labelNarrow struct {
	// locationIDs restricts to the rows placed AT one of these locations
	// (component and system). It is the row's own location_id, matching what
	// LocationLabel reads; it is deliberately not a subtree, because a
	// component's label reads the label of the location it sits at and nothing
	// about that location's ancestors.
	//
	// A slice rather than a single id because the recompute VERB can move
	// thousands of location labels at once, and following each one with its own
	// cascade would be the N+1 this file exists to avoid, one level up from
	// where anyone would look for it.
	locationIDs []string
	// primarySystemID restricts to the components whose PRIMARY membership is
	// this system.
	primarySystemID *string
	// rowID restricts to one row, the membership cascade's case.
	rowID *string
}

// labelScanQuery builds the row query a recompute reads from: the scoped-tree
// predicate applied TWICE (once for read scope, once for the action scope an
// apply also needs), the pen filter, and the narrowing.
//
// The two scope predicates are what makes "an operator cannot apply over rows
// outside their read scope" and "outside their update scope" one query rather
// than a filter pass. A preview passes an all-scope action set, so its second
// predicate is a constant.
func labelScanQuery(tbl scopeTable, cols string, read, action scope.Set, n labelNarrow) (string, []any) {
	readRoots, _, readSelf := arcScopeArgs(read)
	actionRoots, _, actionSelf := arcScopeArgs(action)
	t := string(tbl)
	args := []any{readRoots, readSelf, actionRoots, actionSelf, read.All, action.All}
	sql := `
		with recursive rsub(id) as (
			select id from ` + t + ` where id = any($1::uuid[])
			union all
			select x.id from ` + t + ` x join rsub on x.parent_id = rsub.id
		) cycle id set is_cycle using path,
		asub(id) as (
			select id from ` + t + ` where id = any($3::uuid[])
			union all
			select x.id from ` + t + ` x join asub on x.parent_id = asub.id
		) cycle id set is_cycle using path
		select ` + cols + ` from ` + t + `
		where display_name_generated
		  and ($5::boolean or id in (select id from rsub) or id = any($2::uuid[]))
		  and ($6::boolean or id in (select id from asub) or id = any($4::uuid[]))`
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if n.locationIDs != nil {
		sql += ` and location_id = any(` + next(n.locationIDs) + `::uuid[])`
	}
	if n.primarySystemID != nil {
		sql += ` and exists (select 1 from system_member m
			where m.component_id = ` + t + `.id and m.is_primary and m.system_id = ` + next(*n.primarySystemID) + `::uuid)`
	}
	if n.rowID != nil {
		sql += ` and id = ` + next(*n.rowID) + `::uuid`
	}
	// Ordered by id, not by name: this is the order rows are LOCKED in, and a
	// single order shared by every recompute is what keeps two of them from
	// deadlocking on each other's rows.
	return sql + ` order by id`, args
}

// labelDrift is the recompute itself: select the platform-owned rows a
// narrowing admits, render each against its current rules, and return the ones
// whose stored label disagrees.
//
// It is flat in row count by construction. The classification half is resolved
// once per DISTINCT product (component) or per distinct classifier pair (system,
// location), both of which are bounded by the registry rather than by the
// estate; the placement half is one statement for the whole page; the global
// rule and the acronym dictionary are read once each. The counting instrument
// from #650 is what holds that to being true rather than intended.
func (p *PG) labelDrift(ctx context.Context, q querier, eng *label.Engine, kind string, n labelNarrow, read, action scope.Set, forUpdate bool) ([]LabelChange, error) {
	if read.Empty() || action.Empty() {
		return nil, nil
	}
	switch kind {
	case "component":
		return componentDrift(ctx, q, eng, n, read, action, forUpdate)
	case "system":
		return systemDrift(ctx, q, eng, n, read, action, forUpdate)
	case "location":
		return locationDrift(ctx, q, eng, n, read, action, forUpdate)
	default:
		return nil, fmt.Errorf("%w: %q", ErrLabelKindUnknown, kind)
	}
}

func forUpdateSuffix(forUpdate bool) string {
	if forUpdate {
		return " for update"
	}
	return ""
}

func componentDrift(ctx context.Context, q querier, eng *label.Engine, n labelNarrow, read, action scope.Set, forUpdate bool) ([]LabelChange, error) {
	sql, args := labelScanQuery(componentTable, componentCols, read, action, n)
	rows, err := q.Query(ctx, sql+forUpdateSuffix(forUpdate), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: scan components for a label recompute: %w", err)
	}
	var cs []*Component
	ids := make([]string, 0, 16)
	err = func() error {
		defer rows.Close()
		for rows.Next() {
			c, err := scanComponent(rows)
			if err != nil {
				return fmt.Errorf("storage: scan component for a label recompute: %w", err)
			}
			cs = append(cs, c)
			ids = append(ids, c.ID)
		}
		return rows.Err()
	}()
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, nil
	}
	places, err := componentPlacements(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	// Resolved once per DISTINCT product: every component sharing a product
	// resolves the same rule and the same classification facts, which is the
	// split componentLabelInputs exists for.
	chains := make(map[string]componentLabelInputs, 8)
	var out []LabelChange
	for _, c := range cs {
		if c.ProductID == nil {
			continue
		}
		in, done := chains[*c.ProductID]
		if !done {
			in, err = componentLabelChain(ctx, q, *c.ProductID)
			if err != nil {
				return nil, err
			}
			chains[*c.ProductID] = in
		}
		to := renderLabel(eng, in.rule, componentLabelData(c, in, places[c.ID]))
		if to != c.DisplayName {
			out = append(out, LabelChange{Kind: "component", ID: c.ID, Name: c.Name, From: c.DisplayName, To: to})
		}
	}
	return out, nil
}

func systemDrift(ctx context.Context, q querier, eng *label.Engine, n labelNarrow, read, action scope.Set, forUpdate bool) ([]LabelChange, error) {
	sql, args := labelScanQuery(systemTable, systemCols, read, action, n)
	rows, err := q.Query(ctx, sql+forUpdateSuffix(forUpdate), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: scan systems for a label recompute: %w", err)
	}
	var ss []*System
	ids := make([]string, 0, 16)
	err = func() error {
		defer rows.Close()
		for rows.Next() {
			s, err := scanSystem(rows)
			if err != nil {
				return fmt.Errorf("storage: scan system for a label recompute: %w", err)
			}
			ss = append(ss, s)
			ids = append(ids, s.ID)
		}
		return rows.Err()
	}()
	if err != nil {
		return nil, err
	}
	if len(ss) == 0 {
		return nil, nil
	}
	global, err := globalLabelRule(ctx, q, "system")
	if err != nil {
		return nil, err
	}
	places, err := systemPlacements(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	chains := make(map[string]systemLabelInputs, 8)
	var out []LabelChange
	for _, s := range ss {
		key := derefID(s.StandardID) + "\x00" + derefID(s.SystemTypeID)
		in, done := chains[key]
		if !done {
			in, err = systemLabelChainWith(ctx, q, s.StandardID, s.SystemTypeID, global)
			if err != nil {
				return nil, err
			}
			chains[key] = in
		}
		to := renderLabel(eng, in.rule, systemLabelData(s, in, places[s.ID]))
		if to != s.DisplayName {
			out = append(out, LabelChange{Kind: "system", ID: s.ID, Name: s.Name, From: s.DisplayName, To: to})
		}
	}
	return out, nil
}

func locationDrift(ctx context.Context, q querier, eng *label.Engine, n labelNarrow, read, action scope.Set, forUpdate bool) ([]LabelChange, error) {
	sql, args := labelScanQuery(locationTable, locationCols, read, action, n)
	rows, err := q.Query(ctx, sql+forUpdateSuffix(forUpdate), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: scan locations for a label recompute: %w", err)
	}
	var ls []*Location
	err = func() error {
		defer rows.Close()
		for rows.Next() {
			l, err := scanLocation(rows)
			if err != nil {
				return fmt.Errorf("storage: scan location for a label recompute: %w", err)
			}
			ls = append(ls, l)
		}
		return rows.Err()
	}()
	if err != nil {
		return nil, err
	}
	if len(ls) == 0 {
		return nil, nil
	}
	global, err := globalLabelRule(ctx, q, "location")
	if err != nil {
		return nil, err
	}
	chains := make(map[string]locationLabelInputs, 8)
	var out []LabelChange
	for _, l := range ls {
		in, done := chains[l.LocationTypeID]
		if !done {
			in, err = locationLabelChainWith(ctx, q, l.LocationTypeID, global)
			if err != nil {
				return nil, err
			}
			chains[l.LocationTypeID] = in
		}
		to := renderLabel(eng, in.rule, locationLabelData(l, in))
		if to != l.DisplayName {
			out = append(out, LabelChange{Kind: "location", ID: l.ID, Name: l.Name, From: l.DisplayName, To: to})
		}
	}
	return out, nil
}

// derefID reads an optional id column as the empty string, so a pair of them
// makes a map key without a nil check at every use.
func derefID(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

// writeLabelChanges writes a whole recompute in ONE statement per kind,
// whatever the size of the change set: the alternative is one UPDATE per row,
// which is the N+1 this file exists to avoid on the write side as well as the
// read side.
//
// An empty render stores SQL NULL rather than a blank, the same distinction the
// single-row stamp makes: a rule that has nothing to say about this row today
// says something tomorrow, and the pen stays with the platform either way.
func writeLabelChanges(ctx context.Context, q txQuerier, tbl scopeTable, changes []LabelChange) error {
	if len(changes) == 0 {
		return nil
	}
	ids := make([]string, len(changes))
	labels := make([]*string, len(changes))
	for i, ch := range changes {
		ids[i] = ch.ID
		if ch.To != "" {
			to := ch.To
			labels[i] = &to
		}
	}
	if _, err := q.Exec(ctx, `
		update `+string(tbl)+` set display_name = v.label, updated_at = now()
		from (select * from unnest($1::uuid[], $2::text[]) as t(id, label)) v
		where `+string(tbl)+`.id = v.id`, ids, labels); err != nil {
		return fmt.Errorf("storage: write %d recomputed labels to %s: %w", len(changes), tbl, err)
	}
	return nil
}

// labelTable maps an entity kind to the table its labels live on.
func labelTable(kind string) (scopeTable, error) {
	switch kind {
	case "component":
		return componentTable, nil
	case "system":
		return systemTable, nil
	case "location":
		return locationTable, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrLabelKindUnknown, kind)
	}
}

// PreviewLabelRecompute lists exactly the rows a recompute would change, in the
// caller's read scope, and leaves the estate as it found it. See the file's own
// doc comment for why it does that by rolling back rather than by not writing.
//
// It is also the recompute-and-compare invariant, promoted from a test shim to
// a gateway method: "this returns nothing" IS the statement that no stored
// label has drifted from its rule, and a test can now assert it over a whole
// estate rather than row by row.
func (p *PG) PreviewLabelRecompute(ctx context.Context, kind string, read scope.Set) ([]LabelChange, error) {
	if _, err := labelTable(kind); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin preview label recompute: %w", err)
	}
	// Rolled back on every path, including the happy one: that is the whole
	// mechanism, not an error handler.
	defer func() { _ = tx.Rollback(ctx) }()
	return p.recomputeInTx(ctx, tx, kind, read, scope.Set{All: true})
}

// RecomputeLabels applies the recompute PreviewLabelRecompute describes, over
// the rows in the caller's read AND update scope, and returns exactly what it
// changed.
//
// One audit row for the whole operation, not one per changed entity (ADR-0100).
func (p *PG) RecomputeLabels(ctx context.Context, actorID, kind string, read, action scope.Set) ([]LabelChange, error) {
	if _, err := labelTable(kind); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin recompute labels: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	changes, err := p.recomputeInTx(ctx, tx, kind, read, action)
	if err != nil {
		return nil, err
	}
	if err := writeLabelRecomputeAudit(ctx, tx, actorID, kind, changes); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit recompute labels: %w", err)
	}
	return changes, nil
}

// recomputeInTx is the verb, and the ONE body a preview and an apply share.
//
// The second half is the part a read-only preview could not have: recomputing
// locations moves what LocationLabel reads, so the rows placed at every
// location whose READ LADDER moved are stale the instant the first half
// commits. They are followed here, in one narrowed cascade for the whole set
// rather than one per location, and the changes join the returned set so a
// caller sees the true blast radius of the button they are about to press.
//
// It terminates after that second hop by construction: nothing in any data map
// reads a component's or a system's label, so restamping one stales nothing.
func (p *PG) recomputeInTx(ctx context.Context, tx pgx.Tx, kind string, read, action scope.Set) ([]LabelChange, error) {
	tbl, err := labelTable(kind)
	if err != nil {
		return nil, err
	}
	// The acronym dictionary is resolved ONCE for the whole operation and
	// passed down, rather than per row or even per kind: it is a settings read,
	// and a recompute that took one per rendered row would have reintroduced
	// the N+1 through the back door (ADR-0099 names this as the reason the
	// render functions take an engine rather than reaching for one).
	eng, err := p.labelEngine(ctx, tx)
	if err != nil {
		return nil, err
	}
	changes, err := p.lockedRecompute(ctx, tx, eng, tbl, kind, labelNarrow{}, read, action)
	if err != nil {
		return nil, err
	}
	if kind != "location" {
		return changes, nil
	}
	moved := make([]string, 0, len(changes))
	for _, ch := range changes {
		if ladderOf(ch.From, ch.Name) != ladderOf(ch.To, ch.Name) {
			moved = append(moved, ch.ID)
		}
	}
	if len(moved) == 0 {
		return changes, nil
	}
	// Scoped exactly as the recompute that caused it, rather than estate-wide
	// the way a rename's cascade is: this is still the operator's own bulk
	// gesture, and its blast radius is the one their scope allows.
	downstream, err := p.cascadeLocationLabelsWith(ctx, tx, eng, moved, read, action)
	if err != nil {
		return nil, err
	}
	return append(changes, downstream...), nil
}

// ladderOf is locationReadLabel over a change's two projections: the label if
// there is one, else the name. A recompute never moves a name, so the name is
// the same on both sides.
func ladderOf(label, name string) string {
	if label != "" {
		return label
	}
	return name
}

// lockedRecompute is the shared body of every apply, the verb's and every
// cascade's: take the operation lock, read the drift with the rows locked, and
// write it. Everything above it differs only in the narrowing and in what, if
// anything, it audits.
func (p *PG) lockedRecompute(ctx context.Context, tx pgx.Tx, eng *label.Engine, tbl scopeTable, kind string, n labelNarrow, read, action scope.Set) ([]LabelChange, error) {
	if err := lockAdvisory(ctx, tx, labelRecomputeKey); err != nil {
		return nil, err
	}
	changes, err := p.labelDrift(ctx, tx, eng, kind, n, read, action, true)
	if err != nil {
		return nil, err
	}
	if err := writeLabelChanges(ctx, tx, tbl, changes); err != nil {
		return nil, err
	}
	return changes, nil
}

// writeLabelRecomputeAudit records ONE row for the operation, naming the rule
// tier that changed and how many entities followed it (ADR-0100, epic decision
// D1).
//
// Per changed entity was the alternative, and it loses on both halves of the
// question it claims to answer better. It writes 15,000 audit rows for one
// click, in one transaction, on a table every other write shares. And the
// per-entity trail it buys is a restatement: a generated label is DERIVED, so
// "why does this row read what it reads" is answered by the rule and the row's
// own facts, both of which the trail already holds, where "who changed the
// estate's labels and when" is answered by exactly this row and by nothing in a
// per-entity trail. The nearest precedent in this gateway agrees: a health
// recompute cascades across a whole ownership chain and audits nothing at all,
// because the recompute is a consequence of an act that is itself audited.
//
// Which is also why a CASCADE writes no row of its own: the rename or the
// reclassify that triggered it already has one, and a second row saying the
// same act also restamped six labels would be a duplicate keyed on a different
// resource.
// The row is keyed on the RULE, not on the entities: resource label_rule,
// resource_id the entity kind, which is that table's actual primary key. That
// is the D1 recommendation implemented literally rather than paraphrased, and
// it is the only key available that a rename cannot orphan.
func writeLabelRecomputeAudit(ctx context.Context, tx pgx.Tx, actorID, entityKind string, changes []LabelChange) error {
	perKind := map[string]int{}
	for _, ch := range changes {
		perKind[ch.Kind]++
	}
	return writeAuditRes(ctx, tx, actorID, "recompute", "label_rule", entityKind, nil, map[string]any{
		"entity_kind": entityKind,
		"changed":     len(changes),
		"by_kind":     perKind,
	})
}

// --- the cascades -------------------------------------------------------
//
// Each one runs inside the transaction of the act that triggered it, so the
// estate is never observably stale: a reader who sees the rename sees the
// labels that follow from it.
//
// None of them is scope-filtered, and that is deliberate rather than an
// oversight. A cascade is not a query the operator asked for, it is the rest of
// the write they already made: leaving a row stale because it sits outside the
// grant that let them rename the location would break the one invariant the
// stored label rests on, and would do it silently. The same reasoning the
// health recompute already runs on, which crosses scope boundaries for the same
// reason.

// cascadeLocationLabels restamps the components and systems placed AT a
// location whose own label or name just moved.
//
// The blast radius is the rows at that one location, not its subtree: a
// component reads the label of the location it sits at, and nothing about that
// location's ancestors, so a building's rename stales nothing in the rooms
// beneath it. If a later slice gives a location an ancestry fact, this is the
// function that grows a subtree arm, and TestMovingALocationChangesNoLabelUnderIt
// is the test that fails first.
func (p *PG) cascadeLocationLabels(ctx context.Context, tx pgx.Tx, locationID string) error {
	eng, err := p.labelEngine(ctx, tx)
	if err != nil {
		return err
	}
	_, err = p.cascadeLocationLabelsWith(ctx, tx, eng, []string{locationID}, scope.Set{All: true}, scope.Set{All: true})
	return err
}

// cascadeLocationLabelsWith is the same cascade over a SET of locations, with
// the engine already resolved and with the scope the caller decides. The verb
// passes its operator's own scope (their bulk gesture, their blast radius); a
// rename passes all-scope, because there the cascade is not a query anyone
// asked for but the rest of a write they already made.
func (p *PG) cascadeLocationLabelsWith(ctx context.Context, tx pgx.Tx, eng *label.Engine, locationIDs []string, read, action scope.Set) ([]LabelChange, error) {
	n := labelNarrow{locationIDs: locationIDs}
	var out []LabelChange
	for _, at := range []struct {
		kind string
		tbl  scopeTable
	}{{"component", componentTable}, {"system", systemTable}} {
		changes, err := p.lockedRecompute(ctx, tx, eng, at.tbl, at.kind, n, read, action)
		if err != nil {
			return nil, err
		}
		out = append(out, changes...)
	}
	return out, nil
}

// cascadeSystemMemberLabels restamps the components whose PRIMARY system is
// this one, for a system whose type just changed.
func (p *PG) cascadeSystemMemberLabels(ctx context.Context, tx pgx.Tx, systemID string) error {
	eng, err := p.labelEngine(ctx, tx)
	if err != nil {
		return err
	}
	_, err = p.lockedRecompute(ctx, tx, eng, componentTable, "component",
		labelNarrow{primarySystemID: &systemID}, scope.Set{All: true}, scope.Set{All: true})
	return err
}

// cascadeComponentLabel restamps one component, for an act that changed which
// system is its primary. It is the single-row case of the same engine rather
// than a call to stampComponentLabel, because the row it must restamp is not
// the row the caller is holding: a membership write returns no component at
// all, and re-reading one to stamp it would be a second definition of the same
// thing.
func (p *PG) cascadeComponentLabel(ctx context.Context, tx pgx.Tx, componentID string) error {
	eng, err := p.labelEngine(ctx, tx)
	if err != nil {
		return err
	}
	_, err = p.lockedRecompute(ctx, tx, eng, componentTable, "component",
		labelNarrow{rowID: &componentID}, scope.Set{All: true}, scope.Set{All: true})
	return err
}
