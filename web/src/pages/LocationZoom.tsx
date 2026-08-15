import { For, Show, createEffect, createMemo } from "solid-js";
import { useNavigate, useParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import HealthBadge from "../components/HealthBadge";
import SystemCard from "../components/SystemCard";
import ZoomLadder from "../components/ZoomLadder";
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
} from "../lib/fleet";
import { zoomChips } from "../lib/zoom";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";

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
  const types = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));

  const anchor = createMemo(() => (view.data ? locationIndex(view.data).get(id()) : undefined));

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
  const bands = createMemo<Band[]>(() => (view.data ? bandsOf(view.data, byChildOfLocation(id())) : []));
  const holes = createMemo(() => (view.data ? holesUnder(id(), view.data) : new Map()));
  const chips = createMemo(() => (view.data ? zoomChips("location", { locationId: id() }, view.data) : []));

  const crumbs = createMemo(() => {
    if (!view.data) return [];
    const chain = ancestors(id(), locationIndex(view.data));
    return [
      { key: "fleet", label: "Fleet", onClick: () => navigate("/fleet") },
      ...chain.map((l, i) => ({
        key: l.id,
        label: entityLabel(l),
        onClick: i === chain.length - 1 ? undefined : () => navigate(`/locations/${l.id}?zoom=1`),
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
      subtitle={anchor()?.location_type ?? ""}
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
          <ZoomLadder
            chips={chips()}
            onSelect={(chip) => {
              if (chip.id === "fleet") navigate("/fleet");
            }}
          />
          <div class="flex flex-col gap-5">
            <For each={bands()}>{(band) => <ZoomBand band={band} view={view.data!} />}</For>
            <Show when={bands().length === 0 && holes().size === 0}>
              <p class="text-sm text-base-content/60">Nothing under this location yet.</p>
            </Show>
          </div>
          <Show when={childTypes().length > 0}>
            <div data-testid="allowed-child-types" class="flex flex-wrap items-center gap-2 border-t border-base-content/10 pt-3 text-xs text-base-content/60">
              <span>May contain:</span>
              <For each={childTypes()}>
                {(t) => <span class="rounded-md border border-base-content/15 px-1.5 py-0.5">{entityLabel(t)}</span>}
              </For>
            </div>
          </Show>
        </Show>
      </Show>
    </Page>
  );

  function ZoomBand(props: { band: Band; view: FleetView }) {
    const isHere = () => props.band.key === id();
    const bandHoles = () => holes().get(props.band.key) ?? [];
    const counts = () => {
      const b = props.band;
      const parts = [b.systemCount === 1 ? "1 system" : `${b.systemCount} systems`];
      if (b.depth > 1) parts.push(`${b.depth} levels`);
      return parts.join(", ");
    };
    return (
      <section data-testid={`zoomband-${props.band.key}`} class="flex gap-4">
        <div class="w-52 flex-none">
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
                <span class="truncate font-medium">{props.band.label}</span>
              </div>
              <div class="mt-1 flex items-center gap-2">
                <Show when={props.band.sublabel}>
                  <span class="text-[10px] uppercase tracking-wider text-base-content/50">{props.band.sublabel}</span>
                </Show>
              </div>
              <div class="mt-1 text-xs text-base-content/60">{counts()}</div>
            </button>
          </Show>
        </div>
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap gap-3">
            <For each={props.band.clusters}>{(cluster) => <SystemCard cluster={cluster} onOpen={(sid) => navigate(`/systems/${sid}?zoom=1`)} />}</For>
          </div>
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
