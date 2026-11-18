import { api } from "../api/client";
import { entityLabel } from "./entities";
import type { Grant, CreateGrant } from "./users";

// The user-groups data layer: a group holds role x scope grants that its
// members inherit. A thin typed wrapper over the generated client, gated by
// user_group (management) and user_grant (granting) on the server.

export type Group = { id: string; name: string; label?: string; description?: string; member_count?: number; grant_count?: number };
// A roster row carries the two facts apart: `name` is the member's identifier
// (a person's username, a machine user's name) and `label` is the
// friendly string, which only a person has. They used to arrive as one field
// carrying either, which is what #563 ended.
export type GroupMember = { user_id: string; kind: string; name?: string; label?: string };

export const GROUPS_KEY = ["user-groups"] as const;

// A just-created group opens its blade directly in edit mode, so the operator adds

// groupName is a group's human label, through the one renderer (#683).
export function groupName(g: Group): string {
  return entityLabel(g);
}
// memberName is what a roster shows for a member: its identifier first, since
// that is what identifies the user whatever kind it is, then a human's
// friendly string, then the uuid.
export function memberName(m: GroupMember): string {
  return m.name || m.label || m.user_id;
}

export async function listGroups(): Promise<Group[]> {
  const { data, error } = await api.GET("/user-groups");
  if (error) throw error;
  return (data?.groups ?? []) as Group[];
}

export async function getGroup(id: string): Promise<Group> {
  const { data, error } = await api.GET("/user-groups/{group}", { params: { path: { group: id } } });
  if (error) throw error;
  return data as Group;
}

export async function createGroup(body: { name: string; label?: string; description?: string }): Promise<Group> {
  const { data, error } = await api.POST("/user-groups", { body });
  if (error) throw error;
  return data as Group;
}

// No `name` here: a rename is POST /user-groups/{group}:rename, and this body
// declares `additionalProperties: false`, so sending one is a 422.
export async function updateGroup(id: string, body: { label?: string; description?: string }): Promise<Group> {
  const { data, error } = await api.PATCH("/user-groups/{group}", { params: { path: { group: id } }, body });
  if (error) throw error;
  return data as Group;
}

export async function deleteGroup(id: string): Promise<void> {
  const { error } = await api.DELETE("/user-groups/{group}", { params: { path: { group: id } } });
  if (error) throw error;
}

export async function listGroupMembers(id: string): Promise<GroupMember[]> {
  const { data, error } = await api.GET("/user-groups/{group}/members", { params: { path: { group: id } } });
  if (error) throw error;
  return (data?.members ?? []) as GroupMember[];
}

export async function addGroupMember(id: string, userId: string): Promise<void> {
  const { error } = await api.POST("/user-groups/{group}/members", { params: { path: { group: id } }, body: { user_id: userId } });
  if (error) throw error;
}

export async function removeGroupMember(id: string, userId: string): Promise<void> {
  const { error } = await api.DELETE("/user-groups/{group}/members/{user}", { params: { path: { group: id, user: userId } } });
  if (error) throw error;
}

export async function listGroupGrants(id: string): Promise<Grant[]> {
  const { data, error } = await api.GET("/user-groups/{group}/grants", { params: { path: { group: id } } });
  if (error) throw error;
  return (data?.grants ?? []) as Grant[];
}

export async function createGroupGrant(id: string, body: CreateGrant): Promise<Grant> {
  const { data, error } = await api.POST("/user-groups/{group}/grants", { params: { path: { group: id } }, body });
  if (error) throw error;
  return data as Grant;
}

export async function revokeGroupGrant(id: string, grantId: string): Promise<void> {
  const { error } = await api.DELETE("/user-groups/{group}/grants/{grantId}", { params: { path: { group: id, grantId } } });
  if (error) throw error;
}
