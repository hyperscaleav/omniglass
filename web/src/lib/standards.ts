import { api } from "../api/client";
import type { CatalogIdRef } from "./catalog_key";

// The standards data layer: thin typed wrappers over the generated client. A
// standard is the contract a system is built to, the system-side counterpart of a
// product: it names a shape ("Meeting room", "Huddle space") and declares the
// property contract every system conforming to it exposes. A standard may be a
// VARIANT of another (parent_standard_id), and a system need not conform to one at
// all (a one-off system is first class).
//
// Official (seed-owned) standards are read-only; custom standards are operator
// created. This module carries the catalog itself; the declared-property contract
// is the classifier-generic layer in lib/classifier_properties.
export type Standard = {
  // The namespace the row lives in (ADR-0318): og shipped, local the operator's.
  namespace: string;
  id: string;
  name: string;
  label: string;
  official: boolean;
  parent_standard?: string;
  parent_standard_id?: string;
};

export const STANDARDS_KEY = ["standards"] as const;

export async function listStandards(): Promise<Standard[]> {
  const { data, error } = await api.GET("/standards");
  if (error) throw error;
  return (data?.standards ?? []) as Standard[];
}

export type CreateStandard = {
  // The name. The uuid is the database\'s to mint.
  name: string;
  label: string;
  parent_standard_id?: string;
};

export async function createStandard(body: CreateStandard): Promise<Standard> {
  const { data, error } = await api.POST("/standards", { body });
  if (error) throw error;
  return data as Standard;
}

export type UpdateStandard = {
  label?: string;
  parent_standard_id?: string;
};

export async function updateStandard(ref: CatalogIdRef, body: UpdateStandard): Promise<Standard> {
  const { data, error } = await api.PATCH("/standards/{namespace}/{id}", { params: { path: { namespace: ref.namespace, id: ref.id } }, body });
  if (error) throw error;
  return data as Standard;
}

export async function deleteStandard(ref: CatalogIdRef): Promise<void> {
  const { error } = await api.DELETE("/standards/{namespace}/{id}", { params: { path: { namespace: ref.namespace, id: ref.id } } });
  if (error) throw error;
}
