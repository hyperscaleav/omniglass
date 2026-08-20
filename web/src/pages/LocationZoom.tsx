import { For, Show, createEffect, createMemo, createSignal } from "solid-js";
import { useNavigate, useParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import HealthBadge from "../components/HealthBadge";
import SystemCard from "../components/SystemCard";
import FleetShell from "../components/FleetShell";
import { fleetTiles } from "../lib/fleet_tiles";
import { buildPredicate, type Chip, type FilterKey } from "../lib/predicate";
import {
  FLEET_VIEW_KEY,
  ancestors,
  bandsOf,
  byChildOfLocation,
  fleetView,
  holesUnder,
  locationIndex,
  type Band,
  type FleetView,
  type SystemCluster,
} from "../lib/fleet";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { entityLabel } from "../lib/entities";
import { durationText } from "../lib/timeline";
import { sinceOf } from "../lib/system_zoom";
import { describeError, fmtTime } from "../lib/format";
import { locationHealth, locationHealthKey } from "../lib/health";

// The location zoom (#635): the same canvas one level down, at the identity
// route behind ?zoom=1 (ADR-0126). One band per direct child whatever its
// type, the placed-here band first with this location's own systems as cards,
// the subtree's holes dashed under the child that contains them, and the
// allowed child types named beneath: a child can be any type this one allows,
// which is the no-fixed-ladder fact this zoom teaches.
export default function LocationZoom() {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  const id = () => params.id;
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const locHealth = useQuery(() => ({ queryKey: locationHealthKey(id()), queryFn: () => locationHealth(id()) }));
  // Pinned at setup, like the system zoom's: a moving now re-ages the line.
  const pageNow = Date.now();
  const types = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));

  const anchor = createMemo(() => (view.data ? locationIndex(view.data).get(id()) : undefined));
  const tiles = createMemo(() => (view.data ? fleetTiles(view.data) : undefined));
  const [chips, setChips] = createSignal<Chip[]>([]);
  const filterKeys: FilterKey<SystemCluster>[] = [
    { key: "verdict", type: "string", hint: "exact", get: (c) => c.verdict ?? "unknown", values: () => ["outage", "degraded", "incomplete", "healthy"] },
    { key: "system", type: "string", hint: "substring", get: (c) => c.label },
  ];

  // A name-shaped address resolves to the uuid and the URL is rewritten to
  // keep saying what it means, query string included (#759's rule, applied
  // here because the zoom branch runs before the inventory detail's own
  // fallback ever could). Only an unambiguous name resolves: names scope to
  // placement, so a bare name can legally be two rows, and guessing between
  // them would open the wrong building.
  createEffect(() => {
    if (!view.data || anchor()) return;
    const matches = (view.data.locations ?? []).filter((l) => l.name === id());
    if (matches.length === 1) navigate(`/locations/${matches[0].id}?zoom=1`, { replace: true });
  });
  const bands = createMemo<Band[]>(() => {
    if (!view.data) return [];
    const pred = buildPredicate(filterKeys, chips());
    return bandsOf(view.data, byChildOfLocation(id())).map((b) => ({ ...b, clusters: b.clusters.filter(pred) }));
  });
  const holes = createMemo(() => (view.data ? holesUnder(id(), view.data) : new Map()));
  // This subtree's own attention count, unfiltered: what the header chip
  // reports and the chip's click narrows the cards to.
  const ATTENTION = ["outage", "degraded", "incomplete"];
  const attention = createMemo(() => {
    if (!view.data) return 0;
    return bandsOf(view.data, byChildOfLocation(id())).reduce(
      (n, b) => n + b.clusters.filter((c) => c.verdict !== null && c.verdict !== "healthy").length,
      0,
    );
  });
  const attentionOn = () => chips().some((c) => c.key === "verdict" && c.values.some((v) => ATTENTION.includes(v)));
  const toggleAttention = () => {
    const rest = chips().filter((c) => c.key !== "verdict");
    setChips(attentionOn() ? rest : [...rest, { key: "verdict", op: "eq", values: ATTENTION }]);
  };

  const crumbs = createMemo(() => {
    if (!view.data) return [];
    const chain = ancestors(id(), locationIndex(view.data));
    return [
      { key: "fleet", label: "Fleet", onClick: () => navigate("/fleet") },
      // The trail ends at the parent: the current location is the page title,
      // and repeating it as the last crumb would say it twice.
      ...chain.slice(0, -1).map((l) => ({
        key: l.id,
        label: entityLabel(l),
        onClick: () => navigate(`/locations/${l.id}?zoom=1`),
      })),
    ];
  });

  // The types this location may contain: those whose allowed parents name
  // this location's type, or that constrain nothing at all.
  const childTypes = createMemo(() => {
    const t = anchor()?.location_type;
    if (!t) return [];
    return (types.data ?? []).filter((x) => (x.allowed_parent_types ?? []).length === 0 || x.allowed_parent_types!.includes(t));
  });

  return (
    <Page
      title={anchor() ? entityLabel(anchor()!) : "Location"}
      breadcrumb={<Breadcrumb crumbs={crumbs()} />}
    >
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
            placeholder="Filter by verdict or system…"
            header={
              <div data-testid="location-header" class="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
                <HealthBadge verdict={anchor()?.verdict ?? undefined} size="sm" />
                <Show when={locHealth.data && sinceOf(locHealth.data, pageNow)}>
                  {(sc) => <span data-testid="since-line" class="tabular-nums text-base-content/70">since {fmtTime(sc().ts)} · {durationText(sc().ms)}</span>}
                </Show>
                <Show when={attention() > 0}>
                  <button
                    type="button"
                    class="inline-flex cursor-pointer items-center gap-1.5 rounded-field border px-2 py-0.5 text-xs"
                    classList={{ "border-primary bg-primary/10": attentionOn(), "border-base-300": !attentionOn() }}
                    onClick={toggleAttention}
                  >
                    <span class="h-1.5 w-1.5 flex-none rounded-full bg-warning" />
                    {attention()} need attention
                  </button>
                </Show>
              </div>
            }
          >
            <div class="flex flex-col divide-y divide-base-300">
              <For each={bands()}>{(band) => <ZoomBand band={band} view={view.data!} />}</For>
              <Show when={bands().length === 0 && holes().size === 0}>
                <p class="px-4 py-6 text-sm text-base-content/60">Nothing under this location yet.</p>
              </Show>
              <div data-testid="add-location-hole" class="flex flex-wrap items-center gap-3 px-4 py-3">
                <div class="inline-flex flex-col rounded-field border border-dashed border-base-content/25 px-3 py-1.5 text-xs text-base-content/50">
                  <span class="font-medium text-base-content/70">+ Location</span>
                  <Show when={childTypes().length > 0}>
                    <span data-testid="allowed-child-types" class="truncate">{childTypes().map((t) => entityLabel(t).toLowerCase()).join(", ")}</span>
                  </Show>
                </div>
              </div>
            </div>
          </FleetShell>
        </Show>
      </Show>
    </Page>
  );

  function ZoomBand(props: { band: Band; view: FleetView }) {
    const isHere = () => props.band.key === id();
    const bandHoles = () => holes().get(props.band.key) ?? [];
    const counts = () => {
      const b = props.band;
      return b.systemCount === 1 ? "1 system" : `${b.systemCount} systems`;
    };
    return (
      <section data-testid={`zoomband-${props.band.key}`} class="flex gap-4 px-4 py-3">
        <div class="w-60 flex-none">
          <Show
            when={!isHere()}
            fallback={
              <div class="p-1">
                <span class="font-medium">Placed here</span>
                <div class="mt-1 text-xs text-base-content/60">{counts()}</div>
              </div>
            }
          >
            <button
              type="button"
              class="block w-full cursor-pointer rounded-lg p-1 text-left hover:bg-base-content/5"
              onClick={() => navigate(`/locations/${props.band.key}?zoom=1`)}
            >
              <div class="flex items-center gap-2">
                <HealthBadge verdict={props.band.recordedVerdict ?? undefined} size="xs" />
                <span class="min-w-0 truncate font-medium" title={props.band.label}>{props.band.label}</span>
              </div>
              <div class="mt-0.5 flex items-baseline gap-2 truncate text-xs text-base-content/60">
                <Show when={props.band.sublabel}>
                  <span class="text-[10px] uppercase tracking-wider text-base-content/50">{props.band.sublabel}</span>
                </Show>
                <span>{counts()}</span>
              </div>
            </button>
          </Show>
        </div>
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap gap-2">
            <For each={props.band.clusters}>{(cluster) => <SystemCard cluster={cluster} view={props.view} onOpen={(sid) => navigate(`/systems/${sid}?zoom=1`)} />}</For>
            <For each={bandHoles()}>
              {(hole) => (
                <div class="flex w-40 flex-none flex-col justify-center gap-0.5 rounded-md border border-dashed border-primary/40 px-2 py-2 text-xs text-base-content/50">
                  <div class="font-medium text-primary/80">+ System</div>
                  <div class="truncate">{entityLabel(hole)} has none</div>
                </div>
              )}
            </For>
          </div>
        </div>
      </section>
    );
  }
}
