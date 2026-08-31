import { childrenIndex, locationIndex, type FleetLocation, type FleetSystem, type FleetView } from "./fleet";

// The place cut (#837): which level of a root's tree becomes a card.
//
// The rule that matters is what this does NOT do. It never counts levels from
// the root. `location_type` and `allowed_parent_types` make depth a customer's
// fact, not a layout: one estate's roots are buildings, the next one's root is
// a campus holding six of them, a third runs campus, building, wing, floor,
// room, and two branches of one tree can disagree with each other. A renderer
// that says "level one is a site, level two is a floor" puts a card labelled
// Campus beside a card labelled Building and loses everything past level three.
//
// So the cut is chosen per root, from the types that root actually contains,
// and the card names its own type: a non-uniform estate then reads as
// non-uniform instead of being flattened into a shape it does not have.
//
// The tier order is derived from the tree rather than from a list of type
// names. The fleet wire carries a location's type but not the type graph, and
// hard-coding campus > building > floor > room would work only for the
// vocabulary this repo happens to seed. Minimum observed depth answers the
// same question from data the client already holds, and it keeps working when
// a customer invents a type nobody here has heard of.

// Two memos, keyed on the view object exactly as fleet.ts keys its own: the
// cut is asked for once per root per render, and both of these answer a
// question about the WHOLE view, so recomputing them per node turns a render
// into O(locations x systems) for nothing.
const leafCache = new WeakMap<FleetView, Set<string>>();
const systemCache = new WeakMap<FleetView, Map<string, FleetSystem[]>>();

function leafTypes(view: FleetView): Set<string> {
  const hit = leafCache.get(view);
  if (hit) return hit;
  const children = childrenIndex(view);
  const containers = new Set<string>();
  const all = new Set<string>();
  for (const loc of view.locations ?? []) {
    all.add(loc.location_type);
    if ((children.get(loc.id) ?? []).length > 0) containers.add(loc.location_type);
  }
  const leaves = new Set([...all].filter((t) => !containers.has(t)));
  leafCache.set(view, leaves);
  return leaves;
}

function systemsByLocation(view: FleetView): Map<string, FleetSystem[]> {
  const hit = systemCache.get(view);
  if (hit) return hit;
  const index = new Map<string, FleetSystem[]>();
  for (const s of view.systems ?? []) {
    if (!s.location) continue;
    const list = index.get(s.location);
    if (list) list.push(s);
    else index.set(s.location, [s]);
  }
  systemCache.set(view, index);
  return index;
}

// A cheap bound for every walk. The tree comes from the wire, so a parent
// cycle is data we do not control, and a renderer must not hang on it.
function guardOf(view: FleetView): number {
  return (view.locations?.length ?? 0) + 1;
}

// depthOf measures a location's distance from its root, bounded.
function depthOf(loc: FleetLocation, index: Map<string, FleetLocation>, guard: number): number {
  let depth = 0;
  let current: FleetLocation | undefined = loc;
  let left = guard;
  while (current?.parent && left-- > 0) {
    const next = index.get(current.parent);
    if (!next) break;
    current = next;
    depth++;
  }
  return depth;
}

// typeTiers is each type's shallowest observed depth across the whole view:
// the closest thing to a tier order that can be read off the data. Types no
// location uses are absent rather than present at infinity.
export function typeTiers(view: FleetView): Map<string, number> {
  const index = locationIndex(view);
  const guard = guardOf(view);
  const tiers = new Map<string, number>();
  for (const loc of view.locations ?? []) {
    const depth = depthOf(loc, index, guard);
    const seen = tiers.get(loc.location_type);
    if (seen === undefined || depth < seen) tiers.set(loc.location_type, depth);
  }
  return tiers;
}

// isLeafType is true when no location of that type contains another location.
// A leaf type is where systems live, not a level to cut at: cutting there
// would make one card per room, which is the failure the budgets exist to
// prevent rather than to produce.
export function isLeafType(view: FleetView, type: string): boolean {
  return leafTypes(view).has(type);
}

// walk visits a root's subtree, breadth-first, with the visitor deciding
// whether to descend. Bounded by a seen-set so a cycle terminates.
function walk(view: FleetView, rootId: string, visit: (loc: FleetLocation, depth: number) => boolean): void {
  const index = locationIndex(view);
  const children = childrenIndex(view);
  const root = index.get(rootId);
  if (!root) return;
  const seen = new Set<string>();
  let frontier: Array<{ loc: FleetLocation; depth: number }> = [{ loc: root, depth: 0 }];
  let guard = guardOf(view);
  while (frontier.length > 0 && guard-- > 0) {
    const next: Array<{ loc: FleetLocation; depth: number }> = [];
    for (const { loc, depth } of frontier) {
      if (seen.has(loc.id)) continue;
      seen.add(loc.id);
      if (!visit(loc, depth)) continue;
      for (const child of children.get(loc.id) ?? []) next.push({ loc: child, depth: depth + 1 });
    }
    frontier = next;
  }
}

