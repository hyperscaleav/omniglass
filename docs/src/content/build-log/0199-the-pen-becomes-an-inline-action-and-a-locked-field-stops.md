---
title: "The pen becomes an inline action, and a locked field stops being a disabled one"
date: 2026-08-11
refs: []
form: bullet
---

- **The pen becomes an inline action, and a locked field stops being a disabled one** (folded into
  [#698](https://github.com/hyperscaleav/omniglass/pull/698) after the rollup review,
  [ADR-0104](/architecture/decisions/#adr-0104-a-create-form-shows-the-name-it-can-know-and-never-mints-one-to-preview-it)
  amended). The lock #699 shipped was a text button on the field's label row, a second visual language
  for an idea the console already had: a square icon in the field's join, which is what a Variables row,
  a secret's reveal, and a setting's Restore to default have all been since they were built. It moved
  inside the field, and both actions now read as Settings' own does, an opening padlock to take the
  pen and the restore arrow to hand it back, with the same words on the tooltip.

  Dropping the text was not a restyle, because of what held the locked state: the field was
  **`disabled`**. A disabled input fires no click, so click-to-override is impossible on one, and it
  is out of the tab order, so the value the row is about to carry had no keyboard path at all. Losing
  the button's text without fixing that would have left the affordance discoverable only by hovering
  a field that cannot be focused. The fields are **`readonly`** instead: not editable, still
  focusable, still fires events.

  The locked LOOK then had to be drawn, and the reason is worth recording because the old code read
  as if it were already handled. daisyUI 5 ships **no `.input-disabled` class at all**, only
  `:disabled` and `[disabled]` selectors, so the `input-disabled` in the markup had been a no-op
  class since #699 and every bit of the locked appearance came from the attribute being removed.
  `.input-locked` in `app.css` carries it now, unlayered so it beats daisyUI's own `.input`, with the
  hover border and the pointer cursor as affordance on top.

  **Focus does not take the pen, and that is a deliberate departure** from the direction, which asked
  for click OR focus. A locked field is a tab stop, by the same decision that made it readonly, so
  focus-to-override would mean tabbing from the pickers to the Create button claimed both pens and
  blanked both fields on the way past, which is the state the locking exists to prevent. Clicking is
  a deliberate act; passing through is not. The click accelerator is also one-way: the way back
  discards what the operator typed, so it belongs on the button. A test walks the section's tab order
  and asserts all four controls are stops in both states, and another tabs through and asserts both
  fields are still locked on the platform's answer.

  The hints lost their instruction halves, since the icon and its tooltip carry the action now, and
  kept the facts: the placement the name has to be unique in, the rule that rendered the label, and
  the fact that is MISSING where nothing generates. Those three keep a "so name it yourself" tail,
  because that is the one state with no button in the field and the words are the only thing left
  carrying the next move.
