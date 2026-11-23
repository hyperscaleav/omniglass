import { api } from "../api/client";
import type { CatalogNameRef } from "./catalog_key";

// The Metrics catalog data layer: thin typed wrappers over the /metric-types
// surface, the numeric lane of the signal catalog and the twin of
// lib/properties. A metric type is a canonical numeric series a sample measures,
// carrying the numeric facts the property lane never holds: a display unit (ms,
// dB, percent) and a precision (decimal places a rendered value keeps). Official
// metric types are seed-owned and read-only; custom ones are operator-created.

export type MetricDataType = "int" | "float";

export const METRIC_DATA_TYPES: MetricDataType[] = ["int", "float"];

export type MetricRow = {
  // The namespace the row lives in (ADR-0318): og shipped, local the operator's.
  namespace: string;
  name: string;
  data_type: string;
  label?: string;
  description?: string;
  unit?: string;
  precision?: number;
  official: boolean;
  // The calc rule that writes this key, when one does (#1019).
  written_by?: { rule: string; rule_id: string };
};

export const METRICS_KEY = ["metric_types"] as const;

export async function listMetricTypes(): Promise<MetricRow[]> {
  const { data, error } = await api.GET("/metric-types");
  if (error) throw error;
  return (data?.metric_types ?? []) as MetricRow[];
}

export type CreateMetricType = {
  name: string;
  data_type: MetricDataType;
  label?: string;
  description?: string;
  unit?: string;
  precision?: number;
};

export async function createMetricType(body: CreateMetricType): Promise<MetricRow> {
  const { data, error } = await api.POST("/metric-types", { body });
  if (error) throw error;
  return data as MetricRow;
}

// A PATCH field left undefined is unchanged server-side (a nil field is
// unchanged), so a blank unit or precision in the console means "leave it";
// clearing a set value is an API operation for now, like a property's
// validation schema.
export type UpdateMetricType = {
  label?: string;
  description?: string;
  unit?: string;
  precision?: number;
};

export async function updateMetricType(ref: CatalogNameRef, body: UpdateMetricType): Promise<void> {
  const { error } = await api.PATCH("/metric-types/{namespace}/{name}", { params: { path: { namespace: ref.namespace, name: ref.name } }, body });
  if (error) throw error;
}

export async function deleteMetricType(ref: CatalogNameRef): Promise<void> {
  const { error } = await api.DELETE("/metric-types/{namespace}/{name}", { params: { path: { namespace: ref.namespace, name: ref.name } } });
  if (error) throw error;
}
