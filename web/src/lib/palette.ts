import type { DrawerKind } from "./drawer_address";
import type { Command } from "../components/CommandPalette";

// The command palette's results (#2444): the pure half. What the server found
// (GET /search) and what the person opened recently are laid out in the palette's
// groups here; the fetching, debouncing and rendering live elsewhere.

export type SearchEntity = { id: string; name: string; label?: string; place?: string; system?: string };
export type SearchCatalog = { kind: string; name: string; label?: string; detail?: string };
export type SearchResult = {
  locations: SearchEntity[];
  systems: SearchEntity[];
  components: SearchEntity[];
  catalog: SearchCatalog[];
};

// A server search starts at this many characters: one letter matches half the fleet.
export const MIN_QUERY = 2;

export const GROUPS = ["Recent", "Pages", "Locations", "Systems", "Components", "Catalog", "Actions"] as const;
export type GroupName = (typeof GROUPS)[number];

// One row: where it goes (a Command, so Enter opens it the way a page does) or
// the ask row, under its group.
export type Hit = { value: string; label: string; detail?: string; group: GroupName; command?: Command; ask?: string };

const CATALOG_KINDS: Record<string, { drawer: DrawerKind; noun: string }> = {
  product: { drawer: "product", noun: "Product" },
  driver: { drawer: "driver", noun: "Driver" },
  calc_rule: { drawer: "calc_rule", noun: "Calc rule" },
  event_rule: { drawer: "event_rule", noun: "Event rule" },
  metric: { drawer: "metric", noun: "Metric" },
};

// A fleet entity opens at its own page, by uuid.
export const fleetHref = (kind: "locations" | "systems" | "components", id: string) => `/${kind}/${encodeURIComponent(id)}`;

// What tells two same-named hits apart: where it sits, and the system it is in.
export function entityDetail(e: SearchEntity): string | undefined {
  const parts = [e.place, e.system ? `in ${e.system}` : ""].filter(Boolean);
  return parts.length ? parts.join(" · ") : undefined;
}

// The words a row reads: the label alone when it is only the name spelled for people
// (Video Bar 1 for videobar-1), the label with the name after it when they differ.
const squash = (s: string) => s.toLowerCase().replace(/[^a-z0-9]/g, "");
export const displayLabel = (name: string, label?: string): string => (label && squash(label) !== squash(name) ? `${label} (${name})` : (label ?? name));

const fleetHit = (group: GroupName, kind: "locations" | "systems" | "components", e: SearchEntity): Hit => ({
  value: `${kind}:${e.id}`,
  label: displayLabel(e.name, e.label),
  detail: entityDetail(e),
  group,
  command: { label: e.name, path: fleetHref(kind, e.id), group },
});

// searchHits is the server's answer as rows, in the palette's group order.
export function searchHits(r: SearchResult | undefined): Hit[] {
  if (!r) return [];
  const catalog: Hit[] = r.catalog.flatMap((c) => {
    const k = CATALOG_KINDS[c.kind];
    if (!k) return [];
    return [
      {
        value: `catalog:${c.kind}:${c.name}`,
        label: displayLabel(c.name, c.label),
        detail: [k.noun, c.detail].filter(Boolean).join(" · "),
        group: "Catalog" as const,
        command: { label: c.name, path: "", group: "Catalog", entity: { kind: k.drawer, address: c.name } },
      },
    ];
  });
  return [
    ...r.locations.map((e) => fleetHit("Locations", "locations", e)),
    ...r.systems.map((e) => fleetHit("Systems", "systems", e)),
    ...r.components.map((e) => fleetHit("Components", "components", e)),
    ...catalog,
  ];
}

export const pageHit = (c: Command): Hit => ({
  value: `page:${c.path}`,
  label: c.label,
  detail: c.group,
  group: "Pages",
  command: c,
});

export const askHit = (query: string): Hit => ({
  value: "ask",
  label: query ? `Ask omnibot: ${query}` : "Ask omnibot",
  group: "Actions",
  ask: query,
});

// layout orders hits into the palette's groups, each group present only when it
// has a row, "Ask omnibot" always the last row of the last group. With no query
// the groups are Recent then every page; with one, the pages that match it, then
// what the server found. `searching` withholds the ask row while a search is
// out: it would otherwise be the only row, and so the one Enter takes, a moment
// before the real matches arrive.
export function layout(opts: { pages: Hit[]; found: Hit[]; recent: Hit[]; query: string; canAsk: boolean; searching: boolean }): Hit[] {
  const typed = opts.query.trim() !== "";
  const rows = typed ? [...opts.pages, ...opts.found] : [...opts.recent, ...opts.pages];
  const ordered = GROUPS.flatMap((g) => rows.filter((h) => h.group === g));
  if (opts.canAsk && !(typed && opts.searching)) ordered.push(askHit(opts.query.trim()));
  return ordered;
}

// sections folds rows (already in group order) into the groups the list draws.
export function sections<T extends { group: GroupName }>(rows: T[]): { label: GroupName; options: T[] }[] {
  const out: { label: GroupName; options: T[] }[] = [];
  for (const r of rows) {
    const last = out[out.length - 1];
    if (last && last.label === r.group) last.options.push(r);
    else out.push({ label: r.group, options: [r] });
  }
  return out;
}

// --- recents ---

// Recent items live in sessionStorage under the person's id: the settings engine
// has no per-user rung yet (architecture/settings: the user tier is a fast-follow),
// and a per-browser store would hand one person's recents to the next at a shared
// workstation. They last for the tab's session.
export const RECENT_MAX = 5;
const recentKey = (userId: string) => `og-palette-recent:${userId}`;
type Stored = { label: string; detail?: string; path: string };

export function readRecent(userId: string, store: Storage = sessionStorage): Hit[] {
  try {
    const raw = JSON.parse(store.getItem(recentKey(userId)) ?? "[]") as Stored[];
    return raw
      .filter((s) => s && typeof s.path === "string" && typeof s.label === "string")
      .slice(0, RECENT_MAX)
      .map((s) => ({ value: `recent:${s.path}`, label: s.label, detail: s.detail, group: "Recent" as const, command: { label: s.label, path: s.path, group: "Recent" } }));
  } catch {
    return [];
  }
}

// remember puts a picked row first in the person's recents, once, capped.
export function remember(userId: string, hit: Hit, path: string, store: Storage = sessionStorage): void {
  if (hit.ask !== undefined) return;
  try {
    const cur = (JSON.parse(store.getItem(recentKey(userId)) ?? "[]") as Stored[]).filter((s) => s.path !== path);
    store.setItem(recentKey(userId), JSON.stringify([{ label: hit.label, detail: hit.detail, path }, ...cur].slice(0, RECENT_MAX)));
  } catch {
    // Storage is full or blocked: the palette works without recents.
  }
}

