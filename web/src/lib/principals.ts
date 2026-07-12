import { createSignal } from "solid-js";
import { api } from "../api/client";
import { fileToBase64 } from "./auth";
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
  // Set when the principal is archived (soft-deleted): hidden from the default
  // directory, cannot authenticate, reversible until purged. Absent means live.
  archived_at?: string;
  // has_avatar is a boolean flag (never the bytes): the image is fetched lazily via
  // principalAvatarUrl only when it is set, so the directory stays cheap to load.
  human?: { username: string; email?: string; display_name?: string; has_avatar?: boolean };
  service?: { label: string };
  grants: Grant[];
  // The principal groups this principal belongs to; the grants they confer ride
  // grants (tagged group_id), this names them for the directory and clickthrough.
  groups?: { id: string; name: string }[];
};

export const PRINCIPALS_KEY = ["principals"] as const;

// A just-created user opens its blade directly in edit mode, so grants (and any
// profile tweak) can be added without a second step. The create flow flags the new
// id here; UserDetail consumes it once its data has loaded and begins editing. Mirror
// of the group create flow. A reactive signal so the consuming effect reruns.
const [pendingEditId, setPendingEditId] = createSignal<string | null>(null);

// openPrincipalInEdit marks a principal to open in edit mode the next time its blade
// mounts.
export function openPrincipalInEdit(id: string): void {
  setPendingEditId(id);
}

// consumePendingPrincipalEdit returns true (and clears the flag) if this id is the
// one flagged to open in edit mode, so the caller begins editing exactly once.
export function consumePendingPrincipalEdit(id: string): boolean {
  if (pendingEditId() !== id) return false;
  setPendingEditId(null);
  return true;
}

// includeArchived surfaces soft-deleted principals (the "show archived"
// directory view), so a hidden account can be restored or purged.
export async function listPrincipals(kind?: "human" | "service", includeArchived?: boolean): Promise<Principal[]> {
  const query: { kind?: "human" | "service"; include_archived?: boolean } = {};
  if (kind) query.kind = kind;
  if (includeArchived) query.include_archived = true;
  const { data, error } = await api.GET("/principals", { params: { query } });
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

// resetPrincipalPassword sets a new password for a human principal (an admin action;
// no current password required). Gated by principal:reset-password; the new password
// must meet the policy (a 422 otherwise, surfaced inline on the field).
export async function resetPrincipalPassword(id: string, password: string): Promise<void> {
  const { error } = await api.POST("/principals/{id}:resetPassword", { params: { path: { id } }, body: { password } });
  if (error) throw error;
}

// setPrincipalAvatar sets another principal's profile picture (an admin action).
// Gated by principal:set-avatar; the server normalizes the upload to a 256x256 JPEG
// and rejects a bad or oversize image with a 422.
export async function setPrincipalAvatar(id: string, file: File): Promise<void> {
  const image_base64 = await fileToBase64(file);
  const { error } = await api.POST("/principals/{id}:setAvatar", { params: { path: { id } }, body: { image_base64 } });
  if (error) throw error;
}

// removePrincipalAvatar clears another principal's profile picture (a no-op if none
// is set). Gated by principal:set-avatar.
export async function removePrincipalAvatar(id: string): Promise<void> {
  const { error } = await api.POST("/principals/{id}:removeAvatar", { params: { path: { id } } });
  if (error) throw error;
}

// principalAvatarUrl reads a principal's profile picture and returns it as a data
// URL, or null when there is none (a 404). Gated by principal:read.
export async function principalAvatarUrl(id: string): Promise<string | null> {
  const { data, error } = await api.GET("/principals/{id}/avatar", { params: { path: { id } } });
  if (error || !data) return null;
  return `data:image/jpeg;base64,${data.image_base64}`;
}

// The soft/hard delete lifecycle: archive hides the account (reversible),
// restore brings it back, and purge hard-deletes an archived one (irreversible).
export async function archivePrincipal(id: string): Promise<void> {
  const { error } = await api.POST("/principals/{id}:archive", { params: { path: { id } } });
  if (error) throw error;
}
export async function restorePrincipal(id: string): Promise<void> {
  const { error } = await api.POST("/principals/{id}:restore", { params: { path: { id } } });
  if (error) throw error;
}
export async function purgePrincipal(id: string): Promise<void> {
  const { error } = await api.POST("/principals/{id}:purge", { params: { path: { id } } });
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

// Presentational helpers shared by the directory columns and the detail body, so a
// principal reads the same in the list and its blade.
export const kindBadge = (kind: string) => `badge badge-soft badge-sm capitalize ${kind === "service" ? "badge-info" : "badge-primary"}`;
export const principalInitials = (p: Principal): string => principalName(p).slice(0, 2).toUpperCase();
