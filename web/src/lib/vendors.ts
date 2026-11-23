import { api } from "../api/client";
import type { CatalogIdRef } from "./catalog_key";

// The vendors data layer: thin typed wrappers over the generated client for
// the vendor catalog (the vendor picker on the product form). A vendor is
// addressed by its id (a kebab id, the create-only address); official
// (seed-owned) rows are read-only past creation, refused server-side on
// update/delete. Each vendor carries a kind
// (manufacturer/integrator/developer).

export type VendorKind = "manufacturer" | "integrator" | "developer";

export type Vendor = {
  // The namespace the row lives in (ADR-0318): og shipped, local the operator's.
  namespace: string;
  id: string;
  name: string;
  label: string;
  kind: VendorKind;
  official: boolean;
  icon?: string;
  support_phone?: string;
  website?: string;
};

export const VENDORS_KEY = ["vendors"] as const;

export async function listVendors(): Promise<Vendor[]> {
  const { data, error } = await api.GET("/vendors");
  if (error) throw error;
  return (data?.vendors ?? []) as Vendor[];
}

export type CreateVendor = {
  // The name. The uuid is the database\'s to mint.
  name: string;
  label: string;
  kind: VendorKind;
  icon?: string;
  support_phone?: string;
  website?: string;
};

export async function createVendor(body: CreateVendor): Promise<Vendor> {
  const { data, error } = await api.POST("/vendors", { body });
  if (error) throw error;
  return data as Vendor;
}

export type UpdateVendor = {
  label?: string;
  kind?: VendorKind;
  icon?: string;
  support_phone?: string;
  website?: string;
};

export async function updateVendor(ref: CatalogIdRef, body: UpdateVendor): Promise<Vendor> {
  const { data, error } = await api.PATCH("/vendors/{namespace}/{id}", { params: { path: { namespace: ref.namespace, id: ref.id } }, body });
  if (error) throw error;
  return data as Vendor;
}

export async function deleteVendor(ref: CatalogIdRef): Promise<void> {
  const { error } = await api.DELETE("/vendors/{namespace}/{id}", { params: { path: { namespace: ref.namespace, id: ref.id } } });
  if (error) throw error;
}
