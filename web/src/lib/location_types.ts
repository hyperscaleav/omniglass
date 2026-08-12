import { api } from "../api/client";

// The location_type registry data layer: typed wrappers over /location-types.
// One classifier, one module: the old lib/types.ts aggregated this registry with
// secret types behind a single query that threw if EITHER fetch failed, which
// made secret:read (off the viewer floor) a prerequisite for reading location
// types (#598). Each registry now stands alone, so neither page's failure can
// take down the other; secret types live in lib/secret_types.ts.
//
// A system's shape is NOT here: the old system_type COLUMN was promoted to the
// STANDARD, a first-class catalog entity with its own registry and a
// declared-property contract (lib/standards.ts). A component's shape is
// likewise the product it is an instance of. The system_type NAME now belongs
// to a different object, the coarse space taxonomy in lib/system_types.ts.

// ROOT_PLACEMENT is the reserved allowed_parent_types member meaning "may sit
// at the top, no parent" (mirrors storage.RootPlacement). CreateLocationType
// refuses this id, so a real type can never collide with it.
export const ROOT_PLACEMENT = "root";

// NameRule is a type's opt-in to NAMING the locations it classifies (#687,
// ADR-0102): a declaration rather than a template, so the console can read the
// shape a create would mint without re-implementing anything. Absent is the
// opt-out, and means an operator names every location of this type.
export type NameRule = {
  // The generated name's prefix. Empty is a POSITIONAL type, whose ordinal
  // genuinely is its name (a parking deck called "1"); no SHIPPED type is one
  // (ADR-0103, which made `floor` nominal).
  stem: string;
  // Suppress the ordinal on the first of this stem under one parent, so the
  // only wing there is "wing" and the second is "wing-2". Ignored when stem is
  // empty.
  bare_first?: boolean;
};

export type LocationType = {
  // The uuid, the stable handle that survives a rename; name is the kebab
  // handle the rest of the estate stores and compares (ADR-0062), so
  // allowed_parent_types members are names and joins go through name.
  id: string;
  name: string;
  display_name: string;
  official: boolean;
  // A glyph key (kebab, e.g. "building") resolved to an SVG for the tree's
  // leading icon; resolveIcon falls back to map-pin for an unknown key.
  icon: string;
  // The placement constraint: a set of location_type names and/or the reserved
  // ROOT_PLACEMENT sentinel this type may be placed under. Empty means
  // unconstrained. Drives the reparent picker's candidate filter.
  allowed_parent_types: string[];
  // How the platform names locations of this type, absent when an operator
  // names every one of them. The create form reads it to show what a nameless
  // create would be called (lib/namegen.ts).
  name_rule?: NameRule;
};

// The one cache key for the registry, shared by the LocationTypes page and the
// location form's type picker, so a CRUD invalidation here refetches both.
export const LOCATION_TYPES_KEY = ["location_types"] as const;

export async function listLocationTypes(): Promise<LocationType[]> {
  const { data, error } = await api.GET("/location-types");
  if (error) throw error;
  return (data?.location_types ?? []).map((t) => ({
    id: t.id,
    name: t.name,
    display_name: t.display_name,
    official: t.official,
    icon: t.icon,
    allowed_parent_types: t.allowed_parent_types ?? [],
    name_rule: t.name_rule,
  }));
}

export type CreateLocationType = {
  // The name. The uuid is the database's to mint.
  name: string;
  display_name: string;
  icon?: string;
  allowed_parent_types?: string[];
};

export async function createLocationType(body: CreateLocationType): Promise<LocationType> {
  const { data, error } = await api.POST("/location-types", { body });
  if (error) throw error;
  const t = data!;
  return { id: t.id, name: t.name, display_name: t.display_name, official: t.official, icon: t.icon, allowed_parent_types: t.allowed_parent_types ?? [] };
}

export type UpdateLocationType = {
  display_name?: string;
  icon?: string;
  allowed_parent_types?: string[];
};

export async function updateLocationType(id: string, body: UpdateLocationType): Promise<void> {
  const { error } = await api.PATCH("/location-types/{id}", { params: { path: { id } }, body });
  if (error) throw error;
}

export async function deleteLocationType(id: string): Promise<void> {
  const { error } = await api.DELETE("/location-types/{id}", { params: { path: { id } } });
  if (error) throw error;
}
