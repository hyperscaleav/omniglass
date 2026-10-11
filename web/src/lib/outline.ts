import { type FleetView } from "./fleet";
import { verdictOf, type Verdict } from "./health";
import { entityLabel } from "./entities";

// The outline's model (#866): every place in the fleet as one tree, each
// place wearing the system that sits there.
//
// Operators think of a room AS its system: the location is the place in the
// tree, and the system is what the place is (its standard, its health), not a
// second node beneath it. So the tree holds places only. A place holding one
// system wears it on its own row and lists that system's components directly
// beneath; a place holding several groups its components under one header per
// system. Location types are customer data (ADR-0102), so nothing here reads a
// type's name: the tree comes from parent links, counts come from the
// registry's own labels, and folding comes from the tree's shape.
//
// Pure, so everything an operator could dispute (where a component lands, what
// a count says, what folds) is unit-tested without a DOM.

export type Lights = { healthy: number; incomplete: number; degraded: number; outage: number };

export type SystemRef = { id: string; name: string; label: string; standard: string; health: Verdict | null };

export type ComponentRow = {
  id: string;
  name: string;
  label: string;
  product: string;
  // The component type's icon key (resolved by components/icons.tsx).
  icon: string;
  health: Verdict | null;
  // The other systems a shared component serves, by label.
  also: string[];
};

export type ComponentGroup = { system: SystemRef | null; components: ComponentRow[] };

export type PlaceNode = {
  // The deepest place in a folded chain: what selection, expansion and the
  // side panel act on.
  id: string;
  // The places this row stands for, top first: one, or a folded chain.
  chain: { id: string; label: string; typeName: string }[];
  type: string;
  typeName: string;
  icon: string;
  systems: SystemRef[];
  groups: ComponentGroup[];
  children: PlaceNode[];
  contents: string;
  lights: Lights;
  // The one system's health when the place holds exactly one; otherwise the
  // row's lights carry it.
  health: Verdict | null;
};

export type Outline = {
  roots: PlaceNode[];
  unplaced: PlaceNode | null;
  systemCount: number;
  tags: Map<string, Record<string, string>>;
};

type TypeInfo = { name: string; label: string; icon: string };

export type OutlineInput = {
  view: FleetView;
  components: Array<{ id: string; name: string; label?: string; product?: string; system_id?: string; system_count?: number; location_id?: string }>;
  systems: Array<{ id: string; standard?: string }>;
  types: TypeInfo[];
  standardLabel: (handle: string) => string;
  productLabel: (handle: string) => string;
  // Effective tags by place, system or component id, for the tag filter.
  tags?: Map<string, Record<string, string>>;
  // A component's icon key from its product handle (product to component
  // type to that type's resolved icon).
  componentIcon?: (product: string) => string;
};

const emptyLights = (): Lights => ({ healthy: 0, incomplete: 0, degraded: 0, outage: 0 });

function addLights(into: Lights, from: Lights): void {
  into.healthy += from.healthy;
  into.incomplete += from.incomplete;
  into.degraded += from.degraded;
  into.outage += from.outage;
}

const byLabel = <T extends { label: string; id: string }>(a: T, b: T) => a.label.localeCompare(b.label) || a.id.localeCompare(b.id);

// A type label in a count reads as the word does in a sentence ("3 rooms"),
// except an acronym, which keeps its capitals ("2 HVAC Zones").
export function countWord(label: string, n: number): string {
  const word = /^[A-Z][a-z]/.test(label) ? label[0].toLowerCase() + label.slice(1) : label;
  if (n === 1) return `${n} ${word}`;
  if (/(s|x|z|ch|sh)$/i.test(word)) return `${n} ${word}es`;
  if (/[^aeiou]y$/i.test(word)) return `${n} ${word.slice(0, -1)}ies`;
  return `${n} ${word}s`;
}

