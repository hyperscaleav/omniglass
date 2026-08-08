import { api } from "../api/client";

// The systems data layer: thin typed wrappers over the generated client. Systems
// form a tree (parent_id) and are placed at a location. Components reference a
// primary system by id; the Components page consumes this list to resolve those
// ids to readable names and to populate the system picker.
//
// A system optionally conforms to a STANDARD (the blueprint that declares its
// property contract, the system-side counterpart of a product). It is optional: a
// one-off system that conforms to no standard is first class.
export type System = {
  id: string;
  name: string;
  display_name?: string;
  // The standard it conforms to, in both forms (api/systems.go): standard is
  // the name an operator reads, standard_id the uuid it resolves to.
  standard?: string;
  standard_id?: string;
  location?: string;
  // location_id and parent_id are the placement's uuids, the stable handles
  // the tree builder keys and resolves on (#627: name uniqueness is scoped
  // to placement, so two systems can now legally share a name and only the
  // uuid tells them apart).
  location_id?: string;
  parent?: string;
  parent_id?: string;
  // How many components are bound into this system, from membership rather than
  // any pointer on the component: membership is what says a component is in a
  // system, and reading it elsewhere is how a fully staffed system reported zero.
  member_count: number;
  actions?: string[];
  effective_tags?: Record<string, string>;
};

export const SYSTEMS_KEY = ["systems"] as const;

export async function listSystems(): Promise<System[]> {
  const { data, error } = await api.GET("/systems");
  if (error) throw error;
  return (data?.systems ?? []) as System[];
}

export async function getSystem(name: string): Promise<System> {
  const { data, error } = await api.GET("/systems/{name}", { params: { path: { name } } });
  if (error) throw error;
  return data as System;
}

export type CreateSystem = {
  name: string;
  // Omit for a one-off system that conforms to no standard.
  standard_id?: string;
  display_name?: string;
  parent?: string;
  location?: string;
};

export async function createSystem(body: CreateSystem): Promise<System> {
  const { data, error } = await api.POST("/systems", { body });
  if (error) throw error;
  return data as System;
}

export type UpdateSystem = {
  display_name?: string;
  standard_id?: string;
};

export async function updateSystem(name: string, body: UpdateSystem): Promise<System> {
  const { data, error } = await api.PATCH("/systems/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return data as System;
}

// A rename is its own call, not a field on the patch body. The API gates it with
// `<resource>:rename` rather than `<resource>:update`, because moving a name breaks
// stored references and is worth being able to withhold on its own.
export async function renameSystem(name: string, to: string): Promise<System> {
  const { data, error } = await api.POST("/systems/{name}:rename", { params: { path: { name } }, body: { name: to } });
  if (error) throw error;
  return data as System;
}

export type NameCheck = { valid: boolean; available: boolean; reason?: string };

// checkSystemName checks availability against a placement bucket (#627: name
// uniqueness is scoped to placement, not the whole estate), the same one
// CreateSystem resolves into: parent wins over location, and neither means
// the unplaced/root bucket. A rename check passes the system's OWN current
// placement, since a rename does not move it.
export async function checkSystemName(name: string, parent?: string, location?: string): Promise<NameCheck> {
  const { data, error } = await api.POST("/systems:checkName", { body: { name, parent, location } });
  if (error) throw error;
  return data as NameCheck;
}

export async function deleteSystem(name: string): Promise<void> {
  const { error } = await api.DELETE("/systems/{name}", { params: { path: { name } } });
  if (error) throw error;
}
