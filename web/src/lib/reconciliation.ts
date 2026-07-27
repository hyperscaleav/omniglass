import { api } from "../api/client";

// The reconciliation data layer: a thin typed wrapper over the generated client.
// The API returns, per declared property, the want (declared, resolved live from
// the cascade), the told (intended, from a command), and the is (observed, from the
// latest-value cache), plus a drift flag computed on read. Shapes follow the
// OpenAPI (see api/reconciliation.go). No client-side derivation: drift is the
// server's, since only it holds the cascade.

// A want/told/is value is an arbitrary JSON value (a string, number, bool, or
// object), or null when that side has no value.
export type ReconValue = unknown;

export type ReconProperty = {
  property: string;
  want: ReconValue | null;
  told: ReconValue | null;
  is: ReconValue | null;
  drift: boolean;
};

export type Reconciliation = { component: string; properties: ReconProperty[] };

export const RECONCILIATION_KEY = (name: string) => ["reconciliation", name] as const;

export async function getReconciliation(name: string): Promise<Reconciliation> {
  const { data, error } = await api.GET("/components/{name}/reconciliation", { params: { path: { name } } });
  if (error) throw error;
  return data as Reconciliation;
}

// fmtValue renders a want/told/is JSON value for display: a bare string without its
// quotes (the common case), anything else as compact JSON. Absence is handled by
// the panel (a placeholder), not here.
export function fmtValue(v: ReconValue): string {
  if (typeof v === "string") return v;
  return JSON.stringify(v);
}
