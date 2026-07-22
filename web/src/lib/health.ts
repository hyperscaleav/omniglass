import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { sortAlarms } from "./alarms";

// The health data layer: thin typed wrappers over the generated client plus the
// pure read-time derivations the console renders.
//
// The model, end to end, because every surface here is a view of it:
//
//   an ALARM on a component names the CAPABILITIES it degrades
//   -> a ROLE requires capabilities and carries a QUORUM; when too few assigned
//      components can still satisfy it, the role is IMPAIRED
//   -> an impaired role contributes its IMPACT (outage, degraded, or none)
//   -> a SYSTEM takes the worst contribution among its roles
//   -> a LOCATION takes the worst verdict among the systems placed beneath it
//
// The verdict is never computed here: the server sends it, and the console shows
// the chain that produced it. Recomputing it in the browser is exactly how a
// console starts disagreeing with the API it reads.

export type Verdict = "healthy" | "degraded" | "outage";
export type HealthRole = components["schemas"]["HealthRoleBody"];
export type HealthAlarm = components["schemas"]["HealthAlarmBody"];
export type HealthSystem = components["schemas"]["HealthSystemBody"];
export type HealthTransition = components["schemas"]["HealthTransitionBody"];
export type EstateHealth = components["schemas"]["EstateHealthOutputBody"];

// One cache namespace per arc, so a system and a location that share a name never
// collide.
export const systemHealthKey = (name: string) => ["system-health", name] as const;
export const locationHealthKey = (name: string) => ["location-health", name] as const;

export async function systemHealth(name: string): Promise<EstateHealth> {
  const { data, error } = await api.GET("/systems/{name}/health", { params: { path: { name } } });
  if (error) throw error;
  return data as EstateHealth;
}

export async function locationHealth(name: string): Promise<EstateHealth> {
  const { data, error } = await api.GET("/locations/{name}/health", { params: { path: { name } } });
  if (error) throw error;
  return data as EstateHealth;
}

// verdictOf narrows whatever the API sent to the three states the console knows.
// Anything else (an unread query, a state a newer server introduces) is null, which
// the badge renders as unknown rather than guessing.
export function verdictOf(v: string | null | undefined): Verdict | null {
  return v === "healthy" || v === "degraded" || v === "outage" ? v : null;
}

// Worst wins, everywhere: a role over its system, a system over its location.
const RANK: Record<Verdict, number> = { healthy: 0, degraded: 1, outage: 2 };

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

export const roles = (h: EstateHealth | undefined): HealthRole[] => h?.roles ?? [];
export const systems = (h: EstateHealth | undefined): HealthSystem[] => h?.systems ?? [];
export const transitions = (h: EstateHealth | undefined): HealthTransition[] => h?.transitions ?? [];

// The roles that explain the verdict, worst impact first, so the reconciliation
// panel leads with the role that took the system down rather than the one that
// merely dented it.
export function impairedRoles(h: EstateHealth | undefined): HealthRole[] {
  return roles(h)
    .filter((r) => r.impaired)
    .sort((a, b) => impactRank(a.impact) - impactRank(b.impact) || (a.display_name || a.name).localeCompare(b.display_name || b.name));
}

// The roles that are holding: named too, because "which roles are fine" is half of
// why a system is only degraded and not out.
export function holdingRoles(h: EstateHealth | undefined): HealthRole[] {
  return roles(h).filter((r) => !r.impaired);
}

function impactRank(impact: string): number {
  return impact === "outage" ? 0 : impact === "degraded" ? 1 : 2;
}

// quorumLabel reads the role's fill against what it wants, in the API's own terms:
// how many assigned components can still satisfy it, and how many it needs.
export function quorumLabel(r: Pick<HealthRole, "satisfying" | "quorum">): string {
  return `${r.satisfying} of ${r.quorum} satisfying`;
}

// A CAUSE is one required capability an alarm has taken away, with the alarms that
// took it. This is the middle link of the chain, and the only one the API does not
// hand over pre-joined.
export type Cause = { capability: string; alarms: HealthAlarm[] };

export function causes(r: HealthRole): Cause[] {
  const alarms = r.alarms ?? [];
  return (r.degraded ?? []).map((capability) => ({
    capability,
    alarms: sortAlarms(alarms.filter((a) => (a.capabilities ?? []).includes(capability))),
  }));
}

// The alarm that best explains the role: the worst, most recent one. Null when the
// role is impaired with no alarm reaching it, which means it is simply short of
// components rather than broken.
export function worstAlarm(r: HealthRole): HealthAlarm | null {
  return sortAlarms(r.alarms ?? [])[0] ?? null;
}

// What an impaired role means for its system, in words rather than an enum.
export function impactPhrase(impact: string): string {
  if (impact === "outage") return "outage";
  if (impact === "degraded") return "degraded";
  return "no change";
}

// chainSentence is the claim this whole slice makes, in one line: which alarm, on
// which component, took which capability away, which role that pushed below quorum,
// and what that contributes to the verdict the operator is looking at. It names
// every link, because a badge that says "degraded" and nothing else is the thing
// operators already have and do not trust.
export function chainSentence(r: HealthRole, verdict: string): string {
  const role = r.display_name || r.name;
  const alarm = worstAlarm(r);
  const lost = (r.degraded ?? []).join(", ");
  if (!alarm) {
    return `No alarm reaches ${role}: it satisfies ${r.satisfying} of ${r.quorum} because too few components are assigned, and ${contribution(r, verdict)}.`;
  }
  const took = lost ? ` degrades ${lost}` : " reaches it";
  return `A ${alarm.severity} alarm on ${alarm.component}${took}, so ${role} satisfies ${r.satisfying} of ${r.quorum} and ${contribution(r, verdict)}.`;
}

// What this role's impact means for the verdict on screen. A role only EXPLAINS the
// verdict when its impact IS the verdict; a degraded role sitting under an outage
// set by a worse role must not claim credit for it, or the panel teaches the
// operator a false rule.
function contribution(r: HealthRole, verdict: string): string {
  const phrase = impactPhrase(r.impact);
  if (r.impact === "none") return "contributes nothing to the verdict";
  if (phrase === verdict) return `contributes ${phrase}, which is why this system reads ${verdict}`;
  return `contributes ${phrase}, though this system reads ${verdict} on a worse role`;
}
