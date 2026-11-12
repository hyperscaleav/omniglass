import { For, Show, createEffect, createMemo, createSignal, useContext, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { Check, Pencil, Plus, Trash, X } from "./icons";
import { byLabel } from "../lib/entities";
import { COMPONENT_TYPES_KEY, listComponentTypes } from "../lib/component_types";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import {
  deleteStandardRole,
  deleteSystemRole,
  setStandardRole,
  setSystemRole,
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
// carries a label, an integer quorum, and TWO sets (accepted types,
// pinned products) rather than a single scalar. Parameterizing ContractEditor
// over both would have meant swapping its picker, its draft shape, its
// validation, and its whole row body, which is the component, so the honest
// move is a sibling that reads the same.
//
// The editing pieces are OWNER-GENERIC: a role is declared either on a standard
// or directly on a system (an ad hoc role, the only way a one-off system gets
// any), and the two share the declaration, the draft, and the controls; only
// the write calls differ, chosen by the RoleOwner. RoleEditor is the standard's
// whole panel; RolesPanel mounts RoleDeclareRow and RoleEditForm for a system's
// ad hoc roles, where the list itself is the resolved Roles panel's.
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
export type RoleDraft = {
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

export const emptyDraft = (): RoleDraft => ({
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
  "label",
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
    label: draft.display.trim() || undefined,
    accepted_types: draft.acceptedTypes,
    pinned_products: draft.pinnedProducts,
    impact: draft.impact,
    capacity,
    position_labels: draft.positionLabels,
  };
}

// The party a role is declared on: a standard (every conforming system inherits
// it) or a system (the role is its own).
export type RoleOwner = { kind: "standard"; id: string } | { kind: "system"; name: string };

// writeRole declares or revises the role on its owner.
function writeRole(owner: RoleOwner, role: string, spec: RoleSpec): Promise<DeclaredRole> {
  return owner.kind === "standard" ? setStandardRole(owner.id, role, spec) : setSystemRole(owner.name, role, spec);
}

// withdrawRole removes the role from its owner.
export function withdrawRole(owner: RoleOwner, role: string): Promise<void> {
  return owner.kind === "standard" ? deleteStandardRole(owner.id, role) : deleteSystemRole(owner.name, role);
}

// draftOf opens a declared role as a draft. Structural over the fields a
// standard's line and a system's resolved role share, so either feeds it.
export function draftOf(r: Pick<DeclaredRole, "label" | "quorum" | "impact" | "capacity" | "accepted_types" | "pinned_products" | "position_labels">): RoleDraft {
  return {
    display: r.label ?? "",
    quorum: String(r.quorum),
    acceptedTypes: [...(r.accepted_types ?? [])],
    pinnedProducts: [...(r.pinned_products ?? [])],
    impact: (r.impact as Impact) || "degraded",
    capacity: r.capacity != null ? String(r.capacity) : "",
    positionLabels: [...(r.position_labels ?? [])],
  };
}

// A generic picked-set editor: the picked names as removable chips, plus a
// picker over what is left in the given catalog. The set replaces the role's
// accepted-types or pinned-products set wholesale on save, so this is the
// whole set, not a delta.
function TokenSet(p: {
  picked: string[];
  options: { name: string; label: string }[];
  placeholder: string;
  emptyLabel: string;
  removeLabel: (v: string) => string;
  onChange: (next: string[]) => void;
}): JSX.Element {
  const left = createMemo(() =>
    [...p.options]
      .filter((o) => !p.picked.includes(o.name))
      .sort(byLabel),
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
          <For each={left()}>{(o) => <option value={o.name}>{o.label} ({o.name})</option>}</For>
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

// RoleDraftFields is the editable body of a role: the label, quorum, capacity,
// and impact on one line beside the caller's action buttons, then the two
// typed-slot sets and the position labels. Declaring and editing differ only in
// the accessible names and the buttons.
function RoleDraftFields(p: {
  draft: RoleDraft;
  onDraft: (next: RoleDraft) => void;
  names: { display: string; quorum: string; capacity: string; impact: string };
  actions: JSX.Element;
}): JSX.Element {
  const typeCatalog = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));
  const productCatalog = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
  return (
    <>
      <div class="flex items-center gap-2">
        <input
          class="input input-bordered input-sm min-w-0 flex-1"
          placeholder="name, e.g. Table microphone"
          aria-label={p.names.display}
          value={p.draft.display}
          onInput={(e) => p.onDraft({ ...p.draft, display: e.currentTarget.value })}
        />
        <input
          class="input input-bordered input-sm w-20 shrink-0 tnum"
          type="number"
          aria-label={p.names.quorum}
          value={p.draft.quorum}
          onInput={(e) => p.onDraft({ ...p.draft, quorum: e.currentTarget.value })}
        />
        <input
          class="input input-bordered input-sm w-20 shrink-0 tnum"
          type="number"
          placeholder="cap"
          aria-label={p.names.capacity}
          value={p.draft.capacity}
          onInput={(e) => p.onDraft({ ...p.draft, capacity: e.currentTarget.value })}
        />
        <select
          class="select select-bordered select-sm w-28 shrink-0"
          aria-label={p.names.impact}
          value={p.draft.impact}
          onChange={(e) => p.onDraft({ ...p.draft, impact: e.currentTarget.value as Impact })}
        >
          <For each={IMPACT_OPTIONS}>{(o) => <option value={o.value}>{o.label}</option>}</For>
        </select>
        {p.actions}
      </div>
      <TokenSet
        picked={p.draft.acceptedTypes}
        options={typeCatalog.data ?? []}
        placeholder="Accept a component type…"
        emptyLabel="any type"
        removeLabel={(c) => `Stop accepting ${c}`}
        onChange={(next) => p.onDraft({ ...p.draft, acceptedTypes: next })}
      />
      <TokenSet
        picked={p.draft.pinnedProducts}
        options={productCatalog.data ?? []}
        placeholder="Pin a product…"
        emptyLabel="any product of an accepted type"
        removeLabel={(c) => `Stop pinning ${c}`}
        onChange={(next) => p.onDraft({ ...p.draft, pinnedProducts: next })}
      />
      <LabelSet
        labels={p.draft.positionLabels}
        placeholder="Label a position, e.g. Left channel…"
        removeLabel={(v) => `Remove position label ${v}`}
        onChange={(next) => p.onDraft({ ...p.draft, positionLabels: next })}
      />
    </>
  );
}

const ErrorAlert = (p: { message: string | null }): JSX.Element => (
  <Show when={p.message}>
    <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{p.message}</span></div>
  </Show>
);

// RoleEditForm is the open edit of one declared role: its own draft, its own
// error (the server's refusal, verbatim), saved in place by a PATCH on the
// owner. onWritten refreshes whatever the host reads the roles from; onClose
// closes the form, on Cancel and after a successful save.
export function RoleEditForm(p: {
  owner: RoleOwner;
  role: Pick<DeclaredRole, "name" | "label" | "quorum" | "impact" | "capacity" | "accepted_types" | "pinned_products" | "position_labels">;
  onWritten: () => Promise<unknown>;
  onClose: () => void;
}): JSX.Element {
  const [draft, setDraft] = createSignal<RoleDraft>(draftOf(p.role));
  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);

  async function save() {
    setErr(null);
    const spec = buildSpec(draft());
    if (typeof spec === "string") { setErr(spec); return; }
    setBusy(true);
    try {
      await writeRole(p.owner, p.role.name, spec);
      await p.onWritten();
      p.onClose();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="flex flex-col gap-1.5">
      <RoleDraftFields
        draft={draft()}
        onDraft={setDraft}
        names={{
          display: `Name for ${p.role.name}`,
          quorum: `Quorum for ${p.role.name}`,
          capacity: `Capacity for ${p.role.name}`,
          impact: `Impact for ${p.role.name}`,
        }}
        actions={
          <>
            <Button square size="xs" intent="action" icon={Check} label={`Save ${p.role.name}`} title="Save" disabled={busy()} onClick={save} />
            <Button square size="xs" icon={X} label="Cancel role edit" title="Cancel" onClick={p.onClose} />
          </>
        }
      />
      <ErrorAlert message={err()} />
    </div>
  );
}

// RoleDeclareRow is the dashed add row: name the role, then the same fields an
// edit carries. The name is operator-invented (no catalog), and a refusal (a
// name the owner's standard already declares is a 409 on a system) reads inline
// here and leaves the draft as typed. Its drafts live as long as it is mounted,
// so a host that unmounts it on leaving edit mode drops them.
export function RoleDeclareRow(p: { owner: RoleOwner; onWritten: () => Promise<unknown> }): JSX.Element {
  const [name, setName] = createSignal("");
  const [draft, setDraft] = createSignal<RoleDraft>(emptyDraft());
  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);

  function reset() {
    setName("");
    setDraft(emptyDraft());
    setErr(null);
  }

  async function declare() {
    const role = name().trim();
    if (!role) return;
    setErr(null);
    const spec = buildSpec(draft());
    if (typeof spec === "string") { setErr(spec); return; }
    setBusy(true);
    try {
      await writeRole(p.owner, role, spec);
      await p.onWritten();
      reset();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="flex flex-col gap-1.5 rounded-box border border-dashed border-base-300 p-2.5">
      <input
        class="input input-bordered input-sm w-full font-data"
        placeholder="Declare a role: name it, e.g. table-mic…"
        aria-label="Role name"
        value={name()}
        onInput={(e) => setName(e.currentTarget.value)}
      />
      <Show when={name().trim()}>
        <RoleDraftFields
          draft={draft()}
          onDraft={setDraft}
          names={{
            display: "Name for the new role",
            quorum: "Quorum for the new role",
            capacity: "Capacity for the new role",
            impact: "Impact for the new role",
          }}
          actions={
            <>
              <Button square size="xs" intent="action" icon={Plus} label="Declare role" title="Declare" disabled={busy()} onClick={declare} />
              <Button square size="xs" icon={X} label="Cancel role declaration" title="Cancel" onClick={reset} />
            </>
          }
        />
      </Show>
      <ErrorAlert message={err()} />
    </div>
  );
}

export default function RoleEditor(props: { id: string; official: boolean }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const owner = (): RoleOwner => ({ kind: "standard", id: props.id });
  const key = () => standardRolesKey(props.id);
  const q = useQuery(() => ({
    queryKey: key(),
    queryFn: () => standardRoles(props.id),
    // Roles are edited inline; a background window-focus refetch would rebuild the
    // list and discard an in-progress edit.
    refetchOnWindowFocus: false,
  }));

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
  // The role whose row is open for editing (one at a time).
  const [editing, setEditing] = createSignal<string | null>(null);

  // Leaving blade edit mode (Save or Cancel alike) discards the open edit and
  // clears any stale error; see ContractEditor for the rationale (a draft
  // surviving Cancel resurrects on the next pencil press). Immediate-apply
  // writes mean an open draft at footer Save is dropped, never flushed. The add
  // row unmounts with canDeclare, taking its own draft with it.
  createEffect((was: boolean | undefined) => {
    const now = bladeEditing();
    if (was && !now) {
      setEditing(null);
      setErr(null);
    }
    return now;
  });

  const refresh = () => qc.invalidateQueries({ queryKey: key() });

  async function withdraw(role: string) {
    if (!confirm(`Withdraw "${role}" from this standard? Every conforming system loses it, and its assignments with it.`)) return;
    setBusy(true);
    setErr(null);
    try {
      await withdrawRole(owner(), role);
      await refresh();
      if (editing() === role) setEditing(null);
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
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
                    <Show when={r.label && r.label !== r.name}>
                      <span class="ml-2 text-[11px] text-base-content/50">{r.label}</span>
                    </Show>
                  </span>
                  <span class="badge badge-ghost badge-sm shrink-0 tnum">{r.quorum} wanted</span>
                  <span class="badge badge-ghost badge-sm shrink-0 capitalize">{r.impact} impact</span>
                  <Show when={canDeclare() && editing() !== r.name}>
                    <Button square size="xs" icon={Pencil} label={`Edit ${r.name}`} title="Edit" onClick={() => { setEditing(r.name); setErr(null); }} />
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
                  <RoleEditForm owner={owner()} role={r} onWritten={refresh} onClose={() => setEditing(null)} />
                </Show>
              </div>
            )}
          </For>
        </div>
      </Show>

      <Show when={canDeclare()}>
        <RoleDeclareRow owner={owner()} onWritten={refresh} />
      </Show>
    </div>
  );
}
