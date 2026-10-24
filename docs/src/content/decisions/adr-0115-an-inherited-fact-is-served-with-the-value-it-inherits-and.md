---
title: "An inherited fact is served with the value it inherits, and the ancestor it came from"
date: 2026-08-14
status: "Accepted"
summary: "A type registry's LISTING serves `inherited_stem`, `inherited_icon` and `inherited_abbrev` beside the raw fields, each with the **name of the ancestor it came from**, because `resolved_icon` ([ADR-0104](#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it)'s #695 amendment) answers what a row SHOWS and an edit blade's placeholder asks what it would show if it stated nothing, which is a different string on every row that states its own: using the shown value would print the string an operator had just deleted back at them as the thing they were about to inherit. The console renders it as the placeholder carrying the VALUE (a placeholder natively means \"leave this blank and you get this\") and a mark naming the ancestor, read from the data rather than written as \"its parent\" because a fact can come from any distance up the chain. The **lock is not borrowed**: ADR-0104 gives it one meaning, the platform owns this value, and an inherited fact is one an operator MAY set. The source is per FACT, since one type can take its stem from a grandparent and its abbrev from its parent. **Amended in the same wave (#716):** \"no new glyph\" is reversed and the read state's `inherited from <ancestor>` sentence is REPLACED by one teal **provenance dot beside the field's LABEL**, present when the value comes from somewhere that is not this row and absent when the row states it, because the sentence was the third telling of one fact on a line with no room for it and could not appear in the edit state at all, where a grey placeholder in a box that looks empty is the state that needed it; the label is the only placement that holds in both states (beside the VALUE, trailing is right in read and lands 380px from a 407px box's placeholder in edit, leading is right in edit and reads as a bullet in read), the mark encodes no distance (a segment-per-rung version swung 8px to 28px on estate shape and never lined up across three labels), it carries the whole fact in its accessible name as a focusable tab stop rather than in a hover no keyboard reaches, it agrees with the hint by construction (one predicate over the text the field is showing, so a keystroke moves both), and it is threaded as a NAME rather than as an element, since a `JSX.Element` prop is a getter that rebuilds the mark under the pointer on every refetch. **Amended again in the same wave (#742):** the hint's `Inherited from <ancestor>.` comes out, since the mark states that in the same field at the same moment, and its conditional twin `Empty inherits from <ancestor>.` STAYS, since it shows only while the box holds a value, where there is no mark and no visible placeholder and it is therefore the only thing telling an operator how to return the field to inheriting; the two take turns off the one predicate, and the CREATE forms keep their own `Leave blank to inherit` hints because neither the mark nor `InheritedField` reaches a row that does not exist yet. Cost measured rather than carried over from #695: the registry read is one statement for a registry twenty levels deeper, so the walk is a pass over rows already in hand. `location_type` is flat and takes none of it. The blade's discard action reads **Restore default** rather than Restore shipped, matching Settings' own restore-to-default vocabulary. **Amended a third time (#743):** the registry LIST renders the value a row TAKES instead of an em dash, and the provenance mark stays a BLADE and detail affordance: no mark belongs in a table row, because a table is for scanning values and the blade is where a value's origin is explained, so a reader running down the Stem column is answering \"what is this type's stem\" and a per-row attribution charges them for a question they did not ask. The Stem and Abbrev cells had gone on rendering an em dash on exactly the rows whose blade showed the value they inherit, so the two surfaces contradicted each other about one fact two clicks apart; both registries now render those cells through one `InheritedCell` primitive off the `inherited_*` fields the listing already carries, at no new query. A stated value and an inherited one render IDENTICALLY, which is the treatment rather than a consequence and is asserted as such, and it settles the dimming question with it (every value in these columns is `text-base-content/60` already). The **Icon** cell, which has shown `resolved_icon` undifferentiated since #695, is the PRECEDENT the other two match rather than the odd column out, and the em dash keeps its one meaning for a row that states nothing with nothing above it. The tab-stop and duplicate-accessible-name questions a list mark would have raised do not arise, there being no mark in a row"
---

- **Date:** 2026-08-14 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/), [the UI](/architecture/ui/)
- **Decision:** The `component_type` and `system_type` LISTINGS serve `inherited_stem`,
  `inherited_icon` and `inherited_abbrev` beside the raw fields, each paired with an
  `inherited_*_source` naming the ancestor the value comes from. The console's edit blade shows the
  value as the field's **placeholder** and the ancestor in a **provenance dot** beside the label, and
  its read state shows the value in place of the em dash it used to render, marked with the same dot
  in the same slot (see the two amendments below: the first replaced the attribution sentence the
  read state shipped with, the second removed the hint sentence that then duplicated the dot).
