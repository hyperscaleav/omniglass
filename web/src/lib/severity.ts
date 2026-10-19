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

// ---- authoring (#930) ---------------------------------------------------------
//
// An operator level is created, renamed, relabelled, recolored, re-ranked and
// deleted from the console as from the CLI; official levels stay read-only. The
// server judges every write (a name or rank taken is a 409, a level an alarm or
// rule names cannot be deleted); the draft below is checked here first, so the
// form can say why before it sends.

export type CreateSeverityLevel = components["schemas"]["CreateSeverityLevelInputBody"];
export type UpdateSeverityLevel = components["schemas"]["UpdateSeverityLevelInputBody"];

// The palette, mildest hue first: the order the color picker offers.
export const SEVERITY_COLORS: SeverityColor[] = ["gray", "blue", "green", "yellow", "orange", "red", "darkred"];

export async function createSeverityLevel(body: CreateSeverityLevel): Promise<SeverityLevel> {
  const { data, error } = await api.POST("/severity-levels", { body });
  if (error) throw error;
  return data as SeverityLevel;
}

export async function updateSeverityLevel(name: string, body: UpdateSeverityLevel): Promise<SeverityLevel> {
  const { data, error } = await api.PATCH("/severity-levels/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return data as SeverityLevel;
}

export async function deleteSeverityLevel(name: string): Promise<void> {
  const { error } = await api.DELETE("/severity-levels/{name}", { params: { path: { name } } });
  if (error) throw error;
}

export type LevelDraft = { name: string; label: string; color: SeverityColor | ""; rank: string };
export type LevelErrors = Partial<Record<"name" | "color" | "rank", string>>;

export function emptyLevelDraft(): LevelDraft {
  return { name: "", label: "", color: "", rank: "" };
}

export function draftFromLevel(l: SeverityLevel): LevelDraft {
  return { name: l.name, label: l.label ?? "", color: l.color, rank: String(l.rank) };
}

const LEVEL_NAME = /^[a-z0-9]+(-[a-z0-9]+)*$/;
const WHOLE = /^\d+$/;

// readRank reads the rank field: a whole number above zero, or null.
export function readRank(raw: string): number | null {
  const t = raw.trim();
  if (!WHOLE.test(t)) return null;
  const n = Number(t);
  return n > 0 && Number.isSafeInteger(n) ? n : null;
}

// The other levels: every level but the one being edited.
const others = (levels: SeverityLevel[], editing?: string) => levels.filter((l) => l.name !== editing);

// validateLevelDraft is the form's one judgement, on create and edit alike
// (editing names the level being edited, which may keep its own name and rank).
export function validateLevelDraft(d: LevelDraft, levels: SeverityLevel[], editing?: string): LevelErrors {
  const e: LevelErrors = {};
  const rest = others(levels, editing);
  const name = d.name.trim();
  if (!name) e.name = "A name for the level.";
  else if (!LEVEL_NAME.test(name)) e.name = "Lowercase letters, digits and single hyphens, e.g. major.";
  else if (rest.some((l) => l.name === name)) e.name = `A level is already named ${name}.`;
  if (!d.color) e.color = "The hue its alarms wear.";
  const rank = readRank(d.rank);
  if (d.rank.trim() === "") e.rank = "Its place among the levels.";
  else if (rank === null) e.rank = "A whole number above zero.";
  else {
    const taken = rest.find((l) => l.rank === rank);
    if (taken) e.rank = `${entityLabel(taken)} already holds rank ${rank}; each level has its own.`;
  }
  return e;
}

// outageFloor is the lowest rank whose alarms take a component out, read off
// the levels the server marks (the official high level holds it), or null
// before they load.
export function outageFloor(levels: SeverityLevel[]): number | null {
  const ranks = levels.filter((l) => l.outage).map((l) => l.rank);
  return ranks.length ? Math.min(...ranks) : null;
}

// rankPlace says where a rank sits among the other levels, the sentence the
// form teaches the gap with: between two, above the worst, or below the mildest.
export function rankPlace(rank: number, levels: SeverityLevel[], editing?: string): string {
  const rest = [...others(levels, editing)].sort((a, b) => a.rank - b.rank);
  const below = [...rest].reverse().find((l) => l.rank < rank);
  const above = rest.find((l) => l.rank > rank);
  const at = (l: SeverityLevel) => `${entityLabel(l)} (${l.rank})`;
  if (below && above) return `Between ${at(below)} and ${at(above)}.`;
  if (below) return `Above ${at(below)}, the worst level.`;
  if (above) return `Below ${at(above)}, the mildest level.`;
  return "The only level.";
}

// levelCreateBody is the POST a valid draft sends; a blank label is left off.
export function levelCreateBody(d: LevelDraft): CreateSeverityLevel {
  const body: CreateSeverityLevel = { name: d.name.trim(), color: d.color, rank: readRank(d.rank) ?? 0 };
  if (d.label.trim()) body.label = d.label.trim();
  return body;
}

// levelUpdateBody is the PATCH an edit sends: only what changed (absent is
// unchanged on the server), so a relabel never re-ranks.
export function levelUpdateBody(d: LevelDraft, l: SeverityLevel): UpdateSeverityLevel {
  const body: UpdateSeverityLevel = {};
  if (d.name.trim() !== l.name) body.name = d.name.trim();
  if (d.label.trim() !== (l.label ?? "")) body.label = d.label.trim();
  if (d.color && d.color !== l.color) body.color = d.color;
  const rank = readRank(d.rank);
  if (rank !== null && rank !== l.rank) body.rank = rank;
  return body;
}
