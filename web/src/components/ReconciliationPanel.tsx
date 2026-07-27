import { For, Show, createMemo } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { RECONCILIATION_KEY, getReconciliation, fmtValue, type ReconValue } from "../lib/reconciliation";

// The Reconciliation panel on the component detail: per declared property, the
// want (declared, resolved live from the cascade), the told (intended, from a
// command), and the is (observed, from the latest-value cache), with a drift marker
// when the observed value disagrees with the declared one. Read-only and
// pedagogical: it teaches the provenance pivot (what we want, what we told the
// device, what it reports) against the component's real values. Drift is the
// server's verdict (only it holds the cascade), rendered here, not re-derived.

// Cell renders one want/told/is value, or a muted placeholder when that side has
// no value (a property with no command yet has a null told; an unobserved property
// has a null is).
function Cell(p: { value: ReconValue | null }) {
  return (
    <Show when={p.value !== null && p.value !== undefined} fallback={<span class="text-base-content/30">not set</span>}>
      <span class="font-data">{fmtValue(p.value)}</span>
    </Show>
  );
}

export default function ReconciliationPanel(p: { name: string }) {
  const q = useQuery(() => ({ queryKey: RECONCILIATION_KEY(p.name), queryFn: () => getReconciliation(p.name) }));
  const rows = createMemo(() => q.data?.properties ?? []);
  const driftCount = createMemo(() => rows().filter((r) => r.drift).length);
  return (
    <div class="flex flex-col gap-1.5">
      <div class="flex items-center gap-2">
        <span class="eyebrow">Reconciliation</span>
        <Show when={rows().length}>
          <span class="text-[11px] text-base-content/40">
            {rows().length} propert{rows().length === 1 ? "y" : "ies"}
            <Show when={driftCount()}>, {driftCount()} drifting</Show>
          </span>
        </Show>
      </div>
      <Show
        when={rows().length}
        fallback={
          <div class="rounded-box border border-dashed border-base-300 px-3 py-4 text-center text-[12px] text-base-content/45">
            <Show when={q.isLoading} fallback="No declared properties to reconcile yet.">Loading reconciliation…</Show>
          </div>
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
                  <tr>
                    <td class="font-data">{r.property}</td>
                    <td><Cell value={r.want} /></td>
                    <td><Cell value={r.told} /></td>
                    <td><Cell value={r.is} /></td>
                    <td>
                      <Show when={r.drift} fallback={<span class="text-base-content/25">·</span>}>
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
