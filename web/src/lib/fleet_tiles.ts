// The workspaces' counts (#795 review, #826): each workspace builds one
// TileSpec for the scope it is on, and FleetShell writes it as the one counts
// line without knowing the scope: a location counts its subtree, a system its
// components, a leaf itself. Pure; no verdict is computed here.

import { bandsOf, byChildOfLocation, holesUnder, type FleetView } from "./fleet";
import { slotStrip } from "./slot_strip";
import type { FleetHealth } from "./system_zoom";

export type TileCount = { key: string; label: string; value: number | string; sub?: string };
export type TileSpec = {
  // What the mix counts: the noun the counts line leads with.
  subject: string;
  ratio: { healthy: number; incomplete: number; degraded: number; outage: number; total: number };
  attention: { outage: number; degraded: number; incomplete: number; total: number };
  counts: TileCount[];
};

function mixOf(verdicts: (string | null | undefined)[]): TileSpec["ratio"] {
  const ratio = { healthy: 0, incomplete: 0, degraded: 0, outage: 0, total: 0 };
  for (const v of verdicts) {
    ratio.total++;
    if (v && v in ratio) ratio[v as keyof typeof ratio]++;
  }
  return ratio;
}

function attentionOf(ratio: TileSpec["ratio"]): TileSpec["attention"] {
  return { outage: ratio.outage, degraded: ratio.degraded, incomplete: ratio.incomplete, total: ratio.outage + ratio.degraded + ratio.incomplete };
}


export function locationTileSpec(view: FleetView, locationId: string): TileSpec {
  const clusters = bandsOf(view, byChildOfLocation(locationId)).flatMap((b) => b.clusters);
  const ratio = mixOf(clusters.map((c) => c.verdict));
  const distinct = new Set<string>();
  for (const c of clusters) for (const d of c.dots) distinct.add(d.componentId);
  const gaps = [...holesUnder(locationId, view).values()].reduce((n, v) => n + v.length, 0);
  const children = (view.locations ?? []).filter((l) => l.parent === locationId).length;
  return {
    subject: "systems",
    ratio,
    attention: attentionOf(ratio),
    counts: [
      { key: "gaps", label: gaps === 1 ? "gap" : "gaps", value: gaps, sub: gaps === 1 ? "location with no system" : "locations with no system" },
      { key: "components", label: "components", value: distinct.size, sub: `across ${ratio.total} systems` },
      { key: "children", label: "children", value: children, sub: "direct locations" },
    ],
  };
}

export function systemTileSpec(view: FleetView, health: FleetHealth | undefined, systemId: string): TileSpec {
  const self = (view.systems ?? []).find((s) => s.id === systemId);
  const dots = self?.dots ?? [];
  const ratio = mixOf(dots.map((d) => d.verdict));
  const strip = health ? slotStrip(health) : undefined;
  const alarmIds = new Set<string>();
  for (const r of health?.roles ?? []) {
    if (!r.active) continue;
    for (const a of r.alarms ?? []) alarmIds.add(a.id);
  }
  const shared = dots.filter((d) => d.shared).length;
  return {
    subject: "components",
    ratio,
    attention: attentionOf(ratio),
    counts: [
      { key: "slots", label: "slots filled", value: strip ? `${strip.filled} of ${strip.total}` : "", sub: "of the standard's slots" },
      { key: "alarms", label: alarmIds.size === 1 ? "active alarm" : "active alarms", value: alarmIds.size, sub: "on members, now" },
      { key: "shared", label: "shared", value: shared, sub: "serving another system too" },
    ],
  };
}

export function componentTileSpec(view: FleetView, componentId: string, activeAlarms: number, interfaces: number): TileSpec {
  let verdict: string | null = null;
  let memberships = 0;
  for (const s of view.systems ?? []) {
    for (const d of s.dots ?? []) {
      if (d.component === componentId) {
        verdict = d.verdict ?? verdict;
        memberships++;
      }
    }
  }
  const ratio = mixOf(verdict === null && memberships === 0 ? [null] : [verdict]);
  return {
    subject: "component",
    ratio,
    attention: attentionOf(ratio),
    counts: [
      { key: "systems", label: memberships === 1 ? "system" : "systems", value: memberships, sub: "slots it fills" },
      { key: "alarms", label: activeAlarms === 1 ? "active alarm" : "active alarms", value: activeAlarms, sub: "on this component, now" },
      { key: "interfaces", label: interfaces === 1 ? "interface" : "interfaces", value: interfaces, sub: "collection paths" },
    ],
  };
}

// The one counts line (#826): what the summary rail said, as one line with
// the zero values left out. The mix total leads (it is never zero on a page
// that has a subject), need-attention follows only when something does, and
// the scope's counts close, each pluralised by its own label; a string count
// (a slot ratio) rides as written, an empty one is dropped. Rendered by the
// shared header on every altitude; nothing else counts anything.
export function countsLine(spec: TileSpec): string[] {
  const parts: string[] = [counted(spec.ratio.total, spec.subject)];
  if (spec.attention.total > 0) parts.push(counted(spec.attention.total, "need attention"));
  for (const c of spec.counts) {
    if (typeof c.value === "number") {
      if (c.value > 0) parts.push(counted(c.value, c.label));
    } else if (c.value.trim() !== "") {
      parts.push(`${c.value} ${c.label}`);
    }
  }
  return parts;
}

// A count reads with its label, singular at one. Labels arrive plural from the
// specs (a type's own display name included), so the singular is derived from
// the ending rather than looked up: "children" to "child", "Campuses" to
// "Campus", "active alarms" to "active alarm"; the attention part is a verb.
function counted(n: number, label: string): string {
  return `${n} ${n === 1 ? singular(label) : label}`;
}

function singular(label: string): string {
  if (label === "need attention") return "needs attention";
  if (label.endsWith("children")) return `${label.slice(0, -8)}child`;
  if (/(ss|sh|ch|x|us)es$/.test(label)) return label.slice(0, -2);
  if (/[^aeiou]ies$/.test(label)) return `${label.slice(0, -3)}y`;
  if (label.endsWith("s") && !label.endsWith("ss")) return label.slice(0, -1);
  return label;
}