export function buildOutline(input: OutlineInput): Outline {
  const locations = input.view.locations ?? [];
  const fleetSystems = input.view.systems ?? [];
  const typeOf = new Map(input.types.map((t) => [t.name, t] as const));
  const standardOf = new Map(input.systems.map((s) => [s.id, s.standard ?? ""] as const));
  const componentOf = new Map(input.components.map((c) => [c.id, c] as const));
  const locationIds = new Set(locations.map((l) => l.id));
  const iconFor = (product: string | undefined) => (product && input.componentIcon ? input.componentIcon(product) : "box");

  const systemRef = (s: (typeof fleetSystems)[number]): SystemRef => {
    const std = standardOf.get(s.id);
    return { id: s.id, name: s.name, label: entityLabel(s), standard: std ? input.standardLabel(std) : "", health: verdictOf(s.verdict) };
  };

  // Which systems each component serves, so a shared one can name the others.
  const servedBy = new Map<string, string[]>();
  for (const s of fleetSystems) for (const d of s.dots ?? []) {
    const list = servedBy.get(d.component) ?? [];
    list.push(entityLabel(s));
    servedBy.set(d.component, list);
  }

  const componentRows = (s: (typeof fleetSystems)[number]): ComponentRow[] =>
    (s.dots ?? [])
      .map((d) => {
        const c = componentOf.get(d.component);
        const label = c ? entityLabel({ name: c.name, label: c.label }) : d.name;
        return {
          id: d.component,
          name: c?.name ?? d.name,
          label,
          product: c?.product ? input.productLabel(c.product) : "",
          icon: iconFor(c?.product),
          health: verdictOf(d.verdict),
          also: (servedBy.get(d.component) ?? []).filter((l) => l !== entityLabel(s)),
        };
      })
      .sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id));

  const systemsAt = new Map<string, (typeof fleetSystems)[number][]>();
  const unplacedSystems: (typeof fleetSystems)[number][] = [];
  for (const s of fleetSystems) {
    if (s.location && locationIds.has(s.location)) {
      const list = systemsAt.get(s.location) ?? [];
      list.push(s);
      systemsAt.set(s.location, list);
    } else {
      unplacedSystems.push(s);
    }
  }

  // Components in no system, under the place they sit at; one placed nowhere
  // the caller can read joins the node of things placed out of sight, so a
  // component created a moment ago without a placement is never invisible.
  const looseAt = new Map<string, ComponentRow[]>();
  const looseNowhere: ComponentRow[] = [];
  for (const c of input.components) {
    if ((c.system_count ?? 0) > 0 || c.system_id) continue;
    const row: ComponentRow = { id: c.id, name: c.name, label: entityLabel({ name: c.name, label: c.label }), product: c.product ? input.productLabel(c.product) : "", icon: iconFor(c.product), health: null, also: [] };
    if (!c.location_id || !locationIds.has(c.location_id)) { looseNowhere.push(row); continue; }
    const list = looseAt.get(c.location_id) ?? [];
    list.push(row);
    looseAt.set(c.location_id, list);
  }

  const childrenOf = new Map<string, typeof locations>();
  for (const l of locations) {
    if (!l.parent || !locationIds.has(l.parent)) continue;
    const list = childrenOf.get(l.parent) ?? [];
    list.push(l);
    childrenOf.set(l.parent, list);
  }

  const groupsFor = (systems: (typeof fleetSystems)[number][], loose: ComponentRow[]): ComponentGroup[] => {
    const groups: ComponentGroup[] = systems
      .map((s) => ({ system: systemRef(s), components: componentRows(s) }))
      .sort((a, b) => byLabel(a.system!, b.system!));
    if (loose.length > 0) groups.push({ system: null, components: [...loose].sort((a, b) => a.name.localeCompare(b.name)) });
    return groups;
  };

  const lightsOf = (systems: SystemRef[]): Lights => {
    const l = emptyLights();
    for (const s of systems) if (s.health) l[s.health]++;
    return l;
  };

  const visiting = new Set<string>();
  const build = (loc: (typeof locations)[number], chain: PlaceNode["chain"]): PlaceNode | null => {
    if (visiting.has(loc.id)) return null;
    visiting.add(loc.id);
    const here = (systemsAt.get(loc.id) ?? []).slice().sort((a, b) => entityLabel(a).localeCompare(entityLabel(b)) || a.id.localeCompare(b.id));
    const loose = looseAt.get(loc.id) ?? [];
    const kids = (childrenOf.get(loc.id) ?? []).filter((k) => !visiting.has(k.id));
    const link = [...chain, { id: loc.id, label: entityLabel(loc), typeName: loc.location_type }];

    // Fold: a place holding nothing of its own whose only child is another
    // place becomes one row with that child.
    if (here.length === 0 && loose.length === 0 && kids.length === 1) {
      const folded = build(kids[0], link);
      if (folded) {
        visiting.delete(loc.id);
        return folded;
      }
    }

    const children = kids
      .map((k) => build(k, []))
      .filter((n): n is PlaceNode => n !== null)
      .sort((a, b) => a.chain[0].label.localeCompare(b.chain[0].label) || a.id.localeCompare(b.id));
    visiting.delete(loc.id);

    const systems = here.map(systemRef);
    const groups = groupsFor(here, loose);
    const lights = lightsOf(systems);
    for (const c of children) addLights(lights, c.lights);

    const componentCount = groups.reduce((n, g) => n + g.components.length, 0);
    const counts = new Map<string, number>();
    // Counted by the place each child row starts at: a folded row is a chain,
    // and its parent holds the first place in it.
    for (const c of children) counts.set(c.chain[0].typeName, (counts.get(c.chain[0].typeName) ?? 0) + 1);
    const parts = [...counts.entries()].map(([t, n]) => countWord(entityLabel(typeOf.get(t) ?? { name: t }), n));
    if (componentCount > 0) parts.push(countWord("component", componentCount));

    const info = typeOf.get(loc.location_type);
    return {
      id: loc.id,
      chain: link,
      type: entityLabel(info ?? { name: loc.location_type }),
      typeName: loc.location_type,
      icon: info?.icon ?? "",
      systems,
      groups,
      children,
      contents: parts.length > 0 ? parts.join(", ") : "Empty",
      lights,
      health: systems.length === 1 ? systems[0].health : null,
    };
  };

  const roots = locations
    .filter((l) => !l.parent || !locationIds.has(l.parent))
    .map((l) => build(l, []))
    .filter((n): n is PlaceNode => n !== null)
    .sort((a, b) => a.chain[0].label.localeCompare(b.chain[0].label) || a.id.localeCompare(b.id));

  let unplaced: PlaceNode | null = null;
  if (unplacedSystems.length > 0 || looseNowhere.length > 0) {
    const systems = unplacedSystems.map(systemRef).sort(byLabel);
    const groups = groupsFor(unplacedSystems, looseNowhere);
    const parts = [
      ...(systems.length > 0 ? [countWord("system", systems.length)] : []),
      ...(looseNowhere.length > 0 ? [countWord("component", looseNowhere.length)] : []),
    ];
    unplaced = {
      id: "unplaced",
      chain: [{ id: "unplaced", label: "Placed nowhere you can see", typeName: "" }],
      type: "",
      typeName: "",
      icon: "",
      systems,
      groups,
      children: [],
      contents: parts.join(", "),
      lights: lightsOf(systems),
      health: null,
    };
  }

  return { roots, unplaced, systemCount: fleetSystems.length, tags: input.tags ?? new Map() };
}

