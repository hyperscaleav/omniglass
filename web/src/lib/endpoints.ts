import { api } from "../api/client";
import { entityLabel } from "./entities";
import type { FilterKey } from "./predicate";

// The endpoints data layer: thin typed wrappers over the generated client, so
// the surfaces stay declarative and unit-testable against a mocked fetch.
// Shapes follow the OpenAPI (see api/endpoints.go, endpointBody). An endpoint is
// a connection on a component (or a server-hosted one): a transport (icmp, tcp,
// ...), an optional node placement, and address/target settings in params (jsonb).
export type EndpointParams = { target?: string; port?: number | string } & Record<string, unknown>;

export type Endpoint = {
  id: string;
  name: string;
  // The friendly string an operator reads, and the only identity string an
  // operator types on an endpoint: the name is derived from the transport, so
  // two ssh endpoints on one component are told apart by this and nothing else.
  label?: string;
  transport: string;
  component?: string;
  // component_id is the owning component's uuid, the stable handle
  // (component names are scoped to placement, #627 Task 10, so a name
  // comparison across an unrelated component list is not reliably unique).
  component_id?: string;
  node?: string;
  params?: EndpointParams;
};

export const ENDPOINTS_KEY = ["endpoints"] as const;

// The transports the platform speaks: a code registry the binary ships
// (ADR-0073), read from GET /transports rather than restated here, so the
// picker can never drift from what the server refuses.
export type Transport = { name: string; description: string; held: boolean; built: boolean };

export const TRANSPORTS_KEY = ["transports"] as const;

export async function listTransports(): Promise<Transport[]> {
  const { data, error } = await api.GET("/transports");
  if (error) throw error;
  return (data?.transports ?? []) as Transport[];
}

// endpointTarget renders an endpoint's probed address from its params, mirroring
// the reachability read's addressFromParams: the target, with :port appended when
// params carry a separate one. Empty when there is no target (real field only).
export function endpointTarget(i: Endpoint): string {
  const t = i.params?.target;
  if (!t) return "";
  const p = i.params?.port;
  return p !== undefined && p !== "" ? `${t}:${p}` : t;
}

export async function listEndpoints(): Promise<Endpoint[]> {
  const { data, error } = await api.GET("/endpoints");
  if (error) throw error;
  return (data?.endpoints ?? []) as Endpoint[];
}

export async function getEndpoint(id: string): Promise<Endpoint> {
  const { data, error } = await api.GET("/endpoints/{id}", { params: { path: { id } } });
  if (error) throw error;
  return data as Endpoint;
}

// The endpoint is transport-named: its name is DERIVED server-side from its
// transport, so the create body carries no name. It carries a LABEL, though, and
// that is the point: the label is the only identity string an operator types
// here, so it has to be settable at create rather than on a following patch (#613).
export type CreateEndpoint = {
  transport: string;
  label?: string;
  component?: string;
  node?: string;
  params?: EndpointParams;
};

export async function createEndpoint(body: CreateEndpoint): Promise<Endpoint> {
  const { data, error } = await api.POST("/endpoints", { body });
  if (error) throw error;
  return data as Endpoint;
}

// The node placement, the params and the label are mutable after creation (name,
// transport, and owning component are set at creation). An empty label clears it;
// omitting it leaves it alone. Addressed by the surrogate id.
export type UpdateEndpoint = { node?: string; params?: EndpointParams; label?: string };

export async function updateEndpoint(id: string, body: UpdateEndpoint): Promise<Endpoint> {
  const { data, error } = await api.PATCH("/endpoints/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data as Endpoint;
}

export async function deleteEndpoint(id: string): Promise<void> {
  const { error } = await api.DELETE("/endpoints/{id}", { params: { path: { id } } });
  if (error) throw error;
}

// The faceted-search fields the shared FilterBar/ListShell drives: name (substring,
// the default), transport (exact), and component (exact). Matching is client-side
// over the loaded rows via lib/predicate.
export const endpointFilterKeys: FilterKey<Endpoint>[] = [
  { key: "name", type: "string", hint: "substring", get: (i) => `${entityLabel(i)} ${i.name}` },
  { key: "transport", type: "string", hint: "exact", get: (i) => i.transport, values: (rows) => [...new Set(rows.map((r) => r.transport))].sort() },
  { key: "component", type: "string", hint: "exact", get: (i) => i.component ?? "", values: (rows) => [...new Set(rows.map((r) => r.component).filter(Boolean) as string[])].sort() },
];
