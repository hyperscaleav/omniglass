import { api } from "../api/client";

// The locations data layer: thin typed wrappers over the generated client, so
// pages stay declarative and the calls are unit-testable against a mocked
// client. Shapes follow the OpenAPI (see api/locations.go).
export type Location = {
  id: string;
  name: string;
  display_name?: string;
  // The LABEL's pen (#682/#683): true means the platform rendered this display
  // name from a label rule, false means an operator typed it. Read-only; the
  // console reads it through lib/entities so nothing branches on it by hand.
  display_name_generated?: boolean;
  location_type: string;
  parent?: string;
  // parent_id is the parent's uuid, the stable handle the tree builder keys
  // and resolves on (#627: name uniqueness is scoped to placement, so two
  // locations can now legally share a name under different parents and only
  // the uuid tells them apart).
  parent_id?: string;
  // path/path_segments/renders (#627 Task 15): the dotted address (no
  // accessor; a location's own address IS its location-tree ancestor chain)
  // and its two display-only compact forms, set on a GET or LIST response
  // (empty on a create/update/move/rename response; a subsequent list
  // refetch fills it).
  path?: string;
  path_segments?: string[];
  renders?: { dash: string; bare: string };
  actions?: string[];
  effective_tags?: Record<string, string>;
};

export const LOCATIONS_KEY = ["locations"] as const;

// The location_type registry (the type picker's rows) lives in
// lib/location_types.ts, its own module beside its page, so the registry and the
// entity never share a fetch or a cache key (#598).

export async function listLocations(): Promise<Location[]> {
  const { data, error } = await api.GET("/locations");
  if (error) throw error;
  return (data?.locations ?? []) as Location[];
}

export async function getLocation(name: string): Promise<Location> {
  const { data, error } = await api.GET("/locations/{name}", { params: { path: { name } } });
  if (error) throw error;
  return data as Location;
}

export type CreateLocation = {
  // Optional (#687, #688): omit it and the platform mints one from the
  // location_type's name rule, and marks name_generated. A type with no rule is
  // the opt-out, and a nameless create against one is a 422 naming the missing
  // fact rather than an invented stem.
  name?: string;
  location_type: string;
  display_name?: string;
  parent?: string;
  // The create form's ordinal precondition (#702): the ordinal the form
  // previewed and locked its name field on. Omitted unless the platform is
  // naming the row, and never a name: posting a name would claim the pen. A
  // create that would land a different number is refused with a 409 the form
  // recovers from by re-reading the draft.
  expected_ordinal?: number;
};

export async function createLocation(body: CreateLocation): Promise<Location> {
  const { data, error } = await api.POST("/locations", { body });
  if (error) throw error;
  return data as Location;
}

export type UpdateLocation = {
  display_name?: string;
  location_type?: string;
};

export async function updateLocation(name: string, body: UpdateLocation): Promise<Location> {
  const { data, error } = await api.PATCH("/locations/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return data as Location;
}

// A rename is its own call, not a field on the patch body. The API gates it with
// `<resource>:rename` rather than `<resource>:update`, because moving a name breaks
// stored references and is worth being able to withhold on its own.
export async function renameLocation(name: string, to: string): Promise<Location> {
  const { data, error } = await api.POST("/locations/{name}:rename", { params: { path: { name } }, body: { name: to } });
  if (error) throw error;
  return data as Location;
}

// A move (re-parent) is its own call too, not a field on the patch body (#627):
// the API gates it with `location:move` rather than `location:update`, because a
// placement change is an authorization act. Moving to root is not supported: the
// server 422s an omitted or empty parent, so parent is required here, not
// optional the way the retired UpdateLocation.parent used to be.
export async function moveLocation(name: string, parent: string): Promise<Location> {
  const { data, error } = await api.POST("/locations/{name}:move", { params: { path: { name } }, body: { parent } });
  if (error) throw error;
  return data as Location;
}

export type NameCheck = { valid: boolean; available: boolean; reason?: string };

// checkLocationName checks availability against a placement bucket (#627:
// name uniqueness is scoped to placement, not the whole estate): under the
// given parent, or among roots when parent is omitted. A rename check
// passes the location's OWN current parent, since a rename does not move it.
export async function checkLocationName(name: string, parent?: string): Promise<NameCheck> {
  const { data, error } = await api.POST("/locations:checkName", { body: { name, parent } });
  if (error) throw error;
  return data as NameCheck;
}

export async function deleteLocation(name: string): Promise<void> {
  const { error } = await api.DELETE("/locations/{name}", { params: { path: { name } } });
  if (error) throw error;
}
