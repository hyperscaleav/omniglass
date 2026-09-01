import { locationIndex, childrenIndex, type FleetLocation, type FleetSystem, type FleetView } from "./fleet";
import { verdictOf, type Verdict } from "./health";
import { entityLabel } from "./entities";
import { aboveCutSystems, cutNodesFor, cutTypeFor, unplacedSystems } from "./place_cut";

// The explorer's view model (#839): the fleet view plus the operator's
// controls, turned into the shapes a renderer draws and nothing else. Pure, so
// the parts an operator could dispute (which card a system lands in, what a
// count says, what order things come in) are unit-testable with no DOM, and so
// the cards, bands, mosaic and matrix renderers all consume one model rather
// than each deriving their own.

export type Grain = "system";
export type Sort = "worst" | "name";

export type Counts = { healthy: number; incomplete: number; degraded: number; outage: number };

export type DotItem = { id: string; label: string; verdict: Verdict | null };

// A field is the nested dot area under one card. It carries no level names on
// purpose: the place tree has no fixed depth, so a renderer spaces by SUBTREE
// HEIGHT rather than by what a level is called. An uneven branch then reads as
// shallower instead of as broken.
export type FieldNode = {
  id: string;
  label: string;
  type: string;
  height: number;
  items: DotItem[];
  children: FieldNode[];
};

export type CardModel = {
  id: string;
  label: string;
  type: string;
  systems: number;
  counts: Counts;
  field: FieldNode;
};

export type SectionModel = {
  id: string;
  label: string;
  type: string;
  cutType: string;
  isOwnCut: boolean;
  cards: CardModel[];
  above: DotItem[];
  systems: number;
  counts: Counts;
};

// include is the filter the chrome applies. The shell owns the chips and the
// predicate; the model only asks whether a system survived them, so the two
// cannot disagree about what is on screen.
export type ExploreOptions = { sort: Sort; include?: (systemId: string) => boolean };

export const emptyCounts = (): Counts => ({ healthy: 0, incomplete: 0, degraded: 0, outage: 0 });

export function countsOf(items: Array<{ verdict: Verdict | null }>): Counts {
  const c = emptyCounts();
  for (const i of items) {
    if (i.verdict) c[i.verdict]++;
    else c.healthy++;
  }
  return c;
}

export function totalOf(c: Counts): number {
  return c.healthy + c.incomplete + c.degraded + c.outage;
}

// What "needs attention" means in one place, so the filter, the counts line
// and any fill that ramps on it can never disagree.
//
// This counts INCOMPLETE, which the console's fleet tiles have always counted
// (lib/fleet_tiles). An earlier version here left it out on the grounds that a
// commissioning gap is not a fault, which is true of how it should be RANKED
// and false of whether somebody has to act on it. Two surfaces in one console
// disagreeing about what needs attention is worse than either answer, and the
// older surface is the console's.
export function attentionOf(c: Counts): number {
  return c.degraded + c.outage + c.incomplete;
}

const RANK: Record<string, number> = { healthy: 0, incomplete: 1, degraded: 2, outage: 3 };

function rank(v: Verdict | null): number {
  return v ? (RANK[v] ?? 0) : 0;
}

function toItem(s: FleetSystem): DotItem {
  return { id: s.id, label: entityLabel(s), verdict: verdictOf(s.verdict) };
}

function sortItems(items: DotItem[], sort: Sort): DotItem[] {
  return [...items].sort((a, b) =>
    sort === "name"
      ? a.label.localeCompare(b.label) || a.id.localeCompare(b.id)
      : rank(b.verdict) - rank(a.verdict) || a.label.localeCompare(b.label) || a.id.localeCompare(b.id),
  );
}

function keep(item: DotItem, opts: ExploreOptions): boolean {
  return !opts.include || opts.include(item.id);
}

// heightOf is how deep a subtree runs below a node, in levels. It is what the
// renderer scales its gaps by, which is why it lives in the model rather than
// being measured from the DOM.
function heightOf(id: string, children: Map<string, FleetLocation[]>, guard: number): number {
  let height = 0;
  let frontier = [id];
  const seen = new Set<string>();
  while (frontier.length > 0 && guard-- > 0) {
    const next: string[] = [];
    for (const nodeId of frontier) {
      if (seen.has(nodeId)) continue;
      seen.add(nodeId);
      for (const child of children.get(nodeId) ?? []) next.push(child.id);
    }
    if (next.length > 0) height++;
    frontier = next;
  }
  return height;
}

