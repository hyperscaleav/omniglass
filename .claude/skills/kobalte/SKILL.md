---
name: kobalte
description: Use when building or reviewing interactive overlay UI in the web/ SPA, which is built on Kobalte (@kobalte/core): tooltips, popovers, dialogs/drawers, menus, selects, comboboxes. Covers the headless-plus-Tailwind/daisyUI styling model, the portal pattern that keeps overlays from being clipped by a card or drawer, and the gotchas that waste the most time, above all that an interactive trigger placed inside a <label> silently breaks hover and steals the label. Reach for this whenever you add a floating element, wire a tooltip/popover/dialog, debug one that will not open or is clipped or has wrong accessibility, or test one in jsdom. Pairs with the solidjs skill.
---

# Kobalte

The console's interactive primitives are [Kobalte](https://kobalte.dev) (`@kobalte/core`): the doctrine is "Kobalte for interactive primitives." Kobalte is **headless**: it ships behavior, focus management, and ARIA, but no styles. You style it with Tailwind and daisyUI classes. Before reaching for Kobalte, check whether daisyUI already has a base visual component to extend (the daisyui-base-then-extend rule); reach for Kobalte when you need real interaction (focus trapping, hover/focus open, portalling, keyboard nav), not just a look.

In this repo today: `Dialog` (the `Drawer` and `CommandPalette`), `Popover` (the `ColumnMenu`), `Tooltip` (the `InfoTip`).

## The structure, and why Portal matters

Every Kobalte overlay follows the same shape:

```tsx
import { Tooltip } from "@kobalte/core/tooltip";

<Tooltip openDelay={150} placement="top">
  <Tooltip.Trigger class="...">trigger</Tooltip.Trigger>
  <Tooltip.Portal>
    <Tooltip.Content class="z-50 ...">content</Tooltip.Content>
  </Tooltip.Portal>
</Tooltip>
```

`*.Portal` mounts the content to `document.body`, **outside** the component's DOM position. This is the whole point: an overlay rendered in-flow gets clipped by any ancestor with `overflow:hidden` (a card, the grid, a drawer body). Portalling escapes that. If a menu or tooltip is being cut off, the fix is almost always "put it in the Portal," not a z-index war. (The `ColumnMenu` exists precisely because the column dropdown was clipped by the grid card; it moved to a portaled `Popover`.)

## The gotcha that costs the most time: triggers do not go inside a `<label>`

A Kobalte trigger is a real, focusable, labelable `<button>`. Putting it **inside a form field's `<label>`** breaks two things at once, both silently:

1. **Hover stops working.** The label's pointer handling interferes with Kobalte's hover tracking, so the tooltip/popover never opens on hover (it may still open on keyboard focus, which masks the bug in a focus-only test).
2. **The label binds to the wrong control.** A `<label>` targets the **first labelable descendant**. The trigger button is labelable and comes before the real input, so the label now labels the button. The input is left with no accessible name, and the button's name pollutes things.

Fix: keep the trigger **outside** the `<label>`, and associate the label to the control by id.

```tsx
// WRONG: button inside the label steals the target and kills hover
<label><span>{name}<InfoTip/></span>{control}</label>

// RIGHT: label targets the control by id; the (i) sits outside it
const id = createUniqueId();
if (control instanceof Element && !control.id) control.id = id;
<div>
  <span class="flex items-center gap-1.5">
    <label for={control.id ?? id}>{name}</label>
    <InfoTip text={hint} />
  </span>
  {control}
</div>
```

(The id trick relies on JSX being a live DOM node in Solid; see [[solidjs]].)

## Opening behavior

Tooltips and most overlays open on **hover and keyboard focus** by default. Useful root props: `openDelay`/`closeDelay` (hover timing), `placement` and `gutter` (position; Kobalte auto-flips and shifts to stay in view), `triggerOnFocusOnly`, `disabled`, `forceMount` (keep mounted for animation). The `Content` carries `data-expanded`/`data-closed` attributes for CSS transitions; you do not need a transition for it to be visible.

Trigger polymorphism: pass `as="div"` (etc.) to render the trigger as a different element, but remember a non-button still needs to be focusable for keyboard users, and a labelable element still has the label problem above.

## Accessibility

Kobalte wires `aria-*`, roles, and focus for you (a tooltip trigger gets `aria-describedby` to its content, a dialog traps focus, a menu does roving tabindex). Do not hand-roll ARIA on top of it. The thing Kobalte cannot fix is structural: keep interactive triggers out of labeling elements (above), and verify the result in the real accessibility tree, not by eyeballing markup.

## Testing Kobalte in jsdom

- The content is **portaled to body**, so query it with `screen.*` (document-wide), not the `render` container, and assert it mounts **outside** an overflow wrapper to prove portalling.
- **Hover is unreliable in jsdom.** Prefer driving **focus** to open: Kobalte shows tooltips on focus only under keyboard modality, so fire a `Tab` keydown first, then focus the trigger:

```tsx
fireEvent.keyDown(document.body, { key: "Tab" });
trigger.focus();
await screen.findByText(helpText);
```

- For the hover path and final positioning, verify in the **built app** (Playwright/`web/e2e/shot.mjs`), because jsdom does not lay out or run the pointer heuristics. A hover bug like the label problem above only shows up in a real browser; a focus-only unit test will pass while the feature is broken.
