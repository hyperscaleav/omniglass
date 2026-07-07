import { api } from "../api/client";

// The audit data layer: a thin typed wrapper over the generated client for the
// read-only audit trail. Gated by audit:read (admin and owner), so a viewer never
// reaches it.
export type AuditEvent = {
  id: string;
  ts: string;
  actor?: string;
  actor_name?: string;
  real_actor?: string;
  real_actor_name?: string;
  verb: string;
  resource: string;
  resource_id?: string;
};

export const AUDIT_KEY = ["audit-log"] as const;

export async function listAuditLog(params?: { resource?: string; verb?: string }): Promise<AuditEvent[]> {
  const query = params && (params.resource || params.verb) ? { query: params } : {};
  const { data, error } = await api.GET("/audit-log", query);
  if (error) throw error;
  return (data?.events ?? []) as AuditEvent[];
}

// actorLabel resolves an event's actor to something human: the username if the
// actor is a human, else the id, else "system" (a platform-internal write).
export function actorLabel(e: AuditEvent): string {
  return e.actor_name || e.actor || "system";
}
