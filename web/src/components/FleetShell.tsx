import { For, Show, createEffect, createSignal, type Accessor, type JSX } from "solid-js";
import Button from "./Button";
import Donut from "./Donut";
import ListShell from "./ListShell";
import { ChevronDown, ChevronLeft } from "./icons";
import type { Chip, FilterKey } from "../lib/predicate";
import type { FleetTiles } from "../lib/fleet_tiles";
import type { SystemCluster } from "../lib/fleet";

// The fleet pages' shared frame (#630, ruled 2026-08-18): the same layout at
// every zoom, taken from the Locations list page. On top, a summary rail of
// badges that expands to a tile board (a verdict donut with a facet legend,
// count cards), every badge and legend row a filter toggle; below it the
// console's ListShell (filter bar + card) around the zoom's own body. The
// summary is FLEET-WIDE at every zoom: it is the standing "how is my fleet"
// while the body zooms. Right-side drawers exist only as detail blades.
const VERDICTS = ["outage", "degraded", "incomplete", "healthy"] as const;
const COLOR: Record<string, string> = {
  outage: "var(--color-error)",
  degraded: "var(--color-warning)",
  incomplete: "var(--og-incomplete)",
  healthy: "var(--color-success)",
};

const tileBox = "flex h-full w-full min-w-0 flex-col gap-2 rounded-box border border-base-300 bg-base-200 p-3.5";

