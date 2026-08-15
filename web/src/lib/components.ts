import { api } from "../api/client";

// The components data layer: thin typed wrappers over the generated client, so
// the page stays declarative and the calls are unit-testable against a mocked
// client. Shapes follow the OpenAPI (see api/components.go). Components form a
// tree (parent_id), is placed at a location, and belongs to zero or more systems
// through membership.
// A component's shape comes from the product it is an instance of (product_id),
// whose contract declares the properties every instance exposes.
export type Component = {
  id: string;
  name: string;
  label?: string;
  // The LABEL's pen (#682/#683): true means the platform rendered this display
  // name from a label rule, false means an operator typed it. Read-only; the
  // console reads it through lib/entities so nothing branches on it by hand.
  label_generated?: boolean;
  location?: string;
  // location_id is the location's uuid, the stable handle the tree builder
  // keys and resolves on (#627: name uniqueness is scoped to placement, so
  // two components can now legally share a name and only the uuid tells them
  // apart).
  location_id?: string;
  parent?: string;
  parent_id?: string;
  // The name of the component's primary system, its default when no system is
  // named, and how many it belongs to in total. Derived from membership: a
  // component can be in several, so there is no single pointer to read.
  system?: string;
  system_id?: string;
  system_count: number;
  product_id?: string;
  // The product's name, the display handle beside the uuid product_id.
  product?: string;
  // Whether the platform picked this name (a server-side generator) rather
  // than an operator typing it.
  name_generated?: boolean;
  // path/path_segments/renders (#627 Task 15): the dotted address and its two
  // display-only compact forms, set on a GET or LIST response (empty on a
  // create/update/move/rename/resetName response; a subsequent list refetch
  // fills it).
  path?: string;
  path_segments?: string[];
  renders?: { dash: string; bare: string };
  actions?: string[];
  effective_tags?: Record<string, string>;
};

export const COMPONENTS_KEY = ["components"] as const;

export async function listComponents(): Promise<Component[]> {
  const { data, error } = await api.GET("/components");
  if (error) throw error;
  return (data?.components ?? []) as Component[];
}

export async function getComponent(name: string): Promise<Component> {
  const { data, error } = await api.GET("/components/{name}", { params: { path: { name } } });
  if (error) throw error;
  return data as Component;
}

export type CreateComponent = {
  // Optional (#627 Task 14): omit it and the platform mints "<stem>-<n>" from
  // the classified product's component_type. The console create form still
  // always sends one today (a later task optionalises the field itself); this
  // type just follows the API contract, which the CLI and a direct API caller
  // can already leave blank.
  name?: string;
  label?: string;
  parent?: string;
  system?: string;
  location?: string;
  product?: string;
  // The create form's name precondition (#702, and its review): the NAME the
  // form previewed and locked its name field on. Omitted unless the platform is
  // naming the row. It asserts what the row will be called and never sets it, so
  // the row stays name_generated; the name carries the stem, the suppression and
  // the number, so a type edited under the open form invalidates the claim where
  // the ordinal alone would not. A create that would land a different name is
  // refused with a 409 the form recovers from by re-reading the draft.
  expected_name?: string;
};

export async function createComponent(body: CreateComponent): Promise<Component> {
  const { data, error } = await api.POST("/components", { body });
  if (error) throw error;
  return data as Component;
}

export type UpdateComponent = {
  label?: string;
};

export async function updateComponent(name: string, body: UpdateComponent): Promise<Component> {
  const { data, error } = await api.PATCH("/components/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return data as Component;
}

// A rename is its own call, not a field on the patch body. The API gates it with
// `<resource>:rename` rather than `<resource>:update`, because moving a name breaks
// stored references and is worth being able to withhold on its own.
export async function renameComponent(name: string, to: string): Promise<Component> {
  const { data, error } = await api.POST("/components/{name}:rename", { params: { path: { name } }, body: { name: to } });
  if (error) throw error;
  return data as Component;
}

// resetComponentName hands the pen back to the platform (#627 Task 15):
// regenerates the name from the component's current type and placement and
// marks name_generated, whether or not it already was. Gated by
// component:rename, the same token :rename uses.
export async function resetComponentName(name: string): Promise<Component> {
  const { data, error } = await api.POST("/components/{name}:resetName", { params: { path: { name } } });
  if (error) throw error;
  return data as Component;
}

export type NameCheck = { valid: boolean; available: boolean; reason?: string };

// checkComponentName checks availability against a placement bucket (#627:
// name uniqueness is scoped to placement, not the whole estate), the same
// one CreateComponent resolves into: parent wins over location, and neither
// means the unplaced/root bucket. A rename check passes the component's OWN
// current placement, since a rename does not move it.
export async function checkComponentName(name: string, parent?: string, location?: string): Promise<NameCheck> {
  const { data, error } = await api.POST("/components:checkName", { body: { name, parent, location } });
  if (error) throw error;
  return data as NameCheck;
}

export async function deleteComponent(name: string): Promise<void> {
  const { error } = await api.DELETE("/components/{name}", { params: { path: { name } } });
  if (error) throw error;
}
