import { api } from "../api/client";
import type { FilterKey } from "./predicate";

// The principals data layer: thin typed wrappers over the generated client, so
// the admin directory page stays declarative and unit-testable against a mocked
// client. Shapes follow the OpenAPI (see api/principals.go). Credentials are never
// returned, so there is no secret here to leak.
// group_id / group_name are set on a grant inherited from a group the principal
// belongs to (absent for a direct grant). An inherited grant is read-only on the
// principal: it is revoked by editing the group, not the member.
export type Grant = { id?: string; role: string; scope_kind: string; scope_id?: string; scope_op?: string; group_id?: string; group_name?: string };

export type Principal = {
  id: string;
  kind: string;
  active: boolean;
  human?: { username: string; email?: string; display_name?: string };
  service?: { label: string };
  grants: Grant[];
  // The principal groups this principal belongs to; the grants they confer ride
  // grants (tagged group_id), this names them for the directory and clickthrough.
  groups?: { id: string; name: string }[];
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

export type CreateGrant = { role: string; scope_kind: ScopeKind; scope_id?: string; scope_op?: "subtree" | "subtree_excl_root" | "self" };

export async function createGrant(principalId: string, body: CreateGrant): Promise<Grant> {
  const { data, error } = await api.POST("/principals/{id}/grants", { params: { path: { id: principalId } }, body });
  if (error) throw error;
  return data as Grant;
}

export async function revokeGrant(principalId: string, grantId: string): Promise<void> {
  const { error } = await api.DELETE("/principals/{id}/grants/{grantId}", { params: { path: { id: principalId, grantId } } });
  if (error) throw error;
}

export async function setPrincipalActive(id: string, active: boolean): Promise<void> {
  const { error } = active
    ? await api.POST("/principals/{id}:enable", { params: { path: { id } } })
    : await api.POST("/principals/{id}:disable", { params: { path: { id } } });
  if (error) throw error;
}

// A role in the catalog, for the grant form's role picker and the Roles view.
export type Role = {
  id: string;
  official: boolean;
  permissions: string[];
  inherits: string[];
  display_name?: string;
  description?: string;
  // What the role actually confers, flattened by the server (inheritance, wildcard,
  // and the :read floor resolved). Present on GET /roles.
  effective_permissions?: string[];
};

export const ROLES_KEY = ["roles"] as const;

export async function listRoles(): Promise<Role[]> {
  const { data, error } = await api.GET("/roles");
  if (error) throw error;
  return (data?.roles ?? []) as Role[];
}

// effectivePerms is what a role actually confers: the server-flattened set
// (inheritance, wildcard, and the read floor resolved) when present, else the
// declared permissions. This is the same set the Roles card renders.
export function effectivePerms(r: Role): string[] {
  return r.effective_permissions ?? r.permissions;
}

// permResources pulls the distinct resource heads a role grants (the `resource`
// of each `resource:action` token, and `>` for the superuser tail), for the
// permission facet's autocomplete catalog.
function permResources(perms: string[]): string[] {
  return perms.map((p) => (p === ">" ? ">" : p.split(":")[0]));
}

const uniqSorted = (xs: string[]): string[] => [...new Set(xs.filter(Boolean))].sort();

// roleFilterKeys are the faceted-search fields for the Roles catalog, consumed by
// the shared FilterBar/ListShell exactly as the inventory lists and the audit
// trail are. `name` is the substring default (a bare term searches the display
// name or id); `id` is exact; `permission` is a substring over the role's
// effective permission strings, so an admin can find every role that grants, for
// example, `audit`. Matching is client-side over the loaded rows via lib/predicate.
export const roleFilterKeys: FilterKey<Role>[] = [
  { key: "name", type: "string", hint: "substring", get: (r) => `${r.display_name ?? ""} ${r.id}`, values: (rows) => uniqSorted(rows.map((r) => r.display_name || r.id)) },
  { key: "id", type: "string", hint: "exact", get: (r) => r.id, values: (rows) => uniqSorted(rows.map((r) => r.id)) },
  { key: "permission", type: "string", hint: "substring", get: (r) => effectivePerms(r).join(" "), values: (rows) => uniqSorted(rows.flatMap((r) => permResources(effectivePerms(r)))) },
];

// The display name for a principal: a human's display name or username, a service
// account's label, else the bare kind.
export function principalName(p: Principal): string {
  return p.human?.display_name || p.human?.username || p.service?.label || p.kind;
}
