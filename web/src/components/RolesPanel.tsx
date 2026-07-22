import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { createStore } from "solid-js/store";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { Check, X } from "./icons";
import { describeError } from "../lib/format";
import { COMPONENTS_KEY, listComponents, type Component as Comp } from "../lib/components";
import {
  assignRole,
  staffingLabel,
  systemRoles,
  systemRolesKey,
  unassignRole,
  type EffectiveRole,
} from "../lib/system_roles";

// RolesPanel lists the roles a system needs filled, resolved. A ROLE is a slot:
// a table microphone, a main display. It names the CAPABILITIES a component must
// all provide to fill it and carries a QUORUM, how many components it wants, so a
// role reads as "2 wanted, 1 assigned" and is UNDERSTAFFED until it has them.
//
// A role reaches a system two ways, and the panel keeps them apart exactly as the
// Properties panel keeps contract values apart from off-contract ones, because
// operators have already learned that distinction here: the roles the system's
// STANDARD declares sit in the solid group (every conforming system inherits them
// live, and they are withdrawn on the standard, not here), and the roles declared
// directly on this system sit in the dashed AD HOC group (a one-off system that
// conforms to no standard has only those).
//
// Assigning is the guarded write, and the refusal is the lesson: the server checks
// the component's RESOLVED capabilities (its product's, plus what it adds, minus
// what it suppresses) and refuses with a message naming the gap, which this panel
// shows verbatim against the role that refused. A generic "something went wrong"
// would throw away the only thing the operator needs.
//
// Writes are immediate, like the tag panel, so the panel has no Save of its own;
// the caller passes canUpdate (the system detail computes it as "in edit mode AND
// holding system:update"), which keeps view read-only per the console invariant.

