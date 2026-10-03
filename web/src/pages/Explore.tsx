import { createEffect, createMemo, createSignal, For, Show } from "solid-js";
import { useNavigate, useSearchParams } from "@solidjs/router";
import { useQueries, useQuery } from "@tanstack/solid-query";
import { Popover } from "@kobalte/core/popover";
import Page from "../components/Page";
import ListShell from "../components/ListShell";
import Button from "../components/Button";
import BladeStack from "../components/BladeStack";
import Outline, { HealthSlots, OUTLINE_COLS, NARROW_HIDDEN, OUTLINE_FRAME, WIDE_ONLY, type AddKind, type OpenTarget } from "../components/Outline";
import { Plus, resolveIcon } from "../components/icons";
import { can, useMe } from "../lib/auth";
import { BladesContext, createBladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import { FLEET_VIEW_KEY, fleetView } from "../lib/fleet";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { STANDARDS_KEY, listStandards } from "../lib/standards";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import { COMPONENT_TYPES_KEY, listComponentTypes, resolveComponentTypeIcon } from "../lib/component_types";
import { systemHealth, systemHealthKey } from "../lib/health";
import { canHoldChildren } from "../lib/location_type_graph";
import { ancestorsOf, buildOutline, entriesOf, type OutlineEntry, type PlaceNode } from "../lib/outline";
import { parseChips, tagFilterKeys, type Chip, type FilterKey } from "../lib/predicate";
import { describeError } from "../lib/format";
import { entityLabel } from "../lib/entities";

// Explore (#861): every place in the fleet as one outline, and what is in it.
//
// A place wears its system (operators think of a room AS its system), so the
// tree holds places only and a place's components sit directly beneath it.
// Nothing here reads a location type's name: the hierarchy, folding, counts
// and what may be created under a row all come from the tree and from the
// registry's allowed_parent_types (lib/outline, lib/location_type_graph).
//
// What is expanded is this browser's; what is filtered rides the URL, so a
// link carries it; ?node= reveals one row.

const OPEN_KEY = "explore-open";
const ATTENTION = ["outage", "degraded", "incomplete"];

function readOpen(): Set<string> {
  try {
    const raw = JSON.parse(localStorage.getItem(OPEN_KEY) ?? "[]");
    return new Set(Array.isArray(raw) ? raw.filter((x): x is string => typeof x === "string") : []);
  } catch { return new Set(); }
}

export default function Explore() {
  const navigate = useNavigate();
  const me = useMe();
  const [search, setSearch] = useSearchParams();
  const param = (k: string) => { const v = search[k]; return Array.isArray(v) ? v[0] : v; };

  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents, enabled: can(me.data, "component", "read") }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems, enabled: can(me.data, "system", "read") }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const types = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards }));
  const products = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
  const componentTypes = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));

  const [open, setOpenSignal] = createSignal<Set<string>>(readOpen());
  const setOpen = (next: Set<string>) => {
    setOpenSignal(next);
    try { localStorage.setItem(OPEN_KEY, JSON.stringify([...next])); } catch { /* a private window: expansion lasts the visit */ }
  };
  const [selected, setSelected] = createSignal<string | null>(null);

  const tags = createMemo(() => {
    const m = new Map<string, Record<string, string>>();
    for (const l of locations.data ?? []) m.set(l.id, l.effective_tags ?? {});
    for (const s of systems.data ?? []) m.set(s.id, s.effective_tags ?? {});
    for (const c of components.data ?? []) m.set(c.id, c.effective_tags ?? {});
    return m;
  });

  const outline = createMemo(() => {
    const standardLabel = new Map((standards.data ?? []).map((s) => [s.name, s.label] as const));
    const product = new Map((products.data ?? []).map((p) => [p.name, p] as const));
    const ctypes = new Map((componentTypes.data ?? []).map((t) => [t.name, t] as const));
    return buildOutline({
      view: view.data ?? { locations: [], systems: [] },
      components: components.data ?? [],
      systems: systems.data ?? [],
      types: types.data ?? [],
      standardLabel: (h) => standardLabel.get(h) ?? h,
      productLabel: (h) => entityLabel(product.get(h) ?? { name: h }),
      componentIcon: (h) => resolveComponentTypeIcon(product.get(h)?.component_type, ctypes),
      tags: tags(),
    });
  });
  const roots = createMemo(() => (outline().unplaced ? [...outline().roots, outline().unplaced!] : outline().roots));
  const entries = createMemo(() => entriesOf(outline()));

  const chips = createMemo<Chip[]>(() => parseChips(param("chips")));

  // The alarm text behind a red light, read only for the troubled systems on
  // screen: those at a place the operator has opened, every place above it
  // open too, and none while the filter shows results instead of the tree.
  // Each read is shared with the system workspace's cache.
  const troubled = createMemo(() => {
    if (chips().length > 0) return [];
    const o = open();
    const ids: string[] = [];
    const walk = (n: PlaceNode) => {
      if (!o.has(n.id)) return;
      for (const g of n.groups) if (g.system && (g.system.health === "degraded" || g.system.health === "outage")) ids.push(g.system.id);
      n.children.forEach(walk);
    };
    roots().forEach(walk);
    return ids;
  });
  const health = useQueries(() => ({
    queries: troubled().map((id) => ({ queryKey: systemHealthKey(id), queryFn: () => systemHealth(id), staleTime: 30_000, retry: false })),
  }));
  // A report names an alarm's component by uuid.
  const alarms = createMemo(() => {
    const out = new Map<string, string>();
    for (const q of health) for (const role of q.data?.roles ?? []) for (const a of role.alarms ?? []) {
      if (a.component && !out.has(a.component)) out.set(a.component, a.message);
    }
    return out;
  });
  const setChips = (next: Chip[]) => setSearch({ chips: next.length ? JSON.stringify(next) : undefined });
  const attentionOn = () => chips().some((c) => c.key === "verdict" && ATTENTION.every((v) => c.values.includes(v)));
  const toggleAttention = () => {
    const rest = chips().filter((c) => c.key !== "verdict");
    setChips(attentionOn() ? rest : [...rest, { key: "verdict", op: "eq", values: ATTENTION }]);
  };
  const needing = () => (view.data?.systems ?? []).filter((s) => ATTENTION.includes(s.verdict)).length;

  const filterKeys = createMemo<FilterKey<OutlineEntry>[]>(() => {
    const tagNames = [...new Set(entries().flatMap((e) => Object.keys(e.tags)))].sort();
    return [
      // The bare typed term lands on the first substring key: a name, a
      // system, a standard or a product.
      { key: "name", type: "string", hint: "substring", get: (e) => e.search },
      { key: "verdict", type: "string", get: (e) => e.verdict ?? "", values: () => ["healthy", "incomplete", "degraded", "outage"] },
      { key: "type", type: "string", get: (e) => e.type, values: (es) => [...new Set(es.map((e) => e.type).filter(Boolean))].sort() },
      { key: "standard", type: "string", get: (e) => e.standard, values: (es) => [...new Set(es.map((e) => e.standard).filter(Boolean))].sort() },
      { key: "product", type: "string", get: (e) => e.product, values: (es) => [...new Set(es.map((e) => e.product).filter(Boolean))].sort() },
      { key: "path", type: "string", hint: "substring", get: (e) => e.path.join(" / ") },
      ...tagFilterKeys<OutlineEntry>(tagNames, new Set(["name", "verdict", "type", "standard", "product", "path"])),
    ];
  });

  // What may be created under a row: what the registry lets sit there, and
  // what the caller may create.
  const addOptions = (n: { typeName: string }): AddKind[] => {
    const kinds: AddKind[] = [];
    if (can(me.data, "location", "create") && types.data && canHoldChildren(types.data, n.typeName)) kinds.push("location");
    if (can(me.data, "system", "create")) kinds.push("system");
    if (can(me.data, "component", "create")) kinds.push("component");
    return kinds;
  };
  const newKinds = (): AddKind[] => (["location", "system", "component"] as AddKind[]).filter((k) => can(me.data, k, "create"));
  const create = (kind: AddKind, under?: string) => navigate(`/${kind}s/create${under ? `?under=${encodeURIComponent(under)}` : ""}`);

  const subtree = (n: PlaceNode): string[] => [n.id, ...n.children.flatMap(subtree)];
  const onToggle = (n: PlaceNode, deep: boolean) => {
    const next = new Set(open());
    if (deep) for (const id of subtree(n)) next.add(id);
    else if (next.has(n.id)) next.delete(n.id);
    else next.add(n.id);
    setOpen(next);
  };

  const blades = createBladeController();
  const openTarget = (t: OpenTarget, key: string) => {
    setSelected(key.split(":").pop() === t.id ? t.id : key);
    blades.close();
    blades.push(t);
  };

  const reveal = (id: string) => {
    const next = new Set(open());
    for (const a of ancestorsOf(outline(), id)) next.add(a);
    setOpen(next);
    setSelected(id);
    queueMicrotask(() => document.querySelector<HTMLElement>(`[role=treeitem][aria-selected=true]`)?.scrollIntoView?.({ block: "nearest" }));
  };
  const revealResult = (id: string) => {
    setSearch({ chips: undefined });
    reveal(id);
  };

  // ?node= reveals one row: a place, a system's place, or a component, by
  // uuid or by a name that names exactly one thing (ADR-0062, #759's rule).
  let revealed = "";
  createEffect(() => {
    const raw = param("node");
    if (!raw || raw === revealed || !view.data) return;
    // Everything the address could name, of every kind: a name that names a
    // place and a system both is ambiguous, and reveals nothing.
    const named = [...(view.data.locations ?? []), ...(view.data.systems ?? []), ...(components.data ?? [])];
    const byId = named.find((x) => x.id === raw)?.id;
    const byName = named.filter((x) => x.name === raw);
    const target = byId ?? (byName.length === 1 ? byName[0].id : undefined);
    if (!target) return;
    revealed = raw;
    reveal(target);
  });

  // Empty means nothing to show at all: a component placed nowhere still has
  // a row, so a fleet with no locations is not empty while one exists.
  const empty = () => outline().roots.length === 0 && !outline().unplaced;

  return (
    <BladesContext.Provider value={blades}>
      <Page title="Explore" subtitle="Every place in the fleet, and what is in it.">
        <Show when={!view.isPending} fallback={<div class="skeleton h-64 w-full" />}>
          <Show
            when={!view.isError}
            fallback={
              <div role="alert" class="alert alert-error alert-soft text-sm">
                <span class="flex-1">{describeError(view.error)}</span>
                <Button size="xs" loading={view.isFetching} onClick={() => void view.refetch()}>Retry</Button>
              </div>
            }
          >
            <div class="flex flex-col gap-3">
              <div data-testid="explore-counts" class="flex flex-wrap items-center gap-4 text-sm">
                <span class="text-base-content/75"><span class="mr-1.5 font-data font-medium text-base-content">{outline().systemCount}</span>{outline().systemCount === 1 ? "system" : "systems"}</span>
                <Show when={needing() > 0 || attentionOn()}>
                  <Button size="sm" intent={attentionOn() ? "action" : "quiet"} pressed={attentionOn()} onClick={toggleAttention} title="Show only what needs attention">
                    <Show when={!attentionOn()}><span class="h-[7px] w-[7px] rounded-full bg-warning" aria-hidden="true" /></Show>
                    <span class="font-data">{needing()}</span> need attention
                  </Button>
                </Show>
                <span class="flex-1" />
                <Show when={newKinds().length > 0}>
                  <NewMenu kinds={newKinds()} onPick={(k) => create(k)} />
                </Show>
              </div>

              <Show
                when={!empty()}
                fallback={
                  <div class="flex flex-col items-center gap-3 rounded-box border border-dashed border-base-300 px-4 py-10 text-center text-sm text-base-content/60">
                    <p>No locations yet.</p>
                    <Show when={can(me.data, "location", "create")}>
                      <Button intent="action" icon={Plus} onClick={() => create("location")}>New location</Button>
                    </Show>
                  </div>
                }
              >
                <ListShell
                  filterKeys={filterKeys()}
                  rows={entries()}
                  chips={chips}
                  onChips={setChips}
                  placeholder="Filter by name, place, health, type, standard, product or tag"
                >
                  {(filtered) => (
                    <Show
                      when={chips().length > 0}
                      fallback={
                        <Outline
                          roots={roots()}
                          expanded={open()}
                          selected={selected()}
                          onToggle={onToggle}
                          onOpen={openTarget}
                          addOptions={addOptions}
                          onAdd={(id, k) => create(k, id)}
                          issueOf={(id) => alarms().get(id)}
                        />
                      }
                    >
                      <Show when={filtered().length > 0} fallback={<p class="px-4 py-8 text-center text-sm text-base-content/60">Nothing matches the filter.</p>}>
                        <Results entries={filtered()} outlineRoots={roots()} onOpen={(e) => {
                          const t = targetOfEntry(e, roots());
                          if (!t) return revealResult(e.id);
                          setSelected(e.id);
                          blades.close();
                          blades.push(t);
                        }} onReveal={(e) => revealResult(e.id)} />
                      </Show>
                    </Show>
                  )}
                </ListShell>
              </Show>
            </div>
          </Show>
        </Show>
      </Page>
      <BladeStack controller={blades} registry={fleetRegistry} />
    </BladesContext.Provider>
  );
}

