import { createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useNavigate, useSearchParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import ListShell from "../components/ListShell";
import TabRail from "../components/TabRail";
import HealthBadge from "../components/HealthBadge";
import Button from "../components/Button";
import InfoTip from "../components/InfoTip";
import { Grid, Maximize, Plus, Rows, X } from "../components/icons";
import DotField, { type Density } from "../components/DotField";
import Mosaic from "../components/Mosaic";
import MatrixFace from "../components/MatrixFace";
import LocationsPage from "./Locations";
import SystemsPage from "./Systems";
import ComponentsPage from "./Components";
import { can, useMe } from "../lib/auth";
import { FLEET_VIEW_KEY, fleetView, locationIndex, ancestors } from "../lib/fleet";
import {
  attentionOf,
  countsLine,
  countsOf,
  insideOf,
  resolveNode,
  placesInView,
  sectionLine,
  sectionsFor,
  systemRows,
  totalOf,
  unplacedFor,
  type CardModel,
  type Counts,
  type DotItem,
  type ExploreOptions,
  type SectionModel,
  type SystemRow,
} from "../lib/explore_view";
import { labelsAffordable, placeBoxesAffordable, type LabelMode } from "../lib/view_budgets";
import { matrixFor } from "../lib/matrix";
import {
  applyTo,
  DEFAULT_STATE,
  loadPresets,
  matches,
  remove as removePreset,
  sanitizeState,
  savePresets,
  STOCK_PRESETS,
  upsert,
  type Preset,
  type PresetState,
  type RendererKey,
} from "../lib/presets";
import { listSystems, SYSTEMS_KEY } from "../lib/systems";
import { entityLabel } from "../lib/entities";
import type { Verdict } from "../lib/health";
import { buildPredicate, parseChips, type Chip, type FilterKey } from "../lib/predicate";
import { describeError } from "../lib/format";

// Explore (#839): one door into the fleet, with a few renderers over one
// model.
//
// It draws a card per CUT NODE, where the cut is chosen per root from the
// types that root actually contains (lib/place_cut.ts), so a campus of
// buildings and a two-level annex sit side by side with each card naming its
// own type. Counting levels from the root would assume a uniform depth the
// location model does not guarantee.
//
// The controls are budgets, not preferences: labels and room boxes appear only
// while the view can afford them (lib/view_budgets.ts), because 602 room names
// do not fit on a screen at any type size. Auto is the default and the manual
// settings are an override for a screenshot.
//
// Faces: this renderer face, and the table face behind ?face=table (the three
// kind tables as tabs), which stays until #828 replaces it with the path-first
// table.

const FACE_KEY = "explore-face";
const PREFS_KEY = "explore-prefs";

// The browser half of a preset. The other half, the drilled node and the
// filter, rides the URL so a link carries it, which is the split ?face=table
// already uses.
type Prefs = Pick<PresetState, "renderer" | "density" | "labelMode" | "roomBox" | "sort">;

const DEFAULT_PREFS: Prefs = {
  renderer: DEFAULT_STATE.renderer,
  density: DEFAULT_STATE.density,
  labelMode: DEFAULT_STATE.labelMode,
  roomBox: DEFAULT_STATE.roomBox,
  sort: DEFAULT_STATE.sort,
};

function readStoredFace(): "cards" | "table" {
  try { return localStorage.getItem(FACE_KEY) === "table" ? "table" : "cards"; } catch { return "cards"; }
}
function storeFace(face: "cards" | "table") {
  try { localStorage.setItem(FACE_KEY, face); } catch { /* a private window: the URL still carries it */ }
}
// How you look at the fleet is a browser preference; WHAT you are looking at
// (the drilled node, the filter) rides the URL so a link carries it. Same split
// ?face=table already uses.
function readPrefs(): Prefs {
  try {
    const raw = localStorage.getItem(PREFS_KEY);
    if (!raw) return DEFAULT_PREFS;
    // Checked on the way in (lib/presets sanitizeState): a renderer key an
    // older build stored must not reach the page as a renderer nothing draws.
    const s = sanitizeState(JSON.parse(raw));
    return { renderer: s.renderer, density: s.density, labelMode: s.labelMode, roomBox: s.roomBox, sort: s.sort };
  } catch { return DEFAULT_PREFS; }
}
function storePrefs(p: Prefs) {
  try { localStorage.setItem(PREFS_KEY, JSON.stringify(p)); } catch { /* nothing to do */ }
}

const KINDS = [
  { key: "locations", label: "Locations", resource: "location", page: LocationsPage },
  { key: "systems", label: "Systems", resource: "system", page: SystemsPage },
  { key: "components", label: "Components", resource: "component", page: ComponentsPage },
] as const;

export default function Explore() {
  const navigate = useNavigate();
  const me = useMe();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const [search, setSearch] = useSearchParams();

  const param = (k: string) => { const v = search[k]; return Array.isArray(v) ? v[0] : v; };
  const face = () => (param("face") === "table" ? "table" : "cards");
  onMount(() => {
    if (!param("face") && !param("node") && readStoredFace() === "table") setSearch({ face: "table" }, { replace: true });
  });
  const setFace = (f: "cards" | "table") => {
    storeFace(f);
    setSearch({ face: f === "table" ? "table" : undefined, kind: f === "table" ? search.kind : undefined });
  };

  const [prefs, setPrefsSignal] = createSignal<Prefs>(readPrefs());
  const setPrefs = (patch: Partial<Prefs>) => {
    const next = { ...prefs(), ...patch };
    setPrefsSignal(next);
    storePrefs(next);
  };

  // The standard is on the systems list, not the fleet wire, and two things
  // here read it: the matrix pivots on it and the filter bar offers it as a
  // key. So the read rides the fleet face rather than the matrix alone, which
  // is what it did at first: on arrival `standard:` then matched nothing and
  // offered no values. It is the read the table face makes anyway, so the
  // cache is usually warm; a caller who may not read systems has none to join.
  const systems = useQuery(() => ({
    queryKey: SYSTEMS_KEY,
    queryFn: listSystems,
    enabled: face() === "cards" && can(me.data, "system", "read"),
  }));

  // The drilled node and the filter live in the URL: a shared link lands on
  // the same thing, which the stored preferences must never override.
  // The address may be a uuid or a unique name (ADR-0062, #759's rule), so it
  // is resolved against the view rather than used as an id directly.
  const site = createMemo(() => {
    const raw = param("node");
    if (!raw || !view.data) return null;
    return resolveNode(view.data, raw);
  });
  const setSite = (id: string | null) => setSearch({ node: id ?? undefined });
  // The chips are the filter. They ride the URL so a link carries what the
  // other person was looking at, the same split ?face=table already uses.
  const chips = createMemo<Chip[]>(() => parseChips(param("chips")));
  const setChips = (next: Chip[]) => setSearch({ chips: next.length ? JSON.stringify(next) : undefined });

  // The console's one definition of needing attention, as a chip, so the counts
  // line's quick filter and the filter bar are the same control.
  const ATTENTION = ["outage", "degraded", "incomplete"];
  const attentionOn = () => chips().some((c) => c.key === "verdict" && ATTENTION.every((v) => c.values.includes(v)));
  const toggleAttention = () => {
    const rest = chips().filter((c) => c.key !== "verdict");
    setChips(attentionOn() ? rest : [...rest, { key: "verdict", op: "eq", values: ATTENTION }]);
  };

  const [saved, setSaved] = createSignal<Preset[]>(loadPresets());
  const [storageWorks, setStorageWorks] = createSignal(true);

  // A preset is a snapshot of the same object the controls write to, so this
  // reads straight off them rather than keeping a second copy in step.
  const currentState = createMemo<PresetState>(() => ({
    ...prefs(),
    attentionOnly: attentionOn(),
    node: site(),
  }));

  const applyPreset = (preset: Preset) => {
    const v = view.data;
    const next = applyTo(preset, (id) => (v ? locationIndex(v).has(id) : false));
    setPrefs({
      renderer: next.renderer,
      density: next.density,
      labelMode: next.labelMode,
      roomBox: next.roomBox,
      sort: next.sort,
    });
    // A preset that wanted the attention filter sets the chip the filter bar
    // owns, rather than a second flag that could disagree with it. That chip is
    // the only one a preset speaks for: whatever else the operator typed into
    // the bar stays, because no preset ever named it.
    const rest = chips().filter((c) => c.key !== "verdict");
    const wanted: Chip[] = next.attentionOnly ? [...rest, { key: "verdict", op: "eq", values: ATTENTION }] : rest;
    setSearch({
      node: next.node ?? undefined,
      chips: wanted.length ? JSON.stringify(wanted) : undefined,
    });
  };

  const saveCurrent = (name: string) => {
    const next = upsert(saved(), name, currentState());
    setSaved(next);
    setStorageWorks(savePresets(next));
  };
  const forget = (name: string) => {
    const next = removePreset(saved(), name);
    setSaved(next);
    setStorageWorks(savePresets(next));
  };

  const [hovered, setHovered] = createSignal<{ label: string; verdict: string } | null>(null);

  const kinds = createMemo(() => KINDS.filter((k) => can(me.data, k.resource, "read")));
  const activeKind = createMemo(() => kinds().find((k) => k.key === param("kind")) ?? kinds()[0]);

  const rows = createMemo<SystemRow[]>(() => (view.data ? systemRows(view.data) : []));
  const filterKeys: FilterKey<SystemRow>[] = [
    // The bare term the operator types lands here (FilterBar's fallback is the
    // first substring key), so it matches a system's name OR where it sits.
    // That is what the search box this replaced did: it looked through systems
    // and locations both, and typing a building name still has to find the
    // things in that building.
    { key: "name", type: "string", hint: "substring", get: (r) => r.search },
    // Path narrows to a place by typing it. Deliberately a substring match and
    // not a facet of location ids: naming a subject is what the drill is for,
    // and it is the line between this page and a dashboard.
    { key: "path", type: "string", hint: "substring", get: (r) => r.path },
    { key: "verdict", type: "string", get: (r) => r.verdict ?? "healthy", values: () => ["healthy", "incomplete", "degraded", "outage"] },
    { key: "type", type: "string", get: (r) => r.locationType, values: (rs) => [...new Set(rs.map((r) => r.locationType).filter(Boolean))].sort() },
    { key: "standard", type: "string", get: (r) => standardOf()(r.id) ?? "", values: (rs) => [...new Set(rs.map((r) => standardOf()(r.id)).filter(Boolean) as string[])].sort() },
  ];

  // The chips are applied here rather than by ListShell, because the body is a
  // tree of cards and not a row list: the model has to know which systems
  // survived so a card that lost all of its own can be dropped. ListShell's own
  // `filtered` memo is pull-based and never runs for a body that ignores it,
  // which is the contract the tree pages already use.
  const kept = createMemo(() => {
    const cs = chips();
    if (cs.length === 0) return null;
    const pass = buildPredicate(filterKeys, cs);
    return new Set(rows().filter(pass).map((r) => r.id));
  });
  const opts = createMemo<ExploreOptions>(() => {
    const set = kept();
    return { sort: prefs().sort, include: set ? (id: string) => set.has(id) : undefined };
  });

  const sections = createMemo<SectionModel[]>(() => {
    const v = view.data;
    if (!v) return [];
    const node = site();
    if (node) {
      const inside = insideOf(v, node, opts());
      return inside ? [inside] : [];
    }
    return sectionsFor(v, opts());
  });

  const unplaced = createMemo<DotItem[]>(() => (view.data && !site() ? unplacedFor(view.data, opts()) : []));

  // The counts line counts where the operator is standing. It sits beside the
  // breadcrumb, so "Headquarters · 41 systems" is read as a fact about
  // Headquarters; and a fleet-wide "2 need attention" offered inside a node
  // that holds neither would filter that node down to nothing. The filter
  // still never moves it: these are the place's counts, not the chips'.
  const standing = createMemo(() => {
    const v = view.data;
    const node = face() === "cards" ? site() : null;
    return (v && node ? insideOf(v, node, { sort: "worst" })?.counts : undefined) ?? countsOf(rows());
  });
  const total = () => totalOf(standing());
  const needing = () => attentionOf(standing());
  // Which controls mean anything under the renderer in force. Labels, density,
  // room boxes and the dot order are properties of a dot field; the mosaic
  // orders by weight and the matrix by place and standard, so offering them
  // there would claim to do something they cannot.
  const drawsDots = () => prefs().renderer === "cards" || prefs().renderer === "bands";

  // The label budget is spent against what is in front of the operator now,
  // which is why drilling gives the names back with no control touched.
  const places = createMemo(() => {
    const v = view.data;
    if (!v) return 0;
    const node = site();
    return placesInView(v, node ? [node] : (v.locations ?? []).filter((l) => !l.parent).map((l) => l.id));
  });
  const showLabels = createMemo(() => labelsAffordable(places(), prefs().labelMode));
  const showBoxes = createMemo(() => placeBoxesAffordable(places(), prefs().labelMode, prefs().roomBox));

  const standardOf = createMemo(() => {
    const byId = new Map((systems.data ?? []).map((s) => [s.id, s.standard]));
    return (id: string) => byId.get(id) || undefined;
  });
  const matrix = createMemo(() =>
    view.data && prefs().renderer === "matrix" ? matrixFor(view.data, standardOf(), opts(), site()) : null,
  );

  const crumbs = createMemo(() => {
    const v = view.data;
    const node = site();
    if (!v || !node) return [] as { id: string; label: string }[];
    return ancestors(node, locationIndex(v)).map((l) => ({ id: l.id, label: entityLabel(l) }));
  });

  const onKey = (e: KeyboardEvent) => {
    const tag = (e.target as HTMLElement | null)?.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
    // A held modifier makes it the browser's key, not this page's: Ctrl+T
    // opens a tab and must not also swap the face underneath it.
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key === "/") { e.preventDefault(); document.querySelector<HTMLInputElement>('input[role="combobox"]')?.focus(); }
    if (e.key === "t") setFace(face() === "cards" ? "table" : "cards");
    if (e.key === "Escape" && site()) setSite(null);
  };
  onMount(() => window.addEventListener("keydown", onKey));
  onCleanup(() => window.removeEventListener("keydown", onKey));

  const openSystem = (item: DotItem) => navigate(`/systems/${encodeURIComponent(item.id)}`);

  const fleetEmpty = () => (view.data?.locations ?? []).length === 0 && (view.data?.systems ?? []).length === 0;

  return (
    <Page title="Explore" subtitle="The whole fleet, however it is shaped.">
      <Show when={!view.isPending} fallback={<div class="skeleton h-32 w-full" />}>
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
            {/* The facts wrap; the face toggle does not. One wrapping row let a
                deep breadcrumb push the toggle onto a line of its own, so the
                control moved every time the drill got one level longer. */}
            <div class="flex items-start gap-3">
              <div data-testid="explore-counts" class="flex min-h-8 min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-1 text-sm text-base-content/70">
                <Show when={face() === "cards"}>
                  {/* The drill, the quick filter and the label state belong to
                      the fleet face: the table face draws no dots to label, and
                      each of its tabs carries its own filter bar, so a control
                      here would claim to do something it cannot. */}
                  <nav aria-label="Path" class="flex flex-wrap items-center gap-1">
                    <button type="button" class="font-medium" classList={{ "cursor-pointer text-primary hover:underline": site() !== null }} disabled={site() === null} onClick={() => setSite(null)}>All locations</button>
                    <For each={crumbs()}>
                      {(c, i) => {
                        const here = () => i() === crumbs().length - 1;
                        return (
                          <>
                            <span aria-hidden="true" class="text-base-content/40">{"›"}</span>
                            <button type="button" classList={{ "cursor-pointer hover:underline": !here(), "font-medium text-base-content": here() }} aria-current={here() ? "location" : undefined} disabled={here()} onClick={() => setSite(c.id)}>{c.label}</button>
                          </>
                        );
                      }}
                    </For>
                  </nav>
                  <span class="text-base-content/30">{"·"}</span>
                </Show>
                <span class="tabular-nums">{total()} {total() === 1 ? "system" : "systems"}</span>
                <Show when={face() === "cards"}>
                  {/* Shown while there is something to filter to, and while the
                      filter is on, so the control that set it can clear it. */}
                  <Show when={needing() > 0 || attentionOn()}>
                    <span class="text-base-content/30">{"·"}</span>
                    <Button size="xs" intent={attentionOn() ? "action" : "quiet"} pressed={attentionOn()} onClick={toggleAttention} title="Filter to what needs attention">
                      {needing()} need{needing() === 1 ? "s" : ""} attention
                    </Button>
                  </Show>
                  <Show when={drawsDots()}>
                    <span class="text-base-content/30">{"·"}</span>
                    <span class="text-xs">{places()} {places() === 1 ? "place" : "places"} in view, labels {showLabels() ? "on" : "off"} ({prefs().labelMode === "auto" ? "auto" : "forced"})</span>
                  </Show>
                  {/* A slot that is always present and takes only what is left
                      of the line. Growing the line on hover reflowed the page
                      under the pointer, which moved the dot out from under the
                      click that was landing on it. */}
                  <span data-testid="explore-hover" aria-live="off" class="min-w-0 flex-1 basis-24 truncate font-data text-xs text-base-content/80">
                    <Show when={hovered()}>{(h) => <>{h().label} {"·"} {h().verdict}</>}</Show>
                  </span>
                </Show>
              </div>
              {/* The same density toggle the location workspace wears (cards
                  against rows), so the two faces are one idiom in two places. */}
              <div data-testid="explore-face" class="join flex-none" role="group" aria-label="Face">
                <Button square icon={Grid} title="Fleet view" label="Fleet view" class="join-item" intent={face() === "cards" ? "action" : "quiet"} pressed={face() === "cards"} onClick={() => setFace("cards")} />
                <Button square icon={Rows} title="Table view" label="Table view" class="join-item" intent={face() === "table" ? "action" : "quiet"} pressed={face() === "table"} onClick={() => setFace("table")} />
              </div>
            </div>

            <Show when={face() === "table"} fallback={
              <>
                <PresetBar
                  presets={[...STOCK_PRESETS, ...saved()]}
                  state={currentState()}
                  storageWorks={storageWorks()}
                  onApply={applyPreset}
                  onSave={saveCurrent}
                  onForget={forget}
                />

                <ListShell
                  filterKeys={filterKeys}
                  rows={rows()}
                  chips={chips}
                  onChips={setChips}
                  placeholder="filter: verdict, type, standard, path, name"
                  trailing={<Controls prefs={prefs()} drawsDots={drawsDots()} onPrefs={setPrefs} />}
                >
                  {() => (
                    <div class="flex flex-col gap-3.5 p-3">
                      <Show when={sections().length > 0 || unplaced().length > 0} fallback={
                        <div class="flex flex-col items-center gap-3 rounded-box border border-dashed border-base-300 px-4 py-8 text-center text-sm text-base-content/60">
                          <Show when={fleetEmpty()} fallback={<p>{chips().length > 0 ? "Nothing here matches the filter." : "No locations to show."}</p>}>
                            <p>No locations yet.</p>
                            <Show when={can(me.data, "location", "create")}>
                              <Button intent="action" icon={Plus} onClick={() => navigate("/locations/create")}>New location</Button>
                            </Show>
                          </Show>
                        </div>
                      }>
                        <Show when={prefs().renderer === "mosaic"}>
                          <Mosaic sections={sections()} onDrill={setSite} onHover={setHovered} />
                        </Show>
                        <Show when={prefs().renderer === "matrix"}>
                          <Show when={matrix()} fallback={<div class="skeleton h-40 w-full" />}>
                            {(m) => <MatrixFace model={m()} onDrill={setSite} onHover={setHovered} />}
                          </Show>
                        </Show>
                        <div class="flex flex-col gap-5" classList={{ hidden: !drawsDots() }}>
                          <For each={sections()}>
                            {(section) => (
                              <SectionView
                                section={section}
                                drilled={site() !== null}
                                canCreateLocation={can(me.data, "location", "create")}
                                canCreateSystem={can(me.data, "system", "create")}
                                onCreate={(kind, under) => navigate(`/${kind}/create?under=${encodeURIComponent(under)}`)}
                                onOpen={(id) => navigate(`/locations/${encodeURIComponent(id)}`)}
                                renderer={prefs().renderer}
                                density={prefs().density}
                                showLabels={showLabels()}
                                showBoxes={showBoxes()}
                                onDrill={setSite}
                                onHover={setHovered}
                                onPick={openSystem}
                              />
                            )}
                          </For>
                          <Show when={unplaced().length > 0}>
                            <section data-testid="explore-unplaced" class="rounded-box border border-dashed border-warning/50 bg-base-200 p-3">
                              <h3 class="text-sm font-semibold">Placed nowhere you can see</h3>
                              <p class="mb-2 text-xs text-base-content/60">
                                {unplaced().length} {unplaced().length === 1 ? "system is" : "systems are"} readable but sit at a location you cannot read, or at none at all.
                              </p>
                              <DotField
                                node={{ id: "unplaced", label: "", type: "", height: 0, items: unplaced(), children: [] }}
                                density={prefs().density}
                                onHover={(h) => setHovered(h)}
                                onPick={openSystem}
                              />
                            </section>
                          </Show>
                        </div>
                      </Show>
                    </div>
                  )}
                </ListShell>
              </>
            }>
              <div data-testid="fleet-list-face" class="flex flex-col gap-3">
                {/* The workspace's own tab rail, riding ?kind= (its first tab
                    is the bare address), so a kind tab here is the same control
                    as Overview and Configure on the page a row opens. */}
                <TabRail param="kind" tabs={kinds().map((k) => ({ key: k.key, label: k.label }))} activeKey={() => activeKind()?.key ?? ""} />
                <Show when={activeKind()}>{(k) => <Dynamic component={k().page} />}</Show>
              </div>
            </Show>
          </div>
        </Show>
      </Show>
    </Page>
  );
}

