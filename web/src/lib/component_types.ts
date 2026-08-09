import { api } from "../api/client";

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
  official: boolean;
  // The parent's name (for display) and id (the canonical handle); both
  // absent for a root type.
  parent?: string;
  parent_id?: string;
  // The auto-generated component name's prefix; empty inherits.
  stem?: string;
  // A glyph key resolved by components/icons.tsx; empty inherits.
  icon?: string;
  // A compact form for the hostname render (fp, cam, dsp); empty inherits.
  abbrev?: string;
  // Tags every instance of this type (or a non-overriding descendant) starts with.
  default_tags: string[];
};

export const COMPONENT_TYPES_KEY = ["component_types"] as const;

export async function listComponentTypes(): Promise<ComponentType[]> {
  const { data, error } = await api.GET("/component-types");
  if (error) throw error;
  return (data?.component_types ?? []).map((t) => ({
    id: t.id,
    name: t.name,
    display_name: t.display_name,
    official: t.official,
    parent: t.parent,
    parent_id: t.parent_id,
    stem: t.stem,
    icon: t.icon,
    abbrev: t.abbrev,
    default_tags: t.default_tags ?? [],
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
    parent: t.parent,
    parent_id: t.parent_id,
    stem: t.stem,
    icon: t.icon,
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

// componentTypeByName indexes the flat registry by name, the join key every
// reference (product.component_type, a node's own parent) resolves through.
export function componentTypeByName(types: ComponentType[]): Map<string, ComponentType> {
  return new Map(types.map((t) => [t.name, t] as const));
}

// The same bound the server's ResolveTypeFacts walk uses
// (internal/storage/component_types.go): defends an inheritance walk against
// a cycle a client sent us, never expected against real data.
const MAX_DEPTH = 32;

// resolveComponentTypeIcon walks the type's ancestor chain for the first
// non-empty icon (first-non-null-wins, the same rule the server's fact
// resolver applies), falling back to "box" (generic-device's icon) only for
// a name the registry does not contain at all.
export function resolveComponentTypeIcon(typeName: string | undefined, byName: Map<string, ComponentType>): string {
  let cur = typeName ? byName.get(typeName) : undefined;
  for (let depth = 0; cur && depth < MAX_DEPTH; depth++) {
    if (cur.icon) return cur.icon;
    cur = cur.parent ? byName.get(cur.parent) : undefined;
  }
  return "box";
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
