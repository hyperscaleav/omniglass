import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { Check, Pencil, Plus, Trash, X } from "./icons";
import { CAPABILITIES_KEY, listCapabilities } from "../lib/capabilities";
import {
  deleteStandardRole,
  setStandardRole,
  standardRoles,
  standardRolesKey,
  type DeclaredRole,
  type RoleSpec,
} from "../lib/system_roles";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";

// RoleEditor is the standard detail-blade panel for curating the ROLES a standard
// declares: the slots every conforming system needs filled (a table microphone, a
// main display), each naming the capabilities a component must all provide to fill
// it and how many the slot wants (its quorum). Systems inherit these live, so a
// role declared here appears on every conforming system's Roles panel at once, and
// withdrawing one takes every assignment made to it with it.
//
// It is a SIBLING of ContractEditor, not a reuse of it: the two share the shape
// (declare / edit in place / withdraw, immediate writes, no Save of its own, and
// an official classifier read-only) but not the row. A contract line picks an
// existing catalog property by name and carries one typed default plus a required
// flag; a role's name is operator-invented (no catalog to pick from), and it
// carries a display name, an integer quorum, and a SET of capabilities rather than
// a single scalar. Parameterizing ContractEditor over both would have meant
// swapping its picker, its draft shape, its validation, and its whole row body,
// which is the component, so the honest move is a sibling that reads the same.
//
// Each role is addressed by name, so a write is a PUT (idempotent: an edit revises
// the role in place) and a withdraw is a DELETE. Declaring needs the standard's
// :update, withdrawing its :delete, and an official (seed-owned) standard's roles
// are read-only: the list renders, the controls do not.

// The draft a row (or the add row) edits: everything a RoleSpec carries.
type RoleDraft = { display: string; quorum: string; capabilities: string[] };

const emptyDraft = (): RoleDraft => ({ display: "", quorum: "1", capabilities: [] });