type TypeFact = { count: number; depth: number };

// The container types inside one root, with how many of each and how shallow
// the shallowest sits. The root's own type is excluded: cutting at it would
// produce one card that is the root wearing another name.
function containersIn(view: FleetView, rootId: string): Map<string, TypeFact> {
  const index = locationIndex(view);
  const root = index.get(rootId);
  const facts = new Map<string, TypeFact>();
  if (!root) return facts;
  const leaves = leafTypes(view);
  walk(view, rootId, (loc, depth) => {
    if (loc.id !== root.id && loc.location_type !== root.location_type && !leaves.has(loc.location_type)) {
      const seen = facts.get(loc.location_type);
      if (seen) {
        seen.count++;
        if (depth < seen.depth) seen.depth = depth;
      } else {
        facts.set(loc.location_type, { count: 1, depth });
      }
    }
    return true;
  });
  return facts;
}

// cutTypeFor picks the shallowest container type this root has at least two
// of. Two is the threshold because one card is not a cut, it is the root
// again. With nothing qualifying, the root is its own card, which is the right
// answer for a small annex and for a building that holds rooms directly.
//
// Ordering is by depth WITHIN this root first, so the choice reflects this
// tree rather than the estate's deepest one; the global tier and then the name
// break ties, which keeps the result independent of the order rows arrive in.
export function cutTypeFor(view: FleetView, rootId: string): string {
  const index = locationIndex(view);
  const root = index.get(rootId);
  if (!root) return "";
  const tiers = typeTiers(view);
  const candidates = [...containersIn(view, rootId).entries()].filter(([, f]) => f.count >= 2);
  if (candidates.length === 0) return root.location_type;
  candidates.sort(
    ([aName, a], [bName, b]) =>
      a.depth - b.depth || (tiers.get(aName) ?? 0) - (tiers.get(bName) ?? 0) || aName.localeCompare(bName),
  );
  return candidates[0][0];
}

function systemsAt(view: FleetView, locationId: string): FleetSystem[] {
  return systemsByLocation(view).get(locationId) ?? [];
}

// Every system beneath a node, at any depth, including any attached to the
// node itself.
function systemsUnderNode(view: FleetView, nodeId: string): FleetSystem[] {
  const out: FleetSystem[] = [];
  walk(view, nodeId, (loc) => {
    out.push(...systemsAt(view, loc.id));
    return true;
  });
  return out;
}

// cutNodesFor returns the nodes that become cards: the shallowest nodes of the
// cut type, and only those with systems beneath them, since an empty card
// teaches nothing. Sorted by label so the render order is stable.
export function cutNodesFor(view: FleetView, rootId: string): FleetLocation[] {
  const cut = cutTypeFor(view, rootId);
  const out: FleetLocation[] = [];
  walk(view, rootId, (loc) => {
    if (loc.location_type === cut) {
      out.push(loc);
      return false; // this node is a card; its inside is the card's business
    }
    return true;
  });
  return out
    .filter((n) => systemsUnderNode(view, n.id).length > 0)
    .sort((a, b) => a.label.localeCompare(b.label) || a.id.localeCompare(b.id));
}

// aboveCutSystems returns the systems attached at or above the cut: a campus
// paging system belongs to no building and no room, and inventing a card for
// it would be a lie an operator acts on. The walk stops at the cut, so a
// system inside a card is never counted here, and each is returned once.
export function aboveCutSystems(view: FleetView, rootId: string): FleetSystem[] {
  const cut = cutTypeFor(view, rootId);
  const index = locationIndex(view);
  const root = index.get(rootId);
  if (!root) return [];
  // When the root is its own cut, the root IS the card and nothing sits above it.
  if (root.location_type === cut) return [];
  const out: FleetSystem[] = [];
  walk(view, rootId, (loc) => {
    if (loc.location_type === cut) return false;
    out.push(...systemsAt(view, loc.id));
    return true;
  });
  return out.sort((a, b) => a.label.localeCompare(b.label) || a.id.localeCompare(b.id));
}

// unplacedSystems is the bucket every walk from the roots must not silently
// drop: a system the caller may read whose LOCATION it may not.
//
// This is reachable rather than theoretical. FleetProjection scopes locations
// and systems through two separate scope sets, each running its own
// scopedTreeQuery, so a principal whose system:read scope is wider than its
// location:read scope receives systems whose location uuid is absent from the
// payload. A renderer that builds cards by walking down from the roots would
// show that system nowhere and count it in nothing, which reads to the
// operator as a system that does not exist.
//
// A system placed nowhere at all (location unset) lands here too, and for the
// same reason: it belongs to no card, so something has to name it.
export function unplacedSystems(view: FleetView): FleetSystem[] {
  const index = locationIndex(view);
  return (view.systems ?? [])
    .filter((s) => !s.location || !index.has(s.location))
    .sort((a, b) => a.label.localeCompare(b.label) || a.id.localeCompare(b.id));
}
