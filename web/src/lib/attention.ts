import type { Chip } from "./predicate";
import { healthOf, type HealthState } from "./health";
import type { FleetView } from "./fleet";

// What needs attention (#2438): one pure count that Explore's chip and Home's
// card both read, so the chip's number is the number of rows its filter leaves
// and Home says the same words for the same fleet.
//
// Verdicts are data. The server decides a system's or component's health (the
// rule that an alarmed member outside a slot degrades its system is the
// server's to change); this module only counts the verdicts it is handed and
// never re-derives one.

export const ATTENTION: HealthState[] = ["outage", "degraded", "incomplete"];

// The chip the need-attention button presses: the filter whose rows are counted.
export const ATTENTION_CHIP: Chip = { key: "health", op: "eq", values: ATTENTION };

export const needsAttention = (h: HealthState | null | undefined): boolean => !!h && ATTENTION.includes(h);

// What a row is for counting: a location wearing one system is that system.
export type CountRow = { kind: "location" | "system" | "component"; health: HealthState | null };

export type AttentionCount = { systems: number; components: number; total: number };

export function attentionCount(rows: Iterable<CountRow>): AttentionCount {
  let systems = 0;
  let components = 0;
  for (const r of rows) {
    if (!needsAttention(r.health)) continue;
    if (r.kind === "component") components++;
    else systems++;
  }
  return { systems, components, total: systems + components };
}

export function attentionParts(c: AttentionCount): { n: number; noun: string }[] {
  const out: { n: number; noun: string }[] = [];
  if (c.systems > 0) out.push({ n: c.systems, noun: c.systems === 1 ? "system" : "systems" });
  if (c.components > 0) out.push({ n: c.components, noun: c.components === 1 ? "component" : "components" });
  return out;
}

export function attentionPhrase(c: AttentionCount): string {
  if (c.total === 0) return "Nothing needs attention";
  return `${attentionParts(c).map((p) => `${p.n} ${p.noun}`).join(", ")} ${c.total === 1 ? "needs" : "need"} attention`;
}

// The fleet view's rows for counting: every system, and every component once
// however many systems it serves (as the outline lists it once).
export function fleetAttentionRows(view: FleetView | undefined): CountRow[] {
  const out: CountRow[] = [];
  const seen = new Set<string>();
  for (const s of view?.systems ?? []) {
    out.push({ kind: "system", health: healthOf(s.health) });
    for (const d of s.dots ?? []) {
      if (seen.has(d.component)) continue;
      seen.add(d.component);
      out.push({ kind: "component", health: healthOf(d.health) });
    }
  }
  return out;
}