// One filterable row: a place or a component, with its path (the places above
// it, top first) and the facts the filter bar matches on.
export type OutlineEntry = {
  kind: "place" | "component";
  id: string;
  label: string;
  path: string[];
  type: string;
  standard: string;
  product: string;
  verdict: Verdict | null;
  tags: Record<string, string>;
  search: string;
};

const worst = (vs: (Verdict | null)[]): Verdict | null => {
  const rank: Record<Verdict, number> = { healthy: 0, incomplete: 1, degraded: 2, outage: 3 };
  let w: Verdict | null = null;
  for (const v of vs) if (v && (!w || rank[v] > rank[w])) w = v;
  return w;
};

export function entriesOf(outline: Outline, roots?: PlaceNode[]): OutlineEntry[] {
  const out: OutlineEntry[] = [];
  const visit = (n: PlaceNode, above: string[]) => {
    const own = n.chain[n.chain.length - 1].label;
    const path = [...above, ...n.chain.slice(0, -1).map((c) => c.label)];
    out.push({
      kind: "place",
      id: n.id,
      label: own,
      path,
      type: n.type,
      standard: n.systems.map((s) => s.standard).filter(Boolean).join(" "),
      product: "",
      // A place's own health is its systems'; a place holding none has none of
      // its own to match on.
      verdict: worst(n.systems.map((s) => s.health)),
      // A place's tags are its own, merged over the ones its systems carry.
      tags: Object.assign({}, ...n.systems.map((s) => outline.tags.get(s.id) ?? {}), outline.tags.get(n.id) ?? {}),
      search: [own, ...n.systems.flatMap((s) => [s.label, s.name, s.standard])].join(" "),
    });
    const below = [...path, own];
    for (const g of n.groups) for (const c of g.components) {
      out.push({
        kind: "component",
        id: c.id,
        label: c.label,
        path: below,
        type: "",
        standard: g.system?.standard ?? "",
        product: c.product,
        verdict: c.health,
        tags: outline.tags.get(c.id) ?? {},
        search: [c.label, c.name, c.product].join(" "),
      });
    }
    for (const k of n.children) visit(k, below);
  };
  if (roots) for (const r of roots) visit(r, []);
  else {
    for (const r of outline.roots) visit(r, []);
    if (outline.unplaced) visit(outline.unplaced, []);
  }
  return out;
}

