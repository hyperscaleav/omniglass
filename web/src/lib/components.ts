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
  display_name?: string;
  location?: string;
  parent?: string;
  // The name of the component's primary system, its default when no system is
  // named, and how many it belongs to in total. Derived from membership: a
  // component can be in several, so there is no single pointer to read.
  system?: string;
  system_count: number;
  product_id?: string;
  // The product's name, the display handle beside the uuid product_id.
  product?: string;
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
  name: string;
  display_name?: string;
  parent?: string;
  system?: string;
  location?: string;
  product?: string;
};

export async function createComponent(body: CreateComponent): Promise<Component> {
  const { data, error } = await api.POST("/components", { body });
  if (error) throw error;
  return data as Component;
}

export type UpdateComponent = {
  name?: string;
  display_name?: string;
};

export async function updateComponent(name: string, body: UpdateComponent): Promise<Component> {
  const { data, error } = await api.PATCH("/components/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return data as Component;
}

export type NameCheck = { valid: boolean; available: boolean; reason?: string };

export async function checkComponentName(name: string): Promise<NameCheck> {
  const { data, error } = await api.POST("/components:checkName", { body: { name } });
  if (error) throw error;
  return data as NameCheck;
}

export async function deleteComponent(name: string): Promise<void> {
  const { error } = await api.DELETE("/components/{name}", { params: { path: { name } } });
  if (error) throw error;
}
