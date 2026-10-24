---
title: "A create form shows the name it can know, and never mints one to preview it"
date: 2026-08-11
status: "Accepted"
summary: "A create form shows the **stem** a generated name will carry, resolved in the browser from the classification the operator just chose, and writes the ordinal as the token `n`, because the ordinal is allocated against live siblings inside the create's own transaction and does not exist until the row does. A **draft-preview verb that mints and rolls back** is refused: its answer is provisional (another create can take the ordinal between the preview and the commit), and the rolled-back mint takes the same advisory lock real creates need, so a form that previewed on every keystroke would serialise the estate's creates behind a UI affordance. **Re-rendering the label rule in TypeScript** is refused outright, as the second implementation of an engine slice 3 swept 42 copies of. The label is therefore not previewed at all: its data map carries `Name` and `Ordinal`, so it is unknowable for the same reason. The placement bucket is shown beside the field as a PATH and never as a prefix inside it, since names became scoped to placement and a name no longer contains its ancestry. **Amended (#699):** a RENDER is not a mint, and both refusals were about allocating, so `:renderLabel` resolves the rule through the same tiers with the same one engine, writes the token where the ordinal goes, and takes no lock; the form now shows both values in LOCKED fields, gated by the entity's `:create` with the placement resolved in the caller's read scope. **Amended again (#657):** the lock is an inline square icon action in the field's join, matching Settings' own Restore to default, and a locked field is `readonly` rather than `disabled`, because a disabled input fires no click and leaves the value out of the tab order; focus does not claim the pen, since a locked field is a tab stop and tabbing past would otherwise blank both fields. **Amended again (#702):** READING the lowest free ordinal is not minting one either, so the form shows `display-3` rather than `display-n` and the token retires; the answer is provisional, so the form posts it back as the create's `expected_ordinal` and a create that would land a different number is a 409 naming the one that moved, located on `body.expected_name` so the form can tell it from a name collision. The NAME's shape stops being client-side, which closes the naming half of #695. **Amended again (#695):** the ICON half closes the same way, the listing serving `resolved_icon` beside the raw `icon`, so `lib/typechain.ts` is deleted and no type-chain walk runs in the browser; it costs no read, since the list already loads the whole registry in one query. **Amended again (#702 review):** the precondition binds the drafted NAME rather than the ordinal, because a name carries the stem and the suppression rule as well as the number and an ordinal claim was met by a create that landed `monitor-1` where the form showed `display-1`; and the draft now REFUSES the parentless bucket its create refuses, reversing this entry's own \"the draft does not rehearse the all-scope gate\", since the previewed ordinal reports which of that bucket's names are taken and the stem asked about is the caller's to choose **Amended again (#693):** the lock is the console's ONE vocabulary for the pen, so it reaches the EDIT BLADE and the list's full-text `Generated` chip retires from both list renderers: an ownership fact belongs beside the field an operator can change it on, not in a cell charging the Name column the width of the word on every platform-labelled row. The NAME's own chip stays, on the blade beside the name, which is the same rule rather than an exception. The blade gains a state the create form has no equivalent for, a locked field showing a value about to change (the hand-back), because `:renderLabel` previews a row that does not exist and would answer an existing row with the NEXT sibling's ordinal; the hint carries it instead. It closes a silent pen theft: every blade posted `display() \\|\\| undefined` seeded from the stored label, so saving any unrelated field posted the platform's own rendering back as an override and took the pen **Amended again (#713):** \"the placement resolved in the caller's read scope\" no longer holds for one reference, the component's `system`, which the create binds as a membership under `system:update` (ADR-0107); the draft resolves it in that same set and carries the same conditional permission, so a preview is never served for a create the platform would refuse"
---

- **Date:** 2026-08-11 | **Status:** Accepted | **Pages:** [work with an entity](/guides/operator/entities/),
  [UI](/architecture/ui/)