// One place's own node, unfolded (#872): what a place's detail view lists
// beneath its card. A fold may have swallowed the place into a chain
// ("Headquarters / West"); rooted at Headquarters, the rest of the chain is
// its one child, and rooted at West the row is West alone. Null when the
// outline holds no such place.
export function rootAt(outline: Outline, id: string): PlaceNode | null {
  const find = (ns: PlaceNode[]): PlaceNode | null => {
    for (const n of ns) {
      const idx = n.chain.findIndex((c) => c.id === id);
      if (idx === n.chain.length - 1) return { ...n, chain: [n.chain[idx]] };
      if (idx >= 0) {
        const rest: PlaceNode = { ...n, chain: n.chain.slice(idx + 1) };
        const own = n.chain[idx];
        return { ...n, id: own.id, chain: [own], systems: [], groups: [], health: null, children: [rest], contents: n.contents, lights: n.lights };
      }
      const hit = find(n.children);
      if (hit) return hit;
    }
    return null;
  };
  return find(outline.roots);
}

// The rows to expand, top first, so the row with this id shows: a place's
// ancestors, or a component's place and that place's ancestors.
export function ancestorsOf(outline: Outline, id: string): string[] {
  const walk = (n: PlaceNode, above: string[]): string[] | null => {
    if (n.chain.some((c) => c.id === id)) return above;
    if (n.groups.some((g) => g.components.some((c) => c.id === id))) return [...above, n.id];
    // A system shows as its place's own row, unless the place heads a row per
    // system (it holds several, or it is the node of things out of sight).
    if (n.groups.some((g) => g.system?.id === id)) return n.id === "unplaced" || n.systems.length > 1 ? [...above, n.id] : above;
    for (const k of n.children) {
      const found = walk(k, [...above, n.id]);
      if (found) return found;
    }
    return null;
  };
  for (const r of [...outline.roots, ...(outline.unplaced ? [outline.unplaced] : [])]) {
    const found = walk(r, []);
    if (found) return found;
  }
  return [];
}