// The preset bar. A preset is a way of looking, named after the job it serves,
// and it is a snapshot of the same object the controls write to, so a saved
// view can never mean something the controls cannot produce.
//
// What is deliberately absent is any way to save a SCOPE. Every field a preset
// carries changes how the fleet is drawn or which live state is filtered; none
// names a subject to include. The moment one needs sharing or its own scope it
// has become a dashboard widget, and that is a promotion rather than a feature
// here.
function PresetBar(props: {
  presets: Preset[];
  state: PresetState;
  storageWorks: boolean;
  onApply: (p: Preset) => void;
  onSave: (name: string) => void;
  onForget: (name: string) => void;
}) {
  const [naming, setNaming] = createSignal(false);
  const [draft, setDraft] = createSignal("");
  const commit = () => {
    const name = draft().trim();
    if (name) props.onSave(name);
    setDraft("");
    setNaming(false);
  };
  return (
    <div data-testid="explore-presets" class="flex flex-wrap items-center gap-2">
      <span class="flex items-center gap-1.5">
        <span class="eyebrow">Presets</span>
        <InfoTip label="Presets" text="A preset is a way of looking at the fleet, named after the job it serves. It changes how the fleet is drawn, never what is in it. Saved views are kept in this browser." />
      </span>
      <For each={props.presets}>
        {(preset) => (
          <span class="join">
            <Button
              size="xs"
              intent={matches(props.state, preset) ? "action" : "quiet"}
              pressed={matches(props.state, preset)}
              class={preset.stock ? "join-item" : "join-item border-dashed"}
              title={preset.why}
              onClick={() => props.onApply(preset)}
            >
              {preset.name}
            </Button>
            <Show when={!preset.stock}>
              <Button
                size="xs"
                square
                icon={X}
                class="join-item"
                title={`Forget ${preset.name}`}
                label={`Forget ${preset.name}`}
                onClick={() => props.onForget(preset.name)}
              />
            </Show>
          </span>
        )}
      </For>
      <Show
        when={naming()}
        fallback={
          <Button size="xs" class="border-dashed" icon={Plus} onClick={() => setNaming(true)}>Save this view</Button>
        }
      >
        <input
          type="text"
          class="input input-xs input-bordered w-40"
          placeholder="Name this view"
          aria-label="Name this view"
          value={draft()}
          autofocus
          onInput={(e) => setDraft(e.currentTarget.value)}
          onKeyDown={(e) => { if (e.key === "Enter") commit(); if (e.key === "Escape") { setDraft(""); setNaming(false); } }}
          onBlur={commit}
        />
      </Show>
      <Show when={!props.storageWorks}>
        <span class="text-xs text-warning">Saved views cannot be stored in this browser; the shipped ones still work.</span>
      </Show>
    </div>
  );
}

