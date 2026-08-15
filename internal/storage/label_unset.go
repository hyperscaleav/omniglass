package storage

import "strings"

// Unset, for a label, is SQL NULL and nothing else (#613, ADR-0118).
//
// This file is the one place the platform decides that. Every write path that
// accepts a label binds through one of the two functions below, so the rule is
// written once rather than at each of the forty call sites, and a behavioural
// guard (label_unset_test.go) drives every one of those paths with an empty
// label and asserts the raw column came back NULL.
//
// It lives in Go rather than in a CHECK or a NOT NULL DEFAULT for the reason the
// reversal was made on: a stored representation that every reader has to undo is
// the wrong representation. `NOT NULL DEFAULT ''` forced seven registry
// orderings to be spelled `order by nullif(label, '') nulls last`, which is the
// read path converting the storage choice back into the thing it should have
// been. With NULL, the orderings are `order by label nulls last, name` and the
// database's own three-valued logic does the work.
//
// Whitespace is unset too. A label of " " renders as a blank row on every
// surface, and `entityLabel` in the console has trimmed before comparing since
// #581, so trimming here is what keeps the two ends agreeing about which rows
// have a label at all.

// labelOrNull renders a label for a write that always sets the column: a
// create, an upsert, or a rule restamp. It trims, and a label that is empty
// after trimming is SQL NULL.
//
// The return is `any` rather than *string because that is what pgx binds, and
// because a *string would put a second nil-able thing in every caller's hands
// for no gain.
func labelOrNull(s string) any {
	if t := strings.TrimSpace(s); t != "" {
		return t
	}
	return nil
}

// labelPatch renders a label for a PATCH, where a nil pointer and an empty
// string are different instructions that would otherwise both arrive as SQL
// NULL: leave the column alone, versus clear it.
//
// It returns the two facts a statement needs, so the SQL reads
//
//	label = case when $N::boolean then $M else label end
//
// with $N the set flag and $M the value. That is one parameter more than a
// `coalesce($M, label)`, and it is what lets the empty-means-NULL decision stay
// here in Go instead of being spelled as an empty-string sentinel in forty
// statements. The flag is appended to each statement's parameter list rather
// than inserted, so no existing placeholder is renumbered.
func labelPatch(p *string) (set bool, value any) {
	if p == nil {
		return false, nil
	}
	return true, labelOrNull(*p)
}
