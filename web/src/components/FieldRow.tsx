import { Show, children, createUniqueId, type JSX } from "solid-js";
import InfoTip from "./InfoTip";
import { IDENTITY_LABELS, type IdentityBinding } from "../lib/entities";

// FieldRow is the console's one form-field wrapper: a label above its control,
// with an optional (i) tooltip beside the label (and an optional docs link) and
// an optional hint below. The tooltip trigger sits OUTSIDE the <label> and the
// label associates to the control by id, so a labelable button never steals the
// control's accessible name. It is the edit-mode sibling of KVStacked (the
// read-only fact) and KVRow (the value row); the Variables / Secrets / TreeList
// blade forms all render their fields through it, so the field shape is defined
// once and cannot drift (it was reimplemented three times before).
type FieldRowBase = {
  // Tooltip text for the (i) affordance beside the label (optional).
  info?: string;
  // "Docs" link target inside the tooltip (optional; pairs with info).
  docHref?: string;
  // A hint rendered under the control (optional; distinct from the tooltip).
  hint?: string;
  // An affordance rendered on the label row, pushed to its trailing edge: the
  // lock/override toggle a generated field carries (#699). It sits OUTSIDE the
  // <label> for the same reason the (i) trigger does, and it is not a cosmetic
  // detail: a labelable button inside a <label> steals the control's accessible
  // name and swallows the click that should have focused the input.
  action?: JSX.Element;
  // Label the field with the small-caps eyebrow instead of the form label style.
  // A blade field reads as a fact when it is not being edited (KVStacked, which
  // is eyebrow-labelled), so an eyebrow here is what keeps the label from
  // changing style the moment the pencil is clicked. Forms keep the default.
  eyebrow?: boolean;
  children: JSX.Element;
};

// A field either names one of the identity facts, taking its label from
// lib/entities, or carries a label of its own. The two are mutually exclusive,
// so "Name" cannot be typed onto a display_name field: on a create form the two
// sit adjacent, which is where they were swapped twice.
export type FieldRowProps =
  | (FieldRowBase & { label: string; bind?: never })
  | (FieldRowBase & { bind: IdentityBinding; label?: never });

export default function FieldRow(p: FieldRowProps): JSX.Element {
  const label = () => (p.bind ? IDENTITY_LABELS[p.bind] : p.label!);
  const id = createUniqueId();
  // Resolve the control once. children() memoizes, so inspecting the node and
  // rendering it use the SAME instance (reading props.children twice would
  // re-resolve to a different element, leaving `for` pointing at a phantom id).
  // Solid JSX is eager DOM: the control is a live element, or several for a
  // fragment. Label the element so `<label for>` targets it and never pollutes
  // its accessible name.
  const resolved = children(() => p.children);
  const control = resolved.toArray().find((c): c is Element => c instanceof Element);
  if (control && !control.id) control.id = id;
  return (
    <div class="flex flex-col" classList={{ "gap-1": !p.eyebrow, "gap-1.5": !!p.eyebrow }}>
      <span class="flex items-center gap-1.5">
        <label
          class={p.eyebrow ? "eyebrow" : "text-[12px] font-medium text-base-content/70"}
          for={control?.id ?? id}
        >
          {label()}
        </label>
        <Show when={p.info}><InfoTip text={p.info!} label={label()} href={p.docHref} hrefText="Docs" /></Show>
        <Show when={p.action}>
          <span class="flex-1" />
          {p.action}
        </Show>
      </span>
      {resolved()}
      <Show when={p.hint}><span class="text-[11px] text-base-content/40">{p.hint}</span></Show>
    </div>
  );
}
