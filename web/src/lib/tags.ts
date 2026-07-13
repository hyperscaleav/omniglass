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

// --- value bindings ---------------------------------------------------------
// The binding layer: set and remove a key's value on one entity, and list the
// bindings set directly on it. Setting a value is the entity's own write (gated
// by the entity's :update), done through the per-entity custom methods.

export type TagBinding = { key: string; value: string };

// entityTagsKey is the TanStack query key for one entity's direct bindings.
export function entityTagsKey(kind: EntityKind, name: string) {
  return ["entity-tags", kind, name] as const;
}

export async function listEntityTags(kind: EntityKind, name: string): Promise<TagBinding[]> {
  const p = { params: { path: { name } } };
  const r =
    kind === "component" ? await api.GET("/components/{name}:listTags", p)
    : kind === "system" ? await api.GET("/systems/{name}:listTags", p)
    : await api.GET("/locations/{name}:listTags", p);
  if (r.error) throw r.error;
  return (r.data?.tags ?? []).map((t) => ({ key: t.key, value: t.value })) as TagBinding[];
}

export async function setTag(kind: EntityKind, name: string, key: string, value: string): Promise<void> {
  const p = { params: { path: { name } }, body: { key, value } };
  const r =
    kind === "component" ? await api.POST("/components/{name}:setTag", p)
    : kind === "system" ? await api.POST("/systems/{name}:setTag", p)
    : await api.POST("/locations/{name}:setTag", p);
  if (r.error) throw r.error;
}

export async function removeTag(kind: EntityKind, name: string, key: string): Promise<void> {
  const p = { params: { path: { name } }, body: { key } };
  const r =
    kind === "component" ? await api.POST("/components/{name}:removeTag", p)
    : kind === "system" ? await api.POST("/systems/{name}:removeTag", p)
    : await api.POST("/locations/{name}:removeTag", p);
  if (r.error) throw r.error;
}
