import { api } from "../api/client";
import type { FilterKey } from "./predicate";

// The interfaces data layer: thin typed wrappers over the generated client, so the
// Interfaces page stays declarative and unit-testable against a mocked fetch.
// Shapes follow the OpenAPI (see api/interfaces.go, InterfaceBody). An interface is
// a connection endpoint on a component (or a server-hosted one): a type (icmp, tcp,
// ...), an optional node placement, and endpoint/target settings in params (jsonb).
export type InterfaceParams = { target?: string; port?: number | string } & Record<string, unknown>;

export type Interface = {
  id: string;
  name: string;
  interface_type: string;
  interface_type_id?: string;
  component?: string;
  node?: string;
  params?: InterfaceParams;
};

export const INTERFACES_KEY = ["interfaces"] as const;

// The built interface types this slice ships. There is no interface_type list
// endpoint, so the type picker offers these rather than a free-text field; a future
// GET /interface-types registry route can replace this constant.
export const INTERFACE_TYPES = ["icmp", "tcp"] as const;

// interfaceTarget renders an interface's probed endpoint from its params, mirroring
// the reachability read's endpointFromParams: the target, with :port appended when
// params carry a separate one. Empty when there is no target (real field only).
export function interfaceTarget(i: Interface): string {
  const t = i.params?.target;
  if (!t) return "";
  const p = i.params?.port;
  return p !== undefined && p !== "" ? `${t}:${p}` : t;
}

export async function listInterfaces(): Promise<Interface[]> {
  const { data, error } = await api.GET("/interfaces");
  if (error) throw error;
  return (data?.interfaces ?? []) as Interface[];
}

export async function getInterface(id: string): Promise<Interface> {
  const { data, error } = await api.GET("/interfaces/{id}", { params: { path: { id } } });
  if (error) throw error;
  return data as Interface;
}

// The interface is protocol-named: its name is DERIVED server-side from its type,
// so the create body carries no name.
export type CreateInterface = {
  interface_type: string;
  component?: string;
  node?: string;
  params?: InterfaceParams;
};

export async function createInterface(body: CreateInterface): Promise<Interface> {
  const { data, error } = await api.POST("/interfaces", { body });
  if (error) throw error;
  return data as Interface;
}

// Only the node placement and the params are mutable after creation (name, type,
// and owning component are set at creation). Addressed by the surrogate id.
export type UpdateInterface = { node?: string; params?: InterfaceParams };

export async function updateInterface(id: string, body: UpdateInterface): Promise<Interface> {
  const { data, error } = await api.PATCH("/interfaces/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data as Interface;
}

export async function deleteInterface(id: string): Promise<void> {
  const { error } = await api.DELETE("/interfaces/{id}", { params: { path: { id } } });
  if (error) throw error;
}

// The faceted-search fields the shared FilterBar/ListShell drives: name (substring,
// the default), type (exact, over the built types), and component (exact). Matching
// is client-side over the loaded rows via lib/predicate.
export const interfaceFilterKeys: FilterKey<Interface>[] = [
  { key: "name", type: "string", hint: "substring", get: (i) => i.name },
  { key: "type", type: "string", hint: "exact", get: (i) => i.interface_type, values: (rows) => [...new Set(rows.map((r) => r.interface_type))].sort() },
  { key: "component", type: "string", hint: "exact", get: (i) => i.component ?? "", values: (rows) => [...new Set(rows.map((r) => r.component).filter(Boolean) as string[])].sort() },
];
