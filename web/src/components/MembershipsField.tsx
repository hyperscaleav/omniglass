import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import BladeField, { EMPTY_VALUE } from "./BladeField";
import Picker, { type PickerOption } from "./ui/Picker";
import { X } from "./icons";
import { entityLabel } from "../lib/entities";
import { bindableSystemNodes, type Member } from "../lib/members";
import type { BladeEdit } from "../lib/blades";

// MembershipsField (#767, #1804): a component's systems, on the component's own
// edit face. The create form offers a System, which is the component's primary
// membership; an assignment an operator made there must be changeable from the
// surface that made it, so the edit face carries the same field.
//
// Three explicit controls, because the relation has three different acts and a
// single select hid which one a pick performed (choosing another system in it
// ADDED a membership and silently made it primary):
//   * "Also serves..." adds a system: the component is then in both
//   * "Primary system" chooses which of the memberships is the default; it
//     only lists systems the component is in or has just been staged into
//   * each chip's remove drops one membership
// Moving a component from one system to another is its own action, "Move to...",
// in the Placement section (MoveEntity), never a side effect of these.
//
// It stages drafts only; EntityForm's one Save turns them into the add /
// setPrimary / remove calls (lib/members planMembershipChanges), so a refusal
// surfaces in the form's own error line and leaves the face in edit.
//
// Read-only when `writable` is false (no system:update, or no system inside
// that scope): the memberships still read, and `why` says what to ask for.
export default function MembershipsField(props: {
  slot: BladeEdit;
  // The component's memberships as the server has them.
  members: () => Member[];
  // Every system the caller can read, for labels and for the picker.
  systems: () => { id: string; name: string; label?: string; parent_id?: string | null; location_id?: string; actions?: string[] }[];
  // Where each system sits, for the picker's secondary text (tells two rooms
  // named alike apart); optional.
  placeOf?: (system: { id: string; location_id?: string }) => string | undefined;
  writable: () => boolean;
  // Why the field is read-only, shown beneath it while editing.
  why: () => string | undefined;
  // The primary as drafted (the server's until the operator touches it).
  primary: () => string;
  onPrimary: (systemID: string) => void;
  // Other memberships the operator has taken out, staged until Save.
  removed: () => ReadonlySet<string>;
  onRemove: (systemID: string) => void;
  // "Also serves...": systems staged to be added, until Save.
  added: () => ReadonlySet<string>;
  onAdd: (systemID: string) => void;
  onUnadd: (systemID: string) => void;
}): JSX.Element {
  const labelOf = (m: { system: string; system_id?: string }) => {
    const hit = props.systems().find((s) => s.id === m.system_id);
    return hit ? entityLabel(hit) : m.system;
  };
  const sysLabel = (id: string) => {
    const hit = props.systems().find((s) => s.id === id);
    return hit ? entityLabel(hit) : id;
  };

  // The memberships as drafted: the server's, less the ones taken out, plus the
  // ones staged in. These are the chips and the only choices for primary.
  const kept = () => props.members().filter((m) => !props.removed().has(m.system_id));
  const chips = createMemo(() => [
    ...kept().map((m) => ({ id: m.system_id, label: labelOf(m), staged: false })),
    ...[...props.added()].map((id) => ({ id, label: sysLabel(id), staged: true })),
  ]);
  const primaryId = () => {
    const want = props.primary();
    return chips().some((c) => c.id === want) ? want : "";
  };
  // What "Also serves..." offers: the systems the caller may write that the
  // component is not already in (or staged into).
  const alsoOptions = createMemo<PickerOption[]>(() => {
    const here = new Set(chips().map((c) => c.id));
    return bindableSystemNodes(props.systems())
      .filter((n) => !here.has(n.id))
      .map((n) => {
        const sys = props.systems().find((s) => s.id === n.id);
        return { value: n.id, label: n.label, detail: sys ? props.placeOf?.(sys) : undefined };
      });
  });

  // The picker adds and is done: it remounts after each pick so it reads empty
  // again instead of holding the text that was typed to find the system.
  const [pickN, setPickN] = createSignal(0);

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
      info="The systems this component serves. The primary is its default when no system is named; a shared device is in several. Also serves adds a system and keeps the others; Move to (under Placement) changes which system it is in. Changing them writes each system's membership, so it needs system:update."
      hint={props.slot.editing() ? props.why() : undefined}
      read={read}
      value={() => props.members().map((m) => m.system).join(", ")}
      children={
        props.slot.editing() ? (
          <Show when={props.writable()} fallback={read}>
            <div class="flex flex-col gap-2">
              <Show when={chips().length > 0} fallback={<span class="text-xs text-base-content/60">In no system.</span>}>
                <span class="flex flex-wrap items-center gap-1.5">
                  <For each={chips()}>
                    {(c) => (
                      <span class="badge badge-sm gap-1" classList={{ "badge-outline": c.staged }}>
                        {c.label}
                        <Show when={c.id === primaryId()}><span class="text-base-content/50">primary</span></Show>
                        <Show when={c.staged}><span class="text-base-content/50">new</span></Show>
                        <button type="button" class="cursor-pointer" aria-label={`Remove ${c.label}`} onClick={() => (c.staged ? props.onUnadd(c.id) : props.onRemove(c.id))}>
                          <X size={12} />
                        </button>
                      </span>
                    )}
                  </For>
                </span>
              </Show>
              <For each={[pickN()]}>
                {() => (
                  <Picker
                    aria-label="Also serves..."
                    placeholder="Also serves..."
                    options={alsoOptions()}
                    value={null}
                    onChange={(id) => {
                      if (!id) return;
                      props.onAdd(id);
                      setPickN((n) => n + 1);
                    }}
                  />
                )}
              </For>
              <Show when={chips().length > 1}>
                <label class="flex items-center gap-2 text-xs text-base-content/60">
                  Primary system
                  <select
                    class="select select-bordered select-sm"
                    value={primaryId()}
                    onChange={(e) => props.onPrimary(e.currentTarget.value)}
                  >
                    <For each={chips()}>{(c) => <option value={c.id} selected={c.id === primaryId()}>{c.label}</option>}</For>
                  </select>
                </label>
              </Show>
            </div>
          </Show>
        ) : undefined
      }
    />
  );
}
