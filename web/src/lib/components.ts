import { api } from "../api/client";

// The components data layer: thin typed wrappers over the generated client, so
// the page stays declarative and the calls are unit-testable against a mocked
// client. Shapes follow the OpenAPI (see api/components.go). Components form a
// tree (parent_id), and each is bound to a primary system and a location by id.
export type Component = {
  id: string;
  name: string;
  display_name?: string;
  component_type: string;
  location_id?: string;
  parent_id?: string;
  system_id?: string;
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
  component_type: string;
  display_name?: string;
  parent?: string;
  system?: string;
  location?: string;
};

export async function createComponent(body: CreateComponent): Promise<Component> {
  const { data, error } = await api.POST("/components", { body });
  if (error) throw error;
  return data as Component;
}

export type UpdateComponent = {
  display_name?: string;
  component_type?: string;
};

export async function updateComponent(name: string, body: UpdateComponent): Promise<Component> {
  const { data, error } = await api.PATCH("/components/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return data as Component;
}

export async function deleteComponent(name: string): Promise<void> {
  const { error } = await api.DELETE("/components/{name}", { params: { path: { name } } });
  if (error) throw error;
}