- **This is a different question from `resolved_icon`, and that is the whole reason it exists.**
  [ADR-0104](#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it)'s
  #695 amendment serves what a row SHOWS, which is the row's own value on every row that states one.
  A placeholder asks what the row would show if it stated **nothing**. Those are the same string only
  on rows that already inherit, so a blade that used `resolved_icon` as its placeholder would print
  the value an operator had just deleted back at them as the thing they were about to inherit. The
  new fields start their climb at the PARENT; `resolved_* = own ?? inherited_*` falls out of that,
  and one walk answers both.
- **A mark of its own, and not the lock.** A placeholder natively means "leave this blank and you get
  this", so the value goes there; the RELATION gets one new mark of its own (below), and the lock was
  available and is refused: ADR-0104 gives it one meaning, **the platform owns this value**, and an
  inherited fact is one an operator MAY set, which is the opposite. Borrowing the icon would cost the
  lock the only meaning it has.
- **Amended, in this same wave (#716): the mark is a teal dot beside the LABEL, and the read state's
  attribution sentence comes out.** This entry first said "no new glyph", with the read state
  rendering the inherited value and `inherited from <ancestor>` under it. That shipped in the morning
  and two spikes against the real console falsified it by lunchtime. The sentence was the third
  telling of one fact on the one line that had no room for it (a dimmed value, an attribution beneath
  it, and a hint beneath the box), and it could not appear in the EDIT state at all, which is the
  state that needs it most: there the inherited value is a grey placeholder in a box that looks
  exactly like an empty one. What ships instead is **one teal dot, present or absent**, present
  meaning this value comes from somewhere that is not this row.
  - **Beside the label, because that is the only placement that works in both states.** Measured on
    the real console at 1320px: beside the VALUE, trailing is right in read (every value starts at
    the same x and the dot follows the last character) and collapses in edit (a 407px input exiles it
    380px from the placeholder it is about, into the slot the console's own in-field actions occupy),
    while leading is right in edit and reads as a list bullet in read, stepping every marked value
    11px out of a column the unmarked ones hold. A hanging gutter lands at two different x in the two
    states. The LABEL is in the same position in both, and `FieldRow`'s label row and `KVStacked`'s
    eyebrow are the same slot, so one treatment covers read and edit and the mark does not move when
    the pencil is clicked.
  - **Present or absent, with no encoding of distance.** A segment-per-rung mark was built first and
    refused on its own frames: its width was driven by how deep that corner of the estate happens to
    nest (8px at two rungs, 28px at six, the same object in the design's mind and a different one on
    screen), and three marks encoding one chain never lined up, because each trailed a label of a
    different length. Distance lives in the hover. The full CHAIN stays a follow-on that needs the
    server to serve it, since `inherited_*_source` names the winning ancestor and nothing between, and
    filling the gap in the browser is the climb #695 deleted.
  - **A hover-only fact is unreachable, so the dot is not one.** The trigger is a button, so it is a
    tab stop that opens on focus, and the whole fact is in its accessible name (`Stem is inherited
    from mic`) rather than only in the tooltip, because a hover has no keyboard and no touch
    equivalent.
  - **The mark and the hint cannot disagree, including mid-keystroke.** The spike caught the dot
    vanishing live as the operator typed, before any save, while the hint under the box still promised
    the inheritance in the present tense. Both answers now come from one predicate over one string,
    the text the field is currently showing, which the field primitive supplies (the persisted value
    while reading, the draft while editing), so the keystroke that empties a box moves both at once.
  - **The mark is threaded as DATA, never as an element.** A `JSX.Element` in a prop compiles to a
    getter, so it is rebuilt whenever anything that getter reads notifies; a mark derived from a query
    is then replaced under the pointer on every refetch, and a `createMemo` over the derived value is
    not a fix, since the array it tracks is rebuilt equal-but-new. The primitives take the ancestor's
    NAME and build the mark themselves, which is the same shape `FieldRow` already had for the (i).
- **Amended again, same wave (#742): the hint stops restating the mark, and keeps the one sentence
  the mark cannot say.** The amendment above left the field saying the relation twice while the box
  was empty: the dot beside the label, and `Inherited from <ancestor>.` appended to the hint, one
  fact in one field at one instant. The appended clause comes out. What stays is its conditional
  twin, `Empty inherits from <ancestor>.`, which the field shows only while the box HOLDS a value,
  and that asymmetry is the point rather than an oversight. In that state there is no dot (the row
  states this value) and no visible placeholder (the box is not empty), so the sentence is the only
  thing that tells an operator emptying the box returns the field to inheriting, and the only thing
  that names what it would inherit. The first sentence described a **state** the mark already shows;
  the second describes an **action** nothing else offers, and deleting both would leave the console
  with no route back to inheriting. The two therefore take turns, off the same single predicate:
  exactly one of the dot and the sentence is on screen in any state, and the keystroke that empties
  the box swaps them. The CREATE forms keep their own `Leave blank to inherit` hints, since neither
  the dot nor `InheritedField` reaches a row that does not exist yet.
- **Amended a third time (#743): the LIST shows the value, and the mark is a blade and detail
  affordance only.** Everything above shipped on the blade alone, so the console's two surfaces
  contradicted each other about one fact two clicks apart: the Stem and Abbrev cells rendered an em
  dash on exactly the rows whose blade showed the value they inherit. Those cells now render the
  value the row takes, through one `InheritedCell` primitive shared by both registries. Nothing is
  fetched for it, since `inherited_*` already rides the listing that is also the blade's read.
  - **The value, undifferentiated: no mark belongs in a table row.** A table is for SCANNING values,
    and the blade is where a value's ORIGIN is explained. Those are two jobs, and the surface that
    does the first is not improved by being made to do the second: a reader running down the Stem
    column is answering "what is this type's stem", and every row that also answered "and whose is
    it" would be charging that reader for a question they did not ask. So a cell states the value and
    stops. The placement rule above (beside the label, in a field) is therefore not extended to
    tables at all, rather than being re-derived for them: it has no list case, because there is no
    list mark to place.
  - **A stated value and an inherited one render IDENTICALLY.** That is the treatment, not a
    consequence of dropping the mark, and it is asserted rather than left to be noticed. It also
    settles the dimming question in the same breath: every value in these columns is muted already
    (`text-base-content/60`), and nothing distinguishes them further.
  - **The Icon cell is the precedent the other two now MATCH.** It has rendered `resolved_icon` since
    #695 with no distinction at all, and that was read as the odd column out while this amendment was
    being drafted the other way round. It is the house treatment for a table. Stem and Abbrev join
    it, and the Icon column changes only in that its fallback chain now lives in the shared cell.
  - **The em dash keeps its one meaning.** A row that states nothing with nothing above it still
    reads as an em dash, because deleting it outright would be this defect pointed the other way, a
    cell asserting a value that does not exist.
  - **What this closes.** The tab-stop and duplicate-accessible-name questions a list mark would have
    raised (up to three focusable marks per inheriting row, in a table body with no other tab stop,
    and one accessible name repeated down a column) do not arise: there is no mark in a row to raise
    them.
- **The ancestor is read, never described.** Both affordances that name it, the mark and the
  conditional hint, name the row the value actually came from rather than saying "its parent",
  because a grandchild whose parent states no stem takes its
  grandparent's, and an operator told to change "its parent" would edit the wrong row. It is per FACT
  rather than per row for the same reason: one type can take its stem from a grandparent and its
  abbrev from its parent, which the e2e fixture is built to exhibit.
- **Nothing is derived in the browser.** [#695](https://github.com/hyperscaleav/omniglass/issues/695)
  deleted `lib/typechain.ts` and #702 and #710 each refused to reintroduce a client-side walk; the
  two facts with no served answer were the reason a blade could not show this at all, so the read
  model grew rather than the console. The web fixtures seed inherited values no client-side climb
  could produce, so a console that derived them would fail.
- **What it cost, measured.** #695's report established that the listing is a single unfiltered
  whole-registry select; that its blade READ is the same query is a separate fact, and it holds
  because `useComponentTypeRow` finds its row in the listing rather than fetching one.
  `TestListComponentTypesCostIsFlatInRegistryDepth` grows the registry twenty levels **deeper**, the
  dimension a per-level query would charge for, and pins the read at one statement, the same number
  the seeded registry costs.
- **`location_type` takes none of it.** It is flat (no parent link) and carries no `stem` or `abbrev`
  column at all, so it has no inherited fact to show. It gains only the vocabulary change below.
- **"Restore shipped" becomes "Restore default".** The blade's destructive slot on a forked shipped
  row named the thing being restored FROM rather than the thing being restored TO, and diverged from
  the console's own restore vocabulary (Settings and the pen both say default, per ADR-0104's #657
  amendment). Both registries that offer it move, with the operator guide; the term is added to the
  `internal/docslint` denylist, and the two historical records the denylist already allowlists
  (`decisions.md`, `build-log.md`) keep the wording that was true on the day they were written.
- **Found by** [#716](https://github.com/hyperscaleav/omniglass/issues/716)'s own review: the clear it
  shipped is what made the empty box reachable on purpose, and an empty box that says nothing is a
  capability that teaches the operator nothing.
