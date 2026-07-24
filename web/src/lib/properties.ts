import { api } from "../api/client";

// The Properties catalog data layer: thin typed wrappers over the /property-types
// surface. A property is a canonical, typed signal named by a key (a dot-hierarchied
// identifier) that a datapoint observes and a field declares. Official properties are
// seed-owned and read-only; custom properties are operator created. validation is a
// JSON Schema fragment.

export type PropertyDataType = "string" | "int" | "float" | "bool" | "json";

export const PROPERTY_DATA_TYPES: PropertyDataType[] = ["string", "int", "float", "bool", "json"];

// The observed kind of a property (omitted for a declared attribute property).
export type PropertyKind = "metric" | "state" | "log";

export type PropertyRow = {
  name: string;
  data_type: string;
  display_name?: string;
  description?: string;
  unit?: string;
  kind?: string;
  validation?: unknown;
  official: boolean;
};

export const PROPERTIES_KEY = ["properties"] as const;

export async function listProperties(): Promise<PropertyRow[]> {
  const { data, error } = await api.GET("/property-types");
  if (error) throw error;
  return (data?.properties ?? []) as PropertyRow[];
}

export type CreateProperty = {
  name: string;
  data_type: PropertyDataType;
  display_name?: string;
  description?: string;
  unit?: string;
  kind?: PropertyKind;
  validation?: unknown;
};

export async function createProperty(body: CreateProperty): Promise<void> {
  const { error } = await api.POST("/property-types", { body });
  if (error) throw error;
}

export type UpdateProperty = {
  display_name?: string;
  description?: string;
  unit?: string;
  validation?: unknown;
};

export async function updateProperty(name: string, body: UpdateProperty): Promise<void> {
  const { error } = await api.PATCH("/property-types/{name}", { params: { path: { name } }, body });
  if (error) throw error;
}

export async function deleteProperty(name: string): Promise<void> {
  const { error } = await api.DELETE("/property-types/{name}", { params: { path: { name } } });
  if (error) throw error;
}