// The control panel. Every control here changes HOW the fleet is drawn, never
// WHAT is in it: that line is what keeps this an explorer rather than a
// dashboard with no owner and no permissions. The one filter allowed,
// need-attention, is a predicate over live state rather than a naming of
// subjects, so it rides the URL with the drilled node.
//
// One row of selects rather than four rows of chips. Chips show every option at
// once, which is the right trade at three options and the wrong one at
// fourteen: the panel ended up taller than the fleet it framed. A select shows
// the setting in force and hides the rest until asked, which is what a setting
// wants, and the presets above cover the common cases without opening one.
//
// A control is offered only under a renderer it changes. Labels, density, sort
// and room boxes all shape a dot field, so under the mosaic and the matrix
// they are absent rather than inert.
//
// These are hard-coded option lists, so ADR-0133's ref binding does not apply:
// there is no async gap in which the control could hold a value it has no
// option for.
//
// The hint sits between the label text and the select, and there is no <label>
// element around the three: a button inside a <label> steals the label's
// target and joins the control's accessible name, so the select is named by
// aria-label and the eyebrow is its visible caption.
function Field(props: { label: string; hint?: string; value: string; options: string[]; onPick: (v: string) => void }) {
  return (
    <span class="flex items-center gap-1.5">
      <span class="eyebrow" aria-hidden="true">{props.label}</span>
      <Show when={props.hint}><InfoTip label={props.label} text={props.hint!} /></Show>
      <select
        class="select select-xs select-bordered w-auto text-xs text-base-content"
        value={props.value}
        onChange={(e) => props.onPick(e.currentTarget.value)}
        aria-label={props.label}
      >
        <For each={props.options}>{(o) => <option value={o}>{o[0].toUpperCase() + o.slice(1)}</option>}</For>
      </select>
    </span>
  );
}

