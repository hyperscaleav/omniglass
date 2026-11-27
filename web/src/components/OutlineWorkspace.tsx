import { createEffect, createMemo, createSignal, For, Show } from "solid-js";
import { useNavigate, useSearchParams } from "@solidjs/router";
import { useQueries, useQuery } from "@tanstack/solid-query";
import Popover from "./ui/Popover";
import ListShell from "./ListShell";
import Button from "./Button";
import Outline, { DetailText, HealthSlots, OUTLINE_COLS, NARROW_HIDDEN, OUTLINE_FRAME, WIDE_ONLY, type AddKind, type OpenTarget } from "./Outline";
import { LayoutDashboard, Plus, resolveIcon } from "./icons";
import { can, useMe } from "../lib/auth";
import { useBlades } from "../lib/blades";
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
import { ancestorsOf, buildOutline, entriesOf, rootAt, type OutlineEntry, type LocationNode } from "../lib/outline";
import { buildPredicate, parseChips, tagFilterKeys, type Chip, type FilterKey } from "../lib/predicate";
import { ATTENTION, ATTENTION_CHIP, attentionCount, attentionParts } from "../lib/attention";
import OutlineName, { type NameKind } from "./OutlineName";
import { describeError } from "../lib/format";
import { entityLabel } from "../lib/entities";
import Tooltip from "./ui/Tooltip";

// OutlineWorkspace (#861, extracted in #872): the outline of locations with its
// counts line, need-attention, New, the chip filter and the matches list.
// Explore renders it over the whole fleet; a location's detail view renders it
// rooted at that location, listing what is beneath it.
//
// A location wears its system (operators think of a room AS its system), so the
// tree holds locations only and a location's components sit directly beneath it.
// Nothing here reads a location type's name: the hierarchy, folding, counts
// and what may be created under a row all come from the tree and from the
// catalog's allowed_parent_types (lib/outline, lib/location_type_graph).
//
// What is expanded is this browser's, shared by every outline; what is
// filtered rides the URL, so a link carries it; ?node= reveals one row (the
// whole-fleet outline only).

const OPEN_KEY = "explore-open";

function readOpen(): Set<string> {
  try {
    const raw = JSON.parse(localStorage.getItem(OPEN_KEY) ?? "[]");
    return new Set(Array.isArray(raw) ? raw.filter((x): x is string => typeof x === "string") : []);
  } catch { return new Set(); }
}

