import { For, Show, createMemo, type JSX } from "solid-js";
import BladeField, { EMPTY_VALUE } from "./BladeField";
import TreeSelect from "./TreeSelect";
import { X } from "./icons";
import { entityLabel } from "../lib/entities";
import { bindableSystemNodes, type Member } from "../lib/members";
import type { BladeEdit } from "../lib/blades";

// MembershipsField (#767): a component's systems, on the component's own edit
// face. The create form offers a System, which is the component's primary
// membership; an assignment an operator made there must be changeable from the
// surface that made it, so the edit face carries the same field.
//
// Two parts, because the relation has two shapes: the PRIMARY (a picker over
// the systems the caller may write, cleared to take the component out of its
// primary system) and the OTHER memberships (chips, each removable). It stages
// drafts only; EntityForm's one Save turns them into the add / setPrimary /
// remove calls (lib/members planMembershipChanges), so a refusal surfaces in
// the form's own error line and leaves the face in edit.
//
// Read-only when `writable` is false (no system:update, or no system inside
// that scope): the memberships still read, and `why` says what to ask for.
export default function MembershipsField(props: {
  slot: BladeEdit;
  // The component's memberships as the server has them.
  members: () => Member[];
  // Every system the caller can read, for labels and for the picker.
  systems: () => { id: string; name: string; label?: string; parent_id?: string | null; actions?: string[] }[];
  writable: () => boolean;
  // Why the field is read-only, shown beneath it while editing.
  why: () => string | undefined;
  // The primary as drafted (the server's until the operator touches it).
  primary: () => string;
  onPrimary: (systemID: string) => void;
  // Other memberships the operator has taken out, staged until Save.
  removed: () => ReadonlySet<string>;
  onRemove: (systemID: string) => void;
}): JSX.Element {
  const labelOf = (m: { system: string; system_id?: string }) => {
    const hit = props.systems().find((s) => s.id === m.system_id);
    return hit ? entityLabel(hit) : m.system;
  };

  // The picker offers what the caller may write, plus the current primary even
  // when it is outside that scope, so the control can show what is there rather
  // than a blank that reads as "none".
  const items = createMemo(() => {
    const nodes = bindableSystemNodes(props.systems());
    const cur = props.members().find((m) => m.primary);
    if (cur?.system_id && !nodes.some((n) => n.id === cur.system_id)) {
      nodes.push({ id: cur.system_id, value: cur.system_id, label: labelOf(cur) });
    }
    return nodes;
  });
  const others = () => props.members().filter((m) => !m.primary && !props.removed().has(m.system_id));

  const read = (
    <Show
      when={props.members().length > 0}
      fallback={<span class="text-base-content/40">{EMPTY_VALUE}</span>}
    >
      <span class="flex flex-wrap items-center gap-1.5">
        <For each={[...props.members()].sort((a, b) => Number(b.primary) - Number(a.primary))}>
          {(m) => (
            <span class="badge badge-sm gap-1">
              {labelOf(m)}
              <Show when={m.primary}><span class="text-base-content/50">primary</span></Show>
            </span>
          )}
        </For>
      </span>
    </Show>
  );

  return (
    <BladeField
      label="Memberships"
      edit={props.slot}
      info="The systems this component serves. The primary is its default when no system is named; a shared device is in several. Changing them writes each system's membership, so it needs system:update."
      hint={props.slot.editing() ? props.why() : undefined}
      read={read}
      value={() => props.members().map((m) => m.system).join(", ")}
      children={
        props.slot.editing() ? (
          <Show when={props.writable()} fallback={read}>
            {/* The select first: FieldRow labels the first element it finds. */}
            <TreeSelect items={items()} value={props.primary()} onChange={props.onPrimary} rootLabel="None" />
            <Show when={others().length > 0}>
              {/* "also in", SystemZoom's word for the same relation, so the
                  select above reads as the primary rather than as one of many. */}
              <span class="flex flex-wrap items-center gap-1.5">
                <span class="text-xs text-base-content/60">also in</span>
                <For each={others()}>
                  {(m) => (
                    <span class="badge badge-sm gap-1">
                      {labelOf(m)}
                      <button type="button" class="cursor-pointer" aria-label={`Remove ${labelOf(m)}`} onClick={() => props.onRemove(m.system_id)}>
                        <X size={12} />
                      </button>
                    </span>
                  )}
                </For>
              </span>
            </Show>
          </Show>
        ) : undefined
      }
    />
  );
}
