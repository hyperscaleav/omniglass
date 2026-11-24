// The six official lifecycle fields (#2366): four ISO-8601 dates on the property
// lane, and replacement cost and expected life on the metric lane. Pure helpers
// the component and product pages share; the catalog (seed YAML) owns the types.
import { entityLabel } from "./entities";
import { fmtDay } from "./format";

export const LIFECYCLE_DATES = ["install-date", "warranty-end", "end-of-support", "end-of-life"] as const;
export const LIFECYCLE_METRICS = ["replacement-cost", "expected-life-years"] as const;
const ORDER: readonly string[] = [...LIFECYCLE_DATES, ...LIFECYCLE_METRICS];

export const isLifecycleDate = (name: string): boolean => (LIFECYCLE_DATES as readonly string[]).includes(name);
export const isLifecycleMetric = (name: string): boolean => (LIFECYCLE_METRICS as readonly string[]).includes(name);

// isIsoDate is true for a real calendar date written YYYY-MM-DD (2030-02-30 is not).
export function isIsoDate(text: string): boolean {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(text);
  if (!m) return false;
  const d = new Date(Date.UTC(+m[1], +m[2] - 1, +m[3]));
  return d.getUTCFullYear() === +m[1] && d.getUTCMonth() === +m[2] - 1 && d.getUTCDate() === +m[3];
}

// formatLifecycleDate reads a stored date as a date; anything else is shown as it came.
export function formatLifecycleDate(value: unknown): string {
  if (typeof value !== "string" || !isIsoDate(value)) return String(value);
  const [y, mo, d] = value.split("-").map(Number);
  return fmtDay(new Date(Date.UTC(y, mo - 1, d)).toISOString(), { year: true });
}

// formatLifecycleCost reads a number in a currency unit (USD) as money.
export function formatLifecycleCost(value: unknown, unit?: string): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return String(value);
  if (unit && /^[A-Z]{3}$/.test(unit)) {
    return new Intl.NumberFormat("en-US", { style: "currency", currency: unit }).format(value);
  }
  return unit ? `${value} ${unit}` : String(value);
}

export type LifecycleRow = { name: string; label: string; text: string };

type PropIn = { property_type_name: string; label?: string | null; value?: unknown; default_value?: unknown };
type MetricIn = { metric_type_name: string; label?: string | null; value?: unknown; default_value?: unknown; unit?: string | null };

// lifecycleRows picks the lifecycle fields that have a value (or contract
// default) out of an owner's properties and metrics, formatted, in canonical order.
export function lifecycleRows(props: PropIn[], metrics: MetricIn[]): LifecycleRow[] {
  const rows: LifecycleRow[] = [];
  for (const p of props ?? []) {
    const v = p.value ?? p.default_value;
    if (!isLifecycleDate(p.property_type_name) || v === null || v === undefined) continue;
    rows.push({ name: p.property_type_name, label: entityLabel({ name: p.property_type_name, label: p.label }), text: formatLifecycleDate(v) });
  }
  for (const m of metrics ?? []) {
    const v = m.value ?? m.default_value;
    if (!isLifecycleMetric(m.metric_type_name) || v === null || v === undefined) continue;
    const unit = m.unit ?? (m.metric_type_name === "replacement-cost" ? "USD" : "years");
    const text = m.metric_type_name === "replacement-cost" ? formatLifecycleCost(v, unit) : `${v} ${unit}`;
    rows.push({ name: m.metric_type_name, label: entityLabel({ name: m.metric_type_name, label: m.label }), text });
  }
  return rows.sort((a, b) => ORDER.indexOf(a.name) - ORDER.indexOf(b.name));
}