// fieldFor builds one card's nested dot area. A node contributes its own
// systems and then each child as a nested group; a branch with nothing in it
// is dropped rather than drawn as an empty box.
export function fieldFor(view: FleetView, nodeId: string, opts: ExploreOptions): FieldNode | null {
  const children = childrenIndex(view);
  const guard = (view.locations?.length ?? 0) + 1;
  const systems = view.systems ?? [];

  const index = locationIndex(view);
  const build = (id: string, depth: number): FieldNode | null => {
    if (depth > guard) return null;
    const here = index.get(id);
    const items = sortItems(
      systems.filter((s) => s.location === id).map(toItem).filter((i) => keep(i, opts)),
      opts.sort,
    );
    const kids: FieldNode[] = [];
    for (const child of children.get(id) ?? []) {
      const built = build(child.id, depth + 1);
      if (built) kids.push(built);
    }
    if (items.length === 0 && kids.length === 0) return null;
    return {
      id,
      label: here ? entityLabel(here) : "",
      type: here?.location_type ?? "",
      height: heightOf(id, children, guard),
      items,
      children: kids,
    };
  };

  return build(nodeId, 0);
}

// Every system under a node, at any depth, which is what a card's headline
// counts. Bounded like every other walk here.
function systemsUnder(view: FleetView, nodeId: string): FleetSystem[] {
  const children = childrenIndex(view);
  const out: FleetSystem[] = [];
  const seen = new Set<string>();
  let frontier = [nodeId];
  let guard = (view.locations?.length ?? 0) + 1;
  const systems = view.systems ?? [];
  while (frontier.length > 0 && guard-- > 0) {
    const next: string[] = [];
    for (const id of frontier) {
      if (seen.has(id)) continue;
      seen.add(id);
      out.push(...systems.filter((s) => s.location === id));
      for (const child of children.get(id) ?? []) next.push(child.id);
    }
    frontier = next;
  }
  return out;
}

// A card for one cut node. An empty node still gets a card while nothing is
// being filtered, because an unfinished tree should be visible rather than
// hidden: a building created a moment ago must not disappear from the fleet.
// Under a filter it is dropped, since an operator triaging outages is not
// asking about rooms that hold nothing.
function cardFor(view: FleetView, node: FleetLocation, opts: ExploreOptions): CardModel | null {
  const field = fieldFor(view, node.id, opts);
  if (!field && opts.include) return null;
  const all = systemsUnder(view, node.id).map(toItem);
  return {
    id: node.id,
    label: entityLabel(node),
    type: node.location_type,
    systems: all.length,
    counts: countsOf(all),
    field: field ?? { id: node.id, label: entityLabel(node), type: node.location_type, height: 0, items: [], children: [] },
  };
}

// sectionsFor is the fleet level: one section per root, its cards at that
// root's own cut, and the systems attached above the cut which belong to no
// card. A section with nothing left to draw after the filter is dropped.
export function sectionsFor(view: FleetView, opts: ExploreOptions): SectionModel[] {
  const roots = (view.locations ?? []).filter((l) => !l.parent);
  const out: SectionModel[] = [];
  for (const root of roots) {
    const cutType = cutTypeFor(view, root.id);
    const cards: CardModel[] = [];
    for (const node of cutNodesFor(view, root.id)) {
      const card = cardFor(view, node, opts);
      if (card) cards.push(card);
    }
    const above = sortItems(aboveCutSystems(view, root.id).map(toItem).filter((i) => keep(i, opts)), opts.sort);
    if (cards.length === 0 && above.length === 0) continue;
    const all = systemsUnder(view, root.id).map(toItem);
    out.push({
      id: root.id,
      label: entityLabel(root),
      type: root.location_type,
      cutType,
      isOwnCut: cutType === root.location_type,
      cards,
      above,
      systems: all.length,
      counts: countsOf(all),
    });
  }
  return out.sort((a, b) => a.label.localeCompare(b.label) || a.id.localeCompare(b.id));
}

// insideOf is the drilled level: one card per child of the node, or the node
// itself when it has no children worth carding. The same shapes as the fleet
// level, so the renderers do not branch on which level they are drawing.
export function insideOf(view: FleetView, nodeId: string, opts: ExploreOptions): SectionModel | null {
  const index = locationIndex(view);
  const node = index.get(nodeId);
  if (!node) return null;
  const children = childrenIndex(view).get(nodeId) ?? [];
  const cards: CardModel[] = [];
  for (const child of children) {
    const card = cardFor(view, child, opts);
    if (card) cards.push(card);
  }
  // Systems attached to the node itself sit above its children, exactly as a
  // campus paging system sits above its buildings.
  const own = sortItems(
    (view.systems ?? []).filter((s) => s.location === nodeId).map(toItem).filter((i) => keep(i, opts)),
    opts.sort,
  );
  // An existing node ALWAYS returns a section, even an empty one. A campus
  // created a moment ago holds nothing, and returning null there would render
  // a blank page with no header, so there would be nowhere to stand and no way
  // to create the first thing inside it.
  const all = systemsUnder(view, nodeId).map(toItem);
  return {
    id: node.id,
    label: entityLabel(node),
    type: node.location_type,
    cutType: children[0]?.location_type ?? node.location_type,
    isOwnCut: children.length === 0,
    cards: cards.sort((a, b) => a.label.localeCompare(b.label) || a.id.localeCompare(b.id)),
    above: own,
    systems: all.length,
    counts: countsOf(all),
  };
}

