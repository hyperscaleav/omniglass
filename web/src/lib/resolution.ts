import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The effective-values data layer: a component's tags as the cascade resolves
// them, with the provenance the flat pill list throws away.
//
// A resolved value is not a fact, it is the survivor of a cascade. Every
// candidate comes back, winner and shadowed alike, so a surface can answer "why
// is it this" rather than only "what is it".
//
// The system band is seeded from MEMBERSHIP, so a component in more than one
// system resolves differently for each. Pass forSystem to ask for one of them;
// omit it to resolve against the component's primary membership, which is what a
// caller with no system in hand gets.

export type ResolvedTag = components["schemas"]["ResolvedTagBody"];

export const effectiveTagsKey = (name: string, forSystem: string) =>
  ["effective-tags", name, forSystem] as const;

export async function effectiveTags(name: string, forSystem: string): Promise<ResolvedTag[]> {
  const { data, error } = await api.GET("/components/{name}/effective-tags", {
    params: { path: { name }, query: forSystem ? { system: forSystem } : {} },
  });
  if (error) throw error;
  return (data?.tags ?? []) as ResolvedTag[];
}

// bandLabel names the tier a value came from, in the operator's words rather than
// the integer the API carries.
export function bandLabel(ownerKind: string): string {
  switch (ownerKind) {
    case "component":
      return "this component";
    case "system":
      return "system";
    case "location":
      return "location";
    default:
      // The install-wide tier was renamed global -> platform (ADR-0057); this
      // label was missed at the time and kept saying global on every panel.
      return "platform";
  }
}

// byKey groups candidates under their key, winner first, so a row can render the
// resolved value with everything it beat underneath.
export function byKey(rows: ResolvedTag[]): { key: string; winner?: ResolvedTag; shadowed: ResolvedTag[] }[] {
  const groups = new Map<string, { key: string; winner?: ResolvedTag; shadowed: ResolvedTag[] }>();
  for (const r of rows) {
    let g = groups.get(r.key);
    if (!g) {
      g = { key: r.key, shadowed: [] };
      groups.set(r.key, g);
    }
    if (r.winner) g.winner = r;
    else g.shadowed.push(r);
  }
  return [...groups.values()].sort((a, b) => a.key.localeCompare(b.key));
}

export type ResolvedVariable = components["schemas"]["ResolvedVariableBody"];
export type ResolvedSecret = components["schemas"]["ResolvedSecretBody"];

// The three cascades are one engine, so the panel reads them as one shape. Kind
// is carried explicitly rather than inferred, because the bands genuinely differ
// and a surface that hides that teaches the wrong model: a tag resolves through
// platform, location, system, and component; a variable seeds its system band
// from the PRIMARY membership only; a secret has no system band at all, since a
// credential's owner is never the room a device happens to serve (ADR-0052).
export type ValueKind = "tag" | "variable" | "secret";

export interface ResolvedValue {
  kind: ValueKind;
  name: string;
  // The winning value as displayed. A secret has no value here: its fields are
  // masked, and the useful answer is WHICH secret applies and from where.
  value: string;
  owner_kind: string;
  owner_name?: string;
  winner: boolean;
}

export const effectiveVariablesKey = (name: string) => ["effective-variables", name] as const;
export const effectiveSecretsKey = (name: string) => ["effective-secrets", name] as const;

export async function effectiveVariables(name: string): Promise<ResolvedVariable[]> {
  const { data, error } = await api.GET("/components/{name}/effective-variables", {
    params: { path: { name } },
  });
  if (error) throw error;
  return (data?.variables ?? []) as ResolvedVariable[];
}

// A caller without secret:read gets a 403, which is not an error worth surfacing
// in a panel about resolution: the operator simply may not see credentials. It
// resolves to nothing and the panel shows the other two kinds.
export async function effectiveSecrets(name: string): Promise<ResolvedSecret[]> {
  const { data, error } = await api.GET("/components/{name}/effective-secrets", {
    params: { path: { name } },
  });
  if (error) return [];
  return (data?.secrets ?? []) as ResolvedSecret[];
}

// mergeResolved flattens the three cascades into one ordered list: by name, then
// kind, so the same name from two kinds sits together and a reader compares like
// with like.
export function mergeResolved(
  tags: ResolvedTag[],
  variables: ResolvedVariable[],
  secrets: ResolvedSecret[],
): ResolvedValue[] {
  const out: ResolvedValue[] = [
    ...tags.map((t) => ({
      kind: "tag" as const, name: t.key, value: t.value,
      owner_kind: t.owner_kind, owner_name: t.owner_name, winner: Boolean(t.winner),
    })),
    ...variables.map((v) => ({
      kind: "variable" as const, name: v.name, value: formatValue(v.value),
      owner_kind: v.owner_kind, owner_name: v.owner_name, winner: Boolean(v.winner),
    })),
    ...secrets.map((s) => ({
      kind: "secret" as const, name: s.name, value: "",
      owner_kind: s.owner_kind, owner_name: s.owner_name, winner: Boolean(s.winner),
    })),
  ];
  return out.sort((a, b) => a.name.localeCompare(b.name) || a.kind.localeCompare(b.kind));
}

// A variable's value is polymorphic (its value_type decides the shape), so it is
// rendered the way it would be typed rather than as [object Object].
function formatValue(v: unknown): string {
  return typeof v === "string" ? v : JSON.stringify(v) ?? "";
}

// byName groups the merged rows the way byKey groups tags: one entry per
// (kind, name) with its winner and everything it beat.
export function byName(rows: ResolvedValue[]): { kind: ValueKind; name: string; winner?: ResolvedValue; shadowed: ResolvedValue[] }[] {
  const groups = new Map<string, { kind: ValueKind; name: string; winner?: ResolvedValue; shadowed: ResolvedValue[] }>();
  for (const r of rows) {
    const id = `${r.kind}:${r.name}`;
    let g = groups.get(id);
    if (!g) {
      g = { kind: r.kind, name: r.name, shadowed: [] };
      groups.set(id, g);
    }
    if (r.winner) g.winner = r;
    else g.shadowed.push(r);
  }
  return [...groups.values()].sort((a, b) => a.name.localeCompare(b.name) || a.kind.localeCompare(b.kind));
}
