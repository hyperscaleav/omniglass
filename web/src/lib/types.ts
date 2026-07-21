import { api } from "../api/client";
import type { SecretTypeField } from "./secrets";

// The Types catalog data layer: a unified aggregator over the three type
// registries (location, system, secret). Each registry is its own typed
// GET/POST/PATCH/DELETE surface under /types/{kind}; this module flattens them
// into one normalized row shape so a single catalog page can list, sort, and
// filter across kinds. secret_type is read-only this slice (no write routes
// exist yet), so the write functions refuse it.
export type TypeKind = "location" | "system" | "secret";

export const TYPE_KINDS: TypeKind[] = ["location", "system", "secret"];

// Only these two are operator-writable; secret_type is read-only this slice.
export const WRITABLE_KINDS: TypeKind[] = ["location", "system"];

// ROOT_PLACEMENT is the reserved allowed_parent_types member meaning "may sit
// at the top, no parent" (mirrors storage.RootPlacement). CreateLocationType
// refuses this id, so a real type can never collide with it.
export const ROOT_PLACEMENT = "root";

// A normalized registry row across all three kinds. icon and allowed_parent_types
// are present only on location; fields only on secret (read-only display).
export type TypeRow = {
  kind: TypeKind;
  id: string;
  display_name: string;
  official: boolean;
  icon?: string;
  allowed_parent_types?: string[];
  fields?: SecretTypeField[];
};

export const TYPES_KEY = ["types"] as const;

export async function listTypes(): Promise<TypeRow[]> {
  const [locationRes, systemRes, secretRes] = await Promise.all([
    api.GET("/types/location"),
    api.GET("/types/system"),
    api.GET("/types/secret"),
  ]);

  if (locationRes.error) throw locationRes.error;
  if (systemRes.error) throw systemRes.error;
  if (secretRes.error) throw secretRes.error;

  const locationRows: TypeRow[] = (locationRes.data?.location_types ?? []).map((t) => ({
    kind: "location" as const,
    id: t.id,
    display_name: t.display_name,
    official: t.official,
    icon: t.icon,
    allowed_parent_types: t.allowed_parent_types ?? [],
  }));

  const systemRows: TypeRow[] = (systemRes.data?.system_types ?? []).map((t) => ({
    kind: "system" as const,
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

  return [...locationRows, ...systemRows, ...secretRows];
}

export type CreateType = {
  id: string;
  display_name: string;
  icon?: string;
  allowed_parent_types?: string[];
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
    case "secret":
      throw new Error("secret types are read-only");
  }
}

export type UpdateType = {
  display_name?: string;
  icon?: string;
  allowed_parent_types?: string[];
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
    case "secret":
      throw new Error("secret types are read-only");
  }
}
