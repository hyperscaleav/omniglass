import { api } from "../api/client";
import type { ClassifierKind } from "./classifier_properties";

// The classifier-metrics data layer: the metric lane of lib/classifier_properties,
// thin typed wrappers over the generated client for the declared-METRIC contract
// of the same three classifiers. A contract line names a catalog metric every
// owner of the classifier carries, with an optional numeric default and a
// required flag:
//
//   product       declares for a component
//   standard      declares for a system
//   location type declares for a location
//
// An owner resolves each declared metric to its series' latest sample, or to the
// default here until one arrives; a metric's values are telemetry, so there is
// no per-owner "set" the way a property has.
//
// A line is addressed by (classifier id, metric name), so the write is a PUT and
// is idempotent: declaring a metric the classifier already declares revises the
// line in place. Official (seed-owned) classifiers are read-only, refused
// server-side.

export type ClassifierMetric = {
  metric_type_name: string;
  metric_type_id: string;
  // The contract default, a number shaped by the metric's data_type; omitted
  // when the contract sets none.
  default_value?: unknown;
  required: boolean;
};

export type SetClassifierMetric = {
  // Omit for no default; the value is validated server-side against the metric's
  // data_type, so the caller coerces the text input before sending it.
  default_value?: unknown;
  required: boolean;
};

// One cache namespace per classifier kind, so two classifiers that share an id
// never collide, and separate from the property lane's namespace.
export const classifierMetricsKey = (kind: ClassifierKind, id: string) => [`${kind}-metrics`, id] as const;

export async function classifierMetrics(kind: ClassifierKind, id: string): Promise<ClassifierMetric[]> {
  const res =
    kind === "standard"
      ? await api.GET("/standards/{id}/metrics", { params: { path: { id } } })
      : kind === "location-type"
        ? await api.GET("/location-types/{id}/metrics", { params: { path: { id } } })
        : await api.GET("/products/{id}/metrics", { params: { path: { id } } });
  if (res.error) throw res.error;
  return (res.data?.metrics ?? []) as ClassifierMetric[];
}

export async function setClassifierMetric(
  kind: ClassifierKind,
  id: string,
  metric: string,
  body: SetClassifierMetric,
): Promise<ClassifierMetric> {
  const path = { id, metric };
  const res =
    kind === "standard"
      ? await api.PUT("/standards/{id}/metrics/{metric}", { params: { path }, body })
      : kind === "location-type"
        ? await api.PUT("/location-types/{id}/metrics/{metric}", { params: { path }, body })
        : await api.PUT("/products/{id}/metrics/{metric}", { params: { path }, body });
  if (res.error) throw res.error;
  return res.data as ClassifierMetric;
}

// deleteClassifierMetric withdraws one line from the contract. Owners keep any
// samples their series already hold, now off-contract.
export async function deleteClassifierMetric(kind: ClassifierKind, id: string, metric: string): Promise<void> {
  const path = { id, metric };
  const res =
    kind === "standard"
      ? await api.DELETE("/standards/{id}/metrics/{metric}", { params: { path } })
      : kind === "location-type"
        ? await api.DELETE("/location-types/{id}/metrics/{metric}", { params: { path } })
        : await api.DELETE("/products/{id}/metrics/{metric}", { params: { path } });
  if (res.error) throw res.error;
}
