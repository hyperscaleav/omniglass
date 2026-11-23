import { For, Show, createMemo, createSignal, createUniqueId, type JSX } from "solid-js";
import { A } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import ConfirmDialog from "./ConfirmDialog";
import Eyebrow from "./Eyebrow";
import HealthBadge from "./HealthBadge";
import { Check, Plus, RefreshCw, Trash, X } from "./icons";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { FLEET_VIEW_KEY, fleetView } from "../lib/fleet";
import { REACHABILITY_KEY, getReachability } from "../lib/reachability";
import { dotHealth, LEAF_POLL_MS } from "../lib/component_leaf";
import { entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";
import {
  DEPENDENCY_KINDS,
  addDependency,
  dependenciesKey,
  dependencyChoices,
  edgeKey,
  groupDependents,
  kindLabel,
  listDependencies,
  refusalWords,
  removeDependency,
  upstreamHolds,
  type Dependency,
  type Dependent,
  type DependencyKind,
} from "../lib/dependencies";

// DependenciesPanel is a component's dependency edges (#2428, #1648), both
// ways: UPSTREAM is what this component needs (power from an outlet, a host,
// a path it is reached through) with each upstream's health and whether it is
// holding this component right now, and DOWNSTREAM is what needs this one, so
// "the switch is down, which holds these six devices" reads off the switch's
// page. Rows link to the other component.
//
// An edge is a fact about the dependent, so adding and removing are on the
// UPSTREAM list and need update on this component (canUpdate, from the page's
// own row); a downstream row is edited from the dependent's page. A viewer
// sees both lists read-only. The server stays the authority: a cycle, a
// self-reference, a repeated edge or a missing outlet is refused and its
// reason shows in place, beside the form that asked.
export default function DependenciesPanel(props: { component: string; canUpdate: boolean }): JSX.Element {
  const qc = useQueryClient();
  const depsQ = useQuery(() => ({
    queryKey: dependenciesKey(props.component),
    queryFn: () => listDependencies(props.component),
    refetchInterval: LEAF_POLL_MS,
  }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  // The same read the page's collection section polls: an endpoint's gate
  // names the upstream that holds it.
  const reach = useQuery(() => ({ queryKey: REACHABILITY_KEY(props.component), queryFn: () => getReachability(props.component) }));

  const upstreams = createMemo(() => depsQ.data?.upstreams ?? []);
  const dependents = createMemo(() => depsQ.data?.dependents ?? []);
  const downstream = createMemo(() => groupDependents(dependents()));
  const hidden = () => depsQ.data?.hidden ?? 0;
  const holds = createMemo(() => upstreamHolds(reach.data));

  const labelOf = (id: string, name: string) => {
    const hit = (components.data ?? []).find((c) => c.id === id);
    return hit ? entityLabel(hit) : name;
  };
  const healthOf = (id: string) => (view.data ? (dotHealth(view.data, id) ?? undefined) : undefined);

  const [adding, setAdding] = createSignal(false);
  const [query, setQuery] = createSignal("");
  const [picked, setPicked] = createSignal("");
  const [kind, setKind] = createSignal<DependencyKind>("power");
  const [outlet, setOutlet] = createSignal("");
  const [refusal, setRefusal] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);
  const formId = createUniqueId();

  const choices = createMemo(() => dependencyChoices(components.data ?? [], props.component, query()));
  const refresh = () => qc.invalidateQueries({ queryKey: dependenciesKey(props.component) });
  const canAdd = () => !!picked() && (kind() !== "power" || !!outlet().trim());

  const openForm = () => {
    setAdding(!adding());
    setQuery("");
    setPicked("");
    setOutlet("");
    setRefusal(null);
  };

  async function add() {
    if (!canAdd()) return;
    setBusy(true);
    setRefusal(null);
    try {
      await addDependency(props.component, picked(), kind(), outlet().trim());
      setAdding(false);
      setPicked("");
      setQuery("");
      setOutlet("");
      await refresh();
    } catch (e) {
      setRefusal(refusalWords(e));
      if ((e as { status?: number } | null)?.status === 404) void qc.invalidateQueries({ queryKey: COMPONENTS_KEY });
    } finally {
      setBusy(false);
    }
  }

  // The upstream a remove is asked of: the dialog is open while it is set, and
  // a refusal shows in the dialog, beside the button that asked.
  const [removing, setRemoving] = createSignal<Dependency | null>(null);
  const [removeRefusal, setRemoveRefusal] = createSignal<string | null>(null);

  async function remove(d: Dependency) {
    setBusy(true);
    setRemoveRefusal(null);
    try {
      await removeDependency(props.component, d.kind, d.upstream_id);
      setRemoving(null);
      await refresh();
    } catch (e) {
      setRemoveRefusal(refusalWords(e));
      if ((e as { status?: number } | null)?.status === 404) void refresh();
    } finally {
      setBusy(false);
    }
  }

  const failure = () => depsQ.error;
  const shown = () => !!failure() || props.canUpdate || upstreams().length + dependents().length + hidden() > 0;

  const KindBadge = (p: { kind: DependencyKind; outlet?: string }) => (
    <span class="badge badge-ghost badge-sm font-data" title={DEPENDENCY_KINDS.find((k) => k.kind === p.kind)?.hint}>
      {kindLabel(p.kind)}
      <Show when={p.outlet}> · outlet {p.outlet}</Show>
    </span>
  );

  const UpstreamRow = (p: { d: Dependency }) => (
    <li data-testid={`dep-up-${edgeKey(p.d)}`} class="flex items-center gap-2 px-3 py-1.5">
      <A href={`/components/${p.d.upstream_id}`} class="min-w-0 flex-1 truncate hover:underline">{labelOf(p.d.upstream_id, p.d.upstream)}</A>
      <KindBadge kind={p.d.kind} outlet={p.d.outlet} />
      <Show when={holds().has(p.d.upstream_id)}>
        <span data-testid="dep-holding" class="badge badge-error badge-sm badge-outline" title="This upstream's gate is closed on this component's endpoints, so they are held until it opens.">holding this</span>
      </Show>
      <HealthBadge health={healthOf(p.d.upstream_id)} size="sm" />
      <Show when={props.canUpdate}>
        <Button square size="xs" icon={X} label={`Remove ${labelOf(p.d.upstream_id, p.d.upstream)} as ${p.d.kind} upstream`} title="Remove" disabled={busy()} onClick={() => { setRemoveRefusal(null); setRemoving(p.d); }} />
      </Show>
    </li>
  );

  const DownstreamRow = (p: { dep: Dependent }) => (
    <li data-testid={`dep-down-${p.dep.id}`} class="flex items-center gap-2 px-3 py-1.5">
      <A href={`/components/${p.dep.id}`} class="min-w-0 flex-1 truncate hover:underline">{labelOf(p.dep.id, p.dep.name)}</A>
      <For each={p.dep.edges}>{(e) => <KindBadge kind={e.kind} outlet={e.outlet} />}</For>
      <HealthBadge health={healthOf(p.dep.id)} size="sm" />
    </li>
  );

  return (
    <Show when={shown()}>
      <section data-testid="leaf-dependencies" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
        <Eyebrow
          label="Dependencies"
          hint="What this component needs and what needs it, apart from where it sits. A power edge is fed from an outlet of the upstream, a host edge is hosted by it, a path edge is reached through it. When an upstream goes down it holds the components that depend on it."
          right={
            <span class="flex items-center gap-3">
              <span class="text-xs tabular-nums text-base-content/55">{upstreams().length} upstream · {downstream().length + hidden()} downstream</span>
              <Show when={props.canUpdate}>
                <Button size="xs" icon={Plus} expanded={adding()} controls={`dep-form-${formId}`} onClick={openForm}>Add upstream</Button>
              </Show>
            </span>
          }
        />
        <Show when={failure()}>
          {(err) => (
            <div role="alert" class="mt-2 flex items-center gap-2 rounded-box border border-error/40 bg-error/5 px-3 py-2 text-xs text-error">
              <span class="flex-1">Could not read its dependencies: {describeError(err())}</span>
              <Button size="xs" icon={RefreshCw} onClick={() => void refresh()}>Retry</Button>
            </div>
          )}
        </Show>

        <Show when={props.canUpdate && adding()}>
          <div id={`dep-form-${formId}`} data-testid="dep-form" class="mt-3 flex flex-col gap-1.5 rounded-field border border-base-300 bg-base-200/60 p-2.5">
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="text-xs text-base-content/60">This component depends on</span>
              <input
                type="search"
                class="input input-bordered input-sm w-44 font-data"
                placeholder="Filter components"
                aria-label="Filter components"
                value={query()}
                onInput={(e) => {
                  setQuery(e.currentTarget.value);
                  setPicked("");
                }}
              />
              <select
                class="select select-bordered select-sm min-w-0 max-w-xs flex-1"
                aria-label="Upstream component"
                value={picked()}
                onChange={(e) => setPicked(e.currentTarget.value)}
              >
                <option value="">{choices().length === 0 ? "No component matches" : `Pick a component (${choices().length})…`}</option>
                <For each={choices()}>{(c) => <option value={c.id}>{entityLabel(c)}</option>}</For>
              </select>
              <select class="select select-bordered select-sm" aria-label="Dependency kind" value={kind()} onChange={(e) => setKind(e.currentTarget.value as DependencyKind)}>
                <For each={DEPENDENCY_KINDS}>{(k) => <option value={k.kind}>{k.label}</option>}</For>
              </select>
              <Show when={kind() === "power"}>
                <input
                  class="input input-bordered input-sm w-24 font-data"
                  placeholder="Outlet"
                  aria-label="Outlet"
                  value={outlet()}
                  onInput={(e) => setOutlet(e.currentTarget.value)}
                />
              </Show>
              <Button size="sm" intent="action" icon={Check} label="Add dependency" loading={busy()} disabled={!canAdd()} onClick={() => void add()}>Add</Button>
              <Button square size="sm" icon={X} label="Cancel" title="Cancel" disabled={busy()} onClick={() => setAdding(false)} />
            </div>
            <Show when={refusal()}>
              {(r) => <div role="alert" class="rounded-box border border-error/40 bg-error/5 px-3 py-2 text-xs text-error">{r()}</div>}
            </Show>
          </div>
        </Show>

        <Show when={upstreams().length + downstream().length + hidden() > 0} fallback={<p class="mt-2 text-base-content/60">No dependencies recorded.</p>}>
        <div class="mt-3 grid gap-4 md:grid-cols-2">
          <div class="flex min-w-0 flex-col gap-1.5">
            <Eyebrow label="Upstream" hint="What this component needs. If one of these is down, it holds this component." />
            <Show when={upstreams().length > 0} fallback={<p class="text-base-content/60">Depends on nothing.</p>}>
              <ul data-testid="dep-upstream" class="divide-y divide-base-300 rounded-field border border-base-300">
                <For each={upstreams()}>{(d) => <UpstreamRow d={d} />}</For>
              </ul>
            </Show>
          </div>
          <div class="flex min-w-0 flex-col gap-1.5">
            <Eyebrow label="Downstream" hint="What needs this component. If this one goes down, it holds these." />
            <Show when={dependents().length + hidden() > 0} fallback={<p class="text-base-content/60">Nothing depends on it.</p>}>
              <ul data-testid="dep-downstream" class="divide-y divide-base-300 rounded-field border border-base-300">
                <For each={downstream()}>{(dep) => <DownstreamRow dep={dep} />}</For>
                <Show when={hidden() > 0}>
                  <li class="px-3 py-1.5 text-xs text-base-content/55">and {hidden()} more you cannot see</li>
                </Show>
              </ul>
            </Show>
          </div>
        </div>
        </Show>
      <ConfirmDialog
        open={!!removing()}
        onClose={() => setRemoving(null)}
        title="Remove this dependency?"
        confirmLabel="Remove"
        confirmIcon={Trash}
        busy={busy()}
        error={removeRefusal()}
        onConfirm={() => { const d = removing(); if (d) void remove(d); }}
      >
        <Show when={removing()}>
          {(d) => (
            <p class="text-sm">
              <span class="font-semibold">{labelOf(d().upstream_id, d().upstream)}</span> stops being a {d().kind} upstream of this
              component, so when it goes down it no longer holds this one.
            </p>
          )}
        </Show>
      </ConfirmDialog>
      </section>
    </Show>
  );
}
