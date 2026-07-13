import { api } from "../api/client";

// The tags data layer: thin typed wrappers over the generated client. A tag is a
// governed key in the tenant-wide vocabulary. This layer covers the key registry
// (mint, edit governance fields, delete); a key is addressed by its name, not an
// id, on the write paths. Value bindings and the effective-tags cascade are a
// separate surface.

export type EntityKind = "component" | "system" | "location";

export const ENTITY_KINDS: EntityKind[] = ["component", "system", "location"];

export type Tag = {
  id: string;
  name: string;
  applies_to: string[];
  propagates: boolean;
};

export const TAGS_KEY = ["tags"] as const;

export type CreateTag = {
  name: string;
  applies_to?: string[];
  propagates?: boolean;
};

export type UpdateTag = {
  applies_to?: string[];
  propagates?: boolean;
};

export async function listTags(): Promise<Tag[]> {
  const { data, error } = await api.GET("/tags");
  if (error) throw error;
  return (data?.tags ?? []) as Tag[];
}

export async function createTag(body: CreateTag): Promise<Tag> {
  const { data, error } = await api.POST("/tags", { body });
  if (error) throw error;
  return data as Tag;
}

// updateTag replaces the governance fields (applies_to, propagates); the key name
// is fixed at creation, so it addresses the row by name.
export async function updateTag(name: string, body: UpdateTag): Promise<Tag> {
  const { data, error } = await api.PATCH("/tags/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return data as Tag;
}

export async function deleteTag(name: string): Promise<void> {
  const { error } = await api.DELETE("/tags/{name}", { params: { path: { name } } });
  if (error) throw error;
}

// appliesToLabel renders a key's applies_to set for a table cell: an empty set is
// universal ("Any"), otherwise the kinds joined.
export function appliesToLabel(appliesTo: string[]): string {
  return appliesTo.length === 0 ? "Any" : appliesTo.join(", ");
}
