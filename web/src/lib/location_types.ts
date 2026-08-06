import { api } from "../api/client";

// The location_type registry data layer: typed wrappers over /location-types.
// One classifier, one module: the old lib/types.ts aggregated this registry with
// secret types behind a single query that threw if EITHER fetch failed, which
// made secret:read (off the viewer floor) a prerequisite for reading location
// types (#598). Each registry now stands alone, so neither page's failure can
// take down the other; secret types live in lib/secret_types.ts.
//
// A system's shape is NOT here: system_type was promoted to the STANDARD, a
// first-class catalog entity with its own registry and a declared-property
// contract (lib/standards.ts). A component's shape is likewise the product it
// is an instance of.

// ROOT_PLACEMENT is the reserved allowed_parent_types member meaning "may sit
// at the top, no parent" (mirrors storage.RootPlacement). CreateLocationType
// refuses this id, so a real type can never collide with it.
export const ROOT_PLACEMENT = "root";

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
