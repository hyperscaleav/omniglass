import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The health data layer: thin typed wrappers over the generated client plus the
// pure read-time derivations the console renders.
//
// The model, end to end, because every surface here is a view of it:
//
//   an ALARM impairs its COMPONENT's own verdict wholesale (#626: no longer
//   per named capability)
//   -> a ROLE carries a QUORUM; when too few assigned components currently
//      occupy it (their own verdict is not outage; degraded still occupies),
//      the role is IMPAIRED
//   -> an impaired role contributes its IMPACT (outage, degraded, or none)
//      UNLESS it belongs to a CHOICE (#626: an exclusive-or group, such as an
//      all-in-one alternate versus a component-built one) whose best-satisfied
//      alternate is a different one; the role's own ACTIVE flag says so, and
//      its impaired/short/spare figures are then read-only trivia, not part
//      of why the system is what it is
//   -> a SYSTEM takes the worst contribution among its ACTIVE roles
//   -> a LOCATION takes the worst verdict among the systems placed beneath it
//
// The verdict is never computed here: the server sends it, and the console shows
// the chain that produced it. Recomputing it in the browser is exactly how a
// console starts disagreeing with the API it reads. The same discipline applies
// to ACTIVE: rendering a role's impaired figure without checking it first is
// exactly the contradiction the field exists to prevent (a badge over an
// impaired list that flatly disagrees with it), so every derivation below
// filters through activeRoles rather than the raw list.

export type Verdict = "healthy" | "incomplete" | "degraded" | "outage";
export type HealthRole = components["schemas"]["HealthRoleBody"];
export type HealthTransition = components["schemas"]["HealthTransitionBody"];
export type FleetHealth = components["schemas"]["FleetHealthOutputBody"];

// One cache namespace per arc, so a system and a location that share a name never
// collide.
export const systemHealthKey = (name: string) => ["system-health", name] as const;
export const locationHealthKey = (name: string) => ["location-health", name] as const;

export async function systemHealth(name: string): Promise<FleetHealth> {
  const { data, error } = await api.GET("/systems/{name}/health", { params: { path: { name } } });
  if (error) throw error;
  return data as FleetHealth;
}

// The BULK verdict read (#653). One request carrying the one fact a list's
// health column renders, for every system the caller can see.
//
// Its own cache namespace, deliberately NOT the per-system one: those entries
// hold a full FleetHealth report that the panels read roles and transitions out
// of, and seeding them from this read would put a verdict-only object behind a
// key whose readers expect the whole report. A write that changes a verdict
// invalidates both, which is why SYSTEM_VERDICTS_KEY sits beside systemHealthKey
// at every invalidation site.
export const SYSTEM_VERDICTS_KEY = ["system-verdicts"] as const;

export async function systemVerdicts(): Promise<Map<string, string>> {
  const { data, error } = await api.GET("/systems:health");
  if (error) throw error;
  return new Map((data?.verdicts ?? []).map((v) => [v.system, v.verdict] as const));
}

export async function locationHealth(name: string): Promise<FleetHealth> {
  const { data, error } = await api.GET("/locations/{name}/health", { params: { path: { name } } });
  if (error) throw error;
  return data as FleetHealth;
}

// verdictOf narrows whatever the API sent to the four states the console knows.
// Anything else (an unread query, a state a newer server introduces) is null, which
// the badge renders as unknown rather than guessing.
export function verdictOf(v: string | null | undefined): Verdict | null {
  return v === "healthy" || v === "incomplete" || v === "degraded" || v === "outage" ? v : null;
}

// Worst wins, everywhere: a role over its system, a system over its location.
// The order is the severity ranking, matching internal/health's Verdict enum
// exactly. incomplete sits between healthy and degraded: a commissioning gap is
// worth surfacing above a clean system and worth burying under anything that is
// actually broken.
const RANK: Record<Verdict, number> = { healthy: 0, incomplete: 1, degraded: 2, outage: 3 };

export function verdictRank(v: Verdict): number {
  return RANK[v];
}

export function worstVerdict(list: (string | null | undefined)[]): Verdict | null {
  let worst: Verdict | null = null;
  for (const raw of list) {
    const v = verdictOf(raw);
    if (v && (!worst || RANK[v] > RANK[worst])) worst = v;
  }
  return worst;
}

const roles = (h: FleetHealth | undefined): HealthRole[] => h?.roles ?? [];

// activeRoles is every role whose own figures actually counted toward the
// verdict: unconditional roles (no choice) plus the role of whichever
// alternate answered its choice. A role whose alternate LOST reads active
// false; it can still be impaired on its own terms, but rendering that
// impairment as part of "why this system reads what it does" is exactly the
// contradiction active exists to catch, so every other derivation here reads
// through this rather than the raw roles list.
export function activeRoles(h: FleetHealth | undefined): HealthRole[] {
  return roles(h).filter((r) => r.active);
}

// inactiveRoles is the complement: roles belonging to a choice's alternate
// that lost. Surfaced separately (not silently dropped) so an operator can
// still see what the unbuilt alternate would need, without it reading as a
// current impairment.
export function inactiveRoles(h: FleetHealth | undefined): HealthRole[] {
  return roles(h).filter((r) => !r.active);
}

// quorumLabel reads the role's fill against what it wants, in the API's own terms:
// how many assigned components can still satisfy it, and how many it needs.
export function quorumLabel(r: Pick<HealthRole, "satisfying" | "quorum">): string {
  return `${r.satisfying} of ${r.quorum} satisfying`;
}

// What an impaired role means for its system, in words rather than an enum.
export function impactPhrase(impact: string): string {
  if (impact === "outage") return "outage";
  if (impact === "degraded") return "degraded";
  return "no change";
}
