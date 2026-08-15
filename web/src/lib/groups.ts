import { api } from "../api/client";
import { entityLabel } from "./entities";
import type { Grant, CreateGrant } from "./principals";

// The principal-groups data layer: a group holds role x scope grants that its
// members inherit. A thin typed wrapper over the generated client, gated by
// principal_group (management) and principal_grant (granting) on the server.

export type Group = { id: string; name: string; label?: string; description?: string; member_count?: number; grant_count?: number };
// A roster row carries the two facts apart: `name` is the member's identifier
// (a human's username, a service account's name) and `label` is the
// friendly string, which only a human has. They used to arrive as one field
// carrying either, which is what #563 ended.
export type GroupMember = { principal_id: string; kind: string; name?: string; label?: string };

export const GROUPS_KEY = ["principal-groups"] as const;

// A just-created group opens its blade directly in edit mode, so the operator adds

// groupName is a group's human label, through the one renderer (#683).
export function groupName(g: Group): string {
  return entityLabel(g);
}
// memberName is what a roster shows for a member: its identifier first, since
// that is what identifies the principal whatever kind it is, then a human's
// friendly string, then the uuid.
export function memberName(m: GroupMember): string {
  return m.name || m.label || m.principal_id;
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

export async function createGroup(body: { name: string; label?: string; description?: string }): Promise<Group> {
  const { data, error } = await api.POST("/principal-groups", { body });
  if (error) throw error;
  return data as Group;
}

// No `name` here: a rename is POST /principal-groups/{id}:rename, and this body
// declares `additionalProperties: false`, so sending one is a 422.
export async function updateGroup(id: string, body: { label?: string; description?: string }): Promise<Group> {
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
