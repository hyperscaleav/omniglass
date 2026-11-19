import { api } from "../api/client";
import { fileToBase64 } from "./auth";
import { entityLabel } from "./entities";
import type { FilterKey } from "./predicate";

// The users data layer: thin typed wrappers over the generated client, so
// the admin directory page stays declarative and unit-testable against a mocked
// client. Shapes follow the OpenAPI (see api/users.go). Credentials are never
// returned, so there is no secret here to leak.
// group_id / group (the name) / group_label are set on a grant inherited from a group the user
// belongs to (absent for a direct grant). An inherited grant is read-only on the
// user: it is revoked by editing the group, not the member.
export type Grant = { id?: string; role: string; scope_kind: string; scope_id?: string; scope_op?: string; group_id?: string; group?: string; group_label?: string };

export type User = {
  id: string;
  kind: string;
  active: boolean;
  // Set when the user is archived (soft-deleted): hidden from the default
  // directory, cannot authenticate, reversible until purged. Absent means live.
  archived_at?: string;
  // has_avatar is a boolean flag (never the bytes): the image is fetched lazily via
  // userAvatarUrl only when it is set, so the directory stays cheap to load.
  person?: { username: string; email?: string; label?: string; has_avatar?: boolean };
  machine?: { name: string };
  grants: Grant[];
  // The user groups this user belongs to; the grants they confer ride
  // grants (tagged group_id), this names them for the directory and clickthrough.
  groups?: { id: string; name: string }[];
};

export const USERS_KEY = ["users"] as const;

// includeArchived surfaces soft-deleted users (the "show archived"
// directory view), so a hidden account can be restored or purged.
export async function listUsers(kind?: "person" | "machine", includeArchived?: boolean): Promise<User[]> {
  const query: { kind?: "person" | "machine"; include_archived?: boolean } = {};
  if (kind) query.kind = kind;
  if (includeArchived) query.include_archived = true;
  const { data, error } = await api.GET("/users", { params: { query } });
  if (error) throw error;
  return (data?.users ?? []) as User[];
}

export async function getUser(id: string): Promise<User> {
  const { data, error } = await api.GET("/users/{id}", { params: { path: { id } } });
  if (error) throw error;
  return data as User;
}

export type CreateUser = {
  username: string;
  label?: string;
  email?: string;
  password?: string;
};

export async function createUser(body: CreateUser): Promise<User> {
  const { data, error } = await api.POST("/users", { body });
  if (error) throw error;
  return data as User;
}

export type UpdateUser = { label?: string; email?: string; username?: string };

