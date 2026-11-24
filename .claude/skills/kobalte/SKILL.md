---
name: kobalte
description: Use when building or reviewing interactive UI in the web/ SPA, which is built on Kobalte (@kobalte/core) behind in-house wrappers in web/src/components/ui/ (ModalDialog, ConfirmDialog, Popover, Tooltip, Select, Picker, Tabs, Toast, Collapsible, Switch, SegmentedControl): tooltips, popovers, dialogs/drawers, menus, selects, comboboxes, tabs, toasts. Covers which wrapper to reach for, how to add a new one, Covers the headless-plus-Tailwind/daisyUI styling model, the portal pattern that keeps overlays from being clipped by a card or drawer, and the gotchas that waste the most time, above all that an interactive trigger placed inside a <label> silently breaks hover and steals the label. Reach for this whenever you add a floating element, wire a tooltip/popover/dialog, debug one that will not open or is clipped or has wrong accessibility, or test one in jsdom. Pairs with the solidjs skill.
---

# Kobalte

The console's interactive primitives are [Kobalte](https://kobalte.dev) (`@kobalte/core`) **behind
in-house wrappers** (ADR-2485): every surface imports a component from `web/src/components/ui/`, and
only that directory imports `@kobalte/core`. `web/src/kobalte-guard.test.ts` fails any other import,
tests included. Kobalte is **headless** (behavior, focus management, ARIA, no styles); each wrapper
carries the house daisyUI/Tailwind classes as its defaults. The authority on which to use is the
design system's "Interactive primitives are wrappers over Kobalte" table.

## Which wrapper

| Need | Import | Props (short) |
|---|---|---|
| Modal surface | `ModalDialog, { DialogHeading, DialogClose }` from `ui/ModalDialog` | `open`, `onClose`, `alert?`, `class`, `frameClass?` (centring layer), `overlayClass?`, `initialFocus?` |
| Confirm that teaches | `ConfirmDialog` from `ui/ConfirmDialog` | `open`, `onClose`, `title`, `confirmLabel`, `onConfirm`, `busy?`, `error?` |
| Floating panel | `Popover` from `ui/Popover` | `content`, children = trigger, `open?`/`onOpenChange?`, `contentClass?`, `contentLabel?`; other attrs on the trigger |
| Hover/focus help | `Tooltip` from `ui/Tooltip` (or `InfoTip`) | `content`, children = trigger, `as?`, `placement?`, `open?`; other attrs on the trigger |
| Short list, drawn options | `Select` from `ui/Select` (or `TreeSelect`) | `options`, `value`, `onChange`, `optionValue`, `optionLabel`, `renderOption?`, `renderValue?` |
| Long list, type to filter | `Picker` from `ui/Picker` | `options: {value,label,detail?,disabled?}[]`, `value`/`onChange` (or `multiple`), `onCreate?` |
| Views with panels | `Tabs` from `ui/Tabs` | `tabs`, `value`, `onChange`, `label`, children = `(value) => panel` |
| Action result | `toast()` from `ui/Toast` | `toast.success(title)`, `toast.error(title, { body })`, `toast({ tone, title, action, ttlMs })` |
| Open/close section | `Collapsible` from `ui/Collapsible` | `title`, children, `open?`/`defaultOpen?` |
| On/off state | `Switch` from `ui/Switch` | `checked`, `onChange`, `label`, `description?` |
| Mode over one view | `SegmentedControl` from `ui/SegmentedControl` | `options`, `value`, `onChange`, `label` |

Native stays where it fits: a plain `<select>` of a few strings, a checkbox in a list of choices, a
radio, a stateless `<details>`. Never hand-roll `role="combobox"`, a tablist or `window.confirm` in a
new surface.

## Adding a wrapper

Need a Kobalte primitive with no wrapper (menu, hover card, slider)? Add it to `ui/` first: house
classes as defaults, a props API in the console's terms (not a re-export of Kobalte's parts), a
`*.test.tsx` beside it that drives the keyboard contract (open, move, select, dismiss, where the
focus goes), and a row in the design-system table. Then consume it.

**Import Kobalte's named exports, never its namespaces.** `import { Content, Root, Title } from
"@kobalte/core/dialog"`, not `Dialog.Content`. Kobalte 0.13 builds each namespace with
`Object.assign` onto a root function, and `Dialog` and `AlertDialog` share that root: whichever
module loaded last owned `Dialog.Content`, and every Drawer announced itself as an alertdialog until
`ModalDialog` moved to the named exports. The same shape can bite any two primitives sharing a root.

Inside a wrapper, the mechanics below still apply.

## The structure, and why Portal matters

Every Kobalte overlay follows the same shape (the wrappers build it; a caller never writes it):

```tsx
// inside ui/Tooltip.tsx only
import { Content, Portal, Root, Trigger } from "@kobalte/core/tooltip";

<Root openDelay={150} placement="top">
  <Trigger class="...">trigger</Trigger>
  <Portal>
    <Content class="z-50 ...">content</Content>
  </Portal>
</Root>
```

`*.Portal` mounts the content to `document.body`, **outside** the component's DOM position. This is the whole point: an overlay rendered in-flow gets clipped by any ancestor with `overflow:hidden` (a card, the grid, a drawer body). Portalling escapes that. If a menu or tooltip is being cut off, the fix is almost always "put it in the Portal," not a z-index war. (The `ColumnMenu` exists precisely because the column dropdown was clipped by the grid card; it moved to a portaled `Popover`.) A list portalled out of a `Drawer` must also be `modal` while open (`Select` and `Picker` set it), or the drawer's dialog marks the new body child `aria-hidden` and the list is unreachable to a screen reader.

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
- **Native radio arrows do not run in jsdom.** `SegmentedControl` (and any radio group) moves on the arrow keys through the browser's own radio-group behavior, which jsdom does not implement; assert the shared `name` and the click path in vitest, and drive the arrows in a real browser.
- **A Select or Combobox moves the focus onto the option (Select) or keeps it in the input with a highlight (Combobox).** Send the next key to `document.activeElement` for a Select, to the input for a Picker; read the highlight from `[data-highlighted]`.
- **Drivers:** `ui/Picker.testkit.ts` (`typeInPicker`, `pickInPicker`) and `TreeSelect.testkit.ts` drive those fields the way a keyboard user does; use them in a consumer's test instead of reaching into the parts.
