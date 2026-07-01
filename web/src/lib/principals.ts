import { api } from "../api/client";

// The principals data layer: thin typed wrappers over the generated client, so
// the admin directory page stays declarative and unit-testable against a mocked
// client. Shapes follow the OpenAPI (see api/principals.go). Credentials are never
// returned, so there is no secret here to leak.
export type Grant = { id?: string; role: string; scope_kind: string; scope_id?: string };

export type Principal = {
  id: string;
  kind: string;
  human?: { username: string; email?: string; display_name?: string };
  service?: { label: string };
  grants: Grant[];
};

export const PRINCIPALS_KEY = ["principals"] as const;

export async function listPrincipals(kind?: "human" | "service"): Promise<Principal[]> {
  const { data, error } = await api.GET("/principals", kind ? { params: { query: { kind } } } : {});
  if (error) throw error;
  return (data?.principals ?? []) as Principal[];
}

export async function getPrincipal(id: string): Promise<Principal> {
  const { data, error } = await api.GET("/principals/{id}", { params: { path: { id } } });
  if (error) throw error;
  return data as Principal;
}

export type CreatePrincipal = {
  username: string;
  display_name?: string;
  email?: string;
  password?: string;
};

export async function createPrincipal(body: CreatePrincipal): Promise<Principal> {
  const { data, error } = await api.POST("/principals", { body });
  if (error) throw error;
  return data as Principal;
}

export type UpdatePrincipal = { display_name?: string; email?: string; username?: string };

export async function updatePrincipal(id: string, body: UpdatePrincipal): Promise<Principal> {
  const { data, error } = await api.PATCH("/principals/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data as Principal;
}

export type ScopeKind = "all" | "location" | "system" | "component" | "group";

export type CreateGrant = { role: string; scope_kind: ScopeKind; scope_id?: string };

export async function createGrant(principalId: string, body: CreateGrant): Promise<Grant> {
  const { data, error } = await api.POST("/principals/{id}/grants", { params: { path: { id: principalId } }, body });
  if (error) throw error;
  return data as Grant;
}

export async function revokeGrant(principalId: string, grantId: string): Promise<void> {
  const { error } = await api.DELETE("/principals/{id}/grants/{grantId}", { params: { path: { id: principalId, grantId } } });
  if (error) throw error;
}

// A role in the catalog, for the grant form's role picker.
export type Role = { id: string; official: boolean; permissions: string[]; inherits: string[] };

export const ROLES_KEY = ["roles"] as const;

export async function listRoles(): Promise<Role[]> {
  const { data, error } = await api.GET("/roles");
  if (error) throw error;
  return (data?.roles ?? []) as Role[];
}

// The display name for a principal: a human's display name or username, a service
// account's label, else the bare kind.
export function principalName(p: Principal): string {
  return p.human?.display_name || p.human?.username || p.service?.label || p.kind;
}
