import { api } from "../api/client";

// The capabilities data layer: thin typed wrappers over the generated client
// for the capability registry (the capability picker on the product form). A
// capability is addressed by its id (a kebab id, the create-only address);
// official (seed-owned) rows are read-only past creation, refused server-side
// on update/delete.

export type Capability = {
  id: string;
  name: string;
  display_name: string;
  official: boolean;
};

export const CAPABILITIES_KEY = ["capabilities"] as const;

export async function listCapabilities(): Promise<Capability[]> {
  const { data, error } = await api.GET("/capabilities");
  if (error) throw error;
  return (data?.capabilities ?? []) as Capability[];
}

export type CreateCapability = {
  // The kebab handle. The uuid is the database\'s to mint.
  name: string;
  display_name: string;
};

export async function createCapability(body: CreateCapability): Promise<Capability> {
  const { data, error } = await api.POST("/capabilities", { body });
  if (error) throw error;
  return data as Capability;
}

export type UpdateCapability = {
  display_name?: string;
};

export async function updateCapability(id: string, body: UpdateCapability): Promise<Capability> {
  const { data, error } = await api.PATCH("/capabilities/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data as Capability;
}

export async function deleteCapability(id: string): Promise<void> {
  const { error } = await api.DELETE("/capabilities/{id}", { params: { path: { id } } });
  if (error) throw error;
}
