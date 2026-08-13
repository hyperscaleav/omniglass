import { entityLabel } from "../lib/entities";
import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { createStore } from "solid-js/store";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { Check, GripVertical, X } from "./icons";
import { describeError } from "../lib/format";
import { COMPONENTS_KEY, listComponents, type Component as Comp } from "../lib/components";
import {
  assignRole,
  staffingLabel,
  swapPath,
  swapRolePositions,
  systemRoles,
  systemRolesKey,
  unassignRole,
  type EffectiveRole,
} from "../lib/system_roles";
import {
  activeRoles as activeRolesOf,
  impactPhrase,
  inactiveRoles as inactiveRolesOf,
  quorumLabel,
  systemHealth,
  systemHealthKey,
  SYSTEM_VERDICTS_KEY,
  type HealthRole,
} from "../lib/health";

// RolesPanel lists the roles a system needs filled, resolved. A ROLE is a slot:
// a table microphone, a main display. It names the TYPED-SLOT guard (#626) a
// filling component's product must clear and carries a QUORUM, how many
// components it wants, so a role reads as "2 wanted, 1 assigned" and is
// UNDERSTAFFED until it has them. UNDERSTAFFED is assignment arithmetic
// (quorum minus rows), health-blind; the panel's SHORT/SPARE figures come
// from the health read instead (satisfying, occupancy-aware: an assigned
// component whose own verdict is outage no longer counts), so a role can
// read fully staffed here and still short there. Both read once an occupant
// is down, since the roles read alone cannot tell the difference (settled by
// #626 Task 9; see web/src/lib/health.ts's own header note).
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
// whether the component's product's type falls within an accepted type (and, if
// pinned, is one of the named products) and refuses with a message naming both
// parties, which this panel shows verbatim against the role that refused. A
// generic "something went wrong" would throw away the only thing the operator
// needs. The guard runs once, at assignment; after that, an occupant keeps its
// slot unless its own health verdict goes to outage (a lesser alarm degrades
// it but does not cost it the slot).
//
// A component fills at most one role per system (#626). The picker offers every
// component, but one already staffing a DIFFERENT role here renders disabled
// with the reason: dropping it silently would let an operator pick it, submit,
// and hit a 422 they could not have anticipated, which throws away the same
// lesson the typed-slot refusal above teaches.
//
// Writes are immediate, like the tag panel, so the panel has no Save of its own;
// the caller passes canUpdate (the system detail computes it as "in edit mode AND
// holding system:update"), which keeps view read-only per the console invariant.

const IMPACT_BADGE: Record<string, string> = { outage: "badge-error", degraded: "badge-warning" };
const impactBadgeClass = (impact: string) => IMPACT_BADGE[impact] ?? "badge-ghost";