export default function FleetShell(props: {
  storageKey: string;
  tiles: FleetTiles | undefined;
  rows: SystemCluster[];
  filterKeys: FilterKey<SystemCluster>[];
  chips: Accessor<Chip[]>;
  onChips: (chips: Chip[]) => void;
  placeholder?: string;
  trailing?: JSX.Element;
  // A zoom's own header line inside the card, above its body (a system's
  // verdict and slot count, say). Renders where the filter bar would when a
  // zoom has nothing to filter; above the body when it has both.
  header?: JSX.Element;
  children: JSX.Element;
}) {
  const [summaryOpen, setSummaryOpen] = createSignal(localStorage.getItem(`${props.storageKey}-sumopen`) === "1");
  createEffect(() => localStorage.setItem(`${props.storageKey}-sumopen`, summaryOpen() ? "1" : "0"));

  // Facet plumbing over the one verdict chip, the same shape ListCtx gives
  // widgets on the inventory pages.
  const facetActive = (v: string) => props.chips().some((c) => c.key === "verdict" && c.values.includes(v));
  const toggleFacet = (v: string) => {
    const cur = props.chips().find((c) => c.key === "verdict");
    const rest = props.chips().filter((c) => c.key !== "verdict");
    if (!cur) return props.onChips([...rest, { key: "verdict", op: "eq", values: [v] }]);
    const values = cur.values.includes(v) ? cur.values.filter((x) => x !== v) : [...cur.values, v];
    props.onChips(values.length ? [...rest, { ...cur, values }] : rest);
  };

  const segs = () => VERDICTS.map((v) => ({ key: v, label: v, value: props.tiles?.ratio[v] ?? 0, color: COLOR[v] }));
  const total = () => props.tiles?.ratio.total ?? 0;
  const attention = () => props.tiles?.attention.total ?? 0;
  const ATTENTION = ["outage", "degraded", "incomplete"];
  const attentionOn = () => ATTENTION.some(facetActive) && !facetActive("healthy");
  const toggleAttention = () => {
    const rest = props.chips().filter((c) => c.key !== "verdict");
    props.onChips(attentionOn() ? rest : [...rest, { key: "verdict", op: "eq", values: ATTENTION }]);
  };

  const badgeCls = (on: boolean) => "inline-flex items-center gap-2 rounded-field border px-2.5 py-1 " + (on ? "border-primary bg-primary/10" : "border-base-300 bg-base-200");

  return (
    <section class="fade-in flex flex-col gap-3.5">
      <Show when={props.tiles}>
        {(t) => (
          <div data-testid="fleet-summary" class="flex flex-col gap-3">
            <Show
              when={summaryOpen()}
              fallback={
                <div class="flex flex-wrap items-center gap-2">
                  <div class="flex min-w-0 flex-1 flex-wrap gap-2">
                    {/* Verdict mix: mini bar + total, expands the board. */}
                    <button class={badgeCls(false)} onClick={() => setSummaryOpen(true)} title="Expand summary">
                      <span class="inline-flex h-2 w-13 flex-none overflow-hidden rounded-full">
                        <For each={segs().filter((s) => s.value)}>{(s) => <span style={{ width: `${(s.value / Math.max(1, total())) * 52}px`, background: s.color }} />}</For>
                      </span>
                      <span class="tnum text-sm font-semibold">{t().systems}</span>
                      <span class="text-[11.5px] text-base-content/60">systems</span>
                    </button>
                    {/* Need attention: one click, three verdicts. */}
                    <button data-testid="badge-attention" class={badgeCls(attentionOn())} onClick={toggleAttention} title="Filter to systems needing attention">
                      <span class="h-1.5 w-1.5 flex-none rounded-full" style={{ background: attention() ? (t().attention.outage ? COLOR.outage : t().attention.degraded ? COLOR.degraded : COLOR.incomplete) : COLOR.healthy }} />
                      <span class="tnum text-sm font-semibold">{attention()}</span>
                      <span class="text-[11.5px] text-base-content/60">need attention</span>
                    </button>
                    <span class={badgeCls(false)} title="Locations with no system">
                      <span class="tnum text-sm font-semibold">{t().gaps}</span>
                      <span class="text-[11.5px] text-base-content/60">{t().gaps === 1 ? "gap" : "gaps"}</span>
                    </span>
                    <span class={badgeCls(false)}>
                      <span class="tnum text-sm font-semibold">{t().components}</span>
                      <span class="text-[11.5px] text-base-content/60">components</span>
                    </span>
                    <span class={badgeCls(false)}>
                      <span class="tnum text-sm font-semibold">{t().roots}</span>
                      <span class="text-[11.5px] text-base-content/60">{t().roots === 1 ? "root" : "roots"}</span>
                    </span>
                  </div>
                  <Button square icon={ChevronLeft} title="Expand summary" label="Expand summary" class="flex-none" onClick={() => setSummaryOpen(true)} />
                </div>
              }
            >
              <div class="flex items-center gap-2">
                <span class="eyebrow">Summary</span>
                <span class="flex-1" />
                <Button icon={ChevronDown} iconTrailing onClick={() => setSummaryOpen(false)}>Collapse</Button>
              </div>
              <div data-testid="fleet-tiles" class="flex flex-wrap items-stretch gap-3">
                <div class="min-w-50 max-w-sm flex-[1_1_260px]">
                  <div class={`${tileBox} flex-row items-center gap-4`}>
                    <Donut
                      segments={segs()}
                      size={92}
                      thickness={11}
                      onSelect={(k) => toggleFacet(k)}
                      active={(k) => facetActive(k)}
                      center={<><span class="tnum text-base font-semibold">{total()}</span><span class="text-[9px] text-base-content/50">systems</span></>}
                    />
                    <ul class="flex flex-col gap-1 text-xs">
                      <For each={segs()}>
                        {(s) => (
                          <li>
                            <button class="flex w-full items-center gap-2 rounded px-1 py-0.5 text-left hover:bg-base-content/5" classList={{ "bg-base-content/5": facetActive(s.key) }} onClick={() => toggleFacet(s.key)} title={`Filter ${s.label}`}>
                              <span class="h-2.5 w-2.5 flex-none rounded-full" style={{ background: s.color }} />
                              <span>{s.label}</span>
                              <span class="tnum ml-auto pl-3 text-base-content/50">{s.value}</span>
                            </button>
                          </li>
                        )}
                      </For>
                    </ul>
                  </div>
                </div>
                <div class="min-w-50 max-w-sm flex-[1_1_220px]">
                  <div class={tileBox}>
                    <span class="eyebrow">Gaps</span>
                    <span class="tnum text-2xl font-semibold">{t().gaps}</span>
                    <span class="text-xs text-base-content/60">{t().gaps === 1 ? "location with no system" : "locations with no system"}</span>
                  </div>
                </div>
                <div class="min-w-50 max-w-sm flex-[1_1_220px]">
                  <div class={tileBox}>
                    <span class="eyebrow">Components</span>
                    <span class="tnum text-2xl font-semibold">{t().components}</span>
                    <span class="text-xs text-base-content/60">across {t().systems} systems</span>
                  </div>
                </div>
                <div class="min-w-50 max-w-sm flex-[1_1_220px]">
                  <div class={tileBox}>
                    <span class="eyebrow">Roots</span>
                    <span class="tnum text-2xl font-semibold">{t().roots}</span>
                    <span class="text-xs text-base-content/60">{t().depth.min === t().depth.max ? `${t().depth.max} levels deep` : `${t().depth.min} to ${t().depth.max} levels deep`}</span>
                  </div>
                </div>
              </div>
            </Show>
          </div>
        )}
      </Show>
      <Show
        when={props.filterKeys.length > 0}
        fallback={
          <div class="og-stack flex flex-col">
            <div class="card overflow-hidden border border-base-300 bg-base-200 p-0">
              <Show when={props.header}>
                <div class="border-b border-base-300 px-4 py-3">{props.header}</div>
              </Show>
              {props.children}
            </div>
          </div>
        }
      >
        <ListShell filterKeys={props.filterKeys} rows={props.rows} chips={props.chips} onChips={props.onChips} trailing={props.trailing} placeholder={props.placeholder}>
          {() => (
            <>
              <Show when={props.header}>
                <div class="border-b border-base-300 px-4 py-3">{props.header}</div>
              </Show>
              {props.children}
            </>
          )}
        </ListShell>
      </Show>
    </section>
  );
}