// `host` "blade" (#2423): a rooted outline inside a blade keeps its filter in
// a signal of its own, so filtering it never rewrites (or reads) the page's
// address, the Explore outline beneath it included.
export default function OutlineWorkspace(props: { rootId?: string; host?: "page" | "blade" }) {
  const navigate = useNavigate();
  const me = useMe();
  const [urlSearch, setUrlSearch] = useSearchParams();
  const [local, setLocal] = createSignal<Record<string, string | undefined>>({});
  const inBlade = props.host === "blade";
  const search = (): Record<string, string | string[] | undefined> => (inBlade ? local() : urlSearch);
  const setSearch = (next: Record<string, string | undefined>) => (inBlade ? setLocal((p) => ({ ...p, ...next })) : setUrlSearch(next));
  const param = (k: string) => { const v = search()[k]; return Array.isArray(v) ? v[0] : v; };

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
  // Rooted, the location's own node (unfolded); otherwise the whole fleet.
  const root = createMemo(() => (props.rootId ? rootAt(outline(), props.rootId) : null));
  const roots = createMemo(() => {
    if (props.rootId) return root()?.children ?? [];
    return outline().unplaced ? [...outline().roots, outline().unplaced!] : outline().roots;
  });
  const entries = createMemo(() => entriesOf(outline(), props.rootId ? roots() : undefined));

  const filterKeys = createMemo<FilterKey<OutlineEntry>[]>(() => {
    const tagNames = [...new Set(entries().flatMap((e) => Object.keys(e.tags)))].sort();
    return [
      // The bare typed term lands on the first substring key: a name, a
      // system, a standard or a product.
      { key: "name", type: "string", hint: "substring", get: (e) => e.search },
      { key: "health", type: "string", get: (e) => e.health ?? "", values: () => ["healthy", "incomplete", "degraded", "outage"] },
      { key: "type", type: "string", get: (e) => e.type, values: (es) => [...new Set(es.map((e) => e.type).filter(Boolean))].sort() },
      { key: "standard", type: "string", get: (e) => e.standard, values: (es) => [...new Set(es.map((e) => e.standard).filter(Boolean))].sort() },
      { key: "product", type: "string", get: (e) => e.product, values: (es) => [...new Set(es.map((e) => e.product).filter(Boolean))].sort() },
      { key: "path", type: "string", hint: "substring", get: (e) => e.path.join(" / ") },
      ...tagFilterKeys<OutlineEntry>(tagNames, new Set(["name", "health", "type", "standard", "product", "path"])),
    ];
  });

  const chips = createMemo<Chip[]>(() => parseChips(param("chips")));
  // The filter narrows as you type: what is typed is previewed as the chip
  // Enter would commit (the same parse), and the results list answers to
  // the committed chips and this one together.
  const [draftChip, setDraftChip] = createSignal<Chip | null>(null);
  const effective = createMemo<Chip[]>(() => [...chips(), ...(draftChip() ? [draftChip()!] : [])]);
  const shown = createMemo(() => entries().filter(buildPredicate(filterKeys(), effective())));

  // The alarm text behind a red light, read only for the troubled systems on
  // screen: those at a location the operator has opened, every location above it
  // open too, and none while the filter shows results instead of the tree.
  // Each read is shared with the system workspace's cache.
  const troubled = createMemo(() => {
    if (effective().length > 0) return [];
    const o = open();
    const ids: string[] = [];
    const walk = (n: LocationNode) => {
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
  // A report names an alarm's component by name and by uuid; the outline keys on the uuid.
  const alarms = createMemo(() => {
    const out = new Map<string, string>();
    for (const q of health) for (const slot of q.data?.slots ?? []) for (const a of slot.alarms ?? []) {
      if (a.component_id && !out.has(a.component_id)) out.set(a.component_id, a.message);
    }
    return out;
  });
  const setChips = (next: Chip[]) => setSearch({ chips: next.length ? JSON.stringify(next) : undefined });
  const attentionOn = () => chips().some((c) => c.key === "health" && ATTENTION.every((v) => c.values.includes(v)));
  const toggleAttention = () => {
    const rest = chips().filter((c) => c.key !== "health");
    setChips(attentionOn() ? rest : [...rest, ATTENTION_CHIP]);
  };
  // Rooted, the counts cover what the list shows: the locations beneath the
  // root, never the root's own systems (their cards sit above the outline,
  // and a count the filter cannot narrow to is a broken promise).
  const rootedLights = () => roots().reduce((a, n) => ({
    healthy: a.healthy + n.lights.healthy, incomplete: a.incomplete + n.lights.incomplete,
    degraded: a.degraded + n.lights.degraded, outage: a.outage + n.lights.outage,
  }), { healthy: 0, incomplete: 0, degraded: 0, outage: 0 });
  // What the chip counts is what pressing it shows: the entries the attention
  // filter leaves alongside the other chips on screen, tallied by the one pure
  // count Home reads too (lib/attention).
  const needing = createMemo(() => {
    const others = effective().filter((c) => c.key !== "health");
    return attentionCount(entries().filter(buildPredicate(filterKeys(), [...others, ATTENTION_CHIP])));
  });
  const systemCount = () => {
    if (!props.rootId) return outline().systemCount;
    const l = rootedLights();
    return l.healthy + l.incomplete + l.degraded + l.outage;
  };

  // What may be created under a row: what the catalog lets sit there, and
  // what the caller may create.
  const addOptions = (n: { typeName: string }): AddKind[] => {
    const kinds: AddKind[] = [];
    if (can(me.data, "location", "create") && types.data && canHoldChildren(types.data, n.typeName)) kinds.push("location");
    if (can(me.data, "system", "create")) kinds.push("system");
    if (can(me.data, "component", "create")) kinds.push("component");
    return kinds;
  };
  // New creates at the top of the fleet, or under the location a rooted
  // outline stands for, by the same rules as that row's +.
  const newKinds = (): AddKind[] => {
    const r = root();
    if (props.rootId) return r ? addOptions(r.chain[0]) : [];
    return (["location", "system", "component"] as AddKind[]).filter((k) => can(me.data, k, "create"));
  };
  // A component is added through the guided flow (#2460), which ends on an
  // endpoint that has answered; its plain form stays one link away there.
  // A caller who may create a component but not its endpoint gets the plain
  // form, since the flow ends on an endpoint they could not make.
  const guided = () => can(me.data, "endpoint", "create");
  const create = (kind: AddKind, under?: string) =>
    navigate(`/${kind}s/${kind === "component" && guided() ? "add" : "create"}${under ? `?under=${encodeURIComponent(under)}` : ""}`);

  const subtree = (n: LocationNode): string[] => [n.id, ...n.children.flatMap(subtree)];
  const onToggle = (n: LocationNode, deep: boolean) => {
    const next = new Set(open());
    if (deep) for (const id of subtree(n)) next.add(id);
    else if (next.has(n.id)) next.delete(n.id);
    else next.add(n.id);
    setOpen(next);
  };

  const blades = useBlades();
  const openTarget = (t: OpenTarget, key: string) => {
    setSelected(key.split(":").pop() === t.id ? t.id : key);
    // On a page a row opens a fresh stack; inside a blade the stack is the
    // one this blade sits on, so the row stacks over it (#2423).
    if (!inBlade) blades.close();
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

  // ?node= reveals one row: a location, a system's location, or a component, by
  // uuid or by a name that names exactly one thing (ADR-0062, #759's rule).
  let revealed = "";
  createEffect(() => {
    const raw = param("node");
    if (props.rootId || !raw || raw === revealed || !view.data) return;
    // Everything the address could name, of every kind: a name that names a
    // location and a system both is ambiguous, and reveals nothing.
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
  const empty = () => (props.rootId ? roots().length === 0 : outline().roots.length === 0 && !outline().unplaced);

  return (
    <>
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
                <span class="text-base-content/75"><span class="mr-1.5 font-data font-medium text-base-content">{systemCount()}</span>{systemCount() === 1 ? "system" : "systems"}</span>
                <Show when={needing().total > 0 || attentionOn()}>
                  <Button size="sm" intent={attentionOn() ? "action" : "quiet"} pressed={attentionOn()} onClick={toggleAttention} title="Show only what needs attention">
                    <span data-testid="attention-dot" class="h-[7px] w-[7px] rounded-full bg-warning" aria-hidden="true" />
                    <span>
                      <Show when={needing().total > 0} fallback="Nothing needs attention">
                        <For each={attentionParts(needing())}>
                          {(p, i) => <>{i() > 0 ? ", " : ""}<span class="font-data">{p.n}</span> {p.noun}</>}
                        </For>
                        {needing().total === 1 ? " needs attention" : " need attention"}
                      </Show>
                    </span>
                  </Button>
                </Show>
                <span class="flex-1" />
                <Show when={newKinds().length > 0}>
                  <NewMenu kinds={newKinds()} onPick={(k) => create(k, props.rootId)} />
                </Show>
              </div>

              <Show
                when={!empty()}
                fallback={
                  <div class="flex flex-col items-center gap-3 rounded-box border border-dashed border-base-300 px-4 py-10 text-center text-sm text-base-content/60">
                    <Show
                      when={props.rootId}
                      fallback={
                        <>
                          <p>No locations yet.</p>
                          <Show when={can(me.data, "location", "create")}>
                            <Button intent="action" icon={Plus} onClick={() => create("location")}>New location</Button>
                          </Show>
                        </>
                      }
                    >
                      <p>Nothing here yet.</p>
                      <Show when={newKinds().length > 0}>
                        <div class="flex flex-wrap justify-center gap-2">
                          <For each={newKinds()}>{(k) => <Button intent={k === newKinds()[0] ? "action" : "quiet"} icon={Plus} onClick={() => create(k, props.rootId)}>{`New ${k}`}</Button>}</For>
                        </div>
                      </Show>
                    </Show>
                  </div>
                }
              >
                <ListShell
                  filterKeys={filterKeys()}
                  rows={entries()}
                  chips={chips}
                  onChips={setChips}
                  onDraft={setDraftChip}
                  placeholder="filter by name, location, health, type, standard, product or tag; Enter pins it as a chip"
                >
                  {(filtered) => (
                    <Show
                      when={effective().length > 0}
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
                      <Show when={shown().length > 0} fallback={<p class="px-4 py-8 text-center text-sm text-base-content/60">Nothing matches the filter.</p>}>
                        <Results entries={shown()} outlineRoots={roots()} onOpen={(e) => {
                          const t = targetOfEntry(e, roots());
                          if (!t) return revealResult(e.id);
                          setSelected(e.id);
                          if (!inBlade) blades.close();
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
    </>
  );
}

// A filter result opens what its tree row would: a location holding one system
// opens that system, any other the location, a component itself.
function findLocation(roots: LocationNode[], id: string): LocationNode | undefined {
  for (const r of roots) {
    if (r.id === id) return r;
    const hit = findLocation(r.children, id);
    if (hit) return hit;
  }
  return undefined;
}
function targetOfEntry(e: OutlineEntry, roots: LocationNode[]): OpenTarget | null {
  if (e.kind === "component") return { kind: "component", id: e.id };
  if (e.kind === "system") return { kind: "system", id: e.id };
  // The node of things out of sight is no location; it has no panel to open.
  if (e.id === "unplaced") return null;
  const n = findLocation(roots, e.id);
  return n && n.systems.length === 1 ? { kind: "system", id: n.systems[0].id } : { kind: "location", id: e.id };
}

function Results(props: { entries: OutlineEntry[]; outlineRoots: LocationNode[]; onOpen: (e: OutlineEntry) => void; onReveal: (e: OutlineEntry) => void }) {
  return (
    <ul role="list" aria-label="Matches" class={`${OUTLINE_FRAME} py-2`}>
      <For each={props.entries}>
        {(e) => {
          const location = () => (e.kind === "location" ? findLocation(props.outlineRoots, e.id) : undefined);
          const Icon = e.kind === "system" ? LayoutDashboard : resolveIcon(location()?.icon || (e.kind === "component" ? "box" : "map-pin"));
          const nameKind: NameKind = e.kind;
          return (
            <Tooltip as="li"
              role="listitem"
              tabindex={0}
              content="Enter shows it in the tree, Space opens it"
              class={`grid ${OUTLINE_COLS} h-9 cursor-pointer items-center px-4 outline-none hover:bg-base-content/[0.03] focus-visible:shadow-[inset_0_0_0_1px_var(--color-primary)]`}
              onClick={() => props.onOpen(e)}
              onKeyDown={(ev) => {
                if (ev.key === "Enter") { ev.preventDefault(); props.onReveal(e); }
                else if (ev.key === " ") { ev.preventDefault(); props.onOpen(e); }
              }}
            >
              <span class="col-span-2 flex min-w-0 items-center gap-2 pr-4 @max-[40rem]:col-span-1">
                <span class="flex-none text-base-content/45"><Icon size={16} /></span>
                <span class="flex min-w-0 items-baseline">
                  <Show when={e.name}>
                    {(name) => (
                      <>
                        <span data-system-label class="min-w-0 shrink-[2] truncate font-semibold">{name()}</span>
                        <span aria-hidden="true" class="mx-1.5 flex-none text-base-content/30">·</span>
                      </>
                    )}
                  </Show>
                  <Show when={e.path.length > 0}><span class="mr-1.5 min-w-0 shrink-[999] truncate text-base-content/40">{e.path.join(" / ")} /</span></Show>
                  <OutlineName kind={e.name ? "address" : nameKind} label={e.label} class="min-w-0 shrink-0 max-w-full" />
                </span>
              </span>
              <span class={`truncate pr-4 text-base-content/75 ${WIDE_ONLY}`}>{e.kind === "component" ? e.product : e.standard}</span>
              <span class={`pr-4 text-[13px] leading-4 text-base-content/55 ${NARROW_HIDDEN}`}><DetailText text={location()?.contents ?? ""} /></span>
              <span>{e.health ? <HealthSlots single={e.health} /> : null}</span>
              <span />
            </Tooltip>
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
    <Popover class="btn btn-action btn-sm gap-1.5"
      open={isOpen()}
      onOpenChange={setIsOpen}
      placement="bottom-end"
      gutter={6}
      aria-label="New"
      contentClass="z-50 w-44 rounded-box border border-base-300 bg-base-100 p-1 shadow-2xl focus:outline-none"
      content={
        <>
          <For each={props.kinds}>
            {(k) => (
              <button type="button" class="block w-full cursor-pointer rounded px-2.5 py-1.5 text-left text-sm hover:bg-base-content/[0.06]" onClick={() => { setIsOpen(false); props.onPick(k); }}>
                {LABEL[k]}
              </button>
            )}
          </For>
        </>
      }
    >
      <Plus size={15} />New
    </Popover>
  );
}
