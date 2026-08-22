// The series reads (#794): one metric series' raw samples, one property
// series' change history, for either owner arc. The server windows and caps.
import { api } from "../api/client";

export type MetricPoint = { ts: string; value: number; instance?: string; provenance: string; source?: string };
export type PropertyPoint = { ts: string; value: string; instance?: string; provenance: string; source?: string };

export const metricSeriesKey = (kind: "components" | "systems", owner: string, metric: string, hours: number) =>
  ["metric-series", kind, owner, metric, hours] as const;

export async function metricSeries(kind: "components" | "systems", owner: string, metric: string, hours: number): Promise<MetricPoint[]> {
  const res = await api.GET(`/${kind}/{name}/metrics/{metric}/samples`, {
    params: { path: { name: owner, metric }, query: { hours } },
  });
  if (res.error) throw res.error;
  return ((res.data as { samples?: MetricPoint[] })?.samples ?? []) as MetricPoint[];
}

export const propertySeriesKey = (kind: "components" | "systems", owner: string, property: string, hours: number) =>
  ["property-series", kind, owner, property, hours] as const;

export async function propertySeries(kind: "components" | "systems", owner: string, property: string, hours: number): Promise<PropertyPoint[]> {
  const res = await api.GET(`/${kind}/{name}/properties/{property}/samples`, {
    params: { path: { name: owner, property }, query: { hours } },
  });
  if (res.error) throw res.error;
  return ((res.data as { samples?: PropertyPoint[] })?.samples ?? []) as PropertyPoint[];
}
