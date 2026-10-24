---
title: "A list row carries both identities"
date: 2026-07-22
refs: []
form: bullet
---

- **A list row carries both identities.** Every estate entity has a **key** (`name`, the kebab
  identifier the API and CLI address it by) and an optional **display name**. The console showed one or
  the other and never both, so the string an operator needs for `omniglass component get <key>` was
  invisible on every list but Nodes. The row now shows the display name with the key beneath it, muted
  and in the data face, always visible rather than on hover: hover does not exist on touch, is not
  discoverable, and cannot be selected to copy, and copying it is the point. When an entity has no
  display name the label **is** the key, so it renders once, in the data face, marking it as an
  identifier. **Nothing is derived:** sentence-casing `hq-boardroom-dsp` gives "Hq boardroom dsp", and
  this domain is acronyms (DSP, HDMI, NVX, PTZ, UC, AVoIP), so mechanical casing mangles them and makes
  an absent display name look like a typo rather than an absence. The rule now lives in one place
  (`entityLabel`), replacing **nine** copies of `display_name || name` across the node helper, three
  page mappers, two local `label` helpers, and the owner and parent pickers.
