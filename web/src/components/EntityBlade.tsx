import { For, Show, createMemo, createSignal } from "solid-js";
import PlaceCard from "./PlaceCard";
import { useNavigate } from "@solidjs/router";
import { useQueries, useQuery, useQueryClient } from "@tanstack/solid-query";
import HealthBadge from "./HealthBadge";
import Button from "./Button";
import EntityForm from "./EntityForm";
import { Maximize } from "./icons";
import { useBlades, useBladeEdit, type BladeDef, type BladeDestructive } from "../lib/blades";
import { FLEET_VIEW_KEY, fleetView } from "../lib/fleet";
import { entityLabel } from "../lib/entities";
import { systemHealth, systemHealthKey, locationHealth, locationHealthKey } from "../lib/health";
import { alarmRows, sinceOf } from "../lib/system_zoom";
import { dotVerdict, leafAlarmSince } from "../lib/component_leaf";
import { SYSTEMS_KEY, listSystems, deleteSystem } from "../lib/systems";
import { LOCATIONS_KEY, listLocations, deleteLocation } from "../lib/locations";
import { COMPONENTS_KEY, listComponents, deleteComponent } from "../lib/components";
import { componentAlarms, componentAlarmsKey, splitAlarms } from "../lib/alarms";
import { componentSystems, componentSystemsKey } from "../lib/members";
import { systemRoles, systemRolesKey } from "../lib/system_roles";
import { STANDARDS_KEY, listStandards } from "../lib/standards";
import { slotStrip } from "../lib/slot_strip";
import { contentsOf, rolesOf, systemsAtPlace } from "../lib/detail";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { describeError, fmtTime } from "../lib/format";
import { durationText } from "../lib/timeline";

// EntityBlade (#799, refit in #826, the glance since #872): ONE blade per
// fleet kind, and it is where an operator lands from a row. Verdict and
// since-when lead, the active alarms say why with their severity, and the
// context the operator came for follows: a system's place and its standard
// and size, a component's place and the systems it serves with its role in
// each, a place's systems with their verdicts. Then the form's identity,
// placement and tags, read or edit through the blade's own footer.
// Configuration (roles, properties, their cascade) and the depth (members,
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

// Why, with the severity first: the reason beside the red light, each
// naming the component when there is one to open.
function Why(props: { rows: { severity: string; message: string; component?: string; onOpen?: () => void }[] }) {
  return (
    <Show when={props.rows.length > 0}>
      <div data-testid="blade-why" class={section}>
        <span class={eyebrow}>Why</span>
        <For each={props.rows}>
          {(a) => (
            <div class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
              <span class="badge badge-xs" classList={{ "badge-error badge-soft": a.severity === "critical", "badge-warning badge-soft": a.severity !== "critical" }}>{a.severity}</span>
              <Show when={a.component}>
                <Show when={a.onOpen} fallback={<span class="font-data text-xs text-base-content/80">{a.component}</span>}>
                  <button type="button" class="cursor-pointer font-data text-xs text-base-content/80 hover:underline" onClick={() => a.onOpen!()}>{a.component}</button>
                </Show>
              </Show>
              <span class="min-w-0 flex-1 text-xs text-base-content/70">{a.message}</span>
            </div>
          )}
        </For>
      </div>
    </Show>
  );
}

function SystemBody(props: { id: string }) {
  const qc = useQueryClient();
  const blades = useBlades();
  const edit = useBladeEdit();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const health = useQuery(() => ({ queryKey: systemHealthKey(props.id), queryFn: () => systemHealth(props.id) }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards }));

  const now = Date.now();
  const cluster = () => view.data?.systems?.find((s) => s.id === props.id);
  const row = () => (systems.data ?? []).find((s) => s.id === props.id);
  const alarms = createMemo(() => (health.data && view.data ? alarmRows(health.data, view.data, props.id) : []));
  const standard = () => {
    const h = row()?.standard;
    const st = h ? (standards.data ?? []).find((x) => x.name === h) : undefined;
    return st ? entityLabel(st) : h ?? "";
  };
  const strip = createMemo(() => (health.data ? slotStrip(health.data) : undefined));
  const count = () => (cluster()?.dots ?? []).length;

  const { destructive, err } = useDelete({
    kindLabel: "system",
    id: props.id,
    name: () => row()?.name,
    actions: () => row()?.actions ?? [],
    remove: (id) => deleteSystem(id),
    invalidate: () => Promise.all([qc.invalidateQueries({ queryKey: [...SYSTEMS_KEY] }), qc.invalidateQueries({ queryKey: [...FLEET_VIEW_KEY] })]),
  });

  return (
    <div class="flex flex-col gap-4 text-sm">
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div class="flex flex-wrap items-center gap-2">
        <HealthBadge verdict={cluster()?.verdict ?? undefined} size="sm" />
        <SinceLine since={health.data ? sinceOf(health.data, now) : undefined} />
      </div>
      <Why rows={alarms().map((a) => ({ severity: a.severity, message: a.message, component: a.component, onOpen: a.componentId ? () => blades.push({ kind: "component", id: a.componentId! }) : undefined }))} />
      <Show when={cluster()?.location}>{(pid) => <PlaceCard placeId={pid()} showName />}</Show>
      <div data-testid="blade-brief" class="flex flex-wrap items-center gap-x-2 text-base-content/70">
        <Show when={standard()}><span>{standard()}</span><span class="text-base-content/30">·</span></Show>
        <span class="tabular-nums">{count()} {count() === 1 ? "component" : "components"}</span>
        <Show when={strip() && strip()!.empty > 0}>
          <span class="text-base-content/30">·</span>
          <span class="tabular-nums text-incomplete">{strip()!.filled} of {strip()!.total} slots filled</span>
        </Show>
      </div>
      <EntityForm kind="system" id={props.id} slot={edit} host="blade" destructive={destructive} />
    </div>
  );
}

