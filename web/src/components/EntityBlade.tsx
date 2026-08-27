import { For, Show, createMemo, createSignal } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import HealthBadge from "./HealthBadge";
import HealthHistory from "./HealthHistory";
import Button from "./Button";
import BladeField from "./BladeField";
import { Maximize } from "./icons";
import { useBlades, useBladeEdit, type BladeDef } from "../lib/blades";
import { FLEET_VIEW_KEY, fleetView, byChildOfLocation, bandsOf } from "../lib/fleet";
import { entityLabel } from "../lib/entities";
import { systemHealth, systemHealthKey, locationHealth, locationHealthKey, transitions } from "../lib/health";
import { systemRoles, systemRolesKey } from "../lib/system_roles";
import { alarmRows, sinceOf, systemZoomVM, componentCards } from "../lib/system_zoom";
import { systemMetrics, systemMetricsKey } from "../lib/system_metrics";
import { dotVerdict, leafAlarmSince, membershipRows, vitalRows } from "../lib/component_leaf";
import { SYSTEMS_KEY, listSystems, updateSystem, deleteSystem } from "../lib/systems";
import { LOCATIONS_KEY, listLocations, updateLocation, deleteLocation } from "../lib/locations";
import { COMPONENTS_KEY, listComponents, updateComponent, deleteComponent } from "../lib/components";
import { componentAlarms, componentAlarmsKey, splitAlarms } from "../lib/alarms";
import { componentSystemsKey, componentSystems } from "../lib/members";
import { describeError, fmtTime } from "../lib/format";
import { durationText } from "../lib/timeline";

// EntityBlade (#799, stage 3 of the ADR-0129 reconciliation): ONE blade per
// fleet kind, a condensed render of the workspace's pure cores. Verdict and
// since lead, then the alarms saying why, the 30-day strip, the members or
// children, and the KPI chips; Expand promotes to the identity route, and the
// label edits in place through the existing gated mutation. Every body
// self-fetches by id, so any page can push any kind; the inventory-era panel
// blades retire from these paths.

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

// One label editor plus the delete, each wired to its existing gated mutation:
// the shared edit half of all three bodies. Binding happens only when the row
// allows the action, so a blade without the permission never grows the pencil
// or the Delete. Delete addresses by uuid (a duplicate name under another
// parent is legal) and confirms first, matching the retired inventory blade.
function useEntityEdit(opts: {
  kindLabel: string;
  id: string;
  name: () => string | undefined;
  label: () => string;
  actions: () => string[];
  save: (name: string, label: string) => Promise<unknown>;
  remove: (id: string) => Promise<unknown>;
  invalidate: () => unknown;
}) {
  const edit = useBladeEdit();
  const blades = useBlades();
  const [draft, setDraft] = createSignal("");
  const [err, setErr] = createSignal<string | null>(null);
  edit.bind({
    editable: () => opts.actions().includes("update") && !!opts.name(),
    seed: () => setDraft(opts.label()),
    save: async () => {
      // Addressed by uuid, like the delete: a duplicate name under another
      // parent is legal, and a bare-name address would 409 on it.
      if (draft().trim() !== opts.label()) await opts.save(opts.id, draft().trim());
      await opts.invalidate();
    },
    destructive: () =>
      opts.actions().includes("delete")
        ? {
            label: "Delete",
            tone: "danger" as const,
            onClick: () => {
              const name = opts.name();
              if (!name || !confirm(`Delete ${opts.kindLabel} "${name}"?`)) return;
              // A failed delete (a 409 on a still-referenced row) keeps the
              // blade open and says so; the blade only closes on success.
              void opts.remove(opts.id).then(
                async () => {
                  blades.close();
                  await opts.invalidate();
                },
                (e: unknown) => setErr(describeError(e)),
              );
            },
          }
        : undefined,
  });
  return { draft, setDraft, err };
}

