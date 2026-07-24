import { api } from "../api/client";

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
// matching the server routes (a location type's contract is part of the type
// registry, so it is gated by type:*, not location:*).
export const CLASSIFIER_RESOURCE: Record<ClassifierKind, string> = {
  product: "product",
  standard: "standard",
  "location-type": "type",
};

export async function classifierProperties(kind: ClassifierKind, id: string): Promise<ClassifierProperty[]> {
  const res =
    kind === "standard"
      ? await api.GET("/standards/{id}/properties", { params: { path: { id } } })
      : kind === "location-type"
        ? await api.GET("/location-types/{id}/properties", { params: { path: { id } } })
        : await api.GET("/products/{id}/properties", { params: { path: { id } } });
  if (res.error) throw res.error;
  return (res.data?.properties ?? []) as ClassifierProperty[];
}

export async function setClassifierProperty(
  kind: ClassifierKind,
  id: string,
  property: string,
  body: SetClassifierProperty,
): Promise<ClassifierProperty> {
  const path = { id, property };
  const res =
    kind === "standard"
      ? await api.PUT("/standards/{id}/properties/{property}", { params: { path }, body })
      : kind === "location-type"
        ? await api.PUT("/location-types/{id}/properties/{property}", { params: { path }, body })
        : await api.PUT("/products/{id}/properties/{property}", { params: { path }, body });
  if (res.error) throw res.error;
  return res.data as ClassifierProperty;
}

// deleteClassifierProperty withdraws one line from the contract. Owners keep any
// value they already set for it, now off-contract.
export async function deleteClassifierProperty(kind: ClassifierKind, id: string, property: string): Promise<void> {
  const path = { id, property };
  const res =
    kind === "standard"
      ? await api.DELETE("/standards/{id}/properties/{property}", { params: { path } })
      : kind === "location-type"
        ? await api.DELETE("/location-types/{id}/properties/{property}", { params: { path } })
        : await api.DELETE("/products/{id}/properties/{property}", { params: { path } });
  if (res.error) throw res.error;
}
