import { api } from "../api/client";

// The drivers data layer: thin typed wrappers over the generated client for the
// driver registry (the driver picker on the product form). A driver is the
// implementation that gets/emits/sets a product's signals, addressed by its id
// (a kebab id, the create-only address); official (seed-owned) rows are
// read-only past creation, refused server-side on update/delete.

export type Driver = {
  id: string;
  display_name: string;
  official: boolean;
  version?: string;
};

export const DRIVERS_KEY = ["drivers"] as const;

export async function listDrivers(): Promise<Driver[]> {
  const { data, error } = await api.GET("/drivers");
  if (error) throw error;
  return (data?.drivers ?? []) as Driver[];
}

export type CreateDriver = {
  id: string;
  display_name: string;
  version?: string;
};

export async function createDriver(body: CreateDriver): Promise<Driver> {
  const { data, error } = await api.POST("/drivers", { body });
  if (error) throw error;
  return data as Driver;
}

export type UpdateDriver = {
  display_name?: string;
  version?: string;
};

export async function updateDriver(id: string, body: UpdateDriver): Promise<Driver> {
  const { data, error } = await api.PATCH("/drivers/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data as Driver;
}

export async function deleteDriver(id: string): Promise<void> {
  const { error } = await api.DELETE("/drivers/{id}", { params: { path: { id } } });
  if (error) throw error;
}