function Controls(props: {
  prefs: Prefs;
  drawsDots: boolean;
  onPrefs: (patch: Partial<Prefs>) => void;
}) {
  return (
    <div data-testid="explore-controls" class="flex flex-wrap items-center gap-x-4 gap-y-2">
      <Field
        label="View"
        hint="Cards and bands draw each system as a dot under the place it sits in. The mosaic sizes each place by how many systems it holds and fills it by the share needing attention. The matrix counts each standard in each place."
        value={props.prefs.renderer}
        options={["cards", "bands", "mosaic", "matrix"]}
        onPick={(v) => props.onPrefs({ renderer: v as RendererKey })}
      />
      <Show when={props.drawsDots}>
        <Field
          label="Labels"
          hint="Auto names the places at the bottom of your tree only while the names fit, which is why they return when you open a card. Always and Off override it."
          value={props.prefs.labelMode}
          options={["auto", "always", "off"]}
          onPick={(v) => props.onPrefs({ labelMode: v as LabelMode })}
        />
        <Field label="Density" value={props.prefs.density} options={["compact", "cozy", "roomy"]} onPick={(v) => props.onPrefs({ density: v as Density })} />
        <Field label="Sort" value={props.prefs.sort} options={["worst", "name"]} onPick={(v) => props.onPrefs({ sort: v as "worst" | "name" })} />
        <label class="flex cursor-pointer items-center gap-1.5 text-xs">
          <input type="checkbox" class="checkbox checkbox-xs" checked={props.prefs.roomBox} onChange={(e) => props.onPrefs({ roomBox: e.currentTarget.checked })} />
          Place boxes
        </label>
      </Show>
    </div>
  );
}

