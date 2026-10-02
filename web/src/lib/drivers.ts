import { api } from "../api/client";

// The drivers data layer: thin typed wrappers over the generated client for the
// driver registry (the driver picker on the product form). A driver is the
// implementation that gets/emits/sets a product's signals. It carries a uuid
// (the stable handle that survives a rename) and a kebab name (the handle an
// operator reads and types, renameable); official (seed-owned) rows are
// read-only past creation, refused server-side on update/delete.

// The declarative spec body a driver may carry (#813): data, not code, the
// menu one generic engine interprets. Shapes follow the wire
// (internal/driver/spec.go); render-only here, authored via the API.
export type DriverSpecInput = {
  name: string;
  kind: "string" | "number" | "secret";
  secret_type?: string;
  required?: boolean;
  default?: string;
};
export type DriverEmit = {
  name: string;
  extract: { oid?: string; regex?: string; jsonpath?: string; key?: string };
  transform?: { cast?: string; scale?: number; map?: Record<string, string> };
};
export type DriverSpec = {
  version: number;
  transport: string;
  inputs?: DriverSpecInput[];
  polls?: { name: string; schedule: { every: string }; request: Record<string, unknown>; emits: DriverEmit[] }[];
  listeners?: { name: string; arm?: string[]; match: { prefix?: string; regex?: string }; emits: DriverEmit[] }[];
  commands?: { command_type: string; request: Record<string, unknown> }[];
};

export type Driver = {
  id: string;
  name: string;
  label: string;
  official: boolean;
  version?: string;
  // The declarative body; absent on a stub that cannot be attached yet.
  spec?: DriverSpec;
};

export const DRIVERS_KEY = ["drivers"] as const;

export async function listDrivers(): Promise<Driver[]> {
  const { data, error } = await api.GET("/drivers");
  if (error) throw error;
  return (data?.drivers ?? []) as Driver[];
}

export type CreateDriver = {
  // The name. The uuid is the database's to mint.
  name: string;
  label: string;
  version?: string;
};

export async function createDriver(body: CreateDriver): Promise<Driver> {
  const { data, error } = await api.POST("/drivers", { body });
  if (error) throw error;
  return data as Driver;
}

export type UpdateDriver = {
  label?: string;
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