// A filter result opens what its tree row would: a place holding one system
// opens that system, any other place the location, a component itself.
function findPlace(roots: PlaceNode[], id: string): PlaceNode | undefined {
  for (const r of roots) {
    if (r.id === id) return r;
    const hit = findPlace(r.children, id);
    if (hit) return hit;
  }
  return undefined;
}
function targetOfEntry(e: OutlineEntry, roots: PlaceNode[]): OpenTarget | null {
  if (e.kind === "component") return { kind: "component", id: e.id };
  // The node of things out of sight is no place; it has no panel to open.
  if (e.id === "unplaced") return null;
  const n = findPlace(roots, e.id);
  return n && n.systems.length === 1 ? { kind: "system", id: n.systems[0].id } : { kind: "location", id: e.id };
}

function Results(props: { entries: OutlineEntry[]; outlineRoots: PlaceNode[]; onOpen: (e: OutlineEntry) => void; onReveal: (e: OutlineEntry) => void }) {
  return (
    <ul role="list" aria-label="Matches" class={`${OUTLINE_FRAME} py-2`}>
      <For each={props.entries}>
        {(e) => {
          const place = () => (e.kind === "place" ? findPlace(props.outlineRoots, e.id) : undefined);
          const Icon = resolveIcon(place()?.icon || (e.kind === "component" ? "box" : "map-pin"));
          return (
            <li
              role="listitem"
              tabindex={0}
              title="Enter shows it in the tree, Space opens it"
              class={`grid ${OUTLINE_COLS} h-9 cursor-pointer items-center px-4 outline-none hover:bg-base-content/[0.03] focus-visible:shadow-[inset_0_0_0_1px_var(--color-primary)]`}
              onClick={() => props.onOpen(e)}
              onKeyDown={(ev) => {
                if (ev.key === "Enter") { ev.preventDefault(); props.onReveal(e); }
                else if (ev.key === " ") { ev.preventDefault(); props.onOpen(e); }
              }}
            >
              <span class="col-span-2 flex min-w-0 items-center gap-2 pr-4 @max-[40rem]:col-span-1">
                <span class="flex-none text-base-content/45"><Icon size={16} /></span>
                <span class="truncate">
                  <Show when={e.path.length > 0}><span class="text-base-content/40">{e.path.join(" / ")} / </span></Show>
                  <span class={e.kind === "component" ? "font-data text-[13px]" : "font-semibold"}>{e.label}</span>
                </span>
              </span>
              <span class={`truncate pr-4 text-base-content/75 ${WIDE_ONLY}`}>{e.kind === "component" ? e.product : e.standard}</span>
              <span class={`truncate pr-4 text-base-content/55 ${NARROW_HIDDEN}`}>{place()?.contents ?? ""}</span>
              <span>{e.verdict ? <HealthSlots single={e.verdict} /> : null}</span>
              <span />
            </li>
          );
        }}
      </For>
    </ul>
  );
}

function NewMenu(props: { kinds: AddKind[]; onPick: (k: AddKind) => void }) {
  const [isOpen, setIsOpen] = createSignal(false);
  const LABEL: Record<AddKind, string> = { location: "Location", system: "System", component: "Component" };
  return (
    <Popover open={isOpen()} onOpenChange={setIsOpen} placement="bottom-end" gutter={6}>
      <Popover.Trigger class="btn btn-action btn-sm gap-1.5" aria-label="New">
        <Plus size={15} />New
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content class="z-50 w-44 rounded-box border border-base-300 bg-base-100 p-1 shadow-2xl focus:outline-none">
          <For each={props.kinds}>
            {(k) => (
              <button type="button" class="block w-full cursor-pointer rounded px-2.5 py-1.5 text-left text-sm hover:bg-base-content/[0.06]" onClick={() => { setIsOpen(false); props.onPick(k); }}>
                {LABEL[k]}
              </button>
            )}
          </For>
        </Popover.Content>
      </Popover.Portal>
    </Popover>
  );
}
