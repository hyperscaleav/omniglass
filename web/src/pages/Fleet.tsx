import { For, Show, createMemo, createSignal } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import HealthBadge from "../components/HealthBadge";
import BandCanvas from "../components/BandCanvas";
import ZoomLadder from "../components/ZoomLadder";
import FilterBar from "../components/FilterBar";
import BladeStack from "../components/BladeStack";
import Button from "../components/Button";
import SystemHealthPanel from "../components/HealthPanel";
import { BladesContext, createBladeController, type BladeDef } from "../lib/blades";
import { FLEET_VIEW_KEY, fleetView, holesByRoot, type Band, type FleetView, type SystemCluster } from "../lib/fleet";
import { fleetTiles, systemMarks } from "../lib/fleet_tiles";
import { zoomChips } from "../lib/zoom";
import { entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";
import { buildPredicate, type Chip, type FilterKey } from "../lib/predicate";

// The fleet zoom (#630, design option B ruled 2026-08-18): the whole fleet at
// a glance. Summary tiles and a filter bar on top, the canvas in the main
// area (one ROUND mark per system, coloured by the system's verdict, banded
// per root, worst first), and the blade drawer on click. Component marks are
// square and live one zoom down: the shape is the grain.
export default function Fleet() {
  const navigate = useNavigate();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const blades = createBladeController();

  const [chips, setChips] = createSignal<Chip[]>([]);
  const [worstFirst] = createSignal(true);

  const tiles = createMemo(() => (view.data ? fleetTiles(view.data) : undefined));

  // The filter runs over SYSTEMS (clusters), the unit this zoom is about.
  const filterKeys: FilterKey<SystemCluster>[] = [
    { key: "verdict", type: "string", hint: "exact", get: (c) => c.verdict ?? "unknown", values: () => ["outage", "degraded", "incomplete", "healthy"] },
    { key: "system", type: "string", hint: "substring", get: (c) => c.label },
    { key: "room", type: "string", hint: "substring", get: (c) => (view.data && c.locationId ? entityLabel(view.data.locations!.find((l) => l.id === c.locationId)!) : "") },
  ];

  const bands = createMemo<Band[]>(() => {
    if (!view.data) return [];
    const pred = buildPredicate(filterKeys, chips());
    return systemMarks(view.data).map((b) => ({ ...b, clusters: b.clusters.filter(pred) }));
  });
  const holes = createMemo(() => (view.data ? holesByRoot(view.data) : new Map()));
  const ladder = createMemo(() => (view.data ? zoomChips("fleet", {}, view.data) : []));

  // The attention tile filters the canvas to what needs it: one chip.
  const attentionOn = () => chips().some((c) => c.key === "verdict");
  const toggleAttention = () =>
    setChips((cs) => (attentionOn() ? cs.filter((c) => c.key !== "verdict") : [...cs, { key: "verdict", op: "eq", values: ["outage", "degraded", "incomplete"] }]));

  // The blade registry: a system's health panel, with drill actions.
  const registry: Record<string, BladeDef> = {
    system: {
      Title: (p) => {
        const sys = view.data?.systems?.find((s) => s.id === p.id);
        return <>{sys ? entityLabel(sys) : "System"}</>;
      },
      Body: (p) => (
        <div class="flex flex-col gap-4">
          <SystemHealthPanel system={p.id} onOpenComponent={(name) => navigate(`/components/${encodeURIComponent(name)}?zoom=1`)} />
          <div class="flex gap-2">
            <Button intent="action" size="sm" onClick={() => { blades.close(); navigate(`/systems/${p.id}?zoom=1`); }}>Open system</Button>
            <Show when={view.data?.systems?.find((s) => s.id === p.id)?.location}>
              {(loc) => <Button size="sm" onClick={() => { blades.close(); navigate(`/locations/${loc()}?zoom=1`); }}>Open location</Button>}
            </Show>
          </div>
        </div>
      ),
    },
  };

  return (
    <BladesContext.Provider value={blades}>
      <Page title="Fleet" subtitle="Explore your environment." breadcrumb={<Breadcrumb crumbs={[{ key: "fleet", label: "Fleet" }]} />}>
        <Show when={!view.isPending} fallback={<div class="skeleton h-32 w-full" />}>
          <Show
            when={!view.isError}
            fallback={
              <div role="alert" class="alert alert-error alert-soft text-sm">
                {describeError(view.error)}
              </div>
            }
          >
            {/* Summary tiles: what the rail carried, over systems at this zoom. */}
            <Show when={tiles()}>
              {(t) => (
                <div data-testid="fleet-tiles" class="grid grid-cols-2 gap-2 md:grid-cols-5">
                  <Tile label="Systems" value={String(t().systems)} note={`${t().components} components`} />
                  <button
                    type="button"
                    data-testid="tile-attention"
                    class="cursor-pointer text-left"
                    classList={{ "ring-1 ring-primary rounded-lg": attentionOn() }}
                    onClick={toggleAttention}
                  >
                    <Tile
                      label="Need attention"
                      value={String(t().attention.total)}
                      tone={t().attention.outage > 0 ? "error" : t().attention.degraded > 0 ? "warning" : t().attention.incomplete > 0 ? "incomplete" : undefined}
                      note={[
                        t().attention.outage ? `${t().attention.outage} down` : "",
                        t().attention.degraded ? `${t().attention.degraded} degraded` : "",
                        t().attention.incomplete ? `${t().attention.incomplete} incomplete` : "",
                      ].filter(Boolean).join(" · ") || "all healthy"}
                    />
                  </button>
                  <Tile label="Gaps" value={String(t().gaps)} note={t().gaps === 1 ? "location, no system" : "locations, no system"} />
                  <div class="rounded-lg border border-base-content/10 p-3">
                    <div class="text-[10px] uppercase tracking-wider text-base-content/50">Health</div>
                    <div data-testid="tile-health" class="mt-2 flex h-1.5 w-full overflow-hidden rounded-full bg-base-content/10" title={`${t().ratio.healthy} healthy · ${t().ratio.incomplete} incomplete · ${t().ratio.degraded} degraded · ${t().ratio.outage} outage`}>
                      <div class="bg-success" style={{ width: pct(t().ratio.healthy, t().ratio.total) }} />
                      <div class="bg-incomplete" style={{ width: pct(t().ratio.incomplete, t().ratio.total) }} />
                      <div class="bg-warning" style={{ width: pct(t().ratio.degraded, t().ratio.total) }} />
                      <div class="bg-error" style={{ width: pct(t().ratio.outage, t().ratio.total) }} />
                    </div>
                    <div class="mt-1 text-xs text-base-content/60">of {t().systems} systems</div>
                  </div>
                  <Tile label="Roots" value={String(t().roots)} note={t().depth.min === t().depth.max ? `${t().depth.max} levels deep` : `${t().depth.min} to ${t().depth.max} levels deep`} />
                </div>
              )}
            </Show>

            {/* The filter bar and the ladder share a row. */}
            <div class="flex flex-wrap items-center gap-3">
              <div class="min-w-64 flex-1">
                <FilterBar keys={filterKeys} rows={bands().flatMap((b) => b.clusters)} chips={chips()} onChips={setChips} placeholder="Filter by verdict, system, room…" />
              </div>
              <ZoomLadder chips={ladder()} hint={worstFirst() ? "Worst first" : ""} />
            </div>

            {/* The canvas: one round mark per system. */}
            <div class="flex flex-col gap-4">
              <For each={bands()}>{(band) => <FleetBand band={band} view={view.data!} onOpen={(id) => navigate(`/locations/${encodeURIComponent(id)}?zoom=1`)} />}</For>
              <Show when={bands().length === 0}>
                <p class="text-sm text-base-content/60">Nothing in scope yet.</p>
              </Show>
              <div data-testid="add-root-hole" class="w-56 rounded-lg border border-dashed border-base-content/25 px-3 py-2 text-xs text-base-content/50">
                <div class="font-medium text-base-content/70">+ Top-level location</div>
                <div>campus, site, or a type you define</div>
              </div>
            </div>
          </Show>
        </Show>
      </Page>
      <BladeStack controller={blades} registry={registry} />
    </BladesContext.Provider>
  );

  function pct(n: number, total: number) {
    return total ? `${(n / total) * 100}%` : "0%";
  }

  function Tile(props: { label: string; value: string; note?: string; tone?: "error" | "warning" | "incomplete" }) {
    return (
      <div class="rounded-lg border border-base-content/10 p-3">
        <div class="text-[10px] uppercase tracking-wider text-base-content/50">{props.label}</div>
        <div
          class="mt-1 text-xl font-semibold tabular-nums"
          classList={{ "text-error": props.tone === "error", "text-warning": props.tone === "warning", "text-incomplete": props.tone === "incomplete" }}
        >
          {props.value}
        </div>
        <Show when={props.note}>
          <div class="text-xs text-base-content/60">{props.note}</div>
        </Show>
      </div>
    );
  }

  function FleetBand(props: { band: Band; view: FleetView; onOpen: (id: string) => void }) {
    const bandHoles = () => holes().get(props.band.key) ?? [];
    const counts = () => {
      const b = props.band;
      const parts = [b.systemCount === 1 ? "1 system" : `${b.systemCount} systems`, b.componentCount === 1 ? "1 component" : `${b.componentCount} components`];
      if (b.depth > 0) parts.push(b.depth === 1 ? "1 level" : `${b.depth} levels`);
      return parts.join(", ");
    };
    return (
      <section data-testid={`band-${props.band.key}`} class="flex gap-4">
        <div class="w-52 flex-none">
          <button type="button" class="block w-full cursor-pointer rounded-lg p-1 text-left hover:bg-base-content/5" onClick={() => props.onOpen(props.band.key)}>
            <div class="flex items-center gap-2">
              <HealthBadge verdict={props.band.recordedVerdict ?? undefined} size="xs" />
              <span class="line-clamp-2 font-medium leading-tight">{props.band.label}</span>
            </div>
            <div class="mt-0.5 flex items-baseline gap-2 text-xs text-base-content/60">
              <Show when={props.band.sublabel}>
                <span class="text-[10px] uppercase tracking-wider text-base-content/50">{props.band.sublabel}</span>
              </Show>
              <span>{counts()}</span>
            </div>
          </button>
        </div>
        <div class="min-w-0 flex-1">
          <BandCanvas
            clusters={props.band.clusters}
            shape="round"
            ariaLabel={`${props.band.label}: ${counts()}`}
            onHoverDot={() => {}}
            onClickDot={(dot) => blades.push({ kind: "system", id: dot.componentId })}
          />
          <Show when={bandHoles().length > 0}>
            <div class="mt-2 flex flex-wrap gap-2">
              <For each={bandHoles()}>
                {(hole) => (
                  <div class="rounded-md border border-dashed border-base-content/25 px-2 py-1 text-xs text-base-content/50">
                    {entityLabel(hole)}
                  </div>
                )}
              </For>
            </div>
          </Show>
        </div>
      </section>
    );
  }
}
