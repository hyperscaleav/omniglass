import { api } from "../api/client";

// The Properties catalog data layer: thin typed wrappers over the /property-types
// surface. A property is a canonical, typed, non-numeric signal named by a key
// that a sample observes and a field declares; numeric signals live on the metric
// lane (/metric-types, a later console slice). Official properties are seed-owned
// and read-only; custom properties are operator created. validation is a JSON
// Schema fragment.

export type PropertyDataType = "string" | "bool" | "json";

export const PROPERTY_DATA_TYPES: PropertyDataType[] = ["string", "bool", "json"];

export type PropertyRow = {
  name: string;
  data_type: string;
  display_name?: string;
  description?: string;
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
  validation?: unknown;
};

export async function createProperty(body: CreateProperty): Promise<PropertyRow> {
  const { data, error } = await api.POST("/property-types", { body });
  if (error) throw error;
  return data as PropertyRow;
}

export type UpdateProperty = {
  display_name?: string;
  description?: string;
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
