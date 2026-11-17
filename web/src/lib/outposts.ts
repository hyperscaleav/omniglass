import { entityLabel } from "./entities";
import { api } from "../api/client";
import type { FilterKey } from "./predicate";

// The outposts data layer: thin typed wrappers over the generated client, so the
// Outposts page stays declarative and unit-testable against a mocked fetch. Shapes
// follow the OpenAPI (see api/outposts.go, OutpostBody / EnrollOutputBody). The list and
// detail carry no secret; the ONLY secret this layer touches is the enrollment
// token from :enroll, which is returned to the caller and never cached or logged.
export type Outpost = {
  name: string;
  label?: string;
  description?: string;
  location?: string;
  enrolled: boolean;
  enrolled_at?: string;
  last_heartbeat_at?: string;
  // Batches the server dropped because no task owned them (#1200), and when the
  // last one arrived.
  orphaned_batches?: number;
  last_orphaned_at?: string;
  // The resolved effective tags (its direct bindings plus propagating globals),
  // mapped from the API `effective_tags` so the row satisfies the shared tag-facet
  // and TagPills contract, exactly as the component row does.
  tags: Record<string, string>;
};

// outpostLabel is the outpost's human label: its label, falling back to the
// name (the key/fleet address) when unset. Used as the blade title and the list
// row label, mirroring how component/system/location present name vs label.
// outpostLabel is entityLabel, kept as a named export because the outpost columns and
// blade title read better with it. The rule itself lives in one place.
export function outpostLabel(n: Outpost): string {
  return entityLabel(n);
}

// The once-shown enrollment token exchange result. It is deliberately NOT stored
// in the query cache: enrollOutpost returns it to the caller, which reveals it once
// and drops it.
export type EnrollOutput = { name: string; token: string };

export const OUTPOSTS_KEY = ["outposts"] as const;

export async function listOutposts(): Promise<Outpost[]> {
  const { data, error } = await api.GET("/outposts");
  if (error) throw error;
  return (data?.outposts ?? []).map((n) => ({ ...n, tags: n.effective_tags ?? {} })) as Outpost[];
}

// One outpost's read, for a surface that needs an outpost or two rather than the
// fleet's list (a component's Collection card, #934).
export const outpostKey = (name: string) => ["outpost", name] as const;

export async function getOutpost(name: string): Promise<Outpost> {
  const { data, error } = await api.GET("/outposts/{name}", { params: { path: { name } } });
  if (error) throw error;
  return { ...data, tags: data?.effective_tags ?? {} } as Outpost;
}

export type CreateOutpost = { name: string; label?: string; description?: string; location?: string };

export async function createOutpost(body: CreateOutpost): Promise<Outpost> {
  const { data, error } = await api.POST("/outposts", { body });
  if (error) throw error;
  return data as Outpost;
}

// The outpost update patch: only the mutable fields (name is the immutable key). A
// field left undefined is unchanged; location set to "" clears the placement.
export type OutpostPatch = { label?: string; description?: string; location?: string };

export async function updateOutpost(name: string, body: OutpostPatch): Promise<Outpost> {
  const { data, error } = await api.PATCH("/outposts/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return data as Outpost;
}

export async function deleteOutpost(name: string): Promise<void> {
  const { error } = await api.DELETE("/outposts/{name}", { params: { path: { name } } });
  if (error) throw error;
}

// enrollOutpost mints (or re-mints) the outpost's enrollment token and returns it ONCE.
// The token is a secret: the server stores only its hash and never logs it, and
// this wrapper hands it straight back without caching it. The caller reveals it in
// the show-once modal and clears it on close.
export async function enrollOutpost(name: string): Promise<EnrollOutput> {
  const { data, error } = await api.POST("/outposts/{name}:enroll", { params: { path: { name } } });
  if (error) throw error;
  return data as EnrollOutput;
}

// The outpost liveness window mirrors the seeded outpost-silent rule (#1707): an outpost
// whose checkin-age passes 90s opens that alarm server-side, so the console pill
// reads down past the same threshold.
export const OUTPOST_DOWN_AFTER_MS = 90_000;

export type OutpostStatus = "up" | "down" | "never";

// outpostStatus derives the liveness pill from last_heartbeat_at against the down
// window: `never` if it has not checked in, `up` within the window, `down` once
// stale. Pure (now is injectable) so it derives client-side from a real field, no
// fabricated status. The boundary (exactly the window) still reads up.
export function outpostStatus(n: Outpost, now: number = Date.now()): OutpostStatus {
  if (!n.last_heartbeat_at) return "never";
  const age = now - new Date(n.last_heartbeat_at).getTime();
  return age <= OUTPOST_DOWN_AFTER_MS ? "up" : "down";
}

// outpostFilterKeys are the faceted-search fields the shared FilterBar/ListShell
// drives, exactly as the other lists do: name (substring, the default) and status
// (exact, over the derived pill). Matching is client-side over the loaded rows via
// lib/predicate.
export const outpostFilterKeys: FilterKey<Outpost>[] = [
  { key: "name", type: "string", hint: "substring", get: (n) => `${outpostLabel(n)} ${n.name}` },
  { key: "status", type: "string", hint: "exact", get: (n) => outpostStatus(n), values: () => ["down", "never", "up"] },
  { key: "location", type: "string", hint: "exact", get: (n) => n.location ?? "", values: (rows) => [...new Set(rows.map((r) => r.location).filter(Boolean) as string[])].sort() },
];
