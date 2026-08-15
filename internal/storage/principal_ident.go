package storage

import "strconv"

// A principal's IDENTIFIER is the string that names it wherever an operator
// reads a TRAIL rather than a row: the actor on an audit entry, the person on
// an acknowledged alarm, a member on a group roster. It is a human's username,
// a service account's name, or a node's name, in that order, and it is empty
// only when a principal has no profile row at all.
//
// It is not a label. Both sources are identifiers, which is what
// `principal_label(uuid)` got wrong twice over (#564): it was a stored SQL
// function, so the platform's answer to "what names this principal" lived in
// the database instead of in Go, and its name made a false statement about its
// own return value. This file is where that answer lives now.
//
// # One statement of the columns, two of the order, and a test between them
//
// principalIdentSources is the only place in the tree that names the tables and
// the columns. Everything below is rendered from it, and a caller picks a SHAPE,
// never a column:
//
//	principalIdent          the resolution itself, in Go, variadic over the
//	                        sources so it cannot disagree with them about arity.
//	principalIdentJoins     LEFT JOINs that bring the sources into scope, with
//	                        principalIdentJoinedCols to project them and
//	                        principalIdentJoinedSQL to fold them in an ORDER BY.
//	                        The shape for a read over MANY rows.
//	principalIdentCols      the sources as correlated sub-selects, with
//	                        principalIdentSQL to fold them. The shape for the two
//	                        positions a join cannot reach.
//
// So the ORDER is written twice, once as Go control flow and once as a coalesce.
// principal_ident_test.go is the invariant between them: it drives every
// principal kind through both against a real database, asserts they agree AND
// asserts what they agree ON, and fails on the first disagreement.
//
// # Which shape, and why it is not one shape
//
// A join is the default and a sub-select is the exception, because the exception
// is measurably expensive over rows. Measured on a 500-member group roster
// against postgres:18: the sub-select shape, projected AND folded in the ORDER
// BY, costs 3011 shared buffer hits and 2000 index searches (2.3 ms); the join
// shape costs 15 buffer hits (0.8 ms). Postgres does not common up two identical
// scalar sub-selects, so a read that projects them and then sorts on them pays
// for all four per row.
//
// Two positions cannot use a join anyway, and both are bounded:
//
//   - alarmCols is read by an `UPDATE ... RETURNING`. RETURNING cannot
//     left-join, and giving the UPDATE a FROM clause would change which rows it
//     updates. It reads one component's alarms.
//   - the audit INSERT denormalizes the actor inside the CALLER's transaction,
//     where a Go resolution would cost a second round trip on every operator
//     write; the alarm write path pins its statement count as an exact equation
//     (alarm_cost_test.go) that counts that insert.
//
// # A node resolves to its own name
//
// `node` is the third profiled principal kind and it is the third source here
// (#738). It was not, for one slice: `principal_label` never read `node.name`,
// and #564 preserved that shape deliberately so a refactor about WHERE the
// answer lives changed nothing about WHAT it says. The cost of preserving it was
// a node principal resolving to the empty string in every position this file
// serves, which is a blank actor on an audit row: the kind of thing found much
// later, from a trail that cannot say who.
//
// The node's own `name` is the identifier, for the same reason the other two
// columns are. It is the only operator-visible handle the row has, it is unique
// (`node_name_key`, beside `human_username_key` and `service_name_key`,
// ADR-0111), and it is already the estate address and the NATS subject token, so
// it is the string an operator would recognise in a trail. `label` is a
// label and cannot be one: it is optional and it is not unique, and the audit
// trail denormalizes this string as bare text where nothing survives beside it.
//
// Nothing seeds a node principal with a grant today, so no shipped estate
// reaches this; the resolution is what a node acting WOULD read as, and the
// arithmetic of adding a third source (one more LEFT JOIN per resolution, one
// more correlated sub-select on the two positions a join cannot reach) is the
// same shape the measurement below already covers.

// principalIdentSource is one source of the identifier: a profile table and the
// column on it that names a principal of that kind.
type principalIdentSource struct{ table, column string }

// principalIdentSources is the policy, in precedence order. The only place in
// the tree that names these tables and columns.
//
// The order is not a contest anybody can enter: `principal.kind` admits exactly
// one profile row per principal, so at most one source is ever non-null. It is
// stated anyway, because a coalesce and a Go loop both need one, and it is the
// order the two shapes are tested to agree on.
var principalIdentSources = []principalIdentSource{
	{"human", "username"},
	{"service", "name"},
	{"node", "name"},
}

// principalIdent is the resolution: the first source that is present. Absence is
// SQL NULL and nothing else, exactly as a coalesce reads it, so an unknown id and
// a purged principal both come back empty and the two shapes agree on every
// input rather than only on the inputs the schema can produce.
//
// An earlier draft treated an empty string as absent too, which is a defence
// against a state the schema forbids (`human.username` and `service.name` are
// both NOT NULL, and neither has a create path that would accept an empty one)
// and which the bound coalesce does not share. A defence one shape has and the
// other does not is a disagreement, and this file's whole claim is that they
// agree.
//
// Variadic over the sources: a caller scans as many columns as
// principalIdentSources declares, so growing the policy cannot leave a fold
// reading one column fewer than its query returned.
func principalIdent(vals ...*string) string {
	for _, v := range vals {
		if v != nil {
			return *v
		}
	}
	return ""
}

// principalIdentJoins renders the LEFT JOINs that bring every source into scope
// for a read over many rows, keyed on the given principal-id expression. The
// alias prefix keeps two resolutions in one statement apart, which the audit
// read needs: it resolves an actor and a real actor side by side.
//
// `alias` and `idExpr` are SQL written in this repository, a fixed prefix and a
// column reference, never values off the wire, so there is nothing here to
// inject through.
func principalIdentJoins(alias, idExpr string) string {
	out := ""
	for i, s := range principalIdentSources {
		a := alias + strconv.Itoa(i)
		out += " left join " + s.table + " " + a + " on " + a + ".principal_id = " + idExpr
	}
	return out
}

// principalIdentJoinedCols projects what principalIdentJoins brought into scope,
// in source order, for a Go fold with principalIdent.
func principalIdentJoinedCols(alias string) string {
	return identList(func(i int, s principalIdentSource) string {
		return alias + strconv.Itoa(i) + "." + s.column
	})
}

// principalIdentJoinedSQL folds those same joined columns into one expression,
// for an ORDER BY that must sort on the identifier the projection resolves.
// Folding the columns the join already produced, rather than re-deriving them,
// is what keeps the sort free.
func principalIdentJoinedSQL(alias string) string {
	return "coalesce(" + principalIdentJoinedCols(alias) + ")"
}

// principalIdentCols renders the sources as correlated sub-selects over the
// principal id in `param`, for the positions a join cannot reach. Valid anywhere
// a select list is, including an UPDATE's RETURNING.
func principalIdentCols(param string) string {
	return identList(func(_ int, s principalIdentSource) string {
		return "(select " + s.column + " from " + s.table + " where principal_id = " + param + ")"
	})
}

// principalIdentSQL folds those sub-selects into one bound expression, for a
// write that must resolve the identifier without a second round trip.
func principalIdentSQL(param string) string {
	return "coalesce(" + principalIdentCols(param) + ")"
}

// identList renders one comma-separated item per source, in order.
func identList(item func(int, principalIdentSource) string) string {
	out := ""
	for i, s := range principalIdentSources {
		if i > 0 {
			out += ", "
		}
		out += item(i, s)
	}
	return out
}
