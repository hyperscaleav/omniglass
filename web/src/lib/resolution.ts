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
      return "global";
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
