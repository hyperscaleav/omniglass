import { api } from "../api/client";
import { type ComponentType, resolveComponentTypeIcon } from "./component_types";

// The products data layer: thin typed wrappers over the generated client for the
// product catalog (the model a component is an instance of, e.g. "Crestron
// TSW-1070"). A product is addressed by its kebab name (ADR-0062: the uuid is
// identity, the name is what an operator reads and types); official
// (seed-owned) rows are read-only past creation, refused server-side on
// update/delete. A product carries a kind (device/app/service; vm retired,
// folded into app, ADR-0086), a required component_type (the device-class
// genus every product is classified under, ADR-0085; also what a system
// role's typed-slot guard checks, #626), an optional vendor and driver, and
// an optional parent product.

export type ProductKind = "device" | "app" | "service";

// References arrive in both forms (api/products.go): vendor/driver/
// parent_product/component_type are the name an operator reads,
// vendor_id/driver_id/parent_product_id/component_type_id the uuids they
// resolve to.
export type Product = {
  id: string;
  name: string;
  display_name: string;
  kind: ProductKind;
  component_type: string;
  component_type_id: string;
  vendor?: string;
  vendor_id?: string;
  driver?: string;
  driver_id?: string;
  parent_product?: string;
  parent_product_id?: string;
  official: boolean;
  // A per-SKU override on the component_type's icon; unset inherits it.
  icon?: string;
};

// resolveProductIcon: a product's own icon override, falling back to the
// icon its component_type resolves to (walking the type's inheritance
// chain). "The icon lives on the type, products may override" (#614): the
// device glyph belongs at the level that spans products, so an unset
// override reads the genus, and a set one wins outright.
export function resolveProductIcon(product: { icon?: string; component_type?: string }, typesByName: Map<string, ComponentType>): string {
  return product.icon || resolveComponentTypeIcon(product.component_type, typesByName);
}

export const PRODUCTS_KEY = ["products"] as const;

export async function listProducts(): Promise<Product[]> {
  const { data, error } = await api.GET("/products");
  if (error) throw error;
  return (data?.products ?? []) as Product[];
}

export type CreateProduct = {
  // The name. The uuid is the database\'s to mint.
  name: string;
  display_name: string;
  kind: ProductKind;
  // The component_type this product is classified under, by name or uuid;
  // every product must belong to one of the tree's nodes (the generics fit
  // anything not yet modeled more specifically).
  component_type: string;
  vendor_id?: string;
  driver_id?: string;
  parent_product_id?: string;
  // A per-SKU icon override; unset inherits the component_type's icon.
  icon?: string;
};

export async function createProduct(body: CreateProduct): Promise<Product> {
  const { data, error } = await api.POST("/products", { body });
  if (error) throw error;
  return data as Product;
}

export type UpdateProduct = {
  display_name?: string;
  kind?: ProductKind;
  // Reclassifies the product; required by the API once named, so this only
  // reclassifies, it never clears.
  component_type?: string;
  vendor_id?: string;
  driver_id?: string;
  parent_product_id?: string;
  icon?: string;
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
