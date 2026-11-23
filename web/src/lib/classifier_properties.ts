import { api } from "../api/client";
import type { CatalogIdRef, CatalogNameRef } from "./catalog_key";

// The classifier-properties data layer: thin typed wrappers over the generated
// client for the CONTRACT side of the property model, generic over the three
// classifiers that declare one. A contract is the set of catalog properties every
// owner of the classifier exposes, each line carrying an optional default and a
// required flag:
//
//   product       declares for a component
//   standard      declares for a system
//   location type declares for a location
//
// An owner resolves each declared property to its own override, or to the default
// here; required means the owner must resolve it to a non-null value.
//
// A line is addressed by (classifier id, property name), so the write is a PUT and
// is idempotent: declaring a property the classifier already declares revises the
// line in place. Official (seed-owned) classifiers are read-only, refused
// server-side.

export type ClassifierKind = "product" | "standard" | "location-type";

export type ClassifierProperty = {
  property_type_name: string;
  property_type_id: string;
  // The contract default, shaped by the property's data_type; omitted when the
  // contract sets none.
  default_value?: unknown;
  required: boolean;
};

export type SetClassifierProperty = {
  // Omit for no default; the value is validated server-side against the property's
  // data_type, so the caller coerces the text input before sending it.
  default_value?: unknown;
  required: boolean;
};

// One cache namespace per classifier kind, so two classifiers that share an id
// never collide.
export const classifierPropertiesKey = (kind: ClassifierKind, id: string) => [`${kind}-properties`, id] as const;

// The authorization resource each classifier's contract writes are gated by,
// matching the server routes (a location type's contract is part of the
// location_type catalog, so it is gated by location_type:*, not location:*).
export const CLASSIFIER_RESOURCE: Record<ClassifierKind, string> = {
  product: "product",
  standard: "standard",
  "location-type": "location_type",
};

export async function classifierProperties(kind: ClassifierKind, parent: CatalogIdRef): Promise<ClassifierProperty[]> {
  const res =
    kind === "standard"
      ? await api.GET("/standards/{namespace}/{id}/properties", { params: { path: { namespace: parent.namespace, id: parent.id } } })
      : kind === "location-type"
        ? await api.GET("/location-types/{namespace}/{id}/properties", { params: { path: { namespace: parent.namespace, id: parent.id } } })
        : await api.GET("/products/{namespace}/{id}/properties", { params: { path: { namespace: parent.namespace, id: parent.id } } });
  if (res.error) throw res.error;
  return (res.data?.properties ?? []) as ClassifierProperty[];
}

export async function setClassifierProperty(
  kind: ClassifierKind,
  parent: CatalogIdRef,
  property: CatalogNameRef,
  body: SetClassifierProperty,
): Promise<ClassifierProperty> {
  const path = { namespace: parent.namespace, id: parent.id, property_namespace: property.namespace, property: property.name };
  const res =
    kind === "standard"
      ? await api.PUT("/standards/{namespace}/{id}/properties/{property_namespace}/{property}", { params: { path }, body })
      : kind === "location-type"
        ? await api.PUT("/location-types/{namespace}/{id}/properties/{property_namespace}/{property}", { params: { path }, body })
        : await api.PUT("/products/{namespace}/{id}/properties/{property_namespace}/{property}", { params: { path }, body });
  if (res.error) throw res.error;
  return res.data as ClassifierProperty;
}

// deleteClassifierProperty withdraws one line from the contract. Owners keep any
// value they already set for it, now off-contract.
export async function deleteClassifierProperty(kind: ClassifierKind, parent: CatalogIdRef, property: CatalogNameRef): Promise<void> {
  const path = { namespace: parent.namespace, id: parent.id, property_namespace: property.namespace, property: property.name };
  const res =
    kind === "standard"
      ? await api.DELETE("/standards/{namespace}/{id}/properties/{property_namespace}/{property}", { params: { path } })
      : kind === "location-type"
        ? await api.DELETE("/location-types/{namespace}/{id}/properties/{property_namespace}/{property}", { params: { path } })
        : await api.DELETE("/products/{namespace}/{id}/properties/{property_namespace}/{property}", { params: { path } });
  if (res.error) throw res.error;
}
