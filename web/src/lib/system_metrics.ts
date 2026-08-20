// The system mirror of component_metrics: the effective read the zoom's KPI
// tiles consume (#790). The standard's contract resolved to the latest
// sample or the declared default; the server does the resolving.
import { api } from "../api/client";
import type { components } from "../api/schema.gen";

export type EffectiveMetric = components["schemas"]["EffectiveMetricBody"];

export const systemMetricsKey = (system: string) => ["system-metrics", system] as const;

export async function systemMetrics(system: string): Promise<EffectiveMetric[]> {
  const res = await api.GET("/systems/{name}/metrics", { params: { path: { name: system } } });
  if (res.error) throw res.error;
  return (res.data?.metrics ?? []) as EffectiveMetric[];
}
