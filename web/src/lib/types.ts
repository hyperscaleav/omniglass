import { api } from "../api/client";
import type { SecretTypeField } from "./secrets";

// The Types catalog data layer: a unified aggregator over the classifier
// registries (location, secret). Each registry is its own typed
// GET/POST/PATCH/DELETE surface under /types/{kind}; this module flattens them
// into one normalized row shape so a single catalog page can list, sort, and
// filter across kinds. secret_type is read-only this slice (no write routes
// exist yet), so the write functions refuse it.
//
// A system's shape is NOT here: system_type was promoted to the STANDARD, a
// first-class catalog entity with its own registry and a declared-property
// contract, so it lives on the Standards page beside Products (lib/standards.ts).
// A component's shape is likewise the product it is an instance of.
export type TypeKind = "location" | "secret";

export const TYPE_KINDS: TypeKind[] = ["location", "secret"];

// Only location is operator-writable; secret_type is read-only this slice.
export const WRITABLE_KINDS: TypeKind[] = ["location"];

// ROOT_PLACEMENT is the reserved allowed_parent_types member meaning "may sit
// at the top, no parent" (mirrors storage.RootPlacement). CreateLocationType
// refuses this id, so a real type can never collide with it.
export const ROOT_PLACEMENT = "root";

// A normalized registry row across both kinds. icon and allowed_parent_types
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
  const [locationRes, secretRes] = await Promise.all([
    api.GET("/location-types"),
    api.GET("/secret-types"),
  ]);

  if (locationRes.error) throw locationRes.error;
  if (secretRes.error) throw secretRes.error;

  const locationRows: TypeRow[] = (locationRes.data?.location_types ?? []).map((t) => ({
    kind: "location" as const,
    id: t.id,
    display_name: t.display_name,
    official: t.official,
    icon: t.icon,
    allowed_parent_types: t.allowed_parent_types ?? [],
  }));

  const secretRows: TypeRow[] = (secretRes.data?.secret_types ?? []).map((t) => ({
    kind: "secret" as const,
    id: t.id,
    display_name: t.display_name,
    official: t.official,
    fields: (t.fields ?? []) as SecretTypeField[],
  }));

  return [...locationRows, ...secretRows];
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
      const { error } = await api.POST("/location-types", { body });
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
      const { error } = await api.PATCH("/location-types/{id}", { params: { path: { id } }, body });
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
      const { error } = await api.DELETE("/location-types/{id}", { params: { path: { id } } });
      if (error) throw error;
      return;
    }
    case "secret":
      throw new Error("secret types are read-only");
  }
}
