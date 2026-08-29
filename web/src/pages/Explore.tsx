import { For, Show, createEffect, createMemo, createSignal, on, onCleanup, onMount } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useNavigate, useSearchParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import HealthBadge from "../components/HealthBadge";
import BladeStack from "../components/BladeStack";
import TabRail from "../components/TabRail";
import Button from "../components/Button";
import EntityForm from "../components/EntityForm";
import LocationsPage from "./Locations";
import SystemsPage from "./Systems";
import ComponentsPage from "./Components";
import { can, useMe } from "../lib/auth";
import { BladesContext, createBladeController, createEditSlot } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import { FLEET_VIEW_KEY, fleetView, locationIndex } from "../lib/fleet";
import { columnsFor, pathForNode, pathLabel, searchTree, subtreeVerdict, systemsUnder, type ExploreRow } from "../lib/explore";
import { entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";

// Explore (#826 slice 2): find a system by walking the place tree. A
// depth-agnostic Miller-column drill built from the fleet view's parent
// pointers (lib/explore.ts holds the rules), with the glance as its
// rightmost column: the EntityForm in read mode under the monitoring line,
// editable in place. Selecting a location is a first-class state with its
// own glance and the two creates that make sense under it. The header's
// toggle wears today's list face (#798's kind tabs) at table density behind
// ?face=table; #828 replaces that face with the path-first table. Verdicts
// here are a glance; monitoring lives on dashboards and the workspaces.

const FACE_KEY = "explore-face";

function readStoredFace(): "tree" | "table" {
  try { return localStorage.getItem(FACE_KEY) === "table" ? "table" : "tree"; } catch { return "tree"; }
}
function storeFace(face: "tree" | "table") {
  try { localStorage.setItem(FACE_KEY, face); } catch { /* a private window or blocked storage: the URL still carries it */ }
}

export default function Explore() {
  const navigate = useNavigate();
  const me = useMe();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const blades = createBladeController();
  const [search, setSearch] = useSearchParams();

  // The face: the URL wins, then this browser's last choice, then the tree.
  // A ?node= address is a tree intent (it names a node to open), so the
  // stored table face never overrides it: a shared link lands where it says.
  const param = (k: string) => { const v = search[k]; return Array.isArray(v) ? v[0] : v; };
  const face = () => (param("face") === "table" ? "table" : "tree");
  onMount(() => {
    if (!param("face") && !param("node") && readStoredFace() === "table") setSearch({ face: "table" }, { replace: true });
  });
  const setFace = (f: "tree" | "table") => {
    storeFace(f);
    setSearch({ face: f === "table" ? "table" : undefined, kind: f === "table" ? search.kind : undefined });
  };

  // The list face (#798): the classic index pages as kind tabs, unchanged
  // until #828. Tabs a principal cannot read are dropped.
  const KINDS = [
    { key: "locations", label: "Locations", resource: "location", Face: LocationsPage },
    { key: "systems", label: "Systems", resource: "system", Face: SystemsPage },
    { key: "components", label: "Components", resource: "component", Face: ComponentsPage },
  ];
  const kinds = createMemo(() => KINDS.filter((k) => can(me.data, k.resource, "read")));
  const activeKind = createMemo(() => kinds().find((k) => k.key === param("kind")) ?? kinds()[0]);

  // The walk: a path of location ids (root-first) and the selected row.
  const [path, setPath] = createSignal<string[]>([]);
  const [selected, setSelected] = createSignal<string | null>(null);
  const [query, setQuery] = createSignal("");
  let searchBox: HTMLInputElement | undefined;

  // ?node= is an intent consumed when it changes: rebuild the columns down
  // to the node and select it. Selecting writes the param back (replace), so
  // a copied address lands where the operator stood.
  createEffect(on(() => [view.data, param("node")] as const, ([v, node]) => {
    if (!v || !node || node === selected()) return;
    const hit = pathForNode(v, node);
    if (!hit) return;
    setPath(hit.path);
    setSelected(hit.selected);
  }));
  const select = (id: string | null, nextPath?: string[]) => {
    if (nextPath) setPath(nextPath);
    setSelected(id);
    setSearch({ node: id ?? undefined }, { replace: true });
  };

  const columns = createMemo(() => (view.data ? columnsFor(view.data, path()) : []));
  const hits = createMemo(() => (view.data ? searchTree(view.data, query()) : []));
  const crumbs = createMemo(() => {
    const v = view.data;
    if (!v) return [] as { id: string; label: string }[];
    const index = locationIndex(v);
    return path().map((id) => ({ id, label: index.get(id) ? entityLabel(index.get(id)!) : id }));
  });

  const onRow = (row: ExploreRow, depth: number) => {
    const base = path().slice(0, depth);
    if (row.kind === "location") select(row.id, [...base, row.id]);
    else select(row.id, row.inLocation ? base : base);
  };
  const onHit = (id: string) => {
    const v = view.data;
    if (!v) return;
    const hit = pathForNode(v, id);
    setQuery("");
    if (hit) select(hit.selected, hit.path);
  };

  // Keys: "/" focuses search, "t" toggles the face; neither fires inside an input.
  const onKey = (e: KeyboardEvent) => {
    const tag = (e.target as HTMLElement | null)?.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
    if (e.key === "/") { e.preventDefault(); searchBox?.focus(); }
    if (e.key === "t") setFace(face() === "tree" ? "table" : "tree");
  };
  onMount(() => window.addEventListener("keydown", onKey));
  onCleanup(() => window.removeEventListener("keydown", onKey));

  // Arrow keys move within a column; Right drills a location, Left backs out.
  const onColumnKey = (e: KeyboardEvent, depth: number) => {
    const col = e.currentTarget as HTMLElement;
    const items = Array.from(col.querySelectorAll<HTMLButtonElement>("button[data-row]"));
    const i = items.indexOf(document.activeElement as HTMLButtonElement);
    if (e.key === "ArrowDown") { e.preventDefault(); items[Math.min(i + 1, items.length - 1)]?.focus(); }
    if (e.key === "ArrowUp") { e.preventDefault(); items[Math.max(i - 1, 0)]?.focus(); }
    if (e.key === "ArrowLeft" && depth > 0) { e.preventDefault(); setPath(path().slice(0, depth - 1)); select(path()[depth - 2] ?? null); }
    if (e.key === "ArrowRight" && i >= 0) { e.preventDefault(); items[i].click(); }
  };

  return (
    <BladesContext.Provider value={blades}>
      <Page title="Explore" subtitle="Find a system by walking the place tree.">
        <Show when={!view.isPending} fallback={<div class="skeleton h-32 w-full" />}>
          <Show
            when={!view.isError}
            fallback={<div role="alert" class="alert alert-error alert-soft text-sm">{describeError(view.error)}</div>}
          >
            <div class="flex flex-col gap-3">
              <div class="flex flex-wrap items-center gap-2">
                <input
                  ref={searchBox}
                  type="search"
                  role="searchbox"
                  class="input input-bordered w-full max-w-xl"
                  placeholder="Search: a system by name or path, a location (/)"
                  value={query()}
                  onInput={(e) => setQuery(e.currentTarget.value)}
                  onKeyDown={(e) => { if (e.key === "Escape") setQuery(""); if (e.key === "Enter" && hits()[0]) onHit(hits()[0].id); }}
                />
                <span class="flex-1" />
                <div class="join" role="group" aria-label="Face">
                  <Button size="sm" class="join-item" intent={face() === "tree" ? "action" : undefined} aria-pressed={face() === "tree"} onClick={() => setFace("tree")}>tree</Button>
                  <Button size="sm" class="join-item" intent={face() === "table" ? "action" : undefined} aria-pressed={face() === "table"} onClick={() => setFace("table")}>table</Button>
                </div>
              </div>

              <Show when={face() === "table"} fallback={
                <Show when={query().trim() === ""} fallback={
                  <div data-testid="explore-hits" class="card border border-base-300 bg-base-200">
                    <div class="flex items-center justify-between px-3 py-2 text-xs text-base-content/60">
                      <span>{hits().length === 1 ? "1 match" : `${hits().length} matches`} · systems first, then locations</span>
                      <span>Esc clears · Enter opens</span>
                    </div>
                    <For each={hits()}>
                      {(h) => (
                        <button type="button" class="flex w-full cursor-pointer items-center gap-3 border-t border-base-300 px-3 py-2 text-left text-sm hover:bg-base-content/5" onClick={() => onHit(h.id)}>
                          <span class="w-3 flex-none text-xs" classList={{ "text-primary": h.kind === "system", "text-base-content/50": h.kind === "location" }}>{h.kind === "system" ? "◆" : "▸"}</span>
                          <span class="min-w-0 flex-1">
                            <span class="font-medium">{h.label}</span>
                            <span class="block truncate text-xs text-base-content/60">{h.path}</span>
                          </span>
                          <HealthBadge verdict={h.verdict ?? undefined} size="xs" />
                        </button>
                      )}
                    </For>
                    <Show when={hits().length === 0}><p class="px-3 py-4 text-sm text-base-content/60">Nothing matches.</p></Show>
                  </div>
                }>
                  <nav aria-label="Path" class="text-xs text-base-content/60">
                    <button type="button" class="hover:underline" onClick={() => { setPath([]); select(null); }}>Locations</button>
                    <For each={crumbs()}>{(c, i) => <><span> / </span><button type="button" class="hover:underline" onClick={() => { setPath(path().slice(0, i() + 1)); select(c.id); }}>{c.label}</button></>}</For>
                  </nav>
                  <div class="flex overflow-x-auto rounded-box border border-base-300 bg-base-200" style={{ "min-height": "20rem" }}>
                    <For each={columns()}>
                      {(col, depth) => (
                        <div data-testid={`explore-column-${col.header}`} class="flex w-52 flex-none flex-col border-r border-base-300 py-1" onKeyDown={(e) => onColumnKey(e, depth())}>
                          <span class="px-3 pb-1 pt-2 text-[10px] uppercase tracking-wider text-base-content/50">{col.header}</span>
                          <For each={col.rows}>
                            {(row) => (
                              <button
                                type="button"
                                data-row={row.id}
                                class="flex cursor-pointer items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-base-content/5"
                                classList={{ "bg-base-content/10": selected() === row.id || path()[depth()] === row.id }}
                                aria-current={selected() === row.id ? "true" : undefined}
                                onClick={() => onRow(row, depth())}
                              >
                                <span class="w-3 flex-none text-xs" classList={{ "text-primary": row.kind === "system", "text-base-content/50": row.kind === "location" }}>{row.kind === "system" ? "◆" : "▸"}</span>
                                <span class="min-w-0 flex-1">
                                  <span class="truncate">{row.label}</span>
                                  <Show when={row.kind === "location"}><span class="ml-1.5 text-[10px] uppercase tracking-wider text-base-content/50">{(row as { type: string }).type}</span></Show>
                                  <Show when={row.kind === "system" && row.inLocation}>{(l) => <span class="block text-xs text-base-content/50">in {l().label}</span>}</Show>
                                </span>
                                <Show when={row.kind === "location" && row.systems === 0 && row.children === 0} fallback={
                                  <span class="flex flex-none items-center gap-1">
                                    <HealthBadge verdict={row.verdict ?? undefined} size="xs" />
                                    <Show when={row.kind === "location"}><span class="text-xs text-base-content/50">{(row as { systems: number }).systems}</span></Show>
                                  </span>
                                }>
                                  <span class="flex-none rounded border border-dashed border-base-content/25 px-1.5 text-[10px] text-base-content/50">empty</span>
                                </Show>
                              </button>
                            )}
                          </For>
                          <Show when={col.rows.length === 0}><span class="px-3 py-2 text-xs text-base-content/50">Nothing here yet.</span></Show>
                        </div>
                      )}
                    </For>
                    <Show when={selected()}>{(id) => <Glance id={id()} />}</Show>
                  </div>
                </Show>
              }>
                <div data-testid="fleet-list-face" class="card overflow-hidden border border-base-300 bg-base-200 pb-3">
                  <TabRail param="kind" tabs={kinds().map((k) => ({ key: k.key, label: k.label }))} />
                  <div class="px-3 pt-3">
                    <Show when={activeKind()}>{(k) => <Dynamic component={k().Face} />}</Show>
                  </div>
                </div>
              </Show>
            </div>
          </Show>
        </Show>
      </Page>
      <BladeStack controller={blades} registry={fleetRegistry} />
    </BladesContext.Provider>
  );

  // The glance: the rightmost column. For a system, its verdict and path over
  // the EntityForm; for a location, the roll-up and what sits under it, the
  // form, and the creates that make sense there. Its own edit slot, since it
  // is neither a blade nor a page.
  function Glance(props: { id: string }) {
    const v = () => view.data!;
    const loc = () => locationIndex(v()).get(props.id);
    const sys = () => (v().systems ?? []).find((s) => s.id === props.id);
    const slot = createEditSlot();
    const kind = () => (loc() ? "location" : "system");
    const canCreateLoc = () => can(me.data, "location", "create");
    const canCreateSys = () => can(me.data, "system", "create");
    const under = () => {
      const l = loc();
      if (!l) return null;
      const children = v().locations!.filter((x) => x.parent === l.id).length;
      const systems = systemsUnder(v(), l.id).length;
      const parts: string[] = [];
      if (children) parts.push(children === 1 ? "1 location" : `${children} locations`);
      parts.push(systems === 1 ? "1 system" : `${systems} systems`);
      return parts.join(" · ");
    };
    return (
      <aside data-testid="explore-glance" class="flex w-80 flex-none flex-col gap-3 border-l border-base-300 bg-base-100 p-3 text-sm">
        <span class="text-[10px] uppercase tracking-wider text-base-content/50">{slot.editing() ? "Edit" : "Glance"}</span>
        <Show when={loc() ?? sys()} fallback={<span class="text-base-content/60">Nothing answers this address.</span>}>
          {(row) => (
            <>
              <div>
                <div data-testid="glance-title" class="text-base font-semibold">{entityLabel(row() as { name: string; label?: string })}</div>
                <div class="text-xs text-base-content/60">{loc() ? pathLabel(v(), props.id) : (sys()?.location ? pathLabel(v(), sys()!.location!) : "Unplaced")}</div>
              </div>
              <div class="flex flex-wrap items-center gap-2">
                <HealthBadge verdict={(loc() ? subtreeVerdict(v(), props.id) : sys()?.verdict) ?? undefined} size="sm" />
                <Show when={loc()}><span class="text-xs text-base-content/60">{under()}</span></Show>
              </div>
              <EntityForm kind={kind()} id={props.id} slot={slot} host="glance" />
              <div class="flex flex-wrap items-center gap-2 border-t border-base-300 pt-3">
                <Show when={slot.editing()} fallback={
                  <>
                    <Button intent="action" onClick={() => navigate(loc() ? `/locations/${encodeURIComponent(props.id)}` : `/systems/${encodeURIComponent(props.id)}`)}>{loc() ? "Open location" : "Open workspace"}</Button>
                    <Show when={slot.editable()}><Button onClick={() => slot.begin()}>Edit</Button></Show>
                  </>
                }>
                  <Button onClick={() => slot.cancel()}>Cancel</Button>
                  <Button intent="action" loading={slot.saving()} disabled={!slot.valid()} onClick={() => slot.save().catch(() => { /* the form's alert says why; the slot stays editing */ })}>Save changes</Button>
                </Show>
              </div>
              <Show when={loc() && (canCreateLoc() || canCreateSys()) && !slot.editing()}>
                <div class="flex flex-wrap gap-2 rounded-field border border-dashed border-base-content/25 px-3 py-2 text-xs">
                  <Show when={canCreateLoc()}><Button size="xs" onClick={() => navigate(`/locations/create?under=${encodeURIComponent(props.id)}`)}>+ Location here</Button></Show>
                  <Show when={canCreateSys()}><Button size="xs" onClick={() => navigate(`/systems/create?under=${encodeURIComponent(props.id)}`)}>+ System here</Button></Show>
                  <span class="w-full text-base-content/50">the same form, empty, with placement prefilled</span>
                </div>
              </Show>
            </>
          )}
        </Show>
      </aside>
    );
  }
}
