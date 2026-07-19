import { Show, children, createUniqueId, type JSX } from "solid-js";
import InfoTip from "./InfoTip";

// FieldRow is the console's one form-field wrapper: a label above its control,
// with an optional (i) tooltip beside the label (and an optional docs link) and
// an optional hint below. The tooltip trigger sits OUTSIDE the <label> and the
// label associates to the control by id, so a labelable button never steals the
// control's accessible name. It is the edit-mode sibling of KVStacked (the
// read-only fact) and KVRow (the value row); the Variables / Secrets / TreeList
// blade forms all render their fields through it, so the field shape is defined
// once and cannot drift (it was reimplemented three times before).
export default function FieldRow(p: {
  label: string;
  // Tooltip text for the (i) affordance beside the label (optional).
  info?: string;
  // "Docs" link target inside the tooltip (optional; pairs with info).
  docHref?: string;
  // A hint rendered under the control (optional; distinct from the tooltip).
  hint?: string;
  children: JSX.Element;
}): JSX.Element {
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
    <div class="flex flex-col gap-1">
      <span class="flex items-center gap-1.5">
        <label class="text-[12px] font-medium text-base-content/70" for={control?.id ?? id}>{p.label}</label>
        <Show when={p.info}><InfoTip text={p.info!} label={p.label} href={p.docHref} hrefText="Docs" /></Show>
      </span>
      {resolved()}
      <Show when={p.hint}><span class="text-[11px] text-base-content/40">{p.hint}</span></Show>
    </div>
  );
}