function ComponentBody(props: { id: string }) {
  const qc = useQueryClient();
  const blades = useBlades();
  const edit = useBladeEdit();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const alarmsQ = useQuery(() => ({ queryKey: componentAlarmsKey(props.id), queryFn: () => componentAlarms(props.id) }));
  const members = useQuery(() => ({ queryKey: componentSystemsKey(props.id), queryFn: () => componentSystems(props.id) }));

  const now = Date.now();
  const row = () => (components.data ?? []).find((c) => c.id === props.id);
  const verdict = () => (view.data ? dotVerdict(view.data, props.id) : null);
  const active = createMemo(() => splitAlarms(alarmsQ.data ?? []).active);
  // The systems it serves, by uuid, each with the role it fills there.
  const served = createMemo(() =>
    (members.data ?? [])
      .map((m) => (view.data?.systems ?? []).find((s) => s.id === m.system_id || (!m.system_id && s.name === m.system)))
      .filter((s): s is NonNullable<typeof s> => !!s),
  );
  const roleReads = useQueries(() => ({
    queries: served().map((s) => ({ queryKey: systemRolesKey(s.id), queryFn: () => systemRoles(s.id), staleTime: 30_000 })),
  }));
  const primarySystem = () => served()[0];
  const placeId = () => row()?.location_id ?? primarySystem()?.location ?? null;

  const { destructive, err } = useDelete({
    kindLabel: "component",
    id: props.id,
    name: () => row()?.name,
    actions: () => row()?.actions ?? [],
    remove: (id) => deleteComponent(id),
    invalidate: () => Promise.all([qc.invalidateQueries({ queryKey: [...COMPONENTS_KEY] }), qc.invalidateQueries({ queryKey: [...FLEET_VIEW_KEY] })]),
  });

  return (
    <div class="flex flex-col gap-4 text-sm">
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div class="flex flex-wrap items-center gap-2">
        <HealthBadge verdict={verdict() ?? undefined} size="sm" />
        <SinceLine since={leafAlarmSince(alarmsQ.data ?? [], now)} />
      </div>
      <Why rows={active().map((a) => ({ severity: a.severity, message: a.message }))} />
      <Show when={placeId()}>
        {(pid) => <PlaceCard placeId={pid()} showName provenance={row()?.location_id ? "set here" : "from its system"} />}
      </Show>
      <Show when={served().length > 0}>
        <div data-testid="blade-serves" class={section}>
          <span class={eyebrow}>Serves</span>
          <For each={served()}>
            {(s, i) => (
              <div class="flex flex-wrap items-center gap-2">
                <button type="button" class="cursor-pointer hover:underline" onClick={() => blades.push({ kind: "system", id: s.id })}>{entityLabel(s)}</button>
                <For each={roleReads[i()]?.data ? rolesOf(row()?.name ?? "", roleReads[i()]!.data!) : []}>
                  {(r) => <span class="badge badge-ghost badge-sm">{r}</span>}
                </For>
              </div>
            )}
          </For>
        </div>
      </Show>
      <EntityForm kind="component" id={props.id} slot={edit} host="blade" destructive={destructive} />
    </div>
  );
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
  const here = createMemo(() => (view.data ? systemsAtPlace(view.data, props.id) : []));
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
        <HealthBadge verdict={anchor()?.verdict ?? undefined} size="sm" />
        <SinceLine since={health.data ? sinceOf(health.data, now) : undefined} />
      </div>
      <div data-testid="blade-contents" class={section}>
        <span class={eyebrow}>Holds</span>
        <span class="text-base-content/70">{contents()}</span>
      </div>
      <Show when={here().length > 0}>
        <div data-testid="blade-systems" class={section}>
          <span class={eyebrow}>{here().length === 1 ? "System here" : "Systems here"}</span>
          <For each={here()}>
            {(s) => (
              <button type="button" class="flex cursor-pointer items-center gap-2 text-left hover:underline" onClick={() => blades.push({ kind: "system", id: s.id })}>
                <span>{entityLabel(s)}</span>
                <HealthBadge verdict={s.verdict ?? undefined} size="xs" />
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
  Body: SystemBody,
  headerExtra: (p) => <ExpandButton to={`/systems/${p.id}`} />,
};

export const componentBlade: BladeDef = {
  Title: (p) => {
    const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
    const c = () => (components.data ?? []).find((x) => x.id === p.id);
    return <>{c() ? entityLabel(c()!) : "Component"}</>;
  },
  Body: ComponentBody,
  headerExtra: (p) => <ExpandButton to={`/components/${p.id}`} />,
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
