import type { Driver, DriverEmit, DriverSpec } from "./drivers";

// The catalog's reverse reads (#2430): given one catalog row, what uses it. Pure
// functions over rows the console already holds (the catalogs are small, fleet-wide
// reference data, unlike the fleet, whose counts come from the list routes' own
// filters: see useFleetUsage). Each takes plain values so the drawers' "Used by"
// sections are unit-testable without a component.

function emitsOf(spec: DriverSpec | undefined): DriverEmit[] {
  if (!spec) return [];
  return [...(spec.polls ?? []).flatMap((p) => p.emits), ...(spec.listeners ?? []).flatMap((l) => l.emits)];
}

// What a driver collects, sorted into the lanes the catalogs say each key lives in
// (a key neither catalog names lands in `unknown`, so it still shows), plus the
// command types it offers. Each name appears once.
export type Collected = { metrics: string[]; properties: string[]; unknown: string[]; commands: string[] };

export function driverCollects(spec: DriverSpec | undefined, metricNames: Set<string>, propertyNames: Set<string>): Collected {
  const keys = [...new Set(emitsOf(spec).map((e) => e.name))].sort();
  return {
    metrics: keys.filter((k) => metricNames.has(k)),
    properties: keys.filter((k) => propertyNames.has(k)),
    unknown: keys.filter((k) => !metricNames.has(k) && !propertyNames.has(k)),
    commands: [...new Set((spec?.commands ?? []).map((c) => c.command_type))].sort(),
  };
}

// The drivers that emit a key (a poll or a listener), in the catalog's order.
export function driversCollecting(drivers: Driver[], key: string): Driver[] {
  return drivers.filter((d) => emitsOf(d.spec).some((e) => e.name === key));
}

// The drivers that bind a command type.
export function driversOffering(drivers: Driver[], commandType: string): Driver[] {
  return drivers.filter((d) => (d.spec?.commands ?? []).some((c) => c.command_type === commandType));
}

// The rules that read a key: event rules judge one sample key, calc rules read
// each input's key.
export function rulesReading<E extends { sample_key?: string }, C extends { inputs?: { key: string }[] | null }>(
  eventRules: E[],
  calcRules: C[],
  key: string,
): { event: E[]; calc: C[] } {
  return {
    event: eventRules.filter((r) => r.sample_key === key),
    calc: calcRules.filter((r) => (r.inputs ?? []).some((i) => i.key === key)),
  };
}

// Who writes a metric or property, for the "Written by" cell: the calc rule when
// one does, else the class of writer the API reports (driver, outpost probe, platform),
// else nothing. A calc rule outranks the class because a rule's output is a key no
// collection writes, so the two never disagree.
export type WriterLabel = { kind: "rule"; rule: string; rule_id: string } | { kind: "class"; text: string; title: string };

// The words each writer class (ADR-0161) reads as, and what it covers, since "collected"
// is the API's word for a driver's emits, a push, and an operator's or a command's value.
const WRITER_WORDS: Record<string, { text: string; title: string }> = {
  collected: { text: "driver", title: "Written by collection: a driver's emits or a push, a command's value, or an operator's value" },
  "outpost-probe": { text: "outpost probe", title: "Written by an outpost's own probe or session machine, under the endpoint it probed" },
  platform: { text: "platform", title: "Written by the Omniglass process itself" },
  "command-lifecycle": { text: "command", title: "Written by a command's own lifecycle" },
  calc: { text: "calc rule", title: "Written by a calc rule's output" },
};

export function writerLabel(r: { written_by?: { rule: string; rule_id: string } | null; writer_class?: string }): WriterLabel | null {
  if (r.written_by) return { kind: "rule", rule: r.written_by.rule, rule_id: r.written_by.rule_id };
  if (r.writer_class) {
    const w = WRITER_WORDS[r.writer_class] ?? { text: r.writer_class.replace(/-/g, " "), title: `Written by ${r.writer_class}` };
    return { kind: "class", ...w };
  }
  return null;
}

// The Explore link that lists fleet rows matching any of the values on one facet
// (the "see all" of a fleet used-by list): one equals chip, values OR'd, carried in
// the address so the link is shareable.
export function exploreHref(key: "product" | "standard", values: string[]): string {
  const q = new URLSearchParams({ chips: JSON.stringify([{ key, op: "eq", values }]) });
  return `/explore?${q.toString()}`;
}