function SystemBody(props: { id: string }) {
  const qc = useQueryClient();
  const blades = useBlades();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const health = useQuery(() => ({ queryKey: systemHealthKey(props.id), queryFn: () => systemHealth(props.id) }));
  const declared = useQuery(() => ({ queryKey: systemRolesKey(props.id), queryFn: () => systemRoles(props.id) }));
  const metricsQ = useQuery(() => ({ queryKey: systemMetricsKey(props.id), queryFn: () => systemMetrics(props.id) }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));

  const now = Date.now();
  const cluster = () => view.data?.systems?.find((s) => s.id === props.id);
  const row = () => (systems.data ?? []).find((s) => s.id === props.id);
  const alarms = createMemo(() => (health.data && view.data ? alarmRows(health.data, view.data, props.id) : []));
  const body = createMemo(() => {
    if (!health.data || !declared.data || !view.data) return undefined;
    return componentCards(systemZoomVM(health.data, declared.data, view.data, props.id));
  });
  const kpis = createMemo(() => vitalRows(metricsQ.data ?? []));

  const { draft, setDraft, err } = useEntityEdit({
    kindLabel: "system",
    id: props.id,
    name: () => row()?.name,
    label: () => (cluster() ? entityLabel(cluster()!) : ""),
    actions: () => row()?.actions ?? [],
    save: (name, label) => updateSystem(name, { label }),
    remove: (id) => deleteSystem(id),
    invalidate: () => Promise.all([qc.invalidateQueries({ queryKey: [...SYSTEMS_KEY] }), qc.invalidateQueries({ queryKey: [...FLEET_VIEW_KEY] })]),
  });

  const cards = () => {
    const b = body();
    if (!b) return [];
    const grouped = b.groups.flatMap((g) => g.memberCards);
    return [...grouped, ...b.cards];
  };

  return (
    <div class="flex flex-col gap-4 text-sm">
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div class="flex flex-wrap items-center gap-2">
        <HealthBadge verdict={cluster()?.verdict ?? undefined} size="sm" />
        <SinceLine since={health.data ? sinceOf(health.data, now) : undefined} />
      </div>
      <BladeField bind="label" value={() => (cluster() ? entityLabel(cluster()!) : "")} draft={draft} onInput={setDraft} placeholder="Operator label" />
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
      <Show when={health.data}>
        <HealthHistory transitions={transitions(health.data)} verdict={cluster()?.verdict ?? undefined} compact />
      </Show>
      <Show when={cards().length > 0}>
        <div class={section}>
          <span class={eyebrow}>Components</span>
          <For each={cards()}>
            {(c) => (
              <button type="button" class="flex cursor-pointer items-center gap-2 rounded px-1 py-0.5 text-left hover:bg-base-content/5" onClick={() => blades.push({ kind: "component", id: c.componentId })}>
                <span class="h-2 w-2 flex-none rounded-full" classList={{ "bg-error": c.down, "bg-success": !c.down }} />
                <span class="min-w-0 truncate font-mono text-xs">{c.name}</span>
                <span class="ml-auto flex flex-none gap-1">
                  <For each={c.roles}>{(r) => <span class="badge badge-ghost badge-xs">{r.label}</span>}</For>
                </span>
              </button>
            )}
          </For>
        </div>
      </Show>
      <Show when={kpis().length > 0}>
        <div class={section}>
          <span class={eyebrow}>Vitals</span>
          <div class="flex flex-wrap gap-2">
            <For each={kpis()}>
              {(k) => (
                <span class="inline-flex items-baseline gap-1.5 rounded-field border border-base-300 px-2 py-0.5">
                  <span class="text-[10.5px] text-base-content/60">{k.label}</span>
                  <span class="tnum text-xs font-semibold">{String(k.value)}</span>
                </span>
              )}
            </For>
          </div>
        </div>
      </Show>
    </div>
  );
}

