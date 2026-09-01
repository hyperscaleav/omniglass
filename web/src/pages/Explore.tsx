import { createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useNavigate, useSearchParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import ListShell from "../components/ListShell";
import HealthBadge from "../components/HealthBadge";
import Button from "../components/Button";
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
  systemRows,
  totalOf,
  type SystemRow,
  resolveNode,
  roomsInView,
  sectionsFor,
  unplacedFor,
  type CardModel,
  type Counts,
  type DotItem,
  type ExploreOptions,
  type SectionModel,
} from "../lib/explore_view";
import { labelsAffordable, roomBoxesAffordable, type LabelMode } from "../lib/view_budgets";
import { matrixFor } from "../lib/matrix";
import {
  applyTo,
  DEFAULT_STATE,
  loadPresets,
  matches,
  remove as removePreset,
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
import { buildPredicate, type Chip, type FilterKey } from "../lib/predicate";
import { describeError } from "../lib/format";

// Explore (#839): one door into the fleet, with a few renderers over one
// model.
//
// What replaced the Miller columns and why: the columns spent a screen on four
// levels of tree, and they assumed a uniform depth the location model does not
// guarantee. This draws a card per CUT NODE instead, where the cut is chosen
// per root from the types that root actually contains (lib/place_cut.ts), so a
// campus of buildings and a two-level annex sit side by side with each card
// naming its own type.
//
// The controls are budgets, not preferences: labels and room boxes appear only
// while the view can afford them (lib/view_budgets.ts), because 602 room names
// do not fit on a screen at any type size. Auto is the default and the manual
// settings are an override for a screenshot.
//
// Faces: this renderer face and today's #798 kind-tab list face behind
// ?face=table, which stays until #828 replaces it with the path-first table.

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
    return raw ? { ...DEFAULT_PREFS, ...(JSON.parse(raw) as Partial<Prefs>) } : DEFAULT_PREFS;
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

  // The standard is on the systems list, not the fleet wire, and the matrix is
  // the only face that needs it. The table face already loads this, so the
  // join is usually free; enabling it only for the matrix keeps it that way.
  const systems = useQuery(() => ({
    queryKey: SYSTEMS_KEY,
    queryFn: listSystems,
    enabled: prefs().renderer === "matrix",
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
  const chips = createMemo<Chip[]>(() => {
    const raw = param("chips");
    if (!raw) return [];
    try { return JSON.parse(raw) as Chip[]; } catch { return []; }
  });
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
    // owns, rather than a second flag that could disagree with it.
    const rest = chips().filter((c) => c.key !== "verdict");
    setSearch({
      node: next.node ?? undefined,
      chips: next.attentionOnly ? JSON.stringify([...rest, { key: "verdict", op: "eq", values: ATTENTION }]) : undefined,
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

  const fleetCounts = createMemo(() => countsOf(rows()));
  const total = () => totalOf(fleetCounts());
  const needing = () => attentionOf(fleetCounts());

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

  // The label budget is spent against what is in front of the operator now,
  // which is why drilling gives the names back with no control touched.
  const rooms = createMemo(() => {
    const v = view.data;
    if (!v) return 0;
    const node = site();
    return roomsInView(v, node ? [node] : (v.locations ?? []).filter((l) => !l.parent).map((l) => l.id));
  });
  const showLabels = createMemo(() => labelsAffordable(rooms(), prefs().labelMode));
  const showBoxes = createMemo(() => roomBoxesAffordable(rooms(), prefs().labelMode, prefs().roomBox));

  const standardOf = createMemo(() => {
    const byId = new Map((systems.data ?? []).map((s) => [s.id, s.standard]));
    return (id: string) => byId.get(id) || undefined;
  });
  const matrix = createMemo(() =>
    view.data && prefs().renderer === "matrix" ? matrixFor(view.data, standardOf(), opts()) : null,
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
    if (e.key === "/") { e.preventDefault(); document.querySelector<HTMLInputElement>('input[role="combobox"]')?.focus(); }
    if (e.key === "t") setFace(face() === "cards" ? "table" : "cards");
    if (e.key === "Escape" && site()) setSite(null);
  };
  onMount(() => window.addEventListener("keydown", onKey));
  onCleanup(() => window.removeEventListener("keydown", onKey));

  const openSystem = (item: DotItem) => navigate(`/systems/${encodeURIComponent(item.id)}`);

  return (
    <Page title="Explore" subtitle="The whole fleet, however it is shaped.">
      <Show when={!view.isPending} fallback={<div class="skeleton h-32 w-full" />}>
        <Show
          when={!view.isError}
          fallback={<div role="alert" class="alert alert-error alert-soft text-sm">{describeError(view.error)}</div>}
        >
          <div class="flex flex-col gap-3">
            <div data-testid="explore-counts" class="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-base-content/70">
              <Show when={face() === "cards"}>
                {/* The drill, the quick filter and the label state belong to the
                    fleet face: the table face draws no dots to label, and each
                    of its tabs carries its own filter bar, so a control here
                    would claim to do something it cannot. */}
                <nav aria-label="Path" class="flex items-center gap-1">
                  <button type="button" class="font-medium hover:underline" classList={{ "text-primary": site() !== null }} disabled={site() === null} onClick={() => setSite(null)}>All locations</button>
                  <For each={crumbs()}>
                    {(c, i) => (
                      <>
                        <span aria-hidden="true">{"\u203a"}</span>
                        <button type="button" class="hover:underline" disabled={i() === crumbs().length - 1} onClick={() => setSite(c.id)}>{c.label}</button>
                      </>
                    )}
                  </For>
                </nav>
                <span class="text-base-content/30">{"\u00b7"}</span>
              </Show>
              <span class="tabular-nums">{total()} {total() === 1 ? "system" : "systems"}</span>
              <Show when={face() === "cards"}>
                <Show when={needing() > 0}>
                  <span class="text-base-content/30">{"\u00b7"}</span>
                  <Button size="xs" intent={attentionOn() ? "action" : "quiet"} pressed={attentionOn()} onClick={toggleAttention} title="Filter to what needs attention">
                    {needing()} need{needing() === 1 ? "s" : ""} attention
                  </Button>
                </Show>
                <span class="text-base-content/30">{"\u00b7"}</span>
                <span class="text-xs">{rooms()} {rooms() === 1 ? "room" : "rooms"} in view, labels {showLabels() ? "on" : "off"} ({prefs().labelMode === "auto" ? "auto" : "forced"})</span>
                {/* A fixed slot, always present. Growing the line on hover
                    reflowed the page under the pointer, which moved the dot out
                    from under the click that was landing on it. */}
                <span data-testid="explore-hover" class="w-56 flex-none truncate font-mono text-xs text-base-content/80">
                  <Show when={hovered()}>{(h) => <>{h().label} {"\u00b7"} {h().verdict}</>}</Show>
                </span>
              </Show>
              <span class="flex-1" />
              <div class="join" role="group" aria-label="Face">
                <Button size="sm" class="join-item" intent={face() === "cards" ? "action" : undefined} aria-pressed={face() === "cards"} onClick={() => setFace("cards")}>fleet</Button>
                <Button size="sm" class="join-item" intent={face() === "table" ? "action" : undefined} aria-pressed={face() === "table"} onClick={() => setFace("table")}>table</Button>
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
                  trailing={<Controls prefs={prefs()} onPrefs={setPrefs} />}
                >
                  {() => (
                    <div class="flex flex-col gap-3.5 p-3">
                <Show when={sections().length > 0 || unplaced().length > 0} fallback={
                  <p class="rounded-box border border-dashed border-base-300 px-4 py-8 text-center text-sm text-base-content/60">
                    {chips().length > 0 ? "Nothing here matches the filter." : "No locations to show."}
                  </p>
                }>
                  <Show when={prefs().renderer === "mosaic"}>
                    <Mosaic sections={sections()} onDrill={setSite} onHover={setHovered} />
                  </Show>
                  <Show when={prefs().renderer === "matrix"}>
                    <Show when={matrix()} fallback={<div class="skeleton h-40 w-full" />}>
                      {(m) => <MatrixFace model={m()} onDrill={setSite} onHover={setHovered} />}
                    </Show>
                  </Show>
                  <div class="flex flex-col gap-5" classList={{ hidden: prefs().renderer === "mosaic" || prefs().renderer === "matrix" }}>
                    <For each={sections()}>
                      {(section) => (
                        <SectionView
                          section={section}
                          drilled={site() !== null}
                          canCreateLocation={can(me.data, "location", "create")}
                          canCreateSystem={can(me.data, "system", "create")}
                          onCreate={(kind, under) => navigate(`/${kind}/create?under=${encodeURIComponent(under)}`)}
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
                <div role="tablist" class="tabs tabs-box w-fit">
                  <For each={kinds()}>
                    {(k) => (
                      <button type="button" role="tab" class="tab" classList={{ "tab-active": activeKind()?.key === k.key }} onClick={() => setSearch({ face: "table", kind: k.key })}>{k.label}</button>
                    )}
                  </For>
                </div>
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
      <span class="text-[10px] uppercase tracking-wider text-base-content/50">Presets</span>
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
                class="join-item px-1.5"
                label={`Forget ${preset.name}`}
                onClick={() => props.onForget(preset.name)}
              >
                ×
              </Button>
            </Show>
          </span>
        )}
      </For>
      <Show
        when={naming()}
        fallback={
          <Button size="xs" class="border-dashed" onClick={() => setNaming(true)}>Save this view</Button>
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
        <span class="text-[10px] text-warning">Saved views cannot be stored in this browser; the shipped ones still work.</span>
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
// These are hard-coded option lists, so ADR-0133's ref binding does not apply:
// there is no async gap in which the control could hold a value it has no
// option for.
function Field(props: { label: string; value: string; options: string[]; onPick: (v: string) => void }) {
  return (
    <label class="flex items-center gap-1.5 text-[10px] uppercase tracking-wider text-base-content/50">
      {props.label}
      <select
        class="select select-xs select-bordered w-auto text-xs normal-case tracking-normal text-base-content"
        value={props.value}
        onChange={(e) => props.onPick(e.currentTarget.value)}
        aria-label={props.label}
      >
        <For each={props.options}>{(o) => <option value={o}>{o[0].toUpperCase() + o.slice(1)}</option>}</For>
      </select>
    </label>
  );
}

function Controls(props: {
  prefs: Prefs;
  onPrefs: (patch: Partial<Prefs>) => void;
}) {
  return (
    <div data-testid="explore-controls" class="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-box border border-base-300 bg-base-200 px-3 py-1.5">
      <Field label="View" value={props.prefs.renderer} options={["cards", "bands", "mosaic", "matrix"]} onPick={(v) => props.onPrefs({ renderer: v as RendererKey })} />
      <Field label="Labels" value={props.prefs.labelMode} options={["auto", "always", "off"]} onPick={(v) => props.onPrefs({ labelMode: v as LabelMode })} />
      <Field label="Density" value={props.prefs.density} options={["compact", "cozy", "roomy"]} onPick={(v) => props.onPrefs({ density: v as Density })} />
      <Field label="Sort" value={props.prefs.sort} options={["worst", "name"]} onPick={(v) => props.onPrefs({ sort: v as "worst" | "name" })} />
      <label class="flex cursor-pointer items-center gap-1.5 text-xs">
        <input type="checkbox" class="checkbox checkbox-xs" checked={props.prefs.roomBox} onChange={(e) => props.onPrefs({ roomBox: e.currentTarget.checked })} />
        Room boxes
      </label>
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

function sectionMeta(section: SectionModel): string {
  const n = section.cards.length;
  const kind = `${n} ${section.cutType}${n === 1 ? "" : "s"}`;
  return `${section.type} · ${kind} · ${countsLine(section.counts)}`;
}

function SectionView(props: {
  section: SectionModel;
  drilled: boolean;
  canCreateLocation: boolean;
  canCreateSystem: boolean;
  onCreate: (kind: "locations" | "systems", under: string) => void;
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
        <div data-testid="explore-section-head" class="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-b border-base-300 pb-1">
          <h2 class="text-base font-semibold">{props.section.label}</h2>
          {/* One text node, not several: a run of JSX expressions renders as
              separate nodes, which reads badly to a screen reader and cannot be
              matched as a phrase. */}
          <span class="font-mono text-[11px] text-base-content/50">{sectionMeta(props.section)}</span>
          <Show when={attention() > 0}><HealthBadge verdict={worstOf(props.section.counts)} size="xs" /></Show>
          {/* Create where you stand: the node in the header is the placement,
              so the form opens already knowing where it lands. */}
          <Show when={props.drilled}>
            <span class="flex-1" />
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
        <div data-testid="explore-above-cut" class="flex flex-wrap items-center gap-2 rounded bg-base-200 px-2 py-1.5">
          <span class="font-mono text-[10px] text-base-content/50">
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
      class="w-full cursor-pointer text-left"
      onClick={() => props.onDrill(props.card.id)}
      aria-label={`Open ${props.card.label}`}
    >
      <span class="block truncate text-sm font-semibold">{props.card.label}</span>
      <span class="block truncate font-mono text-[10px] text-base-content/50">
        {`${props.card.type} · ${countsLine(props.card.counts)}`}
      </span>
    </button>
  );
  return (
    <Show
      when={props.renderer === "cards"}
      fallback={
        <div class="grid grid-cols-[12rem_1fr] items-start gap-4 py-2">
          <div>
            {header}
            <Show when={attention() > 0}><HealthBadge verdict={worstOf(props.card.counts)} size="xs" /></Show>
          </div>
          <DotField node={props.card.field} density={props.density} showLabels={props.showLabels} showBoxes={props.showBoxes} onHover={props.onHover} onPick={props.onPick} />
        </div>
      }
    >
      <div
        class="flex flex-col overflow-hidden rounded-box border bg-base-100"
        classList={{ "border-base-300": attention() === 0, "border-warning/60": attention() > 0 && props.card.counts.outage === 0, "border-error/60": props.card.counts.outage > 0 }}
      >
        <div class="border-b border-base-300 bg-base-200 px-2.5 py-1.5">{header}</div>
        <div class="p-2.5">
          <DotField node={props.card.field} density={props.density} showLabels={props.showLabels} showBoxes={props.showBoxes} onHover={props.onHover} onPick={props.onPick} />
        </div>
      </div>
    </Show>
  );
}
