import { api } from "../api/client";

// The Command Types catalog data layer: thin typed wrappers over the /command-types
// surface, the "do" twin of the Properties and Event Types catalogs. A command type
// is the driver-owned catalog entry for what a component can be told. settle_window_seconds
// is the driver's actuation-timing fact; target_property_type names the property a
// settleable command sets (empty for fire-and-forget). Official types are read-only.

export type CommandTypeRow = {
  name: string;
  display_name?: string;
  description?: string;
  params_schema?: unknown;
  settle_window_seconds: number;
  target_property_type?: string;
  official: boolean;
};

export const COMMAND_TYPES_KEY = ["command_types"] as const;

export async function listCommandTypes(): Promise<CommandTypeRow[]> {
  const { data, error } = await api.GET("/command-types");
  if (error) throw error;
  return (data?.command_types ?? []) as CommandTypeRow[];
}

export type CreateCommandType = {
  name: string;
  display_name?: string;
  description?: string;
  settle_window_seconds?: number;
  target_property_type?: string;
};

export async function createCommandType(body: CreateCommandType): Promise<CommandTypeRow> {
  const { data, error } = await api.POST("/command-types", { body });
  if (error) throw error;
  return data as CommandTypeRow;
}

export type UpdateCommandType = {
  display_name?: string;
  description?: string;
  settle_window_seconds?: number;
  target_property_type?: string;
};

export async function updateCommandType(name: string, body: UpdateCommandType): Promise<void> {
  const { error } = await api.PATCH("/command-types/{name}", { params: { path: { name } }, body });
  if (error) throw error;
}

export async function deleteCommandType(name: string): Promise<void> {
  const { error } = await api.DELETE("/command-types/{name}", { params: { path: { name } } });
  if (error) throw error;
}
