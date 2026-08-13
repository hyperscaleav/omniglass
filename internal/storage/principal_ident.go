package storage

// A principal's IDENTIFIER is the string that names it wherever an operator
// reads a TRAIL rather than a row: the actor on an audit entry, the person on
// an acknowledged alarm, a member on a group roster. It is a human's username
// or a service account's name, in that order, and it is empty when neither
// exists.
//
// It is not a label. Both sources are identifiers, which is what
// `principal_label(uuid)` got wrong twice over (#564): it was a stored SQL
// function, so the platform's answer to "what names this principal" lived in
// the database instead of in Go, and its name made a false statement about its
// own return value. This file is where that answer lives now.
//
// # One statement of the columns, two of the order, and a test between them
//
// principalIdentSources is the only place in the tree that names the columns.
// Everything else is rendered from it, in the three shapes the gateway needs:
//
//	principalIdent      the resolution itself, in Go, for a read that has both
//	                    sources in hand.
//	principalIdentCols  those sources as a two-column select list, which is what
//	                    puts them in hand.
//	principalIdentSQL   the same order folded into one bound expression, for the
//	                    one position Go cannot occupy: the audit insert, which
//	                    denormalizes the actor INSIDE the caller's transaction,
//	                    where resolving in Go would cost a second round trip on
//	                    every operator write (the alarm write path pins its
//	                    statement count as an exact equation, alarm_cost_test.go,
//	                    and that equation counts this insert).
//
// So the ORDER is written twice, once as Go control flow and once as a coalesce.
// principal_ident_test.go is the invariant between them: it drives every
// principal kind through both shapes against a real database and fails if they
// ever disagree, which is the recompute-and-compare shape the gateway uses
// wherever one fact has two readers.
//
// # A node resolves to the empty string
//
// `node` is the third profiled principal kind and it is deliberately NOT a
// source here, because `principal_label` did not read it either and this change
// is not the place to change what an audit row says. A node that acknowledged
// an alarm or wrote an audit entry would read as blank; nothing seeds a node
// grant today, so there is no path that does it, and widening the resolution is
// its own decision with its own test.

// principalIdentSources renders the identifier's sources, in precedence order,
// as scalar subqueries over the principal id in `param`.
//
// `param` is SQL written in this repository (a bind placeholder or a column
// reference), never a value off the wire, so there is nothing here to inject
// through.
func principalIdentSources(param string) [2]string {
	return [2]string{
		"(select username from human where principal_id = " + param + ")",
		"(select label from service where principal_id = " + param + ")",
	}
}

// principalIdentCols projects both sources as a two-column select list, for a
// read that resolves them in Go with principalIdent. Valid anywhere a select
// list is, including an UPDATE's RETURNING, which is why this is a projection
// rather than a join: RETURNING cannot left-join, and giving the UPDATE a FROM
// clause would change which rows it updates.
func principalIdentCols(param string) string {
	s := principalIdentSources(param)
	return s[0] + ", " + s[1]
}

// principalIdentSQL folds the same sources into one expression the gateway
// binds, for a write that must resolve the identifier without a second round
// trip.
func principalIdentSQL(param string) string {
	s := principalIdentSources(param)
	return "coalesce(" + s[0] + ", " + s[1] + ")"
}

// principalIdent is the resolution: the first source that names something. A
// nil or empty column reads as absent, so an unknown id, a purged principal and
// a node all come back empty, which is honest rather than invented.
func principalIdent(username, serviceName *string) string {
	if username != nil && *username != "" {
		return *username
	}
	if serviceName != nil {
		return *serviceName
	}
	return ""
}
