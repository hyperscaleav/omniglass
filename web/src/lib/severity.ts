import { createMemo } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { entityLabel } from "./entities";

// The severity registry (#917): how bad an alarm is, as a list of named levels the
// server owns rather than a fixed set the console hardcodes. Each level carries a
// rank (higher is worse), a color token, and whether an active alarm at it puts
// its component in outage. Every console surface that colors, orders or explains
// a severity reads it through this file, so an operator-defined level ("major",
// orange, between average and high) renders, sorts and gates exactly like a
// shipped one with no console change.
//
// The pure core (severityScale, compareSeverity, severityBadge) takes the level
// list as an argument, so the alarm sorters stay unit-testable; the hook is the
// one place that reads it off the wire.

export type SeverityLevel = components["schemas"]["SeverityLevelBody"];
export type SeverityColor = SeverityLevel["color"];

export const severityLevelsKey = ["severity-levels"] as const;

export async function listSeverityLevels(): Promise<SeverityLevel[]> {
  const { data, error } = await api.GET("/severity-levels");
  if (error) throw error;
  return (data?.severity_levels ?? []) as SeverityLevel[];
}

// One color token -> badge class map, honoring the design system's status pills:
// grey is the app's neutral chip (badge-ghost, per the status-pill recipe), the daisyUI
// semantics carry the hues they have, and the two hues daisyUI has no slot for
// (orange and dark red) wear the domain variants app.css defines for them.
export const SEVERITY_BADGE: Record<SeverityColor, string> = {
  gray: "badge-ghost",
  blue: "badge-soft badge-info",
  green: "badge-soft badge-success",
  yellow: "badge-soft badge-warning",
  orange: "badge-average",
  red: "badge-soft badge-error",
  darkred: "badge-disaster",
};

// The look for a name the registry does not (yet) know: while the levels load, or
// for a level deleted since the alarm was raised. The raw name in a neutral chip,
// never a throw and never a guessed hue.
const NEUTRAL = "badge-ghost";

export type SeverityLook = {
  name: string;
  label: string;
  badge: string;
  // null when the registry does not know the name.
  rank: number | null;
  outage: boolean;
  known: boolean;
};

export function severityBadge(color: string | undefined): string {
  return (color && SEVERITY_BADGE[color as SeverityColor]) || NEUTRAL;
}

export type SeverityScale = {
  // Least to most severe, the registry's own order.
  levels: SeverityLevel[];
  look: (name: string) => SeverityLook;
  // Higher is worse; -Infinity for an unknown name, so it sorts last.
  rank: (name: string) => number;
  outage: (name: string) => boolean;
};

export function severityScale(levels?: SeverityLevel[] | null): SeverityScale {
  const sorted = [...(levels ?? [])].sort((a, b) => a.rank - b.rank);
  const byName = new Map(sorted.map((l) => [l.name, l]));
  const look = (name: string): SeverityLook => {
    const l = byName.get(name);
    if (!l) return { name, label: name, badge: NEUTRAL, rank: null, outage: false, known: false };
    return { name, label: entityLabel(l), badge: severityBadge(l.color), rank: l.rank, outage: l.outage, known: true };
  };
  return {
    levels: sorted,
    look,
    rank: (name) => byName.get(name)?.rank ?? -Infinity,
    outage: (name) => byName.get(name)?.outage ?? false,
  };
}

// compareSeverity orders two level names worst first; unknown names sort after
// every known one and tie among themselves.
export function compareSeverity(levels?: SeverityLevel[] | null): (a: string, b: string) => number {
  const s = severityScale(levels);
  return (a, b) => {
    const ra = s.rank(a);
    const rb = s.rank(b);
    if (ra === rb) return 0;
    return ra < rb ? 1 : -1;
  };
}

// useSeverity reads the registry once per session-ish (levels change rarely and
// only by an admin) and hands back the scale. Before the read lands the scale is
// empty, so every name falls back to the neutral look.
export function useSeverity(): () => SeverityScale {
  const q = useQuery(() => ({
    queryKey: severityLevelsKey,
    queryFn: listSeverityLevels,
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false,
  }));
  return createMemo(() => severityScale(q.data));
}
