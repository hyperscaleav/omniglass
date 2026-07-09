import { createSignal } from "solid-js";
import { api } from "../api/client";
import type { Grant, CreateGrant } from "./principals";

// The principal-groups data layer: a group holds role x scope grants that its
// members inherit. A thin typed wrapper over the generated client, gated by
// principal_group (management) and principal_grant (granting) on the server.

export type Group = { id: string; name: string; display_name?: string; description?: string; member_count?: number; grant_count?: number };
export type GroupMember = { principal_id: string; kind: string; username?: string; display_name?: string };

export const GROUPS_KEY = ["principal-groups"] as const;

// A just-created group opens its blade directly in edit mode, so the operator adds
// members and grants (child resources that need the group's id) without a second
// step. The create flow flags the new id here; GroupDetail consumes it once its data
// has loaded and begins editing. A reactive signal so the consuming effect reruns.
const [pendingEditId, setPendingEditId] = createSignal<string | null>(null);

// openGroupInEdit marks a group to open in edit mode the next time its blade mounts.
export function openGroupInEdit(id: string): void {
  setPendingEditId(id);
}

// consumePendingGroupEdit returns true (and clears the flag) if this id is the one
// flagged to open in edit mode, so the caller begins editing exactly once.
export function consumePendingGroupEdit(id: string): boolean {
  if (pendingEditId() !== id) return false;
  setPendingEditId(null);
  return true;
}

// groupName is a group's human label: its display name, else its name.
export function groupName(g: Group): string {
  return g.display_name || g.name;
}
// memberName is a member's label: a human's username, else a service's display name (its label).
export function memberName(m: GroupMember): string {
  return m.username || m.display_name || m.principal_id;
}

export async function listGroups(): Promise<Group[]> {
  const { data, error } = await api.GET("/principal-groups");
  if (error) throw error;
  return (data?.groups ?? []) as Group[];
}

export async function getGroup(id: string): Promise<Group> {
  const { data, error } = await api.GET("/principal-groups/{id}", { params: { path: { id } } });
  if (error) throw error;
  return data as Group;
}

export async function createGroup(body: { name: string; display_name?: string; description?: string }): Promise<Group> {
  const { data, error } = await api.POST("/principal-groups", { body });
  if (error) throw error;
  return data as Group;
}

export async function updateGroup(id: string, body: { name?: string; display_name?: string; description?: string }): Promise<Group> {
  const { data, error } = await api.PATCH("/principal-groups/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data as Group;
}

export async function deleteGroup(id: string): Promise<void> {
  const { error } = await api.DELETE("/principal-groups/{id}", { params: { path: { id } } });
  if (error) throw error;
}

export async function listGroupMembers(id: string): Promise<GroupMember[]> {
  const { data, error } = await api.GET("/principal-groups/{id}/members", { params: { path: { id } } });
  if (error) throw error;
  return (data?.members ?? []) as GroupMember[];
}

export async function addGroupMember(id: string, principalId: string): Promise<void> {
  const { error } = await api.POST("/principal-groups/{id}/members", { params: { path: { id } }, body: { principal_id: principalId } });
  if (error) throw error;
}

export async function removeGroupMember(id: string, principalId: string): Promise<void> {
  const { error } = await api.DELETE("/principal-groups/{id}/members/{principalId}", { params: { path: { id, principalId } } });
  if (error) throw error;
}

export async function listGroupGrants(id: string): Promise<Grant[]> {
  const { data, error } = await api.GET("/principal-groups/{id}/grants", { params: { path: { id } } });
  if (error) throw error;
  return (data?.grants ?? []) as Grant[];
}

export async function createGroupGrant(id: string, body: CreateGrant): Promise<Grant> {
  const { data, error } = await api.POST("/principal-groups/{id}/grants", { params: { path: { id } }, body });
  if (error) throw error;
  return data as Grant;
}

export async function revokeGroupGrant(id: string, grantId: string): Promise<void> {
  const { error } = await api.DELETE("/principal-groups/{id}/grants/{grantId}", { params: { path: { id, grantId } } });
  if (error) throw error;
}
