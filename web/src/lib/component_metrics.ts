// The metric mirror of component_properties: the effective read the leaf's
// vitals card consumes (#786). Same route grammar, same resolution story
// (contract default until a sample arrives, is_sampled marks the live
// series); the server does the resolving, this file only fetches.
import { api } from "../api/client";
import type { components } from "../api/schema.gen";

export type EffectiveMetric = components["schemas"]["EffectiveMetricBody"];

export const effectiveMetricsKey = (component: string) => ["component-metrics", component] as const;

export async function effectiveMetrics(component: string): Promise<EffectiveMetric[]> {
  const res = await api.GET("/components/{name}/metrics", { params: { path: { name: component } } });
  if (res.error) throw res.error;
  return (res.data?.metrics ?? []) as EffectiveMetric[];
}
