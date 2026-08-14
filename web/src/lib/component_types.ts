import { api } from "../api/client";
import { pickInheritedFacts, type InheritedFacts } from "./catalog";

// The component_type registry data layer: the device-class genus a product is
// classified under (ADR-0085 partially reverses ADR-0047: the shape returns
// above product, not beside component). It nests by parent_id/parent name
// (mic over wireless-mic, ceiling-mic, boundary-mic), and each identity fact
// (stem, icon, abbrev, default_tags) inherits down the tree, overridable at
// any node: empty on a child means "inherit the nearest ancestor's".

export type ComponentType = {
  id: string;
  name: string;
  display_name: string;
  // True for a row this release ships. A shipped row is never written by an
  // operator: an edit forks it (#655, ADR-0095), and `forked` says whether this
  // one carries the operator's version over the shipped values.
  official: boolean;
  forked: boolean;
  // The parent's name (for display) and id (the canonical handle); both
  // absent for a root type.
  parent?: string;
  parent_id?: string;
  // The auto-generated component name's prefix; empty inherits.
  stem?: string;
  // A glyph key resolved by components/icons.tsx; empty inherits.
  icon?: string;
  // The glyph this type SHOWS, resolved by the server over the whole chain
  // (#695): this row's icon when it has one, else the nearest ancestor's. The
  // console no longer climbs the chain itself, so it cannot disagree with the
  // gateway about which glyph a type draws. Absent on a single-row write
  // response, which is why every reader of it comes off the listing.
  resolved_icon?: string;
  // A compact form for the hostname render (fp, cam, dsp); empty inherits.
  abbrev?: string;
  // Tags every instance of this type (or a non-overriding descendant) starts with.
  default_tags: string[];
} & InheritedFacts;

export const COMPONENT_TYPES_KEY = ["component_types"] as const;

export async function listComponentTypes(): Promise<ComponentType[]> {
  const { data, error } = await api.GET("/component-types");
  if (error) throw error;
  return (data?.component_types ?? []).map((t) => ({
    id: t.id,
    name: t.name,
    display_name: t.display_name,
    official: t.official,
    forked: t.forked,
    parent: t.parent,
    parent_id: t.parent_id,
    stem: t.stem,
    icon: t.icon,
    resolved_icon: t.resolved_icon,
    abbrev: t.abbrev,
    default_tags: t.default_tags ?? [],
    // The inherited pairs ride only on the LISTING, which is also the blade's
    // read: useComponentTypeRow finds its row in this same query rather than
    // fetching one, so the blade pays nothing for them (#716).
    ...pickInheritedFacts(t),
  }));
}

export type CreateComponentType = {
  name: string;
  display_name: string;
  // The parent component_type, by name or uuid; omit for a root type.
  parent_id?: string;
  stem?: string;
  icon?: string;
  abbrev?: string;
  default_tags?: string[];
};

export async function createComponentType(body: CreateComponentType): Promise<ComponentType> {
  const { data, error } = await api.POST("/component-types", { body });
  if (error) throw error;
  const t = data!;
  return {
    id: t.id,
    name: t.name,
    display_name: t.display_name,
    official: t.official,
    forked: t.forked,
    parent: t.parent,
    parent_id: t.parent_id,
    stem: t.stem,
    icon: t.icon,
    resolved_icon: t.resolved_icon,
    abbrev: t.abbrev,
    default_tags: t.default_tags ?? [],
  };
}

// No parent_id: the gateway has no reparent leg yet (a correct one needs a
// cycle guard analogous to location's), so a custom type's placement in the
// tree is fixed at create.
export type UpdateComponentType = {
  display_name?: string;
  stem?: string;
  icon?: string;
  abbrev?: string;
  default_tags?: string[];
};

export async function updateComponentType(id: string, body: UpdateComponentType): Promise<void> {
  const { error } = await api.PATCH("/component-types/{id}", { params: { path: { id } }, body });
  if (error) throw error;
}

export async function deleteComponentType(id: string): Promise<void> {
  const { error } = await api.DELETE("/component-types/{id}", { params: { path: { id } } });
  if (error) throw error;
}

// restoreComponentType discards the operator's fork of a shipped row, so reads
// return the values this release ships and later releases can improve them
// again. It is the only removal a shipped row admits: it removes the fork, not
// the row.
export async function restoreComponentType(id: string): Promise<void> {
  const { error } = await api.POST("/component-types/{id}:restore", { params: { path: { id } } });
  if (error) throw error;
}

// componentTypeByName indexes the flat registry by name, the join key every
// reference (product.component_type, a node's own parent) resolves through.
export function componentTypeByName(types: ComponentType[]): Map<string, ComponentType> {
  return new Map(types.map((t) => [t.name, t] as const));
}

// resolveComponentTypeIcon reads the icon the SERVER resolved for a named type,
// falling back to "box" (generic-device's icon) for a name the registry does not
// contain, or a chain that sets no icon anywhere.
//
// It no longer walks (#695). The climb was a second implementation of
// resolveTypeFacts written in TypeScript, and the failure it could produce was
// a wrong glyph on a type whose chain the two disagreed about. The fallback
// stays here because it is a rendering choice (the system registry falls back
// to a different glyph) rather than a fact about the registry.
export function resolveComponentTypeIcon(typeName: string | undefined, byName: Map<string, ComponentType>): string {
  const t = typeName ? byName.get(typeName) : undefined;
  return t?.resolved_icon || "box";
}

export type ComponentTypeNode = ComponentType & { children: ComponentTypeNode[]; depth: number };

// componentTypeTree groups the flat registry into a forest by parent_id, each
// level sorted by display name, for the admin page's rows and the
// classification picker's indented option list.
export function componentTypeTree(types: ComponentType[]): ComponentTypeNode[] {
  const byId = new Map<string, ComponentTypeNode>(types.map((t) => [t.id, { ...t, children: [], depth: 0 }] as const));
  const roots: ComponentTypeNode[] = [];
  for (const t of types) {
    const node = byId.get(t.id)!;
    const parent = t.parent_id ? byId.get(t.parent_id) : undefined;
    if (parent) parent.children.push(node);
    else roots.push(node);
  }
  const sortLevel = (nodes: ComponentTypeNode[], depth: number) => {
    nodes.sort((a, b) => a.display_name.localeCompare(b.display_name));
    for (const n of nodes) {
      n.depth = depth;
      sortLevel(n.children, depth + 1);
    }
  };
  sortLevel(roots, 0);
  return roots;
}

// flattenComponentTypeTree walks the tree depth-first (a parent immediately
// followed by its children) into the flat, indent-ordered row list the admin
// page and the classification picker both render from.
export function flattenComponentTypeTree(roots: ComponentTypeNode[]): ComponentTypeNode[] {
  const out: ComponentTypeNode[] = [];
  const walk = (nodes: ComponentTypeNode[]) => {
    for (const n of nodes) {
      out.push(n);
      walk(n.children);
    }
  };
  walk(roots);
  return out;
}
