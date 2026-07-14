import { api } from "../api/client";

// The systems data layer: thin typed wrappers over the generated client. Systems
// form a tree (parent_id) and are placed at a location. Components reference a
// primary system by id; the Components page consumes this list to resolve those
// ids to readable names and to populate the system picker.
export type System = {
  id: string;
  name: string;
  display_name?: string;
  system_type: string;
  location_id?: string;
  parent_id?: string;
  actions?: string[];
  effective_tags?: Record<string, string>;
};

export const SYSTEMS_KEY = ["systems"] as const;

export async function listSystems(): Promise<System[]> {
  const { data, error } = await api.GET("/systems");
  if (error) throw error;
  return (data?.systems ?? []) as System[];
}

export async function getSystem(name: string): Promise<System> {
  const { data, error } = await api.GET("/systems/{name}", { params: { path: { name } } });
  if (error) throw error;
  return data as System;
}

export type CreateSystem = {
  name: string;
  system_type: string;
  display_name?: string;
  parent?: string;
  location?: string;
};

export async function createSystem(body: CreateSystem): Promise<System> {
  const { data, error } = await api.POST("/systems", { body });
  if (error) throw error;
  return data as System;
}

export type UpdateSystem = {
  name?: string;
  display_name?: string;
  system_type?: string;
};

export async function updateSystem(name: string, body: UpdateSystem): Promise<System> {
  const { data, error } = await api.PATCH("/systems/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return data as System;
}

export type NameCheck = { valid: boolean; available: boolean; reason?: string };

export async function checkSystemName(name: string): Promise<NameCheck> {
  const { data, error } = await api.POST("/systems:checkName", { body: { name } });
  if (error) throw error;
  return data as NameCheck;
}

export async function deleteSystem(name: string): Promise<void> {
  const { error } = await api.DELETE("/systems/{name}", { params: { path: { name } } });
  if (error) throw error;
}
