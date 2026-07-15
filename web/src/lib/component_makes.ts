import { api } from "../api/client";

// The component-makes data layer: thin typed wrappers over the generated
// client for the manufacturer registry (the make picker on the
// component_model form). A make is addressed by its id (a kebab id, the
// create-only address); official (seed-owned) rows are read-only past
// creation, refused server-side on update/delete.

export type ComponentMake = {
  id: string;
  display_name: string;
  official: boolean;
  icon?: string;
  support_phone?: string;
  website?: string;
};

export const COMPONENT_MAKES_KEY = ["component-makes"] as const;

export async function listMakes(): Promise<ComponentMake[]> {
  const { data, error } = await api.GET("/component-makes");
  if (error) throw error;
  return (data?.makes ?? []) as ComponentMake[];
}

export type CreateMake = {
  id: string;
  display_name: string;
  icon?: string;
  support_phone?: string;
  website?: string;
};

export async function createMake(body: CreateMake): Promise<ComponentMake> {
  const { data, error } = await api.POST("/component-makes", { body });
  if (error) throw error;
  return data as ComponentMake;
}

export type UpdateMake = {
  display_name?: string;
  icon?: string;
  support_phone?: string;
  website?: string;
};

export async function updateMake(id: string, body: UpdateMake): Promise<ComponentMake> {
  const { data, error } = await api.PATCH("/component-makes/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data as ComponentMake;
}

export async function deleteMake(id: string): Promise<void> {
  const { error } = await api.DELETE("/component-makes/{id}", { params: { path: { id } } });
  if (error) throw error;
}
