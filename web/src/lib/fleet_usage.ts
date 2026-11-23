import { api } from "../api/client";
import type { Product } from "./products";

// The fleet half of the catalog's "used by" (#2430): how many components or
// systems a catalog row classifies, and the first few of them. The count and the
// page come from the list routes' own filters (components by product, driver or
// component type; systems by standard; products by the metric or property they
// declare), so a drawer never fetches the fleet to count it. The server applies
// the caller's scope before it counts, so a viewer's number is the rows they can
// see.

// How many rows a used-by list shows before "see all".
export const USED_BY_PAGE = 5;

export type FleetRow = { id: string; name: string; label?: string; product?: string };
export type FleetUsage = { rows: FleetRow[]; total: number };

export type ComponentFilter = { product?: string; driver?: string; component_type?: string };

// Under the components root, so any component write refetches the counts with the
// lists they sit beside.
export const componentUsageKey = (f: ComponentFilter) => ["components", "usage", f] as const;
export const systemUsageKey = (standard: string) => ["systems", "usage", standard] as const;
export const declaringKey = (lane: "metric" | "property", name: string) => ["products", "declares", lane, name] as const;

export async function componentUsage(f: ComponentFilter, limit = USED_BY_PAGE): Promise<FleetUsage> {
  const { data, error } = await api.GET("/components", { params: { query: { ...f, limit } } });
  if (error) throw error;
  return { rows: (data?.components ?? []) as FleetRow[], total: data?.total ?? 0 };
}

export async function systemUsage(standard: string, limit = USED_BY_PAGE): Promise<FleetUsage> {
  const { data, error } = await api.GET("/systems", { params: { query: { standard, limit } } });
  if (error) throw error;
  return { rows: (data?.systems ?? []) as FleetRow[], total: data?.total ?? 0 };
}

// The products whose contract declares a metric or a property.
export async function productsDeclaring(lane: "metric" | "property", name: string): Promise<Product[]> {
  const { data, error } = await api.GET("/products", {
    params: { query: lane === "metric" ? { declares_metric: name } : { declares_property: name } },
  });
  if (error) throw error;
  return (data?.products ?? []) as Product[];
}
