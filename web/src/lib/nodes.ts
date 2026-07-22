import { entityLabel } from "./entities";
import { api } from "../api/client";
import type { FilterKey } from "./predicate";

// The nodes data layer: thin typed wrappers over the generated client, so the
// Nodes page stays declarative and unit-testable against a mocked fetch. Shapes
// follow the OpenAPI (see api/nodes.go, NodeBody / EnrollOutputBody). The list and
// detail carry no secret; the ONLY secret this layer touches is the enrollment
// token from :enroll, which is returned to the caller and never cached or logged.
export type Node = {
  name: string;
  display_name?: string;
  description?: string;
  location?: string;
  enrolled: boolean;
  enrolled_at?: string;
  last_heartbeat_at?: string;
  // The resolved effective tags (its direct bindings plus propagating globals),
  // mapped from the API `effective_tags` so the row satisfies the shared tag-facet
  // and TagPills contract, exactly as the component row does.
  tags: Record<string, string>;
};

// nodeLabel is the node's human label: its display_name, falling back to the
// name (the key/estate address) when unset. Used as the blade title and the list
// row label, mirroring how component/system/location present name vs display_name.
// nodeLabel is entityLabel, kept as a named export because the node columns and
// blade title read better with it. The rule itself lives in one place.
export function nodeLabel(n: Node): string {
  return entityLabel(n);
}

// The once-shown enrollment token exchange result. It is deliberately NOT stored
// in the query cache: enrollNode returns it to the caller, which reveals it once
// and drops it.
export type EnrollOutput = { name: string; token: string };

export const NODES_KEY = ["nodes"] as const;

export async function listNodes(): Promise<Node[]> {
  const { data, error } = await api.GET("/nodes");
  if (error) throw error;
  return (data?.nodes ?? []).map((n) => ({ ...n, tags: n.effective_tags ?? {} })) as Node[];
}

export async function getNode(name: string): Promise<Node> {
  const { data, error } = await api.GET("/nodes/{name}", { params: { path: { name } } });
  if (error) throw error;
  return { ...data, tags: data?.effective_tags ?? {} } as Node;
}

export type CreateNode = { name: string; display_name?: string; description?: string; location?: string };

export async function createNode(body: CreateNode): Promise<Node> {
  const { data, error } = await api.POST("/nodes", { body });
  if (error) throw error;
  return data as Node;
}

// The node update patch: only the mutable fields (name is the immutable key). A
// field left undefined is unchanged; location set to "" clears the placement.
export type NodePatch = { display_name?: string; description?: string; location?: string };

export async function updateNode(name: string, body: NodePatch): Promise<Node> {
  const { data, error } = await api.PATCH("/nodes/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return data as Node;
}

export async function deleteNode(name: string): Promise<void> {
  const { error } = await api.DELETE("/nodes/{name}", { params: { path: { name } } });
  if (error) throw error;
}

// enrollNode mints (or re-mints) the node's enrollment token and returns it ONCE.
// The token is a secret: the server stores only its hash and never logs it, and
// this wrapper hands it straight back without caching it. The caller reveals it in
// the show-once modal and clears it on close.
export async function enrollNode(name: string): Promise<EnrollOutput> {
  const { data, error } = await api.POST("/nodes/{name}:enroll", { params: { path: { name } } });
  if (error) throw error;
  return data as EnrollOutput;
}

// The node liveness window mirrors the server's node-down sweep default
// (OMNIGLASS_NODE_DOWN_AFTER, 90s): a node whose last heartbeat predates it is
// swept to `node.down` server-side, so the console pill uses the same threshold.
export const NODE_DOWN_AFTER_MS = 90_000;

export type NodeStatus = "up" | "down" | "never";

// nodeStatus derives the liveness pill from last_heartbeat_at against the down
// window: `never` if it has not checked in, `up` within the window, `down` once
// stale. Pure (now is injectable) so it derives client-side from a real field, no
// fabricated status. The boundary (exactly the window) still reads up.
export function nodeStatus(n: Node, now: number = Date.now()): NodeStatus {
  if (!n.last_heartbeat_at) return "never";
  const age = now - new Date(n.last_heartbeat_at).getTime();
  return age <= NODE_DOWN_AFTER_MS ? "up" : "down";
}

// nodeFilterKeys are the faceted-search fields the shared FilterBar/ListShell
// drives, exactly as the other lists do: name (substring, the default) and status
// (exact, over the derived pill). Matching is client-side over the loaded rows via
// lib/predicate.
export const nodeFilterKeys: FilterKey<Node>[] = [
  { key: "name", type: "string", hint: "substring", get: (n) => `${nodeLabel(n)} ${n.name}` },
  { key: "status", type: "string", hint: "exact", get: (n) => nodeStatus(n), values: () => ["down", "never", "up"] },
  { key: "location", type: "string", hint: "exact", get: (n) => n.location ?? "", values: (rows) => [...new Set(rows.map((r) => r.location).filter(Boolean) as string[])].sort() },
];