- **Context:** eight slices of epic [#657](https://github.com/hyperscaleav/omniglass/issues/657) built a
  generator that names a component, a system and a location from what it is and where it sits, and the
  console was the last surface that could not reach it: two of the three create forms derived the name
  from the display name and refused to submit without one, so every console-created row arrived
  operator-named whether the operator meant that or not. The sub-issue asked the form to "produce both
  values without the operator typing either", and the epic's own mechanics say that only half of one of
  them is knowable. A generated name is `<stem>-<n>`: the **stem** falls out of the classification the
  operator has just chosen, and the **ordinal** is the lowest free number among the LIVE siblings in the
  placement bucket, allocated under an advisory lock inside the create's own transaction
  ([ADR-0097](#adr-0097-allocation-tests-the-name-it-would-mint-rather-than-reading-the-ordinal-it-stored)).
  The label is worse: its data map carries the row's own `Name` and `Ordinal`
  ([ADR-0098](#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)), so it cannot be
  rendered before the row it describes exists.
- **Decision:** the form shows the **shape** and marks the unknown half as unknown. The stem is resolved
  in the browser from the type registry the picker already loaded, and the ordinal is written as the
  literal token `n`, which is not a digit and cannot be misread as the value: `display-n`, `boardroom`
  for a mint that suppresses its first ordinal, `n` alone for a positional type. Each shape travels with
  the sentence that makes it honest, and for a suppressing mint that sentence is load-bearing rather
  than decorative, since the shown `boardroom` becomes `boardroom-2` for the second one in the room.
- **Decision (the placement is context, not a prefix):** the bucket the name has to be unique in is
  shown beside the field as a **path** (`Unique at Headquarters / West Building / Level 2 / Boardroom`),
  read-only. The sub-issue asked for the ancestry as an editable field's read-only prefix
  (`boi-17c-[editable]`), which is stale by two slices: names became scoped to placement, so a name no
  longer contains its ancestry at all, and rendering the path into the field would put back exactly the
  redundancy the scoping removed.
- **Rejected, and why each:**
  - **A draft-preview verb that mints and rolls back.** It matches the acceptance's wording and buys a
    number that is **provisional**: any other create in the same bucket can take it between the preview
    and the commit, so the form would show a value that silently turns out different, which is the one
    outcome this whole affordance exists to prevent. It is also not free to ask: a rolled-back mint
    takes the same `pg_advisory_xact_lock` on the bucket that real creates take, so a form previewing
    as the operator changes a picker would serialise the estate's creates behind a UI affordance.
    Refused on both counts, and the second is the one that would not have shown up in review.
  - **Re-rendering the label rule in TypeScript.** A second implementation of the rule engine in
    another language, which is the defect the epic's third slice swept 42 hand-rolled copies of onto a
    single primitive to end. The label is therefore not previewed at all: the form says a rule will
    render one and marks it Generated, and shows nothing it cannot know. (Amended by #699 below: the
    refusal stands, and the label is now shown, because the ONE engine renders it.)
  - **Fetching the resolved stem from the server per selection.** It removes the one thing that can
    drift (the browser's walk of the type chain versus the gateway's) at the cost of a round trip per
    picker change and a new route. Declined because the walk is not new here: the console already
    climbs both type registries for the icon, and #688 makes that walk a single primitive
    (`lib/typechain.ts`) with the stem as its third consumer, so there is one client-side climb rather
    than three. What closes the gap instead is a browser-tier e2e that reads the shape the console
    showed and asserts the row lands with that stem, which is the only tier that can see both answers.
    **Amended ([#695](https://github.com/hyperscaleav/omniglass/issues/695)):** this argument is
    spent, and it was always an argument about COUNT rather than about kind. The stem consumer went
    with #702 and the two icon consumers go here, so `lib/typechain.ts` is deleted: there is no
    client-side climb at all, one primitive or otherwise. The registry LISTING carries
    `resolved_icon` beside the raw `icon`, resolved in the gateway over rows the list already holds,
    so it costs no read the console did not already make. Both fields ship because they answer
    different questions: an edit blade posts the raw one back (blank means inherit) and a cell draws
    the resolved one.
- **Consequence:** a name is optional on all three estate create forms, and required only where nothing
  will generate one (an unclassified system, a `location_type` with no name rule, a `component_type`
  chain with no stem). Each of those states shows the missing fact by name rather than a disabled
  button with no explanation. The console can now produce a row the platform owns the name of, which
  every earlier slice of this epic could only be reached from the API or the CLI.
- **Amendment (#699, 2026-08-11): a render is not a mint, and the distinction is what this decision
  turned on without naming.** Both refusals above are about ALLOCATING. The provisional-answer
  argument is that a minted ordinal can be taken by another create before the commit; the
  serialisation argument is that a rolled-back mint takes the bucket's `pg_advisory_xact_lock`.
  Neither reaches an operation that allocates nothing: resolve the rule through the same tiers, build
  the same closed data map, execute it with the same one engine, and write the token `n` where the
  ordinal would go. No lock, no write transaction, no allocation, and no second implementation of
  anything. That operation is `POST /<collection>:renderLabel`, and the create form now shows both
  generated values in **locked fields** rather than showing a shape and a promise. The
  no-allocation claim is held by a test that reads back every SQL statement the render issued (#650's
  counting instrument) and by a create five renders later still taking ordinal 1, rather than by this
  paragraph.
  - The TypeScript refusal is **unchanged and is what makes this shape the only one available**: the
    label is shown because the Go engine rendered it, not because the browser learned to.
  - The NAME's shape stays client-side, so the two halves of the form now answer from different tiers.
    That is a deliberate trade rather than an oversight: a stem resolves synchronously from a registry
    the picker has already loaded, so the locked name appears the instant a picker moves, where a
    round trip would leave the form's primary affordance empty until it lands. The cost is the drift
    ADR-0104 already accepted, and the round-trip objection to resolving the stem server-side is now
    void, since the form makes a round trip per picker change regardless. Folding the name into the
    same answer is therefore available and cheap, and is deliberately not taken here.
  - **The gate is the entity's own `:create`,** the permission the create it precedes needs: the
    answer exists to be acted on, and an operator who cannot create has no use for it. The PLACEMENT
    refs are a separate question and resolve within the caller's `location:read` and `system:read`
    scopes, because the rendered string can carry a location's label and a system type's label, so
    without that the route is a disclosure channel. A location draft injects no scope at all, because
    a location's data map reads no other estate row (ADR-0098's exclusion survives on that tier).
    **Amended (#713):** the component's `system` ref resolves in `system:read` **and**
    `system:update`, and the route carries the create's conditional `system:update` permission,
    because the create binds that reference as a membership (ADR-0107). The rule is no longer "the
    placement resolves in a read scope" but "each placement reference resolves exactly where the
    create resolves it", which is the only version of it a preview and its create cannot disagree
    about.
  - **Three states per field, not two,** and the third is not a loading state. Generation is
    unavailable, permanently, for a component_type chain with no stem, a system with no system_type,
    a system_type chain with no stem, and a location_type with no name rule; the label has its own,
    where no rule resolves at any tier, and the field then shows the NAME (the read ladder's third
    rung) rather than sitting locked and empty. That state was every location in a fresh estate when
    this was written, because no location label rule shipped at any tier; a global one ships as of
    [ADR-0105](#adr-0105-a-rule-reads-a-name-as-words-and-the-location-tier-ships-the-restatement-it-once-refused),
    so it is now the state an operator reaches by clearing the rule rather than the state they start in.
- **Amendment (#657, 2026-08-11): the lock is an inline action, and a locked field is `readonly`
  rather than `disabled`.** The affordance moved INSIDE the field, a square icon button in the
  field's daisyUI join, which is where the console already puts an in-field action (a Variables row's
  set / revert, a secret's reveal, a setting's Restore to default). Both actions read as that
  Settings row does, on purpose: handing a field back to the platform IS restoring it to its default,
  and one idea should not carry two visual languages. The button has no text at all, so its tooltip
  ("Override", "Restore to default") is the visible copy and the accessible name says which field.
  - **`disabled` had to go, and that is the substance of this amendment.** A disabled input fires no
    click, so click-to-override cannot work on one, and it is out of the tab order, so the value the
    row is about to carry has no keyboard path. `readonly` is not editable, is still focusable, and
    still fires events. The consequence is that the locked LOOK is now drawn rather than inherited:
    daisyUI 5 ships **no `.input-disabled` class**, only `:disabled` / `[disabled]` selectors, so the
    class the markup carried was a no-op and the whole locked appearance came from the attribute.
    `.input-locked` in `app.css` carries it, and hover only warms the border.
  - **Focus does NOT take the pen**, although a click does. A locked field is a tab stop by the
    decision above, so claiming on focus would mean tabbing from the pickers to the Create button
    claimed both pens and blanked both fields on the way past, which is the state the locking exists
    to prevent. Clicking is a deliberate act; passing through is not. The click accelerator is
    one-way for the same reason: the way back discards what the operator typed and belongs on the
    button. The always-present button is what makes every state keyboard-reachable, and the section's
    tab order is asserted rather than inspected.
- **Amendment (#702, 2026-08-12): reading the lowest free ordinal is not minting one, so the form
  shows the real number and the create carries a precondition.** This decision's two refusals were
  both about ALLOCATING, and #699 already applied that distinction to the label render. The same
  distinction reaches the ordinal itself, which this decision missed: the number a create will take
  is `pickOrdinal` over the sibling names in the placement bucket, and running that computation is a
  read. It takes no `pg_advisory_xact_lock`, opens no write transaction and allocates nothing, so the
  serialisation argument, the one that "would not have shown up in review", does not reach it. The
  form therefore shows `display-3`, not `display-n`, and the token is retired from the codebase
  (`OrdinalToken`, `nameMint.shape` and the console's `ORDINAL_TOKEN` are all gone rather than left
  unused).
  - **The provisional-answer argument stands, and is answered rather than avoided.** Another create
    in the same bucket really can take the number in between, and hiding the number was one way to
    cope with that; refusing is the better one. The form posts the ordinal it was shown as
    `expected_ordinal` on the create, the create allocates under its own lock exactly as it always
    did, and `confirmOrdinal` compares the two inside that transaction before anything is written. A
    create that would land a different name is a **409** naming the number that moved and the name it
    mints; the form re-reads the draft, shows the new name, and the operator submits again. A refusal
    is honest where a silent difference is not, and a silent difference is precisely what the locked
    field exists to prevent.
  - **It is a number, never a name.** A locked field that posted a name would claim the pen and set
    `name_generated` false, inverting the whole affordance, so the precondition is the ordinal and the
    API refuses it (422) beside a supplied name, where nothing is allocated for it to be about. It is
    a pointer with `minimum: 1` so absent and zero cannot both spell "no expectation", and an
    operator-named draft answers with no ordinal at all, which is what makes "the field is locked" and
    "there is a precondition to post" one fact rather than two.
  - **The form has to tell this refusal from every other one,** because its recovery (re-read,
    re-show, resubmit) is specific to it and a create's OTHER 409 is a name collision the operator has
    to resolve themselves. Matching on the message would tie the console to server copy, so both
    refusals a form acts on carry a `huma.ErrorDetail` locating them on the request field the recovery
    touches: `body.expected_ordinal` for the conflict, carrying the ordinal that moved as its value,
    and `body.name` for the four "the platform will not name this row" refusals. That is the RFC 9457
    `errors` array the `ErrorModel` already publishes, so it costs no new wire shape.
  - **The name's shape stops being client-side, which #699 explicitly left available.** That amendment
    called folding the name into the same answer "available and cheap, and deliberately not taken
    here", on the grounds that a stem resolves synchronously from a loaded registry. It is taken now:
    the draft already resolves the stem, the mint and the bucket to render the label, so returning the
    NAME costs nothing, and the form makes a round trip per picker change regardless. The cost it
    removes is the drift this decision accepted, which is the naming half of
    [#695](https://github.com/hyperscaleav/omniglass/issues/695). The consequence is that "the platform
    will not name this row" is now an ANSWER from the server rather than a null the browser computed,
    so the field's three states are unchanged but their source is one tier instead of two, and the
    submit gate waits for that answer rather than flipping synchronously.
  - **The draft takes the same scopes its create takes, in the same order,** because it now resolves
    the same references: the parent decides which bucket the ordinal is read from, so it resolves in
    the caller's `<kind>:create` scope, the set the create resolves it in. A location's draft
    therefore takes a scope where it took none, and what that scope guards is the sibling read rather
    than the render (ADR-0098's exclusion of placement from a location's label data map is untouched).
    The draft does not rehearse the create's own all-scope gate on a ROOT create: it answers what a row
    would be called, not whether the create is permitted, and the permission is the route's gate.

    **Amended (#702 review), and that last sentence is REVERSED.** The draft refuses the parentless
    bucket in the same set the create refuses it, because the answer is not inert: it carries the
    lowest free ordinal, read from the bucket's sibling NAMES, so it reports which of that bucket's
    names are taken. On the root bucket the probe is chosen rather than fixed, since writing a stem
    into a forked type's name rule is ordinary
    ([#703](https://github.com/hyperscaleav/omniglass/issues/703)) and needs only
    `location_type:create`. Driven, the root draft answered `secret-region-2` to a principal whose
    create in that bucket is a 403. The gate is a SCOPE, so it sits beside the route's permission gate
    rather than contradicting it, and it is unconditional rather than only when a name is being
    drafted, for the same reason the create's gate is.
  - **Amended (#702 review): the precondition binds the NAME, not the ordinal.** The claim a locked
    field makes is "the row will be called what I am displaying", and the ordinal is one of three
    inputs to that rather than the claim itself: a name is the stem, the suppression rule and the
    number together, so a claim on the number alone passes unchanged while the other two move. Driven:
    a form drafts `display-1`, `PATCH /component-types/display {"stem":"monitor"}` lands, the create
    posts `expected_ordinal: 1`, and the row is created as `monitor-1` with the precondition MET,
    which is exactly the silent difference the locked field exists to prevent. Nothing races there,
    and forking a type and rewriting what it mints is an ordinary operator act since #703, so the
    console was shielded only by an accident (the stem is not in the draft body, so it is not in the
    query key that would have re-asked) and a CLI or API caller was not shielded at all. `expected_name`
    replaces `expected_ordinal`, and `confirmDraftedName` compares the string. Keeping the number and
    adding the stem and the bucket beside it was refused: it is three fields where one already carries
    all three by construction, and it would need extending again the next time a mint grows an input.
    **It is still a precondition and not the name field:** the create leaves `name` empty, so the row
    is still `name_generated` and the ordinal column is still the platform's, and posting it beside a
    supplied name is the same 422 as before. The 409's `ErrorDetail` moves to `body.expected_name` and
    carries the name the create WOULD have produced as its value, which is what the form shows next.
    The drafted `ordinal` stays on the render's answer as an informational fact, since a surface that
    shows where a value came from teaches the mechanism it operates.
- **Amendment (#693, 2026-08-12): the lock is the console's ONE vocabulary for the pen, so it reaches
  the edit blade and the list's chip retires.** This decision described a create form, and the pen's
  other half was left where it had grown: a full-text `Generated` chip in the identity cell, read by
  18 flat-list pages and every tree. Two things were wrong with that and only one is cosmetic. It
  charged the Name column the width of the word on every platform-labelled row, on the very column
  [#690](https://github.com/hyperscaleav/omniglass/issues/690) had just had to put a floor under. And
  it stated an ownership fact where the operator could not act on it, while the surface that CAN act
  on it, the edit blade, said nothing at all. So `PenToggle` moves out of the create form into its own
  module and the blade's display-name field consumes it, and the chip goes from both list renderers.
  - **The name's own chip stays, and that is the consistency rather than an exception.** The component
    blade marks a platform-picked NAME in its read state, beside the name fact. The rule both now
    follow is that a pen states itself beside the field it owns, on the surface where the operator can
    change it; a list is neither.
  - **The blade has a third state the create form does not,** and it is a locked field showing a value
    that is about to change: "Restore to default" on a row an operator labelled by hand. Nothing can
    know what the rule will render for it, because `:renderLabel` previews a row that does not exist
    and reads the lowest FREE ordinal in the bucket, which for an existing row is the NEXT sibling's
    number. Asking it would answer confidently and wrongly. The field therefore keeps showing the
    label that is there and the hint says the platform rewrites it on save. A lock over an EMPTY field
    was refused here for the same reason it was refused on the create form.
  - **It closes a silent pen theft, which is why this is a fix and not a restyle.** Every blade seeded
    a plain signal from `display_name` and posted `display() || undefined`, so opening the pencil on a
    platform-labelled row and saving ANY other field posted the platform's own rendering back as an
    override. `labelPen` read that as the operator claiming the label, the row stopped following its
    rule, and nothing on screen had said so. The field now posts the PEN's value, which is the empty
    string while it is locked, and the API reads that as "still the platform's". One expression covers
    the no-op and the hand-back, and the hand-back is the console's first way back at all.
  - **`labelGenerated` retires with the chip.** It asked "is there a rendered label here to mark",
    which is a marker's question and returns false for a row whose rule rendered nothing; a field asks
    "who holds the pen", which is true for that same row and must open it locked. One predicate
    answering two questions differently is how a surface marks one thing and does another.
- **Tracked under** [#688](https://github.com/hyperscaleav/omniglass/issues/688), the eighth slice of
  epic [#657](https://github.com/hyperscaleav/omniglass/issues/657), amended by
  [#699](https://github.com/hyperscaleav/omniglass/issues/699), its tenth, by the affordance pass
  folded into [#698](https://github.com/hyperscaleav/omniglass/pull/698), by
  [#702](https://github.com/hyperscaleav/omniglass/issues/702), and by
  [#693](https://github.com/hyperscaleav/omniglass/issues/693).