// One root's section: its cards at that root's own cut, plus anything attached
// above the cut. A card names its own type, which is how a non-uniform fleet
// reads as non-uniform instead of being flattened.
// The badge hue is the WORST thing present, which is not the same question as
// whether anything needs attention: a section whose only trouble is unfinished
// commissioning needs somebody, and is not degraded.
function worstOf(c: Counts): Verdict {
  return c.outage > 0 ? "outage" : c.degraded > 0 ? "degraded" : "incomplete";
}

function SectionView(props: {
  section: SectionModel;
  drilled: boolean;
  canCreateLocation: boolean;
  canCreateSystem: boolean;
  onCreate: (kind: "locations" | "systems", under: string) => void;
  onOpen: (id: string) => void;
  renderer: RendererKey;
  density: Density;
  showLabels: boolean;
  showBoxes: boolean;
  onDrill: (id: string) => void;
  onHover: (h: { label: string; verdict: string } | null) => void;
  onPick: (item: DotItem) => void;
}) {
  const attention = () => attentionOf(props.section.counts);
  return (
    <section data-testid={`explore-section-${props.section.id}`} class="flex flex-col gap-2">
      <Show when={props.drilled || !props.section.isOwnCut || props.section.cards.length > 1}>
        <div data-testid="explore-section-head" class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-base-300 pb-1.5">
          <h2 class="text-base font-semibold">{props.section.label}</h2>
          {/* One text node, not several: a run of JSX expressions renders as
              separate nodes, which reads badly to a screen reader and cannot be
              matched as a phrase. */}
          <span class="font-data text-[11px] text-base-content/50">{sectionLine(props.section)}</span>
          <Show when={attention() > 0}><HealthBadge verdict={worstOf(props.section.counts)} size="xs" /></Show>
          {/* Create where you stand: the node in the header is the placement,
              so the form opens already knowing where it lands. */}
          <Show when={props.drilled}>
            <span class="flex-1" />
            {/* The way from the drill to the location's own workspace (its
                Overview and Configure tabs), which the table face used to be
                the only road to. */}
            <Button size="xs" icon={Maximize} onClick={() => props.onOpen(props.section.id)}>Open location</Button>
            <Show when={props.canCreateLocation}>
              <Button size="xs" onClick={() => props.onCreate("locations", props.section.id)}>+ Location here</Button>
            </Show>
            <Show when={props.canCreateSystem}>
              <Button size="xs" onClick={() => props.onCreate("systems", props.section.id)}>+ System here</Button>
            </Show>
          </Show>
        </div>
      </Show>

      <Show when={props.section.above.length > 0}>
        <div data-testid="explore-above-cut" class="flex flex-wrap items-center gap-2 rounded bg-base-100 px-2 py-1.5">
          <span class="font-data text-[10px] text-base-content/50">
            {props.section.above.length} {props.section.above.length === 1 ? "system" : "systems"} attached above this level
          </span>
          <DotField
            node={{ id: `${props.section.id}-above`, label: "", type: "", height: 0, items: props.section.above, children: [] }}
            density={props.density}
            onHover={props.onHover}
            onPick={props.onPick}
          />
        </div>
      </Show>

      <div classList={{ "grid gap-3 [grid-template-columns:repeat(auto-fill,minmax(16rem,1fr))]": props.renderer === "cards", "flex flex-col divide-y divide-base-300": props.renderer === "bands" }}>
        <For each={props.section.cards}>
          {(card) => (
            <CardView
              card={card}
              renderer={props.renderer}
              density={props.density}
              showLabels={props.showLabels}
              showBoxes={props.showBoxes}
              onDrill={props.onDrill}
              onHover={props.onHover}
              onPick={props.onPick}
            />
          )}
        </For>
      </div>
    </section>
  );
}

