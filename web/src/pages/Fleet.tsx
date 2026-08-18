import { For, Show, createMemo, createSignal } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import HealthBadge from "../components/HealthBadge";
import BandCanvas from "../components/BandCanvas";
import BladeStack from "../components/BladeStack";
import FleetShell from "../components/FleetShell";
import Button from "../components/Button";
import SystemHealthPanel from "../components/HealthPanel";
import { BladesContext, createBladeController, type BladeDef } from "../lib/blades";
import { FLEET_VIEW_KEY, fleetView, holesByRoot, type Band, type FleetView, type SystemCluster } from "../lib/fleet";
import { fleetTiles, systemMarks } from "../lib/fleet_tiles";
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
      <Page title="Fleet" subtitle="Explore your environment.">
        <Show when={!view.isPending} fallback={<div class="skeleton h-32 w-full" />}>
          <Show
            when={!view.isError}
            fallback={
              <div role="alert" class="alert alert-error alert-soft text-sm">
                {describeError(view.error)}
              </div>
            }
          >
            <FleetShell
              storageKey="fleet"
              tiles={tiles()}
              rows={bands().flatMap((b) => b.clusters)}
              filterKeys={filterKeys}
              chips={chips}
              onChips={setChips}
              placeholder="Filter by verdict, system, room…"
              trailing={<span class="text-xs text-base-content/40">{worstFirst() ? "Worst first" : ""}</span>}
            >
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
            </FleetShell>
          </Show>
        </Show>
      </Page>
      <BladeStack controller={blades} registry={registry} />
    </BladesContext.Provider>
  );

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
