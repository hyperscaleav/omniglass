import { api } from "../api/client";

// The products data layer: thin typed wrappers over the generated client for the
// product catalog (the model a component is an instance of, e.g. "Crestron
// TSW-1070"). A product is addressed by its id (a kebab id, the create-only
// address); official (seed-owned) rows are read-only past creation, refused
// server-side on update/delete. A product carries a kind
// (device/app/service/vm), an optional vendor and driver, an optional parent
// product, and a set of capability ids it exposes.

export type ProductKind = "device" | "app" | "service" | "vm";

export type Product = {
  id: string;
  display_name: string;
  kind: ProductKind;
  vendor_id?: string;
  driver_id?: string;
  parent_product_id?: string;
  capabilities: string[];
  official: boolean;
};

export const PRODUCTS_KEY = ["products"] as const;

export async function listProducts(): Promise<Product[]> {
  const { data, error } = await api.GET("/products");
  if (error) throw error;
  return (data?.products ?? []) as Product[];
}

export type CreateProduct = {
  id: string;
  display_name: string;
  kind: ProductKind;
  vendor_id?: string;
  driver_id?: string;
  parent_product_id?: string;
  capabilities?: string[];
};

export async function createProduct(body: CreateProduct): Promise<Product> {
  const { data, error } = await api.POST("/products", { body });
  if (error) throw error;
  return data as Product;
}

export type UpdateProduct = {
  display_name?: string;
  kind?: ProductKind;
  vendor_id?: string;
  driver_id?: string;
  parent_product_id?: string;
  capabilities?: string[];
};

export async function updateProduct(id: string, body: UpdateProduct): Promise<Product> {
  const { data, error } = await api.PATCH("/products/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data as Product;
}

export async function deleteProduct(id: string): Promise<void> {
  const { error } = await api.DELETE("/products/{id}", { params: { path: { id } } });
  if (error) throw error;
}