function CardView(props: {
  card: CardModel;
  renderer: RendererKey;
  density: Density;
  showLabels: boolean;
  showBoxes: boolean;
  onDrill: (id: string) => void;
  onHover: (h: { label: string; verdict: string } | null) => void;
  onPick: (item: DotItem) => void;
}) {
  const attention = () => attentionOf(props.card.counts);
  const header = (
    <button
      type="button"
      data-card={props.card.id}
      class="w-full cursor-pointer rounded text-left focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
      onClick={() => props.onDrill(props.card.id)}
      aria-label={`Open ${props.card.label}`}
    >
      <span class="block truncate text-sm font-semibold">{props.card.label}</span>
      <span class="block truncate font-data text-[10px] text-base-content/50">
        {`${props.card.type} · ${countsLine(props.card.counts)}`}
      </span>
    </button>
  );
  // The card's header already names the node the field is rooted at, so the
  // field is told not to name it again.
  const field = () => (
    <DotField node={props.card.field} rootNamed density={props.density} showLabels={props.showLabels} showBoxes={props.showBoxes} onHover={props.onHover} onPick={props.onPick} />
  );
  return (
    <Show
      when={props.renderer === "cards"}
      fallback={
        <div class="grid grid-cols-[12rem_1fr] items-start gap-4 py-2">
          <div class="flex flex-col items-start gap-1">
            {header}
            <Show when={attention() > 0}><HealthBadge verdict={worstOf(props.card.counts)} size="xs" /></Show>
          </div>
          {field()}
        </div>
      }
    >
      <div
        class="flex flex-col overflow-hidden rounded-box border bg-base-100"
        classList={{
          "border-base-300": attention() === 0,
          "border-incomplete/45": attention() > 0 && props.card.counts.outage === 0 && props.card.counts.degraded === 0,
          "border-warning/60": props.card.counts.degraded > 0 && props.card.counts.outage === 0,
          "border-error/60": props.card.counts.outage > 0,
        }}
      >
        <div class="bg-base-200 px-2.5 py-1.5" classList={{ "border-b border-base-300": props.card.systems > 0 }}>{header}</div>
        {/* An empty node is a header and nothing under it: a blank strip would
            read as dots that failed to load. */}
        <Show when={props.card.systems > 0}>
          <div class="p-2.5">{field()}</div>
        </Show>
      </div>
    </Show>
  );
}