function ComponentBody(props: { id: string }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const blades = useBlades();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const alarmsQ = useQuery(() => ({ queryKey: componentAlarmsKey(props.id), queryFn: () => componentAlarms(props.id) }));
  const memberships = useQuery(() => ({ queryKey: componentSystemsKey(props.id), queryFn: () => componentSystems(props.id) }));

  const now = Date.now();
  const row = () => (components.data ?? []).find((c) => c.id === props.id);
  const verdict = () => (view.data ? dotVerdict(view.data, props.id) : null);
  const active = createMemo(() => splitAlarms(alarmsQ.data ?? []).active);
  const systems = createMemo(() => (view.data && memberships.data ? membershipRows(memberships.data, view.data) : []));

  const { draft, setDraft, err } = useEntityEdit({
    kindLabel: "component",
    id: props.id,
    name: () => row()?.name,
    label: () => (row() ? entityLabel(row()!) : ""),
    actions: () => row()?.actions ?? [],
    save: (name, label) => updateComponent(name, { label }),
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
      <BladeField bind="label" value={() => (row() ? entityLabel(row()!) : "")} draft={draft} onInput={setDraft} placeholder="Operator label" />
      <Show when={active().length > 0}>
        <div class={section}>
          <span class={eyebrow}>Why</span>
          <For each={active()}>
            {(a) => <span class="truncate text-xs text-base-content/70">{a.message}</span>}
          </For>
        </div>
      </Show>
      <Show when={systems().length > 0}>
        <div class={section}>
          <span class={eyebrow}>Serves</span>
          <For each={systems()}>
            {(r) => (
              <button type="button" class="cursor-pointer rounded px-1 py-0.5 text-left text-xs hover:bg-base-content/5" onClick={() => { blades.close(); navigate(`/systems/${r.systemId}`); }}>
                {r.label}
              </button>
            )}
          </For>
        </div>
      </Show>
    </div>
  );
}

function LocationBody(props: { id: string }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const blades = useBlades();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const health = useQuery(() => ({ queryKey: locationHealthKey(props.id), queryFn: () => locationHealth(props.id) }));

  const now = Date.now();
  const row = () => (locations.data ?? []).find((l) => l.id === props.id);
  const anchor = () => view.data?.locations?.find((l) => l.id === props.id);
  const clusters = createMemo(() => (view.data ? bandsOf(view.data, byChildOfLocation(props.id)).flatMap((b) => b.clusters) : []));
  const worst = createMemo(() => clusters().filter((c) => c.verdict !== null && c.verdict !== "healthy"));

  const { draft, setDraft, err } = useEntityEdit({
    kindLabel: "location",
    id: props.id,
    name: () => row()?.name,
    label: () => (row() ? entityLabel(row()!) : ""),
    actions: () => row()?.actions ?? [],
    save: (name, label) => updateLocation(name, { label }),
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
      <BladeField bind="label" value={() => (row() ? entityLabel(row()!) : "")} draft={draft} onInput={setDraft} placeholder="Operator label" />
      <div class={section}>
        <span class={eyebrow}>Beneath</span>
        <span class="text-xs text-base-content/60">
          {clusters().length === 1 ? "1 system" : `${clusters().length} systems`}, {worst().length} need attention
        </span>
      </div>
      <Show when={worst().length > 0}>
        <div class={section}>
          <span class={eyebrow}>Needs attention</span>
          <For each={worst()}>
            {(c) => (
              <button type="button" class="flex cursor-pointer items-center gap-2 rounded px-1 py-0.5 text-left hover:bg-base-content/5" onClick={() => { blades.close(); navigate(`/systems/${c.systemId}`); }}>
                <HealthBadge verdict={c.verdict ?? undefined} size="xs" />
                <span class="min-w-0 truncate text-xs">{c.label}</span>
              </button>
            )}
          </For>
        </div>
      </Show>
      <Show when={health.data}>
        <HealthHistory transitions={transitions(health.data)} verdict={anchor()?.verdict ?? undefined} compact />
      </Show>
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
