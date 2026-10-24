---
title: "An inherited fact shows what it inherits, and Restore reads default"
date: 2026-08-14
refs: [716]
form: bullet
---

- **An inherited fact shows what it inherits, and Restore reads default.** #716's clear made the
  empty box reachable on purpose, and an empty box said nothing: the blade's placeholder read
  "inherits from its parent", which announced that inheritance was happening and never what would be
  inherited. The list cell had answered this all along; the blade, where the editing happens, had
  not.

  The registry listing now serves `inherited_stem`, `inherited_icon` and `inherited_abbrev` beside
  the raw fields, each with the NAME of the ancestor the value comes from
  ([ADR-0115](/architecture/decisions/#adr-0115-an-inherited-fact-is-served-with-the-value-it-inherits-and-the-ancestor-it-came-from)).
  It is a different question from `resolved_icon`, which answers what a row SHOWS and is therefore
  the row's own value on every row that states one: a placeholder asks what the row would show if it
  stated nothing, so answering it with the shown value would print the string an operator had just
  deleted back at them. One walk in `typechain.go` answers both, which is why the agreement test
  against the query walk widened from the icon to all three facts rather than leaving two unchecked.

  The console renders it in three places, and the lock is deliberately not borrowed for any of them:
  ADR-0104 gives the lock one meaning, the platform owns this value, and an inherited fact is one an
  operator MAY set. The placeholder carries the value (a placeholder natively means "leave this blank
  and you get this"). A **teal provenance dot beside the field's label** says the value comes from
  somewhere that is not this row, in read and in edit alike, because Save leaves edit mode and that is
  where an operator lands the instant they clear a box. And the hint carries the one thing neither of
  those can: while the box HOLDS a value, it says that emptying it returns the field to inheriting,
  and names what it would inherit. Nothing climbs the type chain in TypeScript, and the web fixtures
  seed values no client-side climb could produce so a console that derived them would fail.

  The dot replaced an `inherited from <ancestor>` sentence under the read value, which this wave
  shipped in the morning and two spikes against the real console falsified by lunchtime. The sentence
  was the third telling of one fact on the line least able to carry it, and it could not appear in
  the edit state at all, which is the state that needed it: there the inherited value is a grey
  placeholder in a box that looks exactly like an empty one. Four things about the replacement were
  measured rather than drawn. It goes beside the LABEL because that is the one slot that is in the
  same position in both states (beside the value, trailing is right in read and lands 380px from a
  407px input's placeholder, leading is right in edit and reads as a bullet list in read). It encodes
  no DEPTH, because a segment-per-rung version measured 8px on a two-rung chain and 28px on a
  six-rung one and three marks encoding one chain never lined up. It is a focusable tab stop carrying
  the whole fact in its accessible name, since a hover has no keyboard and no touch equivalent. And
  it agrees with the hint by construction: one predicate over the text the field is currently
  showing, so the keystroke that empties the box brings the dot back and retires the hint's
  `Empty inherits from mic.` at the same instant.

  Once the dot was there, the hint was saying the dot's own sentence a second time. On an empty box
  it appended `Inherited from mic.` while the mark beside that same label said exactly that, so the
  clause came out and the hint dropped back to describing the fact. Its conditional twin,
  `Empty inherits from mic.`, stayed, and the asymmetry is the whole finding: that one shows only
  while the box HOLDS a value, where there is no mark (the row states this value) and no visible
  placeholder (the box is not empty), which makes it the only thing on screen that tells an operator
  clearing the box returns the field to inheriting, and the only thing that names what it would
  inherit. One sentence described a state the mark already showed; the other describes an action
  nothing else offers. The CREATE forms are a different surface and keep their own
  `Leave blank to inherit` hints: the row does not exist yet, so there is no served ancestor, no
  `InheritedField` and no mark on those forms at all.

  One rule came out of the spikes and is now the design system's: thread DATA through a field
  primitive, never an element. `FieldRow` and `KVStacked` take the ancestor's NAME and build the mark
  themselves (the shape `info` already had for the (i) affordance), because a `JSX.Element` in a prop
  compiles to a getter, so the element is rebuilt whenever anything that getter reads notifies, and a
  mark derived from a query is then replaced under the pointer on every refetch. Wrapping it in a
  `createMemo` looks like the fix and is not: the memo tracks the derived array a refetch hands back
  equal-but-new. Playwright caught it as an intermittent "element was detached from the DOM"; the
  suite now pins it as an element-identity test over a refetch that changes nothing.

  The cost was measured rather than carried over from #695: the registry read is one statement for a
  registry twenty levels DEEPER, the dimension a per-level query would charge for, and the blade pays
  nothing on top because its read IS that listing (`useComponentTypeRow` finds its row in the same
  query). `location_type` is flat, with no parent link and no stem or abbrev column, so it takes none
  of this.

  The blade's destructive slot on a forked shipped row also stops reading **Restore shipped** and
  reads **Restore default**: it named the thing being restored FROM rather than the thing being
  restored TO, and the console's other restores (Settings, the pen) already said default. The retired
  wording joins the `internal/docslint` denylist, with this log and the decision log keeping the
  words that were true on the day they were written.
