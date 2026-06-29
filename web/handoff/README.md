# Omniglass Console — UI Handoff

This package hands the Omniglass console UI work to the repo. It contains the
**rules**, the **styling spec**, and a **working reference implementation**.

## What's here

```
handoff/
├─ README.md            ← you are here
├─ ui-guide.md          ← THE doc: architecture, ListView contract, conventions,
│                          domain model, and the recipe for adding a page
├─ design-system.html   ← daisyUI 5 + Tailwind 4 + Kobalte spec: tokens, the two
│                          themes, every component's states + Kobalte anatomy map
└─ prototype/           ← runnable reference console (open Omniglass Console.html)
   ├─ Omniglass Console.html
   ├─ entity-list.jsx   ← the generic, config-driven ListView (the heart of it)
   ├─ locations.jsx     ← tree wrapper  (ListView config)
   ├─ systems.jsx       ← flat wrapper  (ListView config)
   ├─ components.jsx    ← flat wrapper  (ListView config)
   ├─ primitives.jsx controls.jsx icons.jsx screens.jsx dashboards.jsx app.jsx
   ├─ theme.css  data.js  tweaks-panel.jsx
   └─ assets/
```

## How to use it

1. **Read `ui-guide.md` first.** It's the contract. §3 (the ListView pattern)
   and §4 (interaction conventions) are the load-bearing parts.
2. **Open `prototype/Omniglass Console.html`** in a browser to see the target
   behavior. Navigate Inventory → Locations / Systems / Components.
3. **Read `entity-list.jsx`** alongside one wrapper (`systems.jsx` is the
   simplest, flat). That pairing shows exactly how a page = a config.
4. **Reference `design-system.html`** for token names, theme variables, and how
   each daisyUI component maps onto a Kobalte primitive's parts.

## Important: prototype ≠ production stack

The prototype is **React via `h()`** (and an in-browser Babel transform) — chosen
for fast iteration, *not* the production stack. The real app is **SolidJS +
Kobalte + daisyUI 5 + Tailwind 4**. When you port:

- Keep the **contracts, layout, conventions, and domain model** from `ui-guide.md`
  exactly.
- Re-express the components in Solid; build interactive widgets (drawer/blade,
  menu, combobox, dialog, tabs, tooltip, toast) on **Kobalte** — do **not**
  hand-roll open/close, focus-trapping, or ARIA.
- Style with **daisyUI classes + Tailwind utilities**; pull colors/spacing/radii
  from the theme variables in `design-system.html`, never hardcoded hex.
- The prototype's `entity-list.jsx` is your blueprint for the Solid `ListView`
  component's props and behavior — mirror its config surface.

## Suggested placement in the repo

- Put `ui-guide.md` where Claude Code will always see it — `web/CLAUDE.md`, or
  `web/docs/ui-guide.md` referenced by a line in your existing `CLAUDE.md`.
- Keep `design-system.html` + `prototype/` under `web/docs/ui/` as reference.