// The systems a walk from the roots would drop: readable, but placed at a
// location this caller cannot read, or placed nowhere at all. Rendered as its
// own section so they are visible rather than silently missing.
export function unplacedFor(view: FleetView, opts: ExploreOptions): DotItem[] {
  return sortItems(unplacedSystems(view).map(toItem).filter((i) => keep(i, opts)), opts.sort);
}

// The rows the chrome filters: every system the view carries, with the facts a
// facet needs already resolved. Deriving them here keeps the page from teaching
// the filter bar about the place tree.
export type SystemRow = {
  id: string;
  name: string;
  label: string;
  verdict: Verdict | null;
  path: string;
  locationType: string;
  // The haystack the bare typed term searches: the system's rendered label, its
  // handle, and the place it sits in. It is built here rather than in the page
  // so the label is already resolved once, by the one renderer, before anything
  // concatenates it.
  search: string;
};

export function systemRows(view: FleetView): SystemRow[] {
  const index = locationIndex(view);
  const pathOf = (id: string | undefined): string => {
    if (!id) return "";
    const parts: string[] = [];
    let cur = index.get(id);
    let guard = (view.locations?.length ?? 0) + 1;
    while (cur && guard-- > 0) {
      parts.unshift(entityLabel(cur));
      cur = cur.parent ? index.get(cur.parent) : undefined;
    }
    return parts.join(" / ");
  };
  return (view.systems ?? []).map((s) => {
    const path = pathOf(s.location);
    return {
      id: s.id,
      name: s.name,
      label: entityLabel(s),
      verdict: verdictOf(s.verdict),
      path,
      locationType: s.location ? (index.get(s.location)?.location_type ?? "") : "",
      search: [entityLabel(s), s.name, path].join(" "),
    };
  });
}

// roomsInView is what the label budget is spent against: the leaf locations
// the operator is currently looking at, not the fleet's total.
export function roomsInView(view: FleetView, nodeIds: string[]): number {
  const children = childrenIndex(view);
  const seen = new Set<string>();
  let count = 0;
  let frontier = [...nodeIds];
  let guard = (view.locations?.length ?? 0) + 1;
  while (frontier.length > 0 && guard-- > 0) {
    const next: string[] = [];
    for (const id of frontier) {
      if (seen.has(id)) continue;
      seen.add(id);
      const kids = children.get(id) ?? [];
      if (kids.length === 0) count++;
      for (const child of kids) next.push(child.id);
    }
    frontier = next;
  }
  return count;
}

// countsLine is the one line every altitude shows. Zeros are left out and the
// singular reads as a fact, both of which the fleet header already does.
export function countsLine(c: Counts): string {
  const parts: string[] = [];
  const total = totalOf(c);
  parts.push(`${total} ${total === 1 ? "system" : "systems"}`);
  if (c.outage) parts.push(`${c.outage} in outage`);
  if (c.degraded) parts.push(`${c.degraded} degraded`);
  if (c.incomplete) parts.push(`${c.incomplete} incomplete`);
  return parts.join(" · ");
}

// resolveNode turns a ?node= address into the location to drill into.
//
// ADR-0062 makes the uuid the address, and #759's rule lets a name-shaped one
// resolve when it names exactly one thing, systems before locations. The drill
// has to keep both: the operator guide links /web/explore?node=huddle by name,
// and a shared link landing on an empty page is worse than one that refuses.
//
// This deliberately does NOT reuse pathForNode. That resolver carries the
// Miller-column collapse rule, where a room holding one system is replaced by
// the system's own row, so it returns the room's PARENT. Cards have no such
// collapse: the room is a real place to stand, and drilling to its parent would
// silently show the operator somewhere other than the address they followed.
export function resolveNode(view: FleetView, address: string): string | null {
  if (!address) return null;
  const index = locationIndex(view);
  if (index.has(address)) return address;

  const systems = view.systems ?? [];
  const byId = systems.find((s) => s.id === address);
  if (byId) return byId.location || null;

  const namedSystems = systems.filter((s) => s.name === address);
  if (namedSystems.length === 1) return namedSystems[0].location || null;
  if (namedSystems.length > 1) return null; // ambiguous: refuse rather than guess

  const namedLocations = (view.locations ?? []).filter((l) => l.name === address);
  return namedLocations.length === 1 ? namedLocations[0].id : null;
}
