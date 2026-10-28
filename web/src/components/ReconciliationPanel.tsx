import { For, Show, createMemo } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { ownerProperties, ownerPropertiesKey, type EffectiveProperty } from "../lib/owner_properties";
import { displayValue } from "../lib/variables";
import { entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";
import { ownerOf } from "./PropertiesPanel";
import InfoTip from "./InfoTip";

// The Reconciliation panel on a component or system detail: per property the
// owner has, the want (declared, resolved live from the cascade), the told
// (intended, from a command), and the is (observed, worst case across every
// live instance, naming the instance it came from), with a drift marker when an
// observation disagrees with the declaration. It reads the owner's properties
// read (#1297), the same cache entry the Properties panel fills, so the two
// never disagree: a key only observed or told appears here, a calculated one
// (no reconciliation) does not. Read-only and pedagogical: it teaches the
// provenance pivot against the owner's real values. Drift is the server's
// verdict (only it holds the cascade), rendered here, not re-derived.

type Reconciled = EffectiveProperty & { reconciliation: NonNullable<EffectiveProperty["reconciliation"]> };

const isReconciled = (p: EffectiveProperty): p is Reconciled => !!p.reconciliation;

// Cell renders one want/told/is value, or a muted placeholder when that side has
// no value (nothing declared, no command yet, nothing observed).
function Cell(p: { value: unknown; via?: string }) {
  return (
    <Show when={p.value !== null && p.value !== undefined} fallback={<span class="text-base-content/30">not set</span>}>
      <span class="font-data">{displayValue(p.value)}</span>
      <Show when={p.via}>
        <span class="ml-1.5 text-[10.5px] text-base-content/40">via {p.via}</span>
      </Show>
    </Show>
  );
}

export default function ReconciliationPanel(p: { component?: string; system?: string }) {
  const owner = createMemo(() => ownerOf(p));
  const q = useQuery(() => ({
    queryKey: ownerPropertiesKey(owner().kind, owner().name),
    queryFn: () => ownerProperties(owner().kind, owner().name),
    // Shares the Properties panel's entry, which is edited inline; a focus
    // refetch would rebuild its rows mid-edit.
    refetchOnWindowFocus: false,
  }));
  const rows = createMemo(() => (q.data ?? []).filter(isReconciled));
  const driftCount = createMemo(() => rows().filter((r) => r.reconciliation.drift).length);
  return (
    <div class="flex flex-col gap-1.5">
      <div class="flex items-center gap-2">
        <span class="eyebrow">Reconciliation</span>
        <InfoTip
          label="reconciliation"
          text="Where each property stands: want is what was declared (the value set here, else the contract default), told is what a command last asked for, and is is what the device reports, worst case across every endpoint that observes it. Drift is an observation disagreeing with want, computed on every read; with nothing declared there is nothing to drift from."
        />
        <Show when={rows().length}>
          <span class="text-[11px] text-base-content/40">
            {rows().length} propert{rows().length === 1 ? "y" : "ies"}
            <Show when={driftCount()}>, {driftCount()} drifting</Show>
          </span>
        </Show>
      </div>
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{describeError(q.error)}</span></div>
      </Show>
      <Show
        when={rows().length}
        fallback={
          <Show when={!q.error}>
            <div class="rounded-box border border-dashed border-base-300 px-3 py-4 text-center text-[12px] text-base-content/45">
              <Show when={q.isLoading} fallback="Nothing declared, observed or told yet.">Loading reconciliation…</Show>
            </div>
          </Show>
        }
      >
        <div class="overflow-x-auto rounded-box border border-base-300">
          <table class="table table-sm">
            <thead>
              <tr class="text-[10px] uppercase tracking-wide text-base-content/45">
                <th>Property</th>
                <th>Want</th>
                <th>Told</th>
                <th>Is</th>
                <th>Drift</th>
              </tr>
            </thead>
            <tbody>
              <For each={rows()}>
                {(r) => (
                  <tr data-property={r.property_type_name}>
                    <td>{entityLabel({ name: r.property_type_name, label: r.label })}</td>
                    <td><Cell value={r.reconciliation.want} /></td>
                    <td><Cell value={r.reconciliation.told} /></td>
                    <td><Cell value={r.reconciliation.is} via={r.reconciliation.is_instance || undefined} /></td>
                    <td>
                      <Show when={r.reconciliation.drift} fallback={<span class="text-base-content/25">·</span>}>
                        <span class="badge badge-soft badge-warning badge-sm">drift</span>
                      </Show>
                    </td>
                  </tr>
                )}
              </For>
            </tbody>
          </table>
        </div>
      </Show>
    </div>
  );
}
