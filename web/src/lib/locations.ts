import { api } from "../api/client";

// The locations data layer: thin typed wrappers over the generated client, so
// pages stay declarative and the calls are unit-testable against a mocked
// client. Shapes follow the OpenAPI (see api/locations.go).
export type Location = {
  id: string;
  name: string;
  display_name?: string;
  location_type: string;
  parent?: string;
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
  name: string;
  location_type: string;
  display_name?: string;
  parent?: string;
};

export async function createLocation(body: CreateLocation): Promise<Location> {
  const { data, error } = await api.POST("/locations", { body });
  if (error) throw error;
  return data as Location;
}

export type UpdateLocation = {
  display_name?: string;
  location_type?: string;
  // Re-parents the location (a tree move) to this location name. Omit to leave
  // the parent unchanged; moving to root (no parent) is not supported this
  // slice (mirrors storage.LocationPatch.ParentName).
  parent?: string;
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

export type NameCheck = { valid: boolean; available: boolean; reason?: string };

export async function checkLocationName(name: string): Promise<NameCheck> {
  const { data, error } = await api.POST("/locations:checkName", { body: { name } });
  if (error) throw error;
  return data as NameCheck;
}

export async function deleteLocation(name: string): Promise<void> {
  const { error } = await api.DELETE("/locations/{name}", { params: { path: { name } } });
  if (error) throw error;
}
