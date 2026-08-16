import { For, Show, createEffect, createMemo } from "solid-js";
import { useNavigate, useParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import ZoomLadder from "../components/ZoomLadder";
import ZoomRail from "../components/ZoomRail";
import { railModel } from "../lib/zoom_rail";
import { FLEET_VIEW_KEY, ancestors, fleetView, locationIndex } from "../lib/fleet";
import { COMPONENTS_KEY, listComponents, type Component as FleetComponent } from "../lib/components";
import { componentSystemsKey, componentSystems } from "../lib/members";
import { REACHABILITY_KEY, getReachability } from "../lib/reachability";
import { NODES_KEY, listNodes } from "../lib/nodes";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import { collectionState, membershipRows } from "../lib/component_leaf";
import { zoomChips } from "../lib/zoom";
import { entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";

// The component leaf (#637): the end of the walk. What it is (product,
// vendor, driver), where it sits (the clickable ancestor chain), the
// memberships with the primary marked, and collection in context: the one
// place a node appears to an operator, because the distinction that matters
// is stated here plainly (a healthy node with a stale sample means the device
// or the path, not collection) and that sentence is why nodes need no zoom of
// their own.
// ageWords renders a sample age in plain words, coarse on purpose: the card
// answers "how stale", not "how many milliseconds".
function ageWords(ts: string, now: number = Date.now()): string {
  const s = Math.max(0, Math.round((now - new Date(ts).getTime()) / 1000));
  if (s < 90) return `${s} s ago`;
  const m = Math.round(s / 60);
  if (m < 90) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 36) return `${h} h ago`;
  return `${Math.round(h / 24)} d ago`;
}

export default function ComponentLeaf() {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  const id = () => params.id;

  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const products = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
  const nodes = useQuery(() => ({ queryKey: NODES_KEY, queryFn: listNodes }));

  const component = createMemo<FleetComponent | undefined>(() =>
    (components.data ?? []).find((c) => c.id === id() || c.name === id()),
  );

  // The leaf is addressed by uuid; a name-shaped address resolves through the
  // component list when unique, keeping the param (#759's rule).
  createEffect(() => {
    if (!components.data) return;
    const c = component();
    if (c && c.id !== id()) navigate(`/components/${c.id}?zoom=1`, { replace: true });
  });

  const memberships = useQuery(() => ({
    queryKey: componentSystemsKey(id()),
    queryFn: () => componentSystems(id()),
    enabled: !!component(),
  }));
  const reach = useQuery(() => ({
    queryKey: REACHABILITY_KEY(id()),
    queryFn: () => getReachability(id()),
    enabled: !!component(),
  }));

  const product = createMemo(() => (products.data ?? []).find((p) => p.name === component()?.product));
  const rows = createMemo(() => (view.data && memberships.data ? membershipRows(memberships.data, view.data) : []));
  const nodeByName = createMemo(() => new Map((nodes.data ?? []).map((n) => [n.name, n])));

  const chips = createMemo(() => {
    if (!view.data) return [];
    const primary = rows().find((r) => r.primary);
    return zoomChips(
      "component",
      { locationId: component()?.location_id ?? null, systemId: primary?.systemId ?? null, componentId: id() },
      view.data,
    );
  });

  const chain = createMemo(() => (view.data && component()?.location_id ? ancestors(component()!.location_id!, locationIndex(view.data)) : []));

  const crumbs = createMemo(() => {
    if (!view.data) return [];
    const chainList = chain();
    return [
      { key: "fleet", label: "Fleet", onClick: () => navigate("/fleet") },
      ...chainList.map((l) => ({ key: l.id, label: entityLabel(l), onClick: () => navigate(`/locations/${l.id}?zoom=1`) })),
      ...(component() ? [{ key: component()!.id, label: entityLabel(component()!) }] : []),
    ];
  });

  return (
    <Page
      title={component() ? entityLabel(component()!) : "Component"}
      subtitle={component()?.product ?? ""}
      breadcrumb={<Breadcrumb crumbs={crumbs()} />}
    >
      <Show when={!view.isPending && !components.isPending} fallback={<div class="skeleton h-32 w-full" />}>
        <Show
          when={!view.isError && !components.isError}
          fallback={
            <div role="alert" class="alert alert-error alert-soft text-sm">
              {describeError(view.error ?? components.error)}
            </div>
          }
        >
          <ZoomLadder
            chips={chips()}
            onSelect={(chip) => {
              if (chip.id === "fleet") navigate("/fleet");
              if (chip.id === "location" && component()?.location_id) navigate(`/locations/${component()!.location_id}?zoom=1`);
              if (chip.id === "system") {
                const primary = rows().find((r) => r.primary);
                if (primary?.systemId) navigate(`/systems/${primary.systemId}?zoom=1`);
              }
            }}
          />
          <div class="flex gap-6">
          <div class="flex min-w-0 flex-1 flex-col gap-5">
            <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
              <section data-testid="leaf-identity" class="rounded-lg border border-base-content/10 p-3 text-sm">
                <h2 class="text-[10px] uppercase tracking-wider text-base-content/50">What it is</h2>
                <dl class="mt-2 grid grid-cols-[6rem_1fr] gap-x-3 gap-y-1">
                  <dt class="text-base-content/50">Product</dt>
                  <dd>{product() ? entityLabel(product()!) : (component()?.product ?? "no product")}</dd>
                  <Show when={product()?.vendor}>
                    <dt class="text-base-content/50">Vendor</dt>
                    <dd>{product()!.vendor}</dd>
                  </Show>
                  <Show when={product()?.driver}>
                    <dt class="text-base-content/50">Driver</dt>
                    <dd class="font-mono text-xs">{product()!.driver}</dd>
                  </Show>
                </dl>
                {/* The one-line form the tests and the old card asserted stays,
                    visually quiet, so a reader can copy it. */}
                <div class="mt-2 text-xs text-base-content/50">
                  {component()?.product ?? "no product"}
                  <Show when={product()?.vendor}>{` by ${product()!.vendor}`}</Show>
                  <Show when={product()?.driver}>{`, driven by ${product()!.driver}`}</Show>
                </div>
              </section>
              <section data-testid="leaf-placement" class="rounded-lg border border-base-content/10 p-3 text-sm">
                <h2 class="text-[10px] uppercase tracking-wider text-base-content/50">Where it sits</h2>
                <dl class="mt-2 grid grid-cols-[6rem_1fr] gap-x-3 gap-y-1">
                  <dt class="text-base-content/50">Location</dt>
                  <dd class="flex flex-wrap gap-1">
                    <For each={chain()}>
                      {(l, i) => (
                        <>
                          <Show when={i() > 0}><span class="text-base-content/30">/</span></Show>
                          <button type="button" class="cursor-pointer text-primary hover:underline" onClick={() => navigate(`/locations/${l.id}?zoom=1`)}>{entityLabel(l)}</button>
                        </>
                      )}
                    </For>
                    <Show when={chain().length === 0}><span class="text-base-content/50">unplaced</span></Show>
                  </dd>
                  <Show when={chain().length > 0}>
                    <dt class="text-base-content/50">Path</dt>
                    <dd data-testid="leaf-type-path" class="font-mono text-xs text-base-content/60">{chain().map((l) => l.location_type).join(" / ")}</dd>
                  </Show>
                  <Show when={rows().find((r) => r.primary)}>
                    {(p) => (
                      <>
                        <dt class="text-base-content/50">System</dt>
                        <dd>
                          <Show when={p().systemId} fallback={p().label}>
                            <button type="button" class="cursor-pointer text-primary hover:underline" onClick={() => navigate(`/systems/${p().systemId}?zoom=1`)}>{p().label}</button>
                          </Show>
                        </dd>
                      </>
                    )}
                  </Show>
                </dl>
              </section>
            </div>

            <section data-testid="leaf-memberships" class="rounded-lg border border-base-content/10 p-3 text-sm">
              <h2 class="text-[10px] uppercase tracking-wider text-base-content/50">Slots it fills</h2>
              <Show when={rows().length > 0} fallback={<p class="text-base-content/60">Not in any system yet.</p>}>
                <ul class="flex flex-col gap-1">
                  <For each={rows()}>
                    {(row) => (
                      <li class="flex items-center gap-2">
                        <Show
                          when={row.systemId}
                          fallback={<span>{row.label}</span>}
                        >
                          <button type="button" class="cursor-pointer hover:underline" onClick={() => navigate(`/systems/${row.systemId}?zoom=1`)}>
                            {row.label}
                          </button>
                        </Show>
                        <Show when={row.where}>
                          <span class="font-mono text-[10px] text-base-content/40">{row.where}</span>
                        </Show>
                        <Show when={row.primary}>
                          <span class="ml-auto text-[10px] uppercase tracking-wider text-base-content/50">primary</span>
                        </Show>
                      </li>
                    )}
                  </For>
                </ul>
                <Show when={rows().length > 1}>
                  <p class="text-xs text-base-content/50">The location above comes from the primary.</p>
                </Show>
              </Show>
            </section>

            <section data-testid="leaf-collection" class="rounded-lg border border-base-content/10 p-3 text-sm">
              <h2 class="text-[10px] uppercase tracking-wider text-base-content/50">Collection</h2>
              <Show when={reach.data && (reach.data.interfaces ?? []).length > 0} fallback={<p class="text-base-content/60">No interface declared yet, so nothing collects from this component.</p>}>
                <ul class="flex flex-col gap-1">
                  <For each={reach.data!.interfaces}>
                    {(iface) => {
                      const state = () => collectionState(iface, iface.node ? nodeByName().get(iface.node) : undefined);
                      return (
                        <li class="flex flex-wrap items-center gap-2 text-xs">
                          <span class="font-medium">{iface.interface}</span>
                          <Show when={iface.node}>
                            <span class="text-base-content/60">via {iface.node}</span>
                          </Show>
                          <Show when={iface.verdict}>
                            <span class="text-base-content/50">last sample {ageWords(iface.verdict!.ts)}</span>
                          </Show>
                          <span data-testid={`collection-${iface.interface}`} class="rounded border border-base-content/20 px-1.5 py-0.5">
                            {
                              {
                                collecting: "collecting",
                                "device-or-path": "stale sample; the node is healthy, so check the device or the path",
                                "node-offline": "node offline",
                                down: "device reports down",
                                unknown: "no sample yet",
                              }[state().kind]
                            }
                          </span>
                        </li>
                      );
                    }}
                  </For>
                </ul>
              </Show>
            </section>
          </div>
          <Show when={view.data}>{(v) => <ZoomRail model={railModel({ zoom: "component", systemId: rows().find((r) => r.primary)?.systemId ?? null, componentId: id() }, v())} />}</Show>
          </div>
        </Show>
      </Show>
    </Page>
  );
}
