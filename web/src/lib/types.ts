import { api } from "../api/client";
import type { SecretTypeField } from "./secrets";

// The Types catalog data layer: a unified aggregator over the four type
// registries (location, system, component, secret). Each registry is its own
// typed GET/POST/PATCH/DELETE surface under /types/{kind}; this module
// flattens them into one normalized row shape so a single catalog page can
// list, sort, and filter across kinds. secret_type is read-only this slice
// (no write routes exist yet), so the write functions refuse it.
export type TypeKind = "location" | "system" | "component" | "secret";

export const TYPE_KINDS: TypeKind[] = ["location", "system", "component", "secret"];

// Only these three are operator-writable; secret_type is read-only this slice.
export const WRITABLE_KINDS: TypeKind[] = ["location", "system", "component"];

// A normalized registry row across all four kinds. icon is present only on
// location; fields only on secret (read-only display).
export type TypeRow = {
  kind: TypeKind;
  id: string;
  display_name: string;
  official: boolean;
  icon?: string;
  fields?: SecretTypeField[];
};

export const TYPES_KEY = ["types"] as const;

export async function listTypes(): Promise<TypeRow[]> {
  const [locationRes, systemRes, componentRes, secretRes] = await Promise.all([
    api.GET("/types/location"),
    api.GET("/types/system"),
    api.GET("/types/component"),
    api.GET("/types/secret"),
  ]);

  if (locationRes.error) throw locationRes.error;
  if (systemRes.error) throw systemRes.error;
  if (componentRes.error) throw componentRes.error;
  if (secretRes.error) throw secretRes.error;

  const locationRows: TypeRow[] = (locationRes.data?.location_types ?? []).map((t) => ({
    kind: "location" as const,
    id: t.id,
    display_name: t.display_name,
    official: t.official,
    icon: t.icon,
  }));

  const systemRows: TypeRow[] = (systemRes.data?.system_types ?? []).map((t) => ({
    kind: "system" as const,
    id: t.id,
    display_name: t.display_name,
    official: t.official,
  }));

  const componentRows: TypeRow[] = (componentRes.data?.component_types ?? []).map((t) => ({
    kind: "component" as const,
    id: t.id,
    display_name: t.display_name,
    official: t.official,
  }));

  const secretRows: TypeRow[] = (secretRes.data?.secret_types ?? []).map((t) => ({
    kind: "secret" as const,
    id: t.id,
    display_name: t.display_name,
    official: t.official,
    fields: (t.fields ?? []) as SecretTypeField[],
  }));

  return [...locationRows, ...systemRows, ...componentRows, ...secretRows];
}

export type CreateType = {
  id: string;
  display_name: string;
  icon?: string;
};

export async function createType(kind: TypeKind, body: CreateType): Promise<void> {
  switch (kind) {
    case "location": {
      const { error } = await api.POST("/types/location", { body });
      if (error) throw error;
      return;
    }
    case "system": {
      const { error } = await api.POST("/types/system", { body });
      if (error) throw error;
      return;
    }
    case "component": {
      const { error } = await api.POST("/types/component", { body });
      if (error) throw error;
      return;
    }
    case "secret":
      throw new Error("secret types are read-only");
  }
}

export type UpdateType = {
  display_name?: string;
  icon?: string;
};

export async function updateType(kind: TypeKind, id: string, body: UpdateType): Promise<void> {
  switch (kind) {
    case "location": {
      const { error } = await api.PATCH("/types/location/{id}", { params: { path: { id } }, body });
      if (error) throw error;
      return;
    }
    case "system": {
      const { error } = await api.PATCH("/types/system/{id}", { params: { path: { id } }, body });
      if (error) throw error;
      return;
    }
    case "component": {
      const { error } = await api.PATCH("/types/component/{id}", { params: { path: { id } }, body });
      if (error) throw error;
      return;
    }
    case "secret":
      throw new Error("secret types are read-only");
  }
}

export async function deleteType(kind: TypeKind, id: string): Promise<void> {
  switch (kind) {
    case "location": {
      const { error } = await api.DELETE("/types/location/{id}", { params: { path: { id } } });
      if (error) throw error;
      return;
    }
    case "system": {
      const { error } = await api.DELETE("/types/system/{id}", { params: { path: { id } } });
      if (error) throw error;
      return;
    }
    case "component": {
      const { error } = await api.DELETE("/types/component/{id}", { params: { path: { id } } });
      if (error) throw error;
      return;
    }
    case "secret":
      throw new Error("secret types are read-only");
  }
}
