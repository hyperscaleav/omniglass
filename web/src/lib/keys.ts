import { api } from "../api/client";

// The Keys catalog data layer: thin typed wrappers over the /keys surface. A key
// is a canonical, typed name (the keyspace) that a datapoint observes and a field
// declares. Official keys are seed-owned and read-only; custom keys are operator
// created. validation is a JSON Schema fragment.

export type KeyDataType = "string" | "int" | "float" | "bool" | "json";

export const KEY_DATA_TYPES: KeyDataType[] = ["string", "int", "float", "bool", "json"];

// The observed kind of a key (omitted for a declared attribute key).
export type KeyKind = "metric" | "state" | "log";

export type KeyRow = {
  name: string;
  data_type: string;
  display_name?: string;
  description?: string;
  unit?: string;
  kind?: string;
  validation?: unknown;
  official: boolean;
};

export const KEYS_KEY = ["keys"] as const;

export async function listKeys(): Promise<KeyRow[]> {
  const { data, error } = await api.GET("/keys");
  if (error) throw error;
  return (data?.keys ?? []) as KeyRow[];
}

export type CreateKey = {
  name: string;
  data_type: KeyDataType;
  display_name?: string;
  description?: string;
  unit?: string;
  kind?: KeyKind;
  validation?: unknown;
};

export async function createKey(body: CreateKey): Promise<void> {
  const { error } = await api.POST("/keys", { body });
  if (error) throw error;
}

export type UpdateKey = {
  display_name?: string;
  description?: string;
  unit?: string;
  validation?: unknown;
};

export async function updateKey(name: string, body: UpdateKey): Promise<void> {
  const { error } = await api.PATCH("/keys/{name}", { params: { path: { name } }, body });
  if (error) throw error;
}

export async function deleteKey(name: string): Promise<void> {
  const { error } = await api.DELETE("/keys/{name}", { params: { path: { name } } });
  if (error) throw error;
}
