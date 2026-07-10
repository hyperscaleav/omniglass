import { api } from "../api/client";

// The secrets data layer: thin typed wrappers over the generated client. A
// secret is a typed, encrypted-at-rest value owned on the exclusive arc (global,
// or one of the location / system / component trees) and resolved down the
// cascade. Fields come back masked: a secret field's value is the fixed
// placeholder, a non-secret field's value is its plaintext.

export type SecretField = {
  name: string;
  value: string;
  secret: boolean;
};

export type SecretTypeField = {
  name: string;
  type: string;
  secret: boolean;
  origin: string;
};

export type SecretType = {
  id: string;
  display_name: string;
  official: boolean;
  fields: SecretTypeField[];
};

export type Secret = {
  id: string;
  name: string;
  secret_type: string;
  owner_kind: string;
  owner_id?: string;
  owner_name?: string;
  fields: SecretField[];
};

// ResolvedSecret is one entry in a component's effective-secrets cascade: where
// the owner sits (band 0 global .. 3 component, depth up the tier's tree) and
// whether it is the resolved winner or a shadowed candidate.
export type ResolvedSecret = {
  id: string;
  name: string;
  secret_type: string;
  owner_kind: string;
  owner_id?: string;
  owner_name?: string;
  band: number;
  depth: number;
  winner: boolean;
  fields: SecretField[];
};

export const SECRETS_KEY = ["secrets"] as const;
export const SECRET_TYPES_KEY = ["secret-types"] as const;
export const effectiveSecretsKey = (component: string) => ["effective-secrets", component] as const;

export async function listSecretTypes(): Promise<SecretType[]> {
  const { data, error } = await api.GET("/secret-types");
  if (error) throw error;
  return (data?.secret_types ?? []) as SecretType[];
}

export async function listSecrets(): Promise<Secret[]> {
  const { data, error } = await api.GET("/secrets");
  if (error) throw error;
  return (data?.secrets ?? []) as Secret[];
}

export type OwnerKind = "global" | "location" | "system" | "component";

export type CreateSecret = {
  name: string;
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
// value); name, type, and owner are fixed at creation.
export async function updateSecret(id: string, fields: Record<string, string>): Promise<Secret> {
  const { data, error } = await api.PATCH("/secrets/{id}", { params: { path: { id } }, body: { fields } });
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

export async function effectiveSecrets(component: string): Promise<ResolvedSecret[]> {
  const { data, error } = await api.GET("/components/{name}/effective-secrets", {
    params: { path: { name: component } },
  });
  if (error) throw error;
  return (data?.secrets ?? []) as ResolvedSecret[];
}
