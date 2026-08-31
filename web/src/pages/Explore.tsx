import { createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useNavigate, useSearchParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import HealthBadge from "../components/HealthBadge";
import Button from "../components/Button";
import DotField, { type Density } from "../components/DotField";
import LocationsPage from "./Locations";
import SystemsPage from "./Systems";
import ComponentsPage from "./Components";
import { can, useMe } from "../lib/auth";
import { FLEET_VIEW_KEY, fleetView, locationIndex, ancestors } from "../lib/fleet";
import { pathForNode, searchTree } from "../lib/explore";
import {
  attentionOf,
  countsLine,
  insideOf,
  roomsInView,
  sectionsFor,
  unplacedFor,
  type CardModel,
  type DotItem,
  type ExploreOptions,
  type SectionModel,
} from "../lib/explore_view";
import { labelsAffordable, roomBoxesAffordable, type LabelMode } from "../lib/view_budgets";
import { entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";

// Explore (#839): one door into the estate, with a few renderers over one
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

type RendererKey = "cards" | "bands";

type Prefs = { renderer: RendererKey; density: Density; labelMode: LabelMode; roomBox: boolean; sort: "worst" | "name" };

const DEFAULT_PREFS: Prefs = { renderer: "cards", density: "compact", labelMode: "auto", roomBox: true, sort: "worst" };

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

  // The drilled node and the filter live in the URL: a shared link lands on
  // the same thing, which the stored preferences must never override.
  const site = () => param("node") ?? null;
  const setSite = (id: string | null) => setSearch({ node: id ?? undefined });
  const attentionOnly = () => param("attention") === "1";
  const setAttentionOnly = (on: boolean) => setSearch({ attention: on ? "1" : undefined });

  const [query, setQuery] = createSignal("");
  const [hovered, setHovered] = createSignal<{ label: string; verdict: string } | null>(null);
  let searchBox: HTMLInputElement | undefined;

  const kinds = createMemo(() => KINDS.filter((k) => can(me.data, k.resource, "read")));
  const activeKind = createMemo(() => kinds().find((k) => k.key === param("kind")) ?? kinds()[0]);

  const opts = createMemo<ExploreOptions>(() => ({ attentionOnly: attentionOnly(), sort: prefs().sort }));

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
  const hits = createMemo(() => (view.data ? searchTree(view.data, query()) : []));

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

  const crumbs = createMemo(() => {
    const v = view.data;
    const node = site();
    if (!v || !node) return [] as { id: string; label: string }[];
    return ancestors(node, locationIndex(v)).map((l) => ({ id: l.id, label: entityLabel(l) }));
  });

  const onHit = (id: string) => {
    const v = view.data;
    if (!v) return;
    setQuery("");
    const hit = pathForNode(v, id);
    // A hit is either a location to drill into or a system to open.
    if (hit && locationIndex(v).has(hit.selected)) setSite(hit.selected);
    else navigate(`/systems/${encodeURIComponent(id)}`);
  };

  const onKey = (e: KeyboardEvent) => {
    const tag = (e.target as HTMLElement | null)?.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
    if (e.key === "/") { e.preventDefault(); searchBox?.focus(); }
    if (e.key === "t") setFace(face() === "cards" ? "table" : "cards");
    if (e.key === "Escape" && site()) setSite(null);
  };
  onMount(() => window.addEventListener("keydown", onKey));
  onCleanup(() => window.removeEventListener("keydown", onKey));

  const openSystem = (item: DotItem) => navigate(`/systems/${encodeURIComponent(item.id)}`);

  return (
    <Page title="Explore" subtitle="The whole estate, however it is shaped.">
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
                <Button size="sm" class="join-item" intent={face() === "cards" ? "action" : undefined} aria-pressed={face() === "cards"} onClick={() => setFace("cards")}>fleet</Button>
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
                <Controls
                  prefs={prefs()}
                  onPrefs={setPrefs}
                  attentionOnly={attentionOnly()}
                  onAttentionOnly={setAttentionOnly}
                  drilled={site() !== null}
                />

                <div data-testid="explore-status" class="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-base-content/60">
                  <nav aria-label="Path" class="flex items-center gap-1">
                    <button type="button" class="font-medium hover:underline" classList={{ "text-primary": site() !== null }} disabled={site() === null} onClick={() => setSite(null)}>All locations</button>
                    <For each={crumbs()}>
                      {(c, i) => (
                        <>
                          <span aria-hidden="true">›</span>
                          <button type="button" class="hover:underline" disabled={i() === crumbs().length - 1} onClick={() => setSite(c.id)}>{c.label}</button>
                        </>
                      )}
                    </For>
                  </nav>
                  <span>{rooms()} {rooms() === 1 ? "room" : "rooms"} in view · labels {showLabels() ? "on" : "off"} ({prefs().labelMode === "auto" ? "auto" : "forced"})</span>
                  <Show when={hovered()}>{(h) => <span class="font-mono text-base-content/80">{h().label} · {h().verdict}</span>}</Show>
                </div>

                <Show when={sections().length > 0 || unplaced().length > 0} fallback={
                  <p class="rounded-box border border-dashed border-base-300 px-4 py-8 text-center text-sm text-base-content/60">
                    {attentionOnly() ? "Nothing needs attention." : "No locations to show."}
                  </p>
                }>
                  <div class="flex flex-col gap-5">
                    <For each={sections()}>
                      {(section) => (
                        <SectionView
                          section={section}
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
              </Show>
            }>
              <div class="flex flex-col gap-3">
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


// The control panel. Every control here changes HOW the estate is drawn, never
// WHAT is in it: that line is what keeps this an explorer rather than a
// dashboard with no owner and no permissions. The one filter allowed,
// need-attention, is a predicate over live state rather than a naming of
// subjects, so it rides the URL with the drilled node.
function Controls(props: {
  prefs: Prefs;
  onPrefs: (patch: Partial<Prefs>) => void;
  attentionOnly: boolean;
  onAttentionOnly: (on: boolean) => void;
  drilled: boolean;
}) {
  const chip = (active: boolean) => `btn btn-xs ${active ? "btn-primary" : "btn-ghost"}`;
  return (
    <div data-testid="explore-controls" class="flex flex-wrap items-center gap-x-5 gap-y-2 rounded-box border border-base-300 bg-base-200 px-3 py-2">
      <div class="flex items-center gap-1">
        <span class="mr-1 text-[10px] uppercase tracking-wider text-base-content/50">View</span>
        <button type="button" class={chip(props.prefs.renderer === "cards")} aria-pressed={props.prefs.renderer === "cards"} onClick={() => props.onPrefs({ renderer: "cards" })}>Cards</button>
        <button type="button" class={chip(props.prefs.renderer === "bands")} aria-pressed={props.prefs.renderer === "bands"} onClick={() => props.onPrefs({ renderer: "bands" })}>Bands</button>
      </div>
      <div class="flex items-center gap-1">
        <span class="mr-1 text-[10px] uppercase tracking-wider text-base-content/50">Labels</span>
        <For each={["auto", "always", "off"] as LabelMode[]}>
          {(m) => (
            <button type="button" class={chip(props.prefs.labelMode === m)} aria-pressed={props.prefs.labelMode === m} onClick={() => props.onPrefs({ labelMode: m })}>{m}</button>
          )}
        </For>
      </div>
      <div class="flex items-center gap-1">
        <span class="mr-1 text-[10px] uppercase tracking-wider text-base-content/50">Density</span>
        <For each={["compact", "cozy", "roomy"] as Density[]}>
          {(d) => (
            <button type="button" class={chip(props.prefs.density === d)} aria-pressed={props.prefs.density === d} onClick={() => props.onPrefs({ density: d })}>{d}</button>
          )}
        </For>
      </div>
      <div class="flex items-center gap-1">
        <span class="mr-1 text-[10px] uppercase tracking-wider text-base-content/50">Sort</span>
        <button type="button" class={chip(props.prefs.sort === "worst")} aria-pressed={props.prefs.sort === "worst"} onClick={() => props.onPrefs({ sort: "worst" })}>Worst first</button>
        <button type="button" class={chip(props.prefs.sort === "name")} aria-pressed={props.prefs.sort === "name"} onClick={() => props.onPrefs({ sort: "name" })}>By name</button>
      </div>
      <label class="flex cursor-pointer items-center gap-2 text-xs">
        <input type="checkbox" class="checkbox checkbox-xs" checked={props.prefs.roomBox} onChange={(e) => props.onPrefs({ roomBox: e.currentTarget.checked })} />
        Room boxes
      </label>
      <label class="flex cursor-pointer items-center gap-2 text-xs">
        <input type="checkbox" class="checkbox checkbox-xs" checked={props.attentionOnly} onChange={(e) => props.onAttentionOnly(e.currentTarget.checked)} />
        Only what needs attention
      </label>
    </div>
  );
}

// One root's section: its cards at that root's own cut, plus anything attached
// above the cut. A card names its own type, which is how a non-uniform estate
// reads as non-uniform instead of being flattened.
function SectionView(props: {
  section: SectionModel;
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
      <Show when={!props.section.isOwnCut || props.section.cards.length > 1}>
        <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-b border-base-300 pb-1">
          <h2 class="text-base font-semibold">{props.section.label}</h2>
          <span class="font-mono text-[11px] text-base-content/50">
            {props.section.type} · {props.section.cards.length} {props.section.cutType}{props.section.cards.length === 1 ? "" : "s"} · {countsLine(props.section.counts)}
          </span>
          <Show when={attention() > 0}><HealthBadge verdict={props.section.counts.outage ? "outage" : "degraded"} size="xs" /></Show>
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
        {props.card.type} · {countsLine(props.card.counts)}
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
            <Show when={attention() > 0}><HealthBadge verdict={props.card.counts.outage ? "outage" : "degraded"} size="xs" /></Show>
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
