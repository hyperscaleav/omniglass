import { For, Show, createEffect, createMemo } from "solid-js";
import { useNavigate, useParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import ZoomLadder from "../components/ZoomLadder";
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

  const crumbs = createMemo(() => {
    if (!view.data) return [];
    const chain = component()?.location_id ? ancestors(component()!.location_id!, locationIndex(view.data)) : [];
    return [
      { key: "fleet", label: "Fleet", onClick: () => navigate("/fleet") },
      ...chain.map((l) => ({ key: l.id, label: entityLabel(l), onClick: () => navigate(`/locations/${l.id}?zoom=1`) })),
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
          <div class="flex flex-col gap-5">
            <section data-testid="leaf-identity" class="flex flex-col gap-1 text-sm">
              <h2 class="text-sm font-semibold">What it is</h2>
              <div class="text-base-content/70">
                {component()?.product ?? "no product"}
                <Show when={product()?.vendor}>{` by ${product()!.vendor}`}</Show>
                <Show when={product()?.driver}>{`, driven by ${product()!.driver}`}</Show>
              </div>
            </section>

            <section data-testid="leaf-memberships" class="flex flex-col gap-1 text-sm">
              <h2 class="text-sm font-semibold">Serving</h2>
              <Show when={rows().length > 0} fallback={<p class="text-base-content/60">No system yet: in the fleet, awaiting commissioning.</p>}>
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
                        <Show when={row.primary}>
                          <span class="rounded border border-base-content/20 px-1 text-[10px]">primary</span>
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

            <section data-testid="leaf-collection" class="flex flex-col gap-1 text-sm">
              <h2 class="text-sm font-semibold">Collection</h2>
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
                          <span data-testid={`collection-${iface.interface}`} class="rounded border border-base-content/20 px-1.5 py-0.5">
                            {
                              {
                                collecting: "collecting",
                                "device-or-path": "sample stale under a healthy node: the device or the path, not collection",
                                "node-offline": "node offline: staleness says nothing about the device",
                                down: "device answering down",
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
        </Show>
      </Show>
    </Page>
  );
}
