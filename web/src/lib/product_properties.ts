import { api } from "../api/client";

// The product declared-properties data layer: thin typed wrappers over the
// generated client for a product's contract. The contract is the set of catalog
// properties every instance of the product exposes, each line carrying an optional
// default and a required flag. A component of the product resolves each declared
// property to its own override, or to the default here; required means the
// component must resolve it to a non-null value.
//
// A line is addressed by (product id, property name), so the write is a PUT and is
// idempotent: declaring a property the product already declares revises the line in
// place. Official (seed-owned) products are read-only, refused server-side.

export type ProductProperty = {
  property_name: string;
  // The contract default, shaped by the property's data_type; omitted when the
  // contract sets none.
  default_value?: unknown;
  required: boolean;
};

export const productPropertiesKey = (id: string) => ["product-properties", id] as const;

export async function productProperties(id: string): Promise<ProductProperty[]> {
  const { data, error } = await api.GET("/products/{id}/properties", { params: { path: { id } } });
  if (error) throw error;
  return (data?.properties ?? []) as ProductProperty[];
}

export type SetProductProperty = {
  // Omit for no default; the value is validated server-side against the property's
  // data_type, so the caller coerces the text input before sending it.
  default_value?: unknown;
  required: boolean;
};

export async function setProductProperty(id: string, property: string, body: SetProductProperty): Promise<ProductProperty> {
  const { data, error } = await api.PUT("/products/{id}/properties/{property}", {
    params: { path: { id, property } },
    body,
  });
  if (error) throw error;
  return data as ProductProperty;
}

// deleteProductProperty withdraws one line from the contract. Instances keep any
// value they already set for it, now off-contract.
export async function deleteProductProperty(id: string, property: string): Promise<void> {
  const { error } = await api.DELETE("/products/{id}/properties/{property}", {
    params: { path: { id, property } },
  });
  if (error) throw error;
}
