---
title: "A tree picker keeps its indent out of the closed value (#766)"
date: 2026-11-12
refs: [766, 772, 1203]
---

`TreeSelect` is now a Kobalte Select over `flattenTree`. The old native `<select>` indented a deep node
with non-breaking spaces and a `└` typed into the option text, and the browser showed that text on the
closed face, so a selected room read as a ragged run of whitespace. The closed trigger now shows the bare
label, with the ancestor path drawn after it by CSS from a `data-path` attribute, and each item in the open,
portaled list carries a depth-based inline `padding-left` with the glyph drawn by a pseudo-element. The
selected option is derived from the value and the options together, so a value set before the options load
still selects (the #772 case) without `bindSelectValue`. `SystemTypeSelect` and `ComponentTypeSelect` wrap
`TreeSelect` instead of repeating the indent. `FieldRow` now points its label at the trigger through a
`data-field-target` marker so the control keeps its label as an accessible name.
