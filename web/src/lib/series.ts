// The series reads (#794): one metric series' raw samples, one property
// series' change history, for either owner arc. The server windows and caps.
// With a resolution (#1269) the same route answers with every regular bucket
// of the window instead, which is what a chart over a long range wants: a
// capped raw read keeps only the newest rows.
import { api } from "../api/client";
import type { ChartBucket } from "./chart";

// SERIES_POLL_MS is a sparkline's cadence (the Data table, the platform
// panels): a line over hours moves too little in the leaf's five seconds to be
// worth a request per series, so a sparkline polls on its own, slower clock.
export const SERIES_POLL_MS = 30_000;

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

// A bucketed metric read: every bucket of the last `hours`, `resolutionSeconds`
// wide, oldest first, and the instant before which retention left nothing to
// read (a bucket earlier than it is empty because of retention). A bucket is one
// instance's measured samples (#1485): `instance` names it, and left undefined
// the server reads the one instance with measured samples in the window, or
// answers 422 when there are several. The empty string is an instance (the
// owner-level series) and is sent as one.
export type MetricBuckets = { buckets: ChartBucket[]; coveredFrom: string | null };

export const metricBucketsKey = (
  kind: "components" | "systems",
  owner: string,
  metric: string,
  hours: number,
  resolutionSeconds: number,
  instance?: string,
) => ["metric-buckets", kind, owner, metric, hours, resolutionSeconds, instance ?? null] as const;

export async function metricBuckets(
  kind: "components" | "systems",
  owner: string,
  metric: string,
  hours: number,
  resolutionSeconds: number,
  instance?: string,
): Promise<MetricBuckets> {
  const res = await api.GET(`/${kind}/{name}/metrics/{metric}/samples`, {
    params: { path: { name: owner, metric }, query: { hours, resolution: resolutionSeconds, ...(instance === undefined ? {} : { instance }) } },
  });
  if (res.error) throw res.error;
  const body = res.data as { buckets?: ChartBucket[]; covered_from?: string } | undefined;
  return { buckets: body?.buckets ?? [], coveredFrom: body?.covered_from ?? null };
}

export type PropertyBucket = { ts: string; count: number; last: string | null };
export type PropertyBuckets = { buckets: PropertyBucket[]; coveredFrom: string | null };

export const propertyBucketsKey = (
  kind: "components" | "systems",
  owner: string,
  property: string,
  hours: number,
  resolutionSeconds: number,
  instance?: string,
) => ["property-buckets", kind, owner, property, hours, resolutionSeconds, instance ?? null] as const;

export async function propertyBuckets(
  kind: "components" | "systems",
  owner: string,
  property: string,
  hours: number,
  resolutionSeconds: number,
  instance?: string,
): Promise<PropertyBuckets> {
  const res = await api.GET(`/${kind}/{name}/properties/{property}/samples`, {
    params: { path: { name: owner, property }, query: { hours, resolution: resolutionSeconds, ...(instance === undefined ? {} : { instance }) } },
  });
  if (res.error) throw res.error;
  const body = res.data as { buckets?: PropertyBucket[]; covered_from?: string } | undefined;
  return { buckets: body?.buckets ?? [], coveredFrom: body?.covered_from ?? null };
}
