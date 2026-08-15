import { Show, children, createUniqueId, type JSX } from "solid-js";
import InfoTip from "./InfoTip";
import ProvenanceDot from "./ProvenanceDot";
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
  // The name of the row this field's value comes from, when it comes from
  // somewhere that is not this row; empty (or absent) marks nothing. It is a
  // STRING rather than an element on purpose: an element in a prop compiles to
  // a getter, so it is rebuilt whenever anything the getter reads notifies, and
  // a mark whose data is derived from a query would be replaced under the
  // pointer on every refetch. A string cannot do that, since `Show` re-renders
  // on a change of truthiness and the name updates in place.
  provenance?: string;
  // Inline action buttons rendered INSIDE the field, in a daisyUI join with the
  // control: the lock/override affordance an identity field carries (#657), the
  // same family KVRow gives a value row (set / revert / copy / reveal). A join
  // reads as one bordered control, which is what "inside the field boundary"
  // means, and it is where the console already puts an in-field action.
  //
  // The wrapper is built HERE rather than taken from the caller, and that is
  // load-bearing: FieldRow labels the first ELEMENT it resolves from `children`,
  // so a caller passing its own `<div class="join">` would hand `for` to the div,
  // and a <label> pointing at a non-labelable element labels nothing at all.
  // Either way the buttons sit OUTSIDE the <label>, for the same reason the (i)
  // trigger does: a labelable button inside a <label> steals the control's
  // accessible name and swallows the click that should have focused the input.
  actions?: JSX.Element;
  // Label the field with the small-caps eyebrow instead of the form label style.
  // A blade field reads as a fact when it is not being edited (KVStacked, which
  // is eyebrow-labelled), so an eyebrow here is what keeps the label from
  // changing style the moment the pencil is clicked. Forms keep the default.
  eyebrow?: boolean;
  children: JSX.Element;
};

// A field either names one of the identity facts, taking its label from
// lib/entities, or carries a label of its own. The two are mutually exclusive,
// so "Name" cannot be typed onto a label field: on a create form the two
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
        {/*
          The provenance mark goes immediately after the label and BEFORE the
          (i), so a field that carries help text and one that does not put the
          dot at the same place, which is the position KVStacked's eyebrow can
          match. Like the (i) it sits outside the <label>: a labelable button in
          there steals the control's accessible name and kills the hover.
        */}
        <Show when={p.provenance}>
          {(from) => <ProvenanceDot label={label()} from={from()} />}
        </Show>
        <Show when={p.info}><InfoTip text={p.info!} label={label()} href={p.docHref} hrefText="Docs" /></Show>
      </span>
      <Show when={p.actions !== undefined} fallback={resolved()}>
        <div class="join w-full">
          {resolved()}
          {p.actions}
        </div>
      </Show>
      <Show when={p.hint}><span class="text-[11px] text-base-content/40">{p.hint}</span></Show>
    </div>
  );
}