export default function RolesPanel(props: { system: string; canUpdate: boolean }): JSX.Element {
  const qc = useQueryClient();
  const key = () => systemRolesKey(props.system);
  const q = useQuery(() => ({
    queryKey: key(),
    queryFn: () => systemRoles(props.system),
    refetchOnWindowFocus: false,
  }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));

  const roles = createMemo<EffectiveRole[]>(() => q.data ?? []);
  const inherited = createMemo(() => roles().filter((r) => r.from_standard));
  const adhoc = createMemo(() => roles().filter((r) => !r.from_standard));

  // Per-role write state: the picked component, and the server's last refusal.
  const [picked, setPicked] = createStore<Record<string, string>>({});
  const [errs, setErrs] = createStore<Record<string, string | undefined>>({});
  const [busy, setBusy] = createSignal(false);

  // The components a role can be offered, by label: those already filling it are
  // dropped, and the ones in this system lead, since that is where an operator
  // staffs from. Everything else is still offered, because the capability guard
  // (not the placement) is what decides, and its refusal is what teaches.
  const label = (c: Comp) => c.display_name || c.name;
  const candidates = (role: EffectiveRole): Comp[] => {
    const taken = new Set(role.assigned_to ?? []);
    // Components whose primary is this system lead. Compared by NAME now that a
    // component reports its primary system directly, with no uuid lookup.
    return [...(components.data ?? [])]
      .filter((c) => !taken.has(c.name))
      .sort((a, b) => {
        const mine = Number(b.system === props.system) - Number(a.system === props.system);
        return mine || label(a).localeCompare(label(b));
      });
  };

  async function run(role: string, write: () => Promise<void>) {
    setBusy(true);
    setErrs(role, undefined);
    try {
      await write();
      await qc.invalidateQueries({ queryKey: key() });
      setPicked(role, "");
    } catch (e) {
      // The server's 422 names the capabilities the component is missing. That
      // message IS the answer, so it is shown as sent.
      setErrs(role, describeError(e));
    } finally {
      setBusy(false);
    }
  }

  const assign = (role: string) => {
    const component = picked[role];
    if (!component) return;
    return run(role, () => assignRole(props.system, role, component));
  };
  const unassign = (role: string, component: string) => run(role, () => unassignRole(props.system, role, component));

  const roleRow = (r: EffectiveRole, first: () => boolean) => (
    <div class="flex flex-col gap-1.5 px-3 py-2.5" classList={{ "border-t border-base-300": !first() }}>
      <div class="flex flex-wrap items-baseline gap-2">
        <span class="text-sm font-medium">{r.display_name || r.name}</span>
        <span class="font-data text-[11px] text-base-content/45">{r.name}</span>
        <span class="flex-1" />
        <Show when={r.understaffed > 0}>
          <span class="badge badge-warning badge-soft badge-sm">understaffed</span>
        </Show>
        <span class="tnum shrink-0 text-[11px] text-base-content/50">{staffingLabel(r)}</span>
      </div>

      <div class="flex flex-wrap items-center gap-1.5">
        <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">requires</span>
        <Show
          when={(r.capabilities ?? []).length}
          fallback={<span class="text-[11px] italic text-base-content/40">nothing: any component can fill it</span>}
        >
          <For each={r.capabilities ?? []}>
            {(c) => <span class="badge badge-ghost badge-sm font-data">{c}</span>}
          </For>
        </Show>
      </div>

      <div class="flex flex-wrap items-center gap-1.5">
        <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">filled by</span>
        <Show
          when={(r.assigned_to ?? []).length}
          fallback={<span class="text-[11px] italic text-base-content/40">nobody yet</span>}
        >
          <For each={r.assigned_to ?? []}>
            {(c) => (
              <span class="badge badge-outline badge-sm gap-1 font-data">
                {c}
                <Show when={props.canUpdate}>
                  <button
                    type="button"
                    class="ml-0.5 inline-flex opacity-60 hover:opacity-100"
                    aria-label={`Unassign ${c} from ${r.name}`}
                    disabled={busy()}
                    onClick={() => void unassign(r.name, c)}
                  >
                    <X size={11} />
                  </button>
                </Show>
              </span>
            )}
          </For>
        </Show>
      </div>

      <Show when={props.canUpdate}>
        <div class="flex items-center gap-1.5">
          <select
            class="select select-bordered select-sm min-w-0 flex-1"
            aria-label={`Component to fill ${r.name}`}
            value={picked[r.name] ?? ""}
            onChange={(e) => setPicked(r.name, e.currentTarget.value)}
          >
            <option value="">Assign a component…</option>
            <For each={candidates(r)}>{(c) => <option value={c.name}>{label(c)}</option>}</For>
          </select>
          <Button
            square
            size="sm"
            intent="action"
            icon={Check}
            label={`Assign to ${r.name}`}
            title="Assign"
            disabled={busy() || !picked[r.name]}
            onClick={() => void assign(r.name)}
          />
        </div>
      </Show>

      {/* The refusal, verbatim: which capabilities the component is missing. */}
      <Show when={errs[r.name]}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{errs[r.name]}</span></div>
      </Show>
    </div>
  );

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">Roles</span>
        <span class="shrink-0 text-[10.5px] text-base-content/40">the slots this system needs filled</span>
      </div>
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{describeError(q.error)}</span></div>
      </Show>
      <Show when={!q.isLoading && !q.error && !roles().length}>
        <p class="text-sm text-base-content/50">
          No roles yet. A role is a slot this system needs filled (a table microphone, a main display), declared by the
          standard it conforms to or directly on the system.
        </p>
      </Show>
      <Show when={inherited().length}>
        <div class="overflow-hidden rounded-box border border-base-300">
          <For each={inherited()}>{(r, i) => roleRow(r, () => i() === 0)}</For>
        </div>
      </Show>
      {/* Ad-hoc roles are the system's own, so they group apart behind a dashed
          border, exactly as an off-contract property does: the standard declares
          nothing about them, and a conforming system's inherited roles are
          withdrawn on the standard instead. */}
      <Show when={adhoc().length}>
        <div class="flex flex-col gap-1" role="group" aria-label="Ad hoc roles">
          <div class="flex items-baseline gap-2">
            <span class="text-[10.5px] font-semibold uppercase tracking-wide text-base-content/50">Ad hoc</span>
            <span class="text-[10.5px] text-base-content/40">declared on this system, not by its standard</span>
          </div>
          <div class="overflow-hidden rounded-box border border-dashed border-base-300">
            <For each={adhoc()}>{(r, i) => roleRow(r, () => i() === 0)}</For>
          </div>
        </div>
      </Show>
    </div>
  );
}
