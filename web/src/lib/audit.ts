import { api } from "../api/client";
import type { FilterKey } from "./predicate";

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

// The number of rows a single page fetches; "load older" pages back from the
// oldest loaded row via the server `before` cursor. Kept under the server's 500 cap.
export const AUDIT_PAGE = 100;

// listAuditLog fetches one page, newest first. `before` (an RFC3339 timestamp)
// pages backward for load-older; `limit` bounds the page. `resource`/`verb` are the
// server-side narrowing the API also supports (the console filters client-side, so
// it does not use them today, but they stay wired for callers that want them).
export async function listAuditLog(params?: {
  resource?: string;
  verb?: string;
  before?: string;
  limit?: number;
}): Promise<AuditEvent[]> {
  const q: Record<string, string | number> = {};
  if (params?.resource) q.resource = params.resource;
  if (params?.verb) q.verb = params.verb;
  if (params?.before) q.before = params.before;
  if (params?.limit) q.limit = params.limit;
  const { data, error } = await api.GET("/audit-log", Object.keys(q).length ? { params: { query: q } } : {});
  if (error) throw error;
  return (data?.events ?? []) as AuditEvent[];
}

// actorLabel resolves an event's actor to something human: the username if the
// actor is a human, else the id, else "system" (a platform-internal write). Under
// impersonation the actor is the principal whose identity was assumed (the target).
export function actorLabel(e: AuditEvent): string {
  return e.actor_name || e.actor || "system";
}

// accountableLabel is who the trail holds responsible for a row: the real actor
// (the impersonator, the human who acted) when the action was impersonated, else
// the actor. This is the name shown as the primary "who"; the assumed identity
// rides alongside as an "as <actor>" tag.
export function accountableLabel(e: AuditEvent): string {
  return e.real_actor_name || e.real_actor || actorLabel(e);
}

const uniqSorted = (xs: string[]): string[] => [...new Set(xs.filter(Boolean))].sort();

// auditFilterKeys are the faceted-search fields for the audit trail, consumed by
// the shared FilterBar exactly as the inventory list views do: who (the acting
// principal), action (the verb), resource (the kind), and id (the resource id).
// Matching is client-side over the loaded rows via lib/predicate. `who` is the
// substring default so a bare term searches the actor. Time is navigated by
// load-older + newest-first, not a facet (the predicate compares gt/lt
// numerically, so an ISO-timestamp facet needs a date-aware operator first).
export const auditFilterKeys: FilterKey<AuditEvent>[] = [
  // `who` matches the accountable actor and, for an impersonated row, the assumed
  // identity too, so a search finds either the impersonator or their target.
  { key: "who", type: "string", hint: "substring", get: (e) => (e.real_actor ? `${accountableLabel(e)} ${actorLabel(e)}` : accountableLabel(e)), values: (rows) => uniqSorted(rows.map(accountableLabel)) },
  { key: "action", type: "string", hint: "exact", get: (e) => e.verb, values: (rows) => uniqSorted(rows.map((e) => e.verb)) },
  { key: "resource", type: "string", hint: "exact", get: (e) => e.resource, values: (rows) => uniqSorted(rows.map((e) => e.resource)) },
  { key: "id", type: "string", hint: "substring", get: (e) => e.resource_id ?? "" },
];
