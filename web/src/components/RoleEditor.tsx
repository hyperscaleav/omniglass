import { For, Show, createEffect, createMemo, createSignal, useContext, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { Check, Pencil, Plus, Trash, X } from "./icons";
import { COMPONENT_TYPES_KEY, listComponentTypes } from "../lib/component_types";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import {
  deleteStandardRole,
  setStandardRole,
  standardRoles,
  standardRolesKey,
  type DeclaredRole,
  type RoleSpec,
} from "../lib/system_roles";
import { useMe, can } from "../lib/auth";
import { BladeEditContext } from "../lib/blades";
import { describeError } from "../lib/format";

// RoleEditor is the standard detail-blade panel for curating the ROLES a standard
// declares: the slots every conforming system needs filled (a table microphone, a
// main display), each naming the TYPED-SLOT guard a filling component's product
// must clear (#626) and how many the slot wants (its quorum). Systems inherit
// these live, so a role declared here appears on every conforming system's Roles
// panel at once, and withdrawing one takes every assignment made to it with it.
//
// It is a SIBLING of ContractEditor, not a reuse of it: the two share the shape
// (declare / edit in place / withdraw, immediate writes, no Save of its own, and
// an official classifier read-only) but not the row. A contract line picks an
// existing catalog property by name and carries one typed default plus a required
// flag; a role's name is operator-invented (no catalog to pick from), and it
// carries a display name, an integer quorum, and TWO sets (accepted types,
// pinned products) rather than a single scalar. Parameterizing ContractEditor
// over both would have meant swapping its picker, its draft shape, its
// validation, and its whole row body, which is the component, so the honest
// move is a sibling that reads the same.
//
// Each role is addressed by name, so a write is a PATCH (idempotent: an edit
// revises the role in place, and declaring is the same route) and a withdraw is
// a DELETE. Writes are immediate, but the blade's edit slot still gates the
// controls (#621): they render only while the
// hosting blade is in edit mode, and in read mode the declared roles render as
// facts alone. Declaring needs the standard's :update, withdrawing its :delete,
// and an official standard's roles are read-only: the list renders,
// the controls do not.

// A role's impact, as the draft edits it: what an impaired role means for its
// system's verdict. Kept a plain string in the draft (a <select> value) and
// narrowed only where RoleSpec needs the literal union.
type Impact = "outage" | "degraded" | "none";

// The draft a row (or the add row) edits: everything a RoleSpec carries. Every
// field the write body accepts belongs here; a field added to RoleSpec but not
// here is silently cleared on every unrelated edit, which is exactly the bug
// this type used to carry (#626, predates the epic: impact, capacity, and
// position_labels were all missing until this commit, see build-log.md).
type RoleDraft = {
  display: string;
  quorum: string;
  acceptedTypes: string[];
  pinnedProducts: string[];
  impact: Impact;
  // Blank means unbounded, never zero: buildSpec omits the field rather than
  // sending a coerced number, and the mask it names turns that omission into a
  // clear (#638), so blanking the box is how an operator lifts a cap.
  capacity: string;
  positionLabels: string[];
};

const emptyDraft = (): RoleDraft => ({
  display: "",
  quorum: "1",
  acceptedTypes: [],
  pinnedProducts: [],
  impact: "degraded",
  capacity: "",
  positionLabels: [],
});

// What an impaired role means for its system: the same three values the
// health chain folds worst-wins.
const IMPACT_OPTIONS: { value: Impact; label: string }[] = [
  { value: "outage", label: "Outage" },
  { value: "degraded", label: "Degraded" },
  { value: "none", label: "None" },
];

// EDITOR_FIELDS is what this editor owns, and therefore exactly what its save
// declares in the update mask (#666). Naming them makes the save say what it
// means in both directions: a field the operator EMPTIED (the last pinned
// product, a capacity, the labels) is written empty rather than read as
// "unchanged", and a field the editor has no control for is left off the mask
// and so cannot be touched at all. alternate is deliberately absent: it has a
// read (#640) but no control here, and naming it would clear a role's alternate
// on an unrelated edit, which is the #626 defect this list exists to prevent.
const EDITOR_FIELDS = [
  "display_name",
  "quorum",
  "capacity",
  "position_labels",
  "accepted_types",
  "pinned_products",
  "impact",
] as const;

// buildSpec coerces the draft into the write body. A blank quorum reads as one
// (the server's own default), and a quorum or capacity that is not a positive
// whole number is reported rather than sent malformed. A blank capacity is a
// capacity CLEARED, since the mask names the field and the body carries no
// value for it, which is the one thing the old wholesale PUT could not express.
export function buildSpec(draft: RoleDraft): RoleSpec | string {
  const text = draft.quorum.trim();
  const quorum = text === "" ? 1 : Number(text);
  if (!Number.isInteger(quorum) || quorum < 1) return `"${text}" is not a whole number of components.`;
  const capText = draft.capacity.trim();
  let capacity: number | undefined;
  if (capText !== "") {
    capacity = Number(capText);
    if (!Number.isInteger(capacity) || capacity < 1) return `"${capText}" is not a whole number of components.`;
  }
  return {
    update_mask: [...EDITOR_FIELDS],
    quorum,
    display_name: draft.display.trim() || undefined,
    accepted_types: draft.acceptedTypes,
    pinned_products: draft.pinnedProducts,
    impact: draft.impact,
    capacity,
    position_labels: draft.positionLabels,
  };
}

export default function RoleEditor(props: { id: string; official: boolean }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const key = () => standardRolesKey(props.id);
  const q = useQuery(() => ({
    queryKey: key(),
    queryFn: () => standardRoles(props.id),
    // Roles are edited inline; a background window-focus refetch would rebuild the
    // list and discard an in-progress edit.
    refetchOnWindowFocus: false,
  }));
  const typeCatalog = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));
  const productCatalog = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));

  const rows = createMemo<DeclaredRole[]>(() => [...(q.data ?? [])].sort((a, b) => a.name.localeCompare(b.name)));

  // The hosting blade's edit slot, read from the ambient context: the panel's
  // controls are edit-state affordances, rendered only while the blade is in
  // edit mode (the pencil is the one route to them). Outside any blade context
  // the panel renders read-only, the same rule BladeField follows.
  const blade = useContext(BladeEditContext);
  const bladeEditing = () => !!blade?.editing();
  // An official standard is release-owned; declaring is its own :update, withdrawing
  // its :delete, as the server gates them.
  const canDeclare = () => bladeEditing() && !props.official && can(me.data, "standard", "update");
  const canWithdraw = () => bladeEditing() && !props.official && can(me.data, "standard", "delete");

  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);
  // The role whose row is open for editing (one at a time), and its draft.
  const [editing, setEditing] = createSignal<string | null>(null);
  const [draft, setDraft] = createSignal<RoleDraft>(emptyDraft());
  // The add row's draft: the invented name, plus the same fields.
  const [addName, setAddName] = createSignal("");
  const [addDraft, setAddDraft] = createSignal<RoleDraft>(emptyDraft());

  // Leaving blade edit mode (Save or Cancel alike) discards the open drafts
  // and clears any stale error; see ContractEditor for the rationale (a draft
  // surviving Cancel resurrects on the next pencil press). Immediate-apply
  // writes mean an open draft at footer Save is dropped, never flushed.
  createEffect((was: boolean | undefined) => {
    const now = bladeEditing();
    if (was && !now) {
      setEditing(null);
      resetAdd();
      setErr(null);
    }
    return now;
  });

  function openEdit(r: DeclaredRole) {
    setEditing(r.name);
    setDraft({
      display: r.display_name ?? "",
      quorum: String(r.quorum),
      acceptedTypes: [...(r.accepted_types ?? [])],
      pinnedProducts: [...(r.pinned_products ?? [])],
      impact: (r.impact as Impact) || "degraded",
      capacity: r.capacity != null ? String(r.capacity) : "",
      positionLabels: [...(r.position_labels ?? [])],
    });
    setErr(null);
  }

  function resetAdd() {
    setAddName("");
    setAddDraft(emptyDraft());
  }

  async function write(role: string, spec: RoleSpec, after: () => void) {
    setBusy(true);
    setErr(null);
    try {
      await setStandardRole(props.id, role, spec);
      await qc.invalidateQueries({ queryKey: key() });
      after();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  async function saveEdit(r: DeclaredRole) {
    setErr(null);
    const spec = buildSpec(draft());
    if (typeof spec === "string") { setErr(spec); return; }
    await write(r.name, spec, () => setEditing(null));
  }

  async function declare() {
    const name = addName().trim();
    if (!name) return;
    setErr(null);
    const spec = buildSpec(addDraft());
    if (typeof spec === "string") { setErr(spec); return; }
    await write(name, spec, resetAdd);
  }

  async function withdraw(role: string) {
    if (!confirm(`Withdraw "${role}" from this standard? Every conforming system loses it, and its assignments with it.`)) return;
    setBusy(true);
    setErr(null);
    try {
      await deleteStandardRole(props.id, role);
      await qc.invalidateQueries({ queryKey: key() });
      if (editing() === role) setEditing(null);
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  // A generic picked-set editor: the picked names as removable chips, plus a
  // picker over what is left in the given catalog. The set replaces the role's
  // accepted-types or pinned-products set wholesale on save, so this is the
  // whole set, not a delta.
  function TokenSet(p: {
    picked: string[];
    options: { name: string; display_name: string }[];
    placeholder: string;
    emptyLabel: string;
    removeLabel: (v: string) => string;
    onChange: (next: string[]) => void;
  }): JSX.Element {
    const left = createMemo(() =>
      [...p.options]
        .filter((o) => !p.picked.includes(o.name))
        .sort((a, b) => a.display_name.localeCompare(b.display_name)),
    );
    return (
      <div class="flex flex-col gap-1.5">
        <div class="flex flex-wrap items-center gap-1.5">
          <Show when={p.picked.length} fallback={<span class="text-[11px] italic text-base-content/40">{p.emptyLabel}</span>}>
            <For each={p.picked}>
              {(c) => (
                <span class="badge badge-outline badge-sm gap-1 font-data">
                  {c}
                  <button
                    type="button"
                    class="ml-0.5 inline-flex opacity-60 hover:opacity-100"
                    aria-label={p.removeLabel(c)}
                    onClick={() => p.onChange(p.picked.filter((x) => x !== c))}
                  >
                    <X size={11} />
                  </button>
                </span>
              )}
            </For>
          </Show>
        </div>
        <Show when={left().length}>
          <select
            class="select select-bordered select-sm w-full"
            aria-label={p.placeholder}
            value=""
            onChange={(e) => { const v = e.currentTarget.value; if (v) p.onChange([...p.picked, v]); e.currentTarget.value = ""; }}
          >
            <option value="">{p.placeholder}</option>
            <For each={left()}>{(o) => <option value={o.name}>{o.display_name} ({o.name})</option>}</For>
          </select>
        </Show>
      </div>
    );
  }

  // A freeform, position-ordered sibling of TokenSet: labels typed rather than
  // picked from a catalog, one per position (index 0 is position 1). Replaces
  // the set wholesale on save, same as the two catalog-backed sets above.
  function LabelSet(p: { labels: string[]; placeholder: string; removeLabel: (v: string) => string; onChange: (next: string[]) => void }): JSX.Element {
    const [text, setText] = createSignal("");
    const add = () => {
      const v = text().trim();
      if (!v) return;
      p.onChange([...p.labels, v]);
      setText("");
    };
    return (
      <div class="flex flex-col gap-1.5">
        <div class="flex flex-wrap items-center gap-1.5">
          <Show when={p.labels.length} fallback={<span class="text-[11px] italic text-base-content/40">unlabeled positions</span>}>
            <For each={p.labels}>
              {(v, i) => (
                <span class="badge badge-outline badge-sm gap-1">
                  {i() + 1}. {v}
                  <button
                    type="button"
                    class="ml-0.5 inline-flex opacity-60 hover:opacity-100"
                    aria-label={p.removeLabel(v)}
                    onClick={() => p.onChange(p.labels.filter((_, j) => j !== i()))}
                  >
                    <X size={11} />
                  </button>
                </span>
              )}
            </For>
          </Show>
        </div>
        <div class="flex items-center gap-1.5">
          <input
            class="input input-bordered input-sm min-w-0 flex-1"
            placeholder={p.placeholder}
            aria-label={p.placeholder}
            value={text()}
            onInput={(e) => setText(e.currentTarget.value)}
            onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); add(); } }}
          />
          <Button square size="xs" icon={Plus} label="Add position label" title="Add" onClick={add} />
        </div>
      </div>
    );
  }

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">Declared roles</span>
        <span class="shrink-0 text-[10.5px] text-base-content/40">
          {props.official ? "official roles, read-only" : "the standard's roles"}
        </span>
      </div>
      <p class="text-[11px] text-base-content/50">
        A role is a slot every system conforming to this standard needs filled. A component may fill one only if its
        product's type falls within an accepted type (any type, if none are named), and, if products are pinned, only
        if its product is one of them.
      </p>

      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{err()}</span></div>
      </Show>
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{describeError(q.error)}</span></div>
      </Show>

      <Show when={!q.isLoading && !q.error && !rows().length}>
        <p class="text-sm text-base-content/50">This standard declares no roles.</p>
      </Show>

      <Show when={rows().length}>
        <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={rows()}>
            {(r) => (
              <div class="flex flex-col gap-1.5 px-3 py-2">
                <div class="flex items-center gap-2">
                  <span class="min-w-0 flex-1 truncate">
                    <span class="font-data text-sm">{r.name}</span>
                    <Show when={r.display_name && r.display_name !== r.name}>
                      <span class="ml-2 text-[11px] text-base-content/50">{r.display_name}</span>
                    </Show>
                  </span>
                  <span class="badge badge-ghost badge-sm shrink-0 tnum">{r.quorum} wanted</span>
                  <span class="badge badge-ghost badge-sm shrink-0 capitalize">{r.impact} impact</span>
                  <Show when={canDeclare() && editing() !== r.name}>
                    <Button square size="xs" icon={Pencil} label={`Edit ${r.name}`} title="Edit" onClick={() => openEdit(r)} />
                  </Show>
                  <Show when={canWithdraw()}>
                    <Button square size="xs" icon={Trash} label={`Withdraw ${r.name}`} title="Withdraw" disabled={busy()} onClick={() => withdraw(r.name)} />
                  </Show>
                </div>

                {/* canDeclare in the condition collapses a role left open when
                    the blade drops out of edit mode. */}
                <Show
                  when={canDeclare() && editing() === r.name}
                  fallback={
                    <div class="flex flex-col gap-1">
                      <div class="flex flex-wrap items-center gap-1.5">
                        <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">accepts</span>
                        <Show
                          when={(r.accepted_types ?? []).length}
                          fallback={<span class="text-[11px] italic text-base-content/40">any type</span>}
                        >
                          <For each={r.accepted_types ?? []}>{(c) => <span class="badge badge-ghost badge-sm font-data">{c}</span>}</For>
                        </Show>
                      </div>
                      <Show when={(r.pinned_products ?? []).length}>
                        <div class="flex flex-wrap items-center gap-1.5">
                          <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">pinned to</span>
                          <For each={r.pinned_products ?? []}>{(c) => <span class="badge badge-ghost badge-sm font-data">{c}</span>}</For>
                        </div>
                      </Show>
                    </div>
                  }
                >
                  <div class="flex flex-col gap-1.5">
                    <div class="flex items-center gap-2">
                      <input
                        class="input input-bordered input-sm min-w-0 flex-1"
                        placeholder="name"
                        aria-label={`Name for ${r.name}`}
                        value={draft().display}
                        onInput={(e) => setDraft({ ...draft(), display: e.currentTarget.value })}
                      />
                      <input
                        class="input input-bordered input-sm w-20 shrink-0 tnum"
                        type="number"
                        min="1"
                        aria-label={`Quorum for ${r.name}`}
                        value={draft().quorum}
                        onInput={(e) => setDraft({ ...draft(), quorum: e.currentTarget.value })}
                      />
                      <input
                        class="input input-bordered input-sm w-20 shrink-0 tnum"
                        type="number"
                        min="1"
                        placeholder="cap"
                        aria-label={`Capacity for ${r.name}`}
                        value={draft().capacity}
                        onInput={(e) => setDraft({ ...draft(), capacity: e.currentTarget.value })}
                      />
                      <select
                        class="select select-bordered select-sm w-28 shrink-0"
                        aria-label={`Impact for ${r.name}`}
                        value={draft().impact}
                        onChange={(e) => setDraft({ ...draft(), impact: e.currentTarget.value as Impact })}
                      >
                        <For each={IMPACT_OPTIONS}>{(o) => <option value={o.value}>{o.label}</option>}</For>
                      </select>
                      <Button square size="xs" intent="action" icon={Check} label={`Save ${r.name}`} title="Save" disabled={busy()} onClick={() => saveEdit(r)} />
                      <Button square size="xs" icon={X} label="Cancel role edit" title="Cancel" onClick={() => setEditing(null)} />
                    </div>
                    <TokenSet
                      picked={draft().acceptedTypes}
                      options={typeCatalog.data ?? []}
                      placeholder="Accept a component type…"
                      emptyLabel="any type"
                      removeLabel={(c) => `Stop accepting ${c}`}
                      onChange={(next) => setDraft({ ...draft(), acceptedTypes: next })}
                    />
                    <TokenSet
                      picked={draft().pinnedProducts}
                      options={productCatalog.data ?? []}
                      placeholder="Pin a product…"
                      emptyLabel="any product of an accepted type"
                      removeLabel={(c) => `Stop pinning ${c}`}
                      onChange={(next) => setDraft({ ...draft(), pinnedProducts: next })}
                    />
                    <LabelSet
                      labels={draft().positionLabels}
                      placeholder="Label a position, e.g. Left channel…"
                      removeLabel={(v) => `Remove position label ${v}`}
                      onChange={(next) => setDraft({ ...draft(), positionLabels: next })}
                    />
                  </div>
                </Show>
              </div>
            )}
          </For>
        </div>
      </Show>

      <Show when={canDeclare()}>
        <div class="flex flex-col gap-1.5 rounded-box border border-dashed border-base-300 p-2.5">
          <input
            class="input input-bordered input-sm w-full font-data"
            placeholder="Declare a role: name it, e.g. table-mic…"
            aria-label="Role name"
            value={addName()}
            onInput={(e) => setAddName(e.currentTarget.value)}
          />
          <Show when={addName().trim()}>
            <div class="flex items-center gap-2">
              <input
                class="input input-bordered input-sm min-w-0 flex-1"
                placeholder="name, e.g. Table microphone"
                aria-label="Name for the new role"
                value={addDraft().display}
                onInput={(e) => setAddDraft({ ...addDraft(), display: e.currentTarget.value })}
              />
              <input
                class="input input-bordered input-sm w-20 shrink-0 tnum"
                type="number"
                min="1"
                aria-label="Quorum for the new role"
                value={addDraft().quorum}
                onInput={(e) => setAddDraft({ ...addDraft(), quorum: e.currentTarget.value })}
              />
              <input
                class="input input-bordered input-sm w-20 shrink-0 tnum"
                type="number"
                min="1"
                placeholder="cap"
                aria-label="Capacity for the new role"
                value={addDraft().capacity}
                onInput={(e) => setAddDraft({ ...addDraft(), capacity: e.currentTarget.value })}
              />
              <select
                class="select select-bordered select-sm w-28 shrink-0"
                aria-label="Impact for the new role"
                value={addDraft().impact}
                onChange={(e) => setAddDraft({ ...addDraft(), impact: e.currentTarget.value as Impact })}
              >
                <For each={IMPACT_OPTIONS}>{(o) => <option value={o.value}>{o.label}</option>}</For>
              </select>
              <Button square size="xs" intent="action" icon={Plus} label="Declare role" title="Declare" disabled={busy()} onClick={declare} />
              <Button square size="xs" icon={X} label="Cancel role declaration" title="Cancel" onClick={resetAdd} />
            </div>
            <TokenSet
              picked={addDraft().acceptedTypes}
              options={typeCatalog.data ?? []}
              placeholder="Accept a component type…"
              emptyLabel="any type"
              removeLabel={(c) => `Stop accepting ${c}`}
              onChange={(next) => setAddDraft({ ...addDraft(), acceptedTypes: next })}
            />
            <TokenSet
              picked={addDraft().pinnedProducts}
              options={productCatalog.data ?? []}
              placeholder="Pin a product…"
              emptyLabel="any product of an accepted type"
              removeLabel={(c) => `Stop pinning ${c}`}
              onChange={(next) => setAddDraft({ ...addDraft(), pinnedProducts: next })}
            />
            <LabelSet
              labels={addDraft().positionLabels}
              placeholder="Label a position, e.g. Left channel…"
              removeLabel={(v) => `Remove position label ${v}`}
              onChange={(next) => setAddDraft({ ...addDraft(), positionLabels: next })}
            />
          </Show>
        </div>
      </Show>
    </div>
  );
}