// props.system is the owning system's address (#627: the console addresses a
// system by uuid now, since its name is scoped to placement, not reliably
// unique; systemRoles/systemHealth dual-accept either, ADR-0062).
export default function RolesPanel(props: { system: string; canUpdate: boolean }): JSX.Element {
  const qc = useQueryClient();
  const key = () => systemRolesKey(props.system);
  const q = useQuery(() => ({
    queryKey: key(),
    queryFn: () => systemRoles(props.system),
    refetchOnWindowFocus: false,
  }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  // The health read carries the occupancy-aware arithmetic (short, spare,
  // satisfying, down, impact) this panel needs; the roles read alone is
  // health-blind. See the header note above and health.ts's own warning
  // about not recomputing what the API already sends.
  const healthQ = useQuery(() => ({
    queryKey: systemHealthKey(props.system),
    queryFn: () => systemHealth(props.system),
    refetchOnWindowFocus: false,
  }));
  // Split active from inactive (#626 choices): a role whose alternate LOST
  // its choice can still read impaired on its own terms, but its own figures
  // did not move the verdict, so it must never render through the same
  // short/spare/impact path as an active role (the exact contradiction fixed
  // one panel over, commit 5472723 on HealthPanel; task 9 review, finding
  // C5, caught it reintroduced here).
  const activeByName = createMemo(() => new Map<string, HealthRole>(activeRolesOf(healthQ.data).map((h) => [h.name, h])));
  const inactiveByName = createMemo(() => new Map<string, HealthRole>(inactiveRolesOf(healthQ.data).map((h) => [h.name, h])));

  const roles = createMemo<EffectiveRole[]>(() => q.data ?? []);
  const inherited = createMemo(() => roles().filter((r) => r.from_standard));
  const adhoc = createMemo(() => roles().filter((r) => !r.from_standard));

  // Per-role write state: the picked component, and the server's last refusal.
  const [picked, setPicked] = createStore<Record<string, string>>({});
  const [errs, setErrs] = createStore<Record<string, string | undefined>>({});
  const [busy, setBusy] = createSignal(false);

  // Every component staffed anywhere in THIS system, by the role it holds
  // (display name, for the disabled-option reason). A component fills at
  // most one role per system, so this is a safe 1:1 lookup once assigned_to
  // is unioned across every role.
  const staffedHere = createMemo(() => {
    const held = new Map<string, string>();
    for (const r of roles()) for (const c of r.assigned_to ?? []) held.set(c, entityLabel(r));
    return held;
  });

  // The components a role can be offered, by label: those already filling it are
  // dropped (re-offering a current occupant is a no-op), and the ones in this
  // system lead, since that is where an operator staffs from. Everything else
  // is still offered, including a component staffed by a DIFFERENT role here,
  // rendered disabled rather than dropped: the disabled reason is what teaches,
  // the same principle behind showing the server's 422 verbatim.
  const label = (c: Comp) => entityLabel(c);
  const candidates = (role: EffectiveRole): Comp[] => {
    const taken = new Set(role.assigned_to ?? []);
    // Components whose primary is this system lead, compared by system_id
    // (#627: props.system is now the system's own uuid, and a component's
    // system_id is the stable form of its primary system, unlike system,
    // which is a name that can collide across placements).
    return [...(components.data ?? [])]
      .filter((c) => !taken.has(c.name))
      .sort((a, b) => {
        const mine = Number(b.system_id === props.system) - Number(a.system_id === props.system);
        return mine || label(a).localeCompare(label(b));
      });
  };
  // The role a candidate already holds elsewhere in this system, if any; null
  // when it is free to pick. staffedHere already excludes this role's own
  // occupants from candidates(), so any hit here is necessarily a DIFFERENT role.
  const heldElsewhere = (c: Comp): string | null => staffedHere().get(c.name) ?? null;

  async function run(role: string, write: () => Promise<void>) {
    setBusy(true);
    setErrs(role, undefined);
    try {
      await write();
      setPicked(role, "");
    } catch (e) {
      // The server's 422 names both parties: what the component is, and what
      // the role wants. That message IS the answer, so it is shown as sent.
      setErrs(role, describeError(e));
    } finally {
      // Both queries invalidate here, not just on success: a reorder issues
      // several independent server-committed swaps in sequence (swapPath),
      // so a failure partway through still leaves the earlier swaps applied;
      // invalidating only in the try block would leave the console showing
      // the pre-drag order while the server holds a partly reordered one
      // (task 9 review, finding C4). The health key invalidates alongside
      // the roles key because short/spare/impact, this panel's primary
      // readout since task 9, come from the health read, not the roles read:
      // an assign, unassign, or reorder that only invalidated roles left
      // those badges frozen at their pre-write values (finding C6).
      await Promise.all([
        qc.invalidateQueries({ queryKey: key() }),
        qc.invalidateQueries({ queryKey: systemHealthKey(props.system) }),
        qc.invalidateQueries({ queryKey: SYSTEM_VERDICTS_KEY }),
      ]);
      setBusy(false);
    }
  }

  const assign = (role: string) => {
    const component = picked[role];
    if (!component) return;
    return run(role, () => assignRole(props.system, role, component));
  };
  const unassign = (role: string, component: string) => run(role, () => unassignRole(props.system, role, component));

  // Reorders a role's occupants by dragging one onto another's slot. The
  // server only exchanges two positions per call, so swapPath decomposes the
  // drag (a moveItem-shaped "from index N to index M") into the REAL-position
  // swaps that reproduce it (positions is EffectiveRoleBody's own per-occupant
  // position array, not assumed index-plus-one: a role's positions are not
  // dense once an unassign leaves a gap, #626), run in order inside one
  // busy/error cycle.
  const reorder = (role: string, positions: number[], from: number, to: number) => {
    if (from === to) return;
    return run(role, async () => {
      for (const [a, b] of swapPath(positions, from, to)) {
        await swapRolePositions(props.system, role, a, b);
      }
    });
  };

  const roleRow = (r: EffectiveRole, first: () => boolean) => {
    const h = () => activeByName().get(r.name);
    const inactiveHealth = () => inactiveByName().get(r.name);
    // Whether an alarm took a component down is a fact about the COMPONENT,
    // not about whether this role's choice is the one currently answering
    // it, so an inactive role still marks its down occupants: only the
    // short/spare/impact VERDICT CONTRIBUTION is suppressed above, never the
    // per-component fact (task 9 re-review: reading down() from the active
    // map alone over-corrected C5 into hiding it here too).
    const down = () => new Set(h()?.down ?? inactiveHealth()?.down ?? []);
    // Drag-to-reorder the occupant list: only worth wiring, and only sound to
    // wire, once there is more than one position to reorder, the caller can
    // write, AND the wire's positions array is actually usable (defends a
    // stale cache from before EffectiveRoleBody carried it, or any future
    // shape it does not match assigned_to length for). dragIdx is this row's
    // own state (reset whenever the row remounts, e.g. after a reorder
    // refetches).
    const [dragIdx, setDragIdx] = createSignal<number | null>(null);
    const canReorder = () =>
      props.canUpdate &&
      (r.assigned_to ?? []).length > 1 &&
      (r.positions ?? []).length === (r.assigned_to ?? []).length;
    return (
    <div class="flex flex-col gap-1.5 px-3 py-2.5" classList={{ "border-t border-base-300": !first() }}>
      <div class="flex flex-wrap items-baseline gap-2">
        <span class="text-sm font-medium">{entityLabel(r)}</span>
        <span class="font-data text-[11px] text-base-content/45">{r.name}</span>
        <span class="flex-1" />
        {/* short/spare are occupancy-aware (health read); understaffed is pure
            assignment arithmetic (roles read) and only falls back here while
            health has not loaded yet, so the row never shows undefined. An
            INACTIVE role (its choice was answered by a different alternate)
            gets neither: its own short/spare/impact never counted toward the
            verdict, so showing them would be the exact contradiction commit
            5472723 fixed on HealthPanel, one panel over. */}
        <Show
          when={h()}
          fallback={
            <Show
              when={inactiveHealth()}
              fallback={
                <Show when={r.understaffed > 0}>
                  <span class="badge badge-warning badge-soft badge-sm">understaffed</span>
                </Show>
              }
            >
              {(ih) => (
                <span
                  class="badge badge-ghost badge-sm"
                  title={`${ih().choice}/${ih().alternate} is not the alternate currently answering this choice, so this role's own figures did not count toward the verdict`}
                >
                  not counted
                </span>
              )}
            </Show>
          }
        >
          {(hr) => (
            <>
              <Show when={hr().short > 0}>
                <span class="badge badge-warning badge-soft badge-sm">short {hr().short}</span>
              </Show>
              <Show when={hr().spare > 0}>
                <span class="badge badge-ghost badge-sm">spare {hr().spare}</span>
              </Show>
              <Show when={hr().impaired}>
                <span class={`badge badge-soft badge-sm ${impactBadgeClass(hr().impact)}`}>{impactPhrase(hr().impact)}</span>
              </Show>
            </>
          )}
        </Show>
        <span class="tnum shrink-0 text-[11px] text-base-content/50">
          <Show when={h()} fallback={staffingLabel(r)}>
            {(hr) => quorumLabel(hr())}
          </Show>
        </span>
      </div>

      <div class="flex flex-col gap-1">
        <div class="flex flex-wrap items-center gap-1.5">
          <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">accepts</span>
          <Show
            when={(r.accepted_types ?? []).length}
            fallback={<span class="text-[11px] italic text-base-content/40">any type</span>}
          >
            <For each={r.accepted_types ?? []}>
              {(c) => <span class="badge badge-ghost badge-sm font-data">{c}</span>}
            </For>
          </Show>
        </div>
        <Show when={(r.pinned_products ?? []).length}>
          <div class="flex flex-wrap items-center gap-1.5">
            <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">pinned to</span>
            <For each={r.pinned_products ?? []}>
              {(c) => <span class="badge badge-ghost badge-sm font-data">{c}</span>}
            </For>
          </div>
        </Show>
        {/* The declared position labels, shown as their own reference list
            rather than zipped onto the occupant chips below: the wire gives
            each role its OCCUPANTS in position order but not each occupant's
            OWN position number, so pairing label[i] with assigned_to[i] would
            mislabel the moment a lower position is vacant (TestUnassignLeavesGap
            leaves exactly that kind of gap). */}
        <Show when={(r.position_labels ?? []).length}>
          <div class="flex flex-wrap items-center gap-1.5">
            <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">positions</span>
            <For each={r.position_labels ?? []}>
              {(pl, i) => <span class="badge badge-ghost badge-sm">{i() + 1}. {pl}</span>}
            </For>
          </div>
        </Show>
      </div>

      <div class="flex flex-wrap items-center gap-1.5">
        <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">
          filled by{canReorder() ? " · drag to reorder" : ""}
        </span>
        <Show
          when={(r.assigned_to ?? []).length}
          fallback={<span class="text-[11px] italic text-base-content/40">nobody yet</span>}
        >
          <For each={r.assigned_to ?? []}>
            {(c, i) => (
              <span
                class={`badge badge-sm gap-1 font-data ${down().has(c) ? "badge-error badge-soft" : "badge-outline"}`}
                classList={{ "cursor-grab": canReorder(), "opacity-40": canReorder() && dragIdx() === i() }}
                title={down().has(c) ? "An active alarm has taken this component down" : "Occupying its slot"}
                draggable={canReorder()}
                onDragStart={() => canReorder() && setDragIdx(i())}
                onDragOver={(e) => { if (canReorder()) e.preventDefault(); }}
                onDrop={() => {
                  const from = dragIdx();
                  if (canReorder() && from !== null && from !== i()) void reorder(r.name, r.positions ?? [], from, i());
                  setDragIdx(null);
                }}
                onDragEnd={() => setDragIdx(null)}
              >
                <Show when={canReorder()}>
                  <GripVertical size={10} />
                </Show>
                {c}
                <Show when={down().has(c)}> down</Show>
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
            <For each={candidates(r)}>
              {(c) => {
                const holder = heldElsewhere(c);
                return (
                  // The uuid, not the name (#627 review, the pre-existing
                  // surface): assignRole resolves the component estate-wide
                  // via scopedByName (internal/storage/roles.go), which
                  // dual-accepts a uuid unambiguously (ADR-0062) but 409s an
                  // estate-wide-ambiguous name regardless of this system's
                  // own scope. A bare name here would contradict the
                  // duplicate-named-component promise Task 15 already made
                  // on every other write in this console.
                  <option value={c.id} disabled={!!holder}>
                    {label(c)}{holder ? ` (already fills ${holder} here)` : ""}
                  </option>
                );
              }}
            </For>
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

      {/* The refusal, verbatim: what the component is, and what the role wants. */}
      <Show when={errs[r.name]}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{errs[r.name]}</span></div>
      </Show>
    </div>
    );
  };

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
