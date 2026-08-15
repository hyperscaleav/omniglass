import { api } from "../api/client";

// The secrets data layer: thin typed wrappers over the generated client. A
// secret is a typed, encrypted-at-rest value owned on the exclusive arc
// (platform, or one of the location / system / component trees) and resolved
// down the cascade. Fields come back masked: a secret field's value is the fixed
// placeholder, a non-secret field's value is its plaintext.

export type SecretField = {
  name: string;
  value: string;
  secret: boolean;
};

// The secret_type registry (the shapes a secret can take) lives in
// lib/secret_types.ts, its own module, so a page that only needs secrets never
// couples to the registry fetch and vice versa (#598).

export type Secret = {
  id: string;
  name: string;
  // The friendly string an operator reads. Optional: a secret with none renders
  // its name verbatim, through entityLabel like every other row.
  label?: string;
  secret_type: string;
  owner_kind: string;

  owner_name?: string;
  fields: SecretField[];
};

export const SECRETS_KEY = ["secrets"] as const;

export async function listSecrets(): Promise<Secret[]> {
  const { data, error } = await api.GET("/secrets");
  if (error) throw error;
  return (data?.secrets ?? []) as Secret[];
}

export type OwnerKind = "platform" | "location" | "component";

export type CreateSecret = {
  name: string;
  label?: string;
  secret_type: string;
  owner_kind: OwnerKind;
  owner?: string;
  fields: Record<string, string>;
};

export async function createSecret(body: CreateSecret): Promise<Secret> {
  const { data, error } = await api.POST("/secrets", { body });
  if (error) throw error;
  return data as Secret;
}

// updateSecret replaces the given field values (an omitted field keeps its
// value) and patches the label; name, type, and owner are fixed at creation.
// An empty label clears it; omitting it leaves it alone.
export type UpdateSecret = {
  fields?: Record<string, string>;
  label?: string;
};

export async function updateSecret(id: string, body: UpdateSecret): Promise<Secret> {
  const { data, error } = await api.PATCH("/secrets/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data as Secret;
}

export async function deleteSecret(id: string): Promise<void> {
  const { error } = await api.DELETE("/secrets/{id}", { params: { path: { id } } });
  if (error) throw error;
}

// revealSecret decrypts a secret's fields for on-screen display (the audited,
// admin-gated reveal). The returned map is field name -> plaintext.
export async function revealSecret(id: string): Promise<Record<string, string>> {
  const { data, error } = await api.POST("/secrets/{id}:reveal", { params: { path: { id } } });
  if (error) throw error;
  return (data?.fields ?? {}) as Record<string, string>;
}

// copySecret decrypts a secret's fields for a clipboard copy: the same exposure
// and gate as reveal, audited under the distinct `copy` verb.
export async function copySecret(id: string): Promise<Record<string, string>> {
  const { data, error } = await api.POST("/secrets/{id}:copy", { params: { path: { id } } });
  if (error) throw error;
  return (data?.fields ?? {}) as Record<string, string>;
}
