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
import { systemHealth, systemHealthKey, locationHealth, locationHealthKey } from "../lib/health";
import { alarmRows, sinceOf } from "../lib/system_zoom";
import { dotVerdict, leafAlarmSince } from "../lib/component_leaf";
import { SYSTEMS_KEY, listSystems, deleteSystem } from "../lib/systems";
import { LOCATIONS_KEY, listLocations, deleteLocation } from "../lib/locations";
import { COMPONENTS_KEY, listComponents, deleteComponent } from "../lib/components";
import { componentAlarms, componentAlarmsKey, splitAlarms } from "../lib/alarms";
import { describeError, fmtTime } from "../lib/format";
import { durationText } from "../lib/timeline";

// EntityBlade (#799, refit in #826): ONE blade per fleet kind. Verdict and
// since-when lead and the active alarms say why (the monitoring header), and
// the rest of the blade IS the one EntityForm, read or edit through the
// blade's own footer: the original blade vision, where view and edit are one
// component and the blade is just where the operator clicked. The members,
// the 30-day strip, and the vitals live on the workspace, one Expand away.
// Every body self-fetches by id, so any page can push any kind.

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

function SystemBody(props: { id: string }) {
  const qc = useQueryClient();
  const blades = useBlades();
  const edit = useBladeEdit();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const health = useQuery(() => ({ queryKey: systemHealthKey(props.id), queryFn: () => systemHealth(props.id) }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));

  const now = Date.now();
  const cluster = () => view.data?.systems?.find((s) => s.id === props.id);
  const row = () => (systems.data ?? []).find((s) => s.id === props.id);
  const alarms = createMemo(() => (health.data && view.data ? alarmRows(health.data, view.data, props.id) : []));

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
      <Show when={alarms().length > 0}>
        <div class={section}>
          <span class={eyebrow}>Why</span>
          <For each={alarms()}>
            {(a) => (
              <div class="flex items-baseline gap-2">
                <Show when={a.componentId} fallback={<span class="font-mono text-xs text-base-content/80">{a.component}</span>}>
                  {(cid) => <button type="button" class="cursor-pointer font-mono text-xs text-base-content/80 hover:underline" onClick={() => blades.push({ kind: "component", id: cid() })}>{a.component}</button>}
                </Show>
                <span class="min-w-0 flex-1 truncate text-xs text-base-content/60">{a.message}</span>
              </div>
            )}
          </For>
        </div>
      </Show>
      <EntityForm kind="system" id={props.id} slot={edit} host="blade" destructive={destructive} />
    </div>
  );
}

function ComponentBody(props: { id: string }) {
  const qc = useQueryClient();
  const edit = useBladeEdit();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const alarmsQ = useQuery(() => ({ queryKey: componentAlarmsKey(props.id), queryFn: () => componentAlarms(props.id) }));

  const now = Date.now();
  const row = () => (components.data ?? []).find((c) => c.id === props.id);
  const verdict = () => (view.data ? dotVerdict(view.data, props.id) : null);
  const active = createMemo(() => splitAlarms(alarmsQ.data ?? []).active);

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
      <Show when={active().length > 0}>
        <div class={section}>
          <span class={eyebrow}>Why</span>
          <For each={active()}>
            {(a) => <span class="truncate text-xs text-base-content/70">{a.message}</span>}
          </For>
        </div>
      </Show>
      <EntityForm kind="component" id={props.id} slot={edit} host="blade" destructive={destructive} />
    </div>
  );
}

function LocationBody(props: { id: string }) {
  const qc = useQueryClient();
  const edit = useBladeEdit();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const health = useQuery(() => ({ queryKey: locationHealthKey(props.id), queryFn: () => locationHealth(props.id) }));

  const now = Date.now();
  const row = () => (locations.data ?? []).find((l) => l.id === props.id);
  const anchor = () => view.data?.locations?.find((l) => l.id === props.id);

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
