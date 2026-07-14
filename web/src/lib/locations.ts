import { api } from "../api/client";

// The locations data layer: thin typed wrappers over the generated client, so
// pages stay declarative and the calls are unit-testable against a mocked
// client. Shapes follow the OpenAPI (see api/locations.go).
export type Location = {
  id: string;
  name: string;
  display_name?: string;
  location_type: string;
  parent_id?: string;
  actions?: string[];
  effective_tags?: Record<string, string>;
};

export const LOCATIONS_KEY = ["locations"] as const;

// A location_type registry row: the type picker on the location form lists these
// (value = id, label = display_name), so a location is classified by a known type
// rather than a free-typed string.
export type LocationType = {
  id: string;
  display_name: string;
  // A glyph key (kebab, e.g. "building") resolved to an SVG for the tree's leading
  // icon; resolveIcon falls back to map-pin for an unknown key.
  icon: string;
  official: boolean;
};

export const LOCATION_TYPES_KEY = ["types", "location"] as const;

export async function listLocations(): Promise<Location[]> {
  const { data, error } = await api.GET("/locations");
  if (error) throw error;
  return (data?.locations ?? []) as Location[];
}

export async function listLocationTypes(): Promise<LocationType[]> {
  const { data, error } = await api.GET("/types/location");
  if (error) throw error;
  return (data?.location_types ?? []) as LocationType[];
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
  name?: string;
  display_name?: string;
  location_type?: string;
};

export async function updateLocation(name: string, body: UpdateLocation): Promise<Location> {
  const { data, error } = await api.PATCH("/locations/{name}", { params: { path: { name } }, body });
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