// buildSpec coerces the draft into the write body. A blank quorum reads as one
// (the server's own default), and a quorum that is not a positive whole number is
// reported rather than sent malformed.
export function buildSpec(draft: RoleDraft): RoleSpec | string {
  const text = draft.quorum.trim();
  const quorum = text === "" ? 1 : Number(text);
  if (!Number.isInteger(quorum) || quorum < 1) return `"${text}" is not a whole number of components.`;
  return { quorum, display_name: draft.display.trim() || undefined, capabilities: draft.capabilities };
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
  const catalog = useQuery(() => ({ queryKey: CAPABILITIES_KEY, queryFn: listCapabilities }));

  const rows = createMemo<DeclaredRole[]>(() => [...(q.data ?? [])].sort((a, b) => a.name.localeCompare(b.name)));

  // An official standard is seed-owned; declaring is its own :update, withdrawing
  // its :delete, as the server gates them.
  const canDeclare = () => !props.official && can(me.data, "standard", "update");
  const canWithdraw = () => !props.official && can(me.data, "standard", "delete");

  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);
  // The role whose row is open for editing (one at a time), and its draft.
  const [editing, setEditing] = createSignal<string | null>(null);
  const [draft, setDraft] = createSignal<RoleDraft>(emptyDraft());
  // The add row's draft: the invented name, plus the same fields.
  const [addName, setAddName] = createSignal("");
  const [addDraft, setAddDraft] = createSignal<RoleDraft>(emptyDraft());

  function openEdit(r: DeclaredRole) {
    setEditing(r.name);
    setDraft({ display: r.display_name ?? "", quorum: String(r.quorum), capabilities: [...(r.capabilities ?? [])] });
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

  // The capability set editor: the picked ids as removable chips, plus a picker
  // over what is left in the registry. The set replaces the role's requirement
  // wholesale on save, so this is the whole requirement, not a delta.
  function CapabilitySet(p: { picked: string[]; label: string; onChange: (next: string[]) => void }): JSX.Element {
    const left = createMemo(() =>
      [...(catalog.data ?? [])]
        .filter((c) => !p.picked.includes(c.id))
        .sort((a, b) => a.display_name.localeCompare(b.display_name)),
    );
    return (
      <div class="flex flex-col gap-1.5">
        <div class="flex flex-wrap items-center gap-1.5">
          <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">requires</span>
          <Show when={p.picked.length} fallback={<span class="text-[11px] italic text-base-content/40">nothing yet</span>}>
            <For each={p.picked}>
              {(c) => (
                <span class="badge badge-outline badge-sm gap-1 font-data">
                  {c}
                  <button
                    type="button"
                    class="ml-0.5 inline-flex opacity-60 hover:opacity-100"
                    aria-label={`Stop requiring ${c}`}
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
            aria-label={p.label}
            value=""
            onChange={(e) => { const v = e.currentTarget.value; if (v) p.onChange([...p.picked, v]); e.currentTarget.value = ""; }}
          >
            <option value="">Require a capability…</option>
            <For each={left()}>{(c) => <option value={c.id}>{c.display_name} ({c.id})</option>}</For>
          </select>
        </Show>
      </div>
    );
  }

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">Declared roles</span>
        <span class="shrink-0 text-[10.5px] text-base-content/40">
          {props.official ? "seed-owned roles, read-only" : "the standard's roles"}
        </span>
      </div>
      <p class="text-[11px] text-base-content/50">
        A role is a slot every system conforming to this standard needs filled. A component may fill one only if it
        provides every capability the role requires.
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
                  <Show when={canDeclare() && editing() !== r.name}>
                    <Button square size="xs" icon={Pencil} label={`Edit ${r.name}`} title="Edit" onClick={() => openEdit(r)} />
                  </Show>
                  <Show when={canWithdraw()}>
                    <Button square size="xs" icon={Trash} label={`Withdraw ${r.name}`} title="Withdraw" disabled={busy()} onClick={() => withdraw(r.name)} />
                  </Show>
                </div>

                <Show
                  when={editing() === r.name}
                  fallback={
                    <div class="flex flex-wrap items-center gap-1.5">
                      <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">requires</span>
                      <Show
                        when={(r.capabilities ?? []).length}
                        fallback={<span class="text-[11px] italic text-base-content/40">nothing: any component can fill it</span>}
                      >
                        <For each={r.capabilities ?? []}>{(c) => <span class="badge badge-ghost badge-sm font-data">{c}</span>}</For>
                      </Show>
                    </div>
                  }
                >
                  <div class="flex flex-col gap-1.5">
                    <div class="flex items-center gap-2">
                      <input
                        class="input input-bordered input-sm min-w-0 flex-1"
                        placeholder="display name"
                        aria-label={`Display name for ${r.name}`}
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
                      <Button square size="xs" intent="action" icon={Check} label={`Save ${r.name}`} title="Save" disabled={busy()} onClick={() => saveEdit(r)} />
                      <Button square size="xs" icon={X} label="Cancel role edit" title="Cancel" onClick={() => setEditing(null)} />
                    </div>
                    <CapabilitySet
                      picked={draft().capabilities}
                      label={`Capability to require for ${r.name}`}
                      onChange={(next) => setDraft({ ...draft(), capabilities: next })}
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
                placeholder="display name, e.g. Table microphone"
                aria-label="Display name for the new role"
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
              <Button square size="xs" intent="action" icon={Plus} label="Declare role" title="Declare" disabled={busy()} onClick={declare} />
              <Button square size="xs" icon={X} label="Cancel role declaration" title="Cancel" onClick={resetAdd} />
            </div>
            <CapabilitySet
              picked={addDraft().capabilities}
              label="Capability to require"
              onChange={(next) => setAddDraft({ ...addDraft(), capabilities: next })}
            />
          </Show>
        </div>
      </Show>
    </div>
  );
}
