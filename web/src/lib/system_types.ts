import { api } from "../api/client";
import { pickInheritedFacts, type InheritedFacts } from "./catalog";
import { byLabel } from "./entities";

// The system_type registry data layer: the coarse taxonomy of what kind of
// space a system is (ADR-0096), a boardroom, a classroom, a video wall. It is
// the system-side counterpart of component_type and it is NOT the standard: the
// type says what kind of space this is, the standard says which blueprint it is
// built to, and one fleet can hold ten signage standards under one coarse
// type. It nests by parent_id/parent name (av over room over board), and each
// identity fact (stem, abbrev, icon) inherits down the tree, overridable at any
// node: empty on a child means "inherit the nearest ancestor's".

export type SystemType = {
  id: string;
  name: string;
  label: string;
  official: boolean;
  // The parent's name (for display) and id (the canonical handle); both absent
  // for a root type.
  parent?: string;
  parent_id?: string;
  // The prefix a generated system name is built from; empty inherits.
  stem?: string;
  // A glyph key resolved by components/icons.tsx; empty inherits.
  icon?: string;
  // The glyph this type SHOWS, resolved by the server over the whole chain
  // (#695): this row's icon when it has one, else the nearest ancestor's.
  resolved_icon?: string;
  // The compact form a label render uses (br, cls, vw); empty inherits.
  abbrev?: string;
} & InheritedFacts;

export const SYSTEM_TYPES_KEY = ["system_types"] as const;

function toSystemType(t: {
  id: string; name: string; label: string; official: boolean;
  parent?: string; parent_id?: string; stem?: string; icon?: string; resolved_icon?: string; abbrev?: string;
} & InheritedFacts): SystemType {
  return {
    id: t.id,
    name: t.name,
    label: t.label,
    official: t.official,
    parent: t.parent,
    parent_id: t.parent_id,
    stem: t.stem,
    icon: t.icon,
    resolved_icon: t.resolved_icon,
    abbrev: t.abbrev,
    // Served on the LISTING only, which is also the blade's read
    // (useSystemTypeRow finds its row in this query rather than fetching one),
    // so the blade pays nothing for them (#716).
    ...pickInheritedFacts(t),
  };
}

export async function listSystemTypes(): Promise<SystemType[]> {
  const { data, error } = await api.GET("/system-types");
  if (error) throw error;
  return (data?.system_types ?? []).map(toSystemType);
}

export type CreateSystemType = {
  name: string;
  label: string;
  // The parent system_type, by name or uuid; omit for a root type (which then
  // needs a stem of its own: there is no ancestor to inherit one from).
  parent_id?: string;
  stem?: string;
  icon?: string;
  abbrev?: string;
};

export async function createSystemType(body: CreateSystemType): Promise<SystemType> {
  const { data, error } = await api.POST("/system-types", { body });
  if (error) throw error;
  return toSystemType(data!);
}

// No parent_id: the gateway has no reparent leg (a correct one needs a cycle
// guard analogous to location's), so a custom type's placement in the tree is
// fixed at create, exactly as component_type's is.
export type UpdateSystemType = {
  label?: string;
  stem?: string;
  icon?: string;
  abbrev?: string;
};

export async function updateSystemType(id: string, body: UpdateSystemType): Promise<void> {
  const { error } = await api.PATCH("/system-types/{id}", { params: { path: { id } }, body });
  if (error) throw error;
}

export async function deleteSystemType(id: string): Promise<void> {
  const { error } = await api.DELETE("/system-types/{id}", { params: { path: { id } } });
  if (error) throw error;
}

// systemTypeByName indexes the flat registry by name, the join key every
// reference (system.system_type, a node's own parent) resolves through.
export function systemTypeByName(types: SystemType[]): Map<string, SystemType> {
  return new Map(types.map((t) => [t.name, t] as const));
}

// resolveSystemTypeIcon reads the icon the SERVER resolved for a named type
// (#695), falling back to "map-pin", which is what components/icons.tsx already
// resolves an unknown key to, for a name the registry does not contain or a
// chain that sets no icon anywhere.
//
// A mid-chain override still beats the root's; that rule now lives in exactly
// one place, the gateway, rather than once here and once there.
export function resolveSystemTypeIcon(typeName: string | undefined, byName: Map<string, SystemType>): string {
  const t = typeName ? byName.get(typeName) : undefined;
  return t?.resolved_icon || "map-pin";
}

export type SystemTypeNode = SystemType & { children: SystemTypeNode[]; depth: number };

// systemTypeTree groups the flat registry into a forest by parent_id, each
// level sorted by label, for the admin page's rows and the
// classification picker's indented option list.
export function systemTypeTree(types: SystemType[]): SystemTypeNode[] {
  const byId = new Map<string, SystemTypeNode>(types.map((t) => [t.id, { ...t, children: [], depth: 0 }] as const));
  const roots: SystemTypeNode[] = [];
  for (const t of types) {
    const node = byId.get(t.id)!;
    const parent = t.parent_id ? byId.get(t.parent_id) : undefined;
    if (parent) parent.children.push(node);
    else roots.push(node);
  }
  const sortLevel = (nodes: SystemTypeNode[], depth: number) => {
    nodes.sort(byLabel);
    for (const n of nodes) {
      n.depth = depth;
      sortLevel(n.children, depth + 1);
    }
  };
  sortLevel(roots, 0);
  return roots;
}

// flattenSystemTypeTree walks the tree depth-first (a parent immediately
// followed by its children) into the flat, indent-ordered row list the admin
// page and the classification picker both render from.
export function flattenSystemTypeTree(roots: SystemTypeNode[]): SystemTypeNode[] {
  const out: SystemTypeNode[] = [];
  const walk = (nodes: SystemTypeNode[]) => {
    for (const n of nodes) {
      out.push(n);
      walk(n.children);
    }
  };
  walk(roots);
  return out;
}
