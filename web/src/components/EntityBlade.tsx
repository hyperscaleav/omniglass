import { For, Show, createMemo, createSignal } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import HealthBadge from "./HealthBadge";
import Button from "./Button";
import EntityForm from "./EntityForm";
import { Maximize } from "./icons";
import { useBlades, useBladeEdit, type BladeDef, type BladeDestructive } from "../lib/blades";
import { FLEET_VIEW_KEY, fleetView } from "../lib/fleet";
import { entityLabel } from "../lib/entities";
import { locationHealth, locationHealthKey } from "../lib/health";
import { sinceOf } from "../lib/system_zoom";
import { LOCATIONS_KEY, listLocations, deleteLocation } from "../lib/locations";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { contentsOf, systemsAtLocation } from "../lib/detail";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { describeError, fmtTime } from "../lib/format";
import { durationText } from "../lib/timeline";
import ComponentDetail from "./ComponentDetail";
import SystemDetail from "./SystemDetail";

// EntityBlade (#799, refit in #826, the glance since #872): ONE blade per
// fleet kind, and it is where an operator lands from a row. HealthState and
// since-when lead, the active alarms say why with their severity, and the
// context the operator came for follows: a system's location and its standard
// and size, a component's location and the systems it serves with its slot in
// each, a location's systems with their health results. Then the form's identity,
// placement and tags, read or edit through the blade's own footer.
// Configuration (slots, properties, their cascade) and the depth (members,
// history, vitals) are the detail view's, one Expand away. Every body
// self-fetches by id, so any page can push any kind.

const section = "flex flex-col gap-1.5";
const eyebrow = "eyebrow";

function SinceLine(props: { since: { ts: string; ms: number } | null | undefined }) {
  return (
    <Show when={props.since}>
      {(s) => <span data-testid="blade-since" class="tabular-nums text-xs text-base-content/60">since {fmtTime(s().ts)} · {durationText(s().ms)}</span>}
    </Show>
  );
}

function ExpandButton(props: { to: string }) {
  const navigate = useNavigate();
  const blades = useBlades();
  return (
    <Button
      square
      icon={Maximize}
      title="Expand"
      label="Expand"
      onClick={() => {
        blades.close();
        navigate(props.to);
      }}
    />
  );
}

// The blade's destructive action, folded into the form's bind: Delete
// addresses by uuid (a duplicate name under another parent is legal) and
// confirms first. A failed delete (a 409 on a still-referenced row) keeps the
// blade open and says so; the blade only closes on success.
function useDelete(opts: {
  kindLabel: string;
  id: string;
  name: () => string | undefined;
  actions: () => string[];
  remove: (id: string) => Promise<unknown>;
  invalidate: () => unknown;
}) {
  const blades = useBlades();
  const [err, setErr] = createSignal<string | null>(null);
  const destructive = (): BladeDestructive | undefined =>
    opts.actions().includes("delete")
      ? {
          label: "Delete",
          tone: "danger" as const,
          onClick: () => {
            const name = opts.name();
            if (!name || !confirm(`Delete ${opts.kindLabel} "${name}"?`)) return;
            void opts.remove(opts.id).then(
              async () => {
                blades.close();
                await opts.invalidate();
              },
              (e: unknown) => setErr(describeError(e)),
            );
          },
        }
      : undefined;
  return { destructive, err };
}

function LocationBody(props: { id: string }) {
  const qc = useQueryClient();
  const blades = useBlades();
  const edit = useBladeEdit();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const health = useQuery(() => ({ queryKey: locationHealthKey(props.id), queryFn: () => locationHealth(props.id) }));

  const now = Date.now();
  const row = () => (locations.data ?? []).find((l) => l.id === props.id);
  const anchor = () => view.data?.locations?.find((l) => l.id === props.id);
  const here = createMemo(() => (view.data ? systemsAtLocation(view.data, props.id) : []));
  const types = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));
  const contents = () => (view.data ? contentsOf(view.data, props.id, types.data ?? []) : "");

  const { destructive, err } = useDelete({
    kindLabel: "location",
    id: props.id,
    name: () => row()?.name,
    actions: () => row()?.actions ?? [],
    remove: (id) => deleteLocation(id),
    invalidate: () => Promise.all([qc.invalidateQueries({ queryKey: [...LOCATIONS_KEY] }), qc.invalidateQueries({ queryKey: [...FLEET_VIEW_KEY] })]),
  });

  return (
    <div class="flex flex-col gap-4 text-sm">
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div class="flex flex-wrap items-center gap-2">
        <HealthBadge health={anchor()?.health ?? undefined} size="sm" />
        <SinceLine since={health.data ? sinceOf(health.data, now) : undefined} />
      </div>
      {/* The locations beneath it; a room holding systems and no locations says
          nothing about locations (its systems are listed below). */}
      <Show when={contents() !== "Empty" || here().length === 0}>
        <div data-testid="blade-contents" class={section}>
          <span class={eyebrow}>Holds</span>
          <span class="text-base-content/70">{contents()}</span>
        </div>
      </Show>
      <Show when={here().length > 0}>
        <div data-testid="blade-systems" class={section}>
          <span class={eyebrow}>{here().length === 1 ? "System here" : "Systems here"}</span>
          <For each={here()}>
            {(s) => (
              <button type="button" class="flex cursor-pointer items-center gap-2 text-left hover:underline" onClick={() => blades.push({ kind: "system", id: s.id })}>
                <span>{entityLabel(s)}</span>
                <HealthBadge health={s.health ?? undefined} size="xs" />
              </button>
            )}
          </For>
        </div>
      </Show>
      <EntityForm kind="location" id={props.id} slot={edit} host="blade" destructive={destructive} />
    </div>
  );
}

export const systemBlade: BladeDef = {
  Title: (p) => {
    const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
    const s = () => view.data?.systems?.find((x) => x.id === p.id);
    return <>{s() ? entityLabel(s()!) : "System"}</>;
  },
  // The system page's own body (#2423): a blade shows what the page shows.
  Body: (p) => <SystemDetail id={p.id} host="blade" />,
  headerExtra: (p) => <ExpandButton to={`/systems/${p.id}`} />,
  width: "wide",
};

export const componentBlade: BladeDef = {
  Title: (p) => {
    const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
    const c = () => (components.data ?? []).find((x) => x.id === p.id);
    return <>{c() ? entityLabel(c()!) : "Component"}</>;
  },
  // The component page's own body (#2423): a blade shows what the page shows.
  Body: (p) => <ComponentDetail id={p.id} host="blade" />,
  headerExtra: (p) => <ExpandButton to={`/components/${p.id}`} />,
  width: "wide",
};

export const locationBlade: BladeDef = {
  Title: (p) => {
    const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
    const l = () => (locations.data ?? []).find((x) => x.id === p.id);
    return <>{l() ? entityLabel(l()!) : "Location"}</>;
  },
  Body: LocationBody,
  headerExtra: (p) => <ExpandButton to={`/locations/${p.id}`} />,
};
