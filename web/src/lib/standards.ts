import { api } from "../api/client";

// The standards data layer: thin typed wrappers over the generated client. A
// standard is the blueprint a system conforms to, the system-side counterpart of a
// product: it names a shape ("Meeting room", "Huddle space") and declares the
// property contract every system conforming to it exposes. A standard may be a
// VARIANT of another (parent_standard_id), and a system need not conform to one at
// all (a one-off system is first class).
//
// Official (seed-owned) standards are read-only; custom standards are operator
// created. This module carries the registry itself; the declared-property contract
// is the classifier-generic layer in lib/classifier_properties.
export type Standard = {
  id: string;
  display_name: string;
  official: boolean;
  parent_standard_id?: string;
};

export const STANDARDS_KEY = ["standards"] as const;

export async function listStandards(): Promise<Standard[]> {
  const { data, error } = await api.GET("/standards");
  if (error) throw error;
  return (data?.standards ?? []) as Standard[];
}

export type CreateStandard = {
  id: string;
  display_name: string;
  parent_standard_id?: string;
};

export async function createStandard(body: CreateStandard): Promise<Standard> {
  const { data, error } = await api.POST("/standards", { body });
  if (error) throw error;
  return data as Standard;
}

export type UpdateStandard = {
  display_name?: string;
  parent_standard_id?: string;
};

export async function updateStandard(id: string, body: UpdateStandard): Promise<Standard> {
  const { data, error } = await api.PATCH("/standards/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data as Standard;
}

export async function deleteStandard(id: string): Promise<void> {
  const { error } = await api.DELETE("/standards/{id}", { params: { path: { id } } });
  if (error) throw error;
}