export async function updateUser(id: string, body: UpdateUser): Promise<User> {
  const { data, error } = await api.PATCH("/users/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data as User;
}

export type ScopeKind = "all" | "location" | "system" | "component";

export type CreateGrant = { role: string; scope_kind: ScopeKind; scope_id?: string; scope_op?: "subtree" | "subtree_excl_root" | "self" };

export async function createGrant(userId: string, body: CreateGrant): Promise<Grant> {
  const { data, error } = await api.POST("/users/{id}/grants", { params: { path: { id: userId } }, body });
  if (error) throw error;
  return data as Grant;
}

export async function revokeGrant(userId: string, grantId: string): Promise<void> {
  const { error } = await api.DELETE("/users/{id}/grants/{grantId}", { params: { path: { id: userId, grantId } } });
  if (error) throw error;
}

export async function setUserActive(id: string, active: boolean): Promise<void> {
  const { error } = active
    ? await api.POST("/users/{id}:enable", { params: { path: { id } } })
    : await api.POST("/users/{id}:disable", { params: { path: { id } } });
  if (error) throw error;
}

// resetUserPassword sets a new password for a person (an admin action;
// no current password required). Gated by user:reset-password; the new password
// must meet the policy (a 422 otherwise, surfaced inline on the field).
export async function resetUserPassword(id: string, password: string): Promise<void> {
  const { error } = await api.POST("/users/{id}:resetPassword", { params: { path: { id } }, body: { password } });
  if (error) throw error;
}

// setUserAvatar sets another user's profile picture (an admin action).
// Gated by user:set-avatar; the server normalizes the upload to a 256x256 JPEG
// and rejects a bad or oversize image with a 422.
export async function setUserAvatar(id: string, file: File): Promise<void> {
  const image_base64 = await fileToBase64(file);
  const { error } = await api.POST("/users/{id}:setAvatar", { params: { path: { id } }, body: { image_base64 } });
  if (error) throw error;
}

// removeUserAvatar clears another user's profile picture (a no-op if none
// is set). Gated by user:set-avatar.
export async function removeUserAvatar(id: string): Promise<void> {
  const { error } = await api.POST("/users/{id}:removeAvatar", { params: { path: { id } } });
  if (error) throw error;
}

// userAvatarUrl reads a user's profile picture and returns it as a data
// URL, or null when there is none (a 404). Gated by user:read.
export async function userAvatarUrl(id: string): Promise<string | null> {
  const { data, error } = await api.GET("/users/{id}/avatar", { params: { path: { id } } });
  if (error || !data) return null;
  return `data:image/jpeg;base64,${data.image_base64}`;
}

// The soft/hard delete lifecycle: archive hides the account (reversible),
// restore brings it back, and purge hard-deletes an archived one (irreversible).
export async function archiveUser(id: string): Promise<void> {
  const { error } = await api.POST("/users/{id}:archive", { params: { path: { id } } });
  if (error) throw error;
}
export async function restoreUser(id: string): Promise<void> {
  const { error } = await api.POST("/users/{id}:restore", { params: { path: { id } } });
  if (error) throw error;
}
export async function purgeUser(id: string): Promise<void> {
  const { error } = await api.POST("/users/{id}:purge", { params: { path: { id } } });
  if (error) throw error;
}

// A role in the catalog, for the grant form's role picker and the Roles view.
export type Role = {
  id: string;
  name: string;
  official: boolean;
  permissions: string[];
  inherits: string[];
  label?: string;
  description?: string;
  // What the role actually confers, flattened by the server (inheritance, wildcard,
  // and the :read floor resolved). Present on GET /roles.
  effective_permissions?: string[];
  // held is the subset of permission_universe this role covers, resolved by the
  // server. permission_universe is the full set of capabilities the API enforces,
  // the same for every role (folded onto each so the blade can show held vs
  // missing without a second query). Both present on GET /roles.
  held?: string[];
  permission_universe?: string[];
};

export const ROLES_KEY = ["roles"] as const;

export async function listRoles(): Promise<Role[]> {
  const { data, error } = await api.GET("/roles");
  if (error) throw error;
  // permission_universe is a payload-level field, identical for every role; fold it
  // onto each so the detail blade reads role.permission_universe alongside role.held.
  const universe = data?.permission_universe ?? [];
  return (data?.roles ?? []).map((r) => ({ ...r, permission_universe: universe })) as Role[];
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
  { key: "name", type: "string", hint: "substring", get: (r) => `${entityLabel(r)} ${r.name}`, values: (rows) => uniqSorted(rows.map(entityLabel)) },
  { key: "id", type: "string", hint: "exact", get: (r) => r.name, values: (rows) => uniqSorted(rows.map((r) => r.name)) },
  { key: "permission", type: "string", hint: "substring", get: (r) => effectivePerms(r).join(" "), values: (rows) => uniqSorted(rows.flatMap((r) => permResources(effectivePerms(r)))) },
];

// The label for a user: a person's label or username, a machine
// user's name, else the bare kind.
//
// NOT entityLabel, and deliberately: a user is addressed by uuid and each
// kind carries its identifier under its own column, and the precedence here runs
// the other way round from an entity's (a username is the identifier, and it
// outranks a machine user's name). Widening entityLabel to express it would
// make the entity rule answer a question it does not have the columns for.
//
// It takes the structural shape rather than a User so the sidebar's `me`
// payload reaches the same rule (#683). That payload nests the kind one level
// deeper and used to restate all four rungs by hand, which is the same drift
// with a different subject.
export type UserIdentity = {
  person?: { username: string; label?: string };
  machine?: { name: string };
  kind: string;
};

export function userName(p: User | UserIdentity): string {
  return p.person?.label || p.person?.username || p.machine?.name || p.kind;
}

// Presentational helpers shared by the directory columns and the detail body, so a
// user reads the same in the list and its blade.
//
// kindLabel is the word an operator reads for a user's kind (ADR-2036): a person
// or a machine, the wire's own values.
export function kindLabel(kind: string): string {
  return kind;
}

export const kindBadge = (kind: string) => `badge badge-soft badge-sm capitalize ${kind === "machine" ? "badge-info" : "badge-primary"}`;
export const userInitials = (p: User): string => userName(p).slice(0, 2).toUpperCase();
