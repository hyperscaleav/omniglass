import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import type { Chip } from "./predicate";
import { entityLabel } from "./entities";

// The template catalog's data layer (#827, #1329): the binary's shipped
// templates, read-only, and the four routes that start something from one. A
// template is an example an operator clones: forking a standard template or
// instantiating a component, system or place template writes ordinary rows the
// operator owns, and nothing points back at the template (ADR-0071). Shapes are
// the generated client's; everything below the fetches is pure, so the page's
// rules (which kinds a URL asks for, what a form posts, where a success lands)
// are unit-tested without a server.

export type Template = components["schemas"]["TemplateBody"];
export type TemplateKind = Template["kind"];
export type TemplatePlace = components["schemas"]["TemplatePlaceBody"];
export type TemplateRole = components["schemas"]["TemplateRoleBody"];

export type ForkStandardBody = components["schemas"]["ForkStandardInputBody"];
export type InstantiateComponentBody = components["schemas"]["InstantiateComponentInputBody"];
export type InstantiateComponentResult = components["schemas"]["InstantiateComponentOutputBody"];
export type InstantiateSystemBody = components["schemas"]["InstantiateSystemInputBody"];
export type InstantiateSystemResult = components["schemas"]["InstantiateSystemOutputBody"];
export type InstantiateLocationsBody = components["schemas"]["InstantiateLocationsInputBody"];
export type InstantiateLocationsResult = components["schemas"]["InstantiateLocationsOutputBody"];

// The kinds in catalog order, and the one word an operator reads for each.
export const TEMPLATE_KINDS: TemplateKind[] = ["standard_template", "system_template", "place_template", "component_template"];
export const KIND_WORD: Record<TemplateKind, string> = {
  standard_template: "standard",
  system_template: "system",
  place_template: "place",
  component_template: "component",
};

// One cache entry for the whole catalog: the page and the Catalog's three
// Templates counts read the same list and filter it by kind.
export const TEMPLATES_KEY = ["templates"] as const;

export async function listTemplates(): Promise<Template[]> {
  const { data, error } = await api.GET("/templates", {});
  if (error) throw error;
  return data?.templates ?? [];
}

export async function forkStandard(body: ForkStandardBody): Promise<{ name: string }> {
  const { data, error } = await api.POST("/standards:fork", { body });
  if (error) throw error;
  return data as { name: string };
}

export async function instantiateComponent(body: InstantiateComponentBody): Promise<InstantiateComponentResult> {
  const { data, error } = await api.POST("/components:instantiate", { body });
  if (error) throw error;
  return data as InstantiateComponentResult;
}

export async function instantiateSystem(body: InstantiateSystemBody): Promise<InstantiateSystemResult> {
  const { data, error } = await api.POST("/systems:instantiate", { body });
  if (error) throw error;
  return data as InstantiateSystemResult;
}

export async function instantiateLocations(body: InstantiateLocationsBody): Promise<InstantiateLocationsResult> {
  const { data, error } = await api.POST("/locations:instantiate", { body });
  if (error) throw error;
  return data as InstantiateLocationsResult;
}

// --- Which kinds, and where they are asked for ------------------------------

const isKind = (s: string): s is TemplateKind => (TEMPLATE_KINDS as string[]).includes(s);

// parseKinds reads `?kind=` (a comma list, so the Catalog's Systems slot can ask
// for standard and system templates at once). Unknown words are dropped, the
// order is the catalog's, and an empty answer means every kind.
export function parseKinds(raw: string | string[] | undefined | null): TemplateKind[] {
  const s = Array.isArray(raw) ? raw.join(",") : (raw ?? "");
  const asked = new Set(s.split(",").map((x) => x.trim()).filter(isKind));
  return TEMPLATE_KINDS.filter((k) => asked.has(k));
}

// templatesHref is the address of the Templates page filtered to kinds, with a
// place to start at carried as the form's default target.
export function templatesHref(kinds: TemplateKind[], place?: string): string {
  const q = new URLSearchParams();
  if (kinds.length) q.set("kind", kinds.join(","));
  if (place) q.set("place", place);
  const qs = q.toString().replace(/%2C/g, ",");
  return `/templates${qs ? `?${qs}` : ""}`;
}

// The kind a fleet create flow starts from: a location's create starts from a
// place template, the others from their own kind.
export const CREATE_KIND: Record<"location" | "system" | "component", TemplateKind> = {
  location: "place_template",
  system: "system_template",
  component: "component_template",
};

// The kind facet is the URL's: an equals chip on `kind` is the `?kind=` list,
// and every other chip stays the page's own. kindChip builds the chip from the
// URL; splitKindChip reads an edited chip set back into the two halves.
export const KIND_FACET = "kind";

export function kindChip(kinds: TemplateKind[]): Chip[] {
  return kinds.length ? [{ key: KIND_FACET, op: "eq", values: [...kinds] }] : [];
}

export function splitKindChip(chips: Chip[]): { kinds: TemplateKind[]; rest: Chip[] } {
  const kinds = new Set<TemplateKind>();
  const rest: Chip[] = [];
  for (const c of chips) {
    if (c.key === KIND_FACET && c.op === "eq") {
      for (const v of c.values) if (isKind(v)) kinds.add(v);
    } else {
      rest.push(c);
    }
  }
  return { kinds: TEMPLATE_KINDS.filter((k) => kinds.has(k)), rest };
}

export function ofKinds(rows: Template[], kinds: TemplateKind[]): Template[] {
  return kinds.length ? rows.filter((t) => kinds.includes(t.kind)) : rows;
}

// --- Who may start one ---------------------------------------------------------

// The permission a kind's start route is gated by: a fork writes a standard, an
// instantiate writes the collection it is a custom method on.
export const START_RESOURCE: Record<TemplateKind, "standard" | "system" | "location" | "component"> = {
  standard_template: "standard",
  system_template: "system",
  place_template: "location",
  component_template: "component",
};

// --- What a template creates ---------------------------------------------------

// summary is the one line a list row says about what the template makes.
export function templateSummary(t: Template): string {
  if (t.standard) {
    const roles = t.standard.roles?.length ?? 0;
    const head = `${roles} ${roles === 1 ? "role" : "roles"}`;
    return t.standard.parent ? `${head}, a variant of ${t.standard.parent}` : head;
  }
  if (t.system) {
    const roles = Object.keys(t.system.roles ?? {}).length;
    return `built to ${t.system.standard}, ${roles} ${roles === 1 ? "component" : "components"}`;
  }
  if (t.place) {
    const rows = placeTreeRows(t.place);
    const places = rows.filter((r) => r.kind === "place").length;
    const systems = rows.filter((r) => r.kind === "system").length;
    const p = `${places} ${places === 1 ? "place" : "places"}`;
    return systems ? `${p}, ${systems} ${systems === 1 ? "system" : "systems"}` : p;
  }
  if (t.component) return t.component.product;
  return "";
}

// A place template's subtree, flattened in pre-order for the indented tree: a
// place row, then the systems that land in it, then the places beneath it.
export type PlaceTreeRow =
  | { kind: "place"; depth: number; type: string; name?: string; label?: string }
  | { kind: "system"; depth: number; template: string; label?: string };

export function placeTreeRows(p: TemplatePlace, depth = 0): PlaceTreeRow[] {
  const out: PlaceTreeRow[] = [{ kind: "place", depth, type: p.type, name: p.name, label: p.label }];
  for (const s of p.systems ?? []) out.push({ kind: "system", depth: depth + 1, template: s.template, label: s.label });
  for (const c of p.places ?? []) out.push(...placeTreeRows(c, depth + 1));
  return out;
}

// A role a system template staffs, with how many components fill it: the
// quorum its standard template declares (one when the catalog does not hold
// it), and what fills it.
export type RoleSlot = { role: string; label: string; count: number; fill: string };

export function systemRoleSlots(t: Template, all: Template[]): RoleSlot[] {
  if (!t.system) return [];
  const std = all.find((x) => x.kind === "standard_template" && x.name === t.system!.standard);
  const declared = new Map((std?.standard?.roles ?? []).map((r) => [r.name, r]));
  return Object.entries(t.system.roles ?? {})
    .map(([role, f]) => {
      const r = declared.get(role);
      const comp = f.component ? all.find((x) => x.kind === "component_template" && x.name === f.component) : undefined;
      return {
        role,
        label: r ? entityLabel(r) : role,
        count: Math.max(1, r?.quorum ?? 1),
        fill: comp ? entityLabel(comp) : (f.component ?? f.product ?? ""),
      };
    })
    .sort((a, b) => declaredOrder(std, a.role) - declaredOrder(std, b.role) || a.role.localeCompare(b.role));
}

function declaredOrder(std: Template | undefined, role: string): number {
  const i = (std?.standard?.roles ?? []).findIndex((r) => r.name === role);
  return i < 0 ? Number.MAX_SAFE_INTEGER : i;
}

// The quorum cell: "2" for an exact role, "1 to 3" for a capped range, "2 or
// more" for an uncapped one beyond its floor.
export function quorumText(r: TemplateRole): string {
  if (r.capacity == null) return r.quorum > 1 ? `${r.quorum} or more` : "1 or more";
  return r.capacity === r.quorum ? String(r.quorum) : `${r.quorum} to ${r.capacity}`;
}

// A declared value, read back as text for a fact.
export function valueText(v: unknown): string {
  return typeof v === "string" ? v : JSON.stringify(v);
}

// --- What each form posts -----------------------------------------------------

const opt = (s: string | undefined): string | undefined => (s && s.trim() ? s.trim() : undefined);

export type ForkDraft = { name: string; label: string; parent: string };

export function forkBody(template: string, d: ForkDraft): ForkStandardBody {
  return { template, name: opt(d.name), label: opt(d.label), parent: opt(d.parent) };
}

export type ComponentDraft = {
  place: string;
  name: string;
  label: string;
  node: string;
  address: string;
  // A driver secret input's name -> the name of a secret of yours; empty omits it.
  secrets: Record<string, string>;
};

// The driver input an address rides on (template.AddressInput in Go).
export const ADDRESS_INPUT = "host";

export function componentBody(template: string, d: ComponentDraft): InstantiateComponentBody {
  const inputs: Record<string, string> = {};
  if (opt(d.address)) inputs[ADDRESS_INPUT] = d.address.trim();
  for (const [k, v] of Object.entries(d.secrets)) if (opt(v)) inputs[k] = v.trim();
  return {
    template,
    location: opt(d.place),
    name: opt(d.name),
    label: opt(d.label),
    node: opt(d.node),
    inputs: Object.keys(inputs).length ? inputs : undefined,
  };
}

export type SystemDraft = {
  place: string;
  name: string;
  label: string;
  standard: string;
  node: string;
  // Per role, one address per component slot in order; an empty one leaves
  // that component unattached.
  addresses: Record<string, string[]>;
};

export function systemBody(template: string, d: SystemDraft): InstantiateSystemBody {
  const attach: Record<string, Record<string, string>[]> = {};
  for (const [role, list] of Object.entries(d.addresses)) {
    // Keep the slots in order up to the last address given, so the second
    // component of a role can attach while the first does not.
    let last = -1;
    list.forEach((a, i) => { if (opt(a)) last = i; });
    if (last < 0) continue;
    attach[role] = list.slice(0, last + 1).map((a): Record<string, string> => (opt(a) ? { [ADDRESS_INPUT]: a.trim() } : {}));
  }
  return {
    template,
    location: opt(d.place),
    name: opt(d.name),
    label: opt(d.label),
    standard: opt(d.standard),
    node: opt(d.node),
    attach: Object.keys(attach).length ? attach : undefined,
  };
}

export type PlacesDraft = { parent: string; name: string; label: string };

export function placesBody(template: string, d: PlacesDraft): InstantiateLocationsBody {
  return { template, parent: opt(d.parent), name: opt(d.name), label: opt(d.label) };
}

// An address with no node to place its endpoint on is refused by the API
// (422); the form says so beside the node before submit.
export function needsNode(d: { node: string; address?: string; addresses?: Record<string, string[]> }): boolean {
  if (opt(d.node)) return false;
  if (opt(d.address)) return true;
  return Object.values(d.addresses ?? {}).some((l) => l.some((a) => !!opt(a)));
}

// --- Where a success lands ------------------------------------------------------

// FROM_PARAM carries the template's name to the created row's page, where the
// notice says it is now the operator's own.
export const FROM_PARAM = "from";

export function landingHref(base: string, template: string, extra: Record<string, string> = {}): string {
  const q = new URLSearchParams({ ...extra, [FROM_PARAM]: template });
  return `${base}?${q.toString()}`;
}

// findCreated picks the created row out of a fresh list by the name the route
// answered and where it was placed: names are scoped to placement, so the name
// alone can be two rows, and the placement's uuid tells them apart.
export function findCreated<T extends { id: string; name: string }>(
  rows: T[],
  name: string,
  where: (r: T) => string | undefined,
  placedAt: string | undefined,
): T | undefined {
  const named = rows.filter((r) => r.name === name);
  if (named.length <= 1) return named[0];
  return named.find((r) => (where(r) ?? "") === (placedAt ?? ""));
}

// --- Which place --------------------------------------------------------------

// A place is offered and posted by its dotted path (hq.west.level-2), which the
// start routes take and which is unique, so the form's choice reads the same in
// a script as on screen; a row the list has not pathed yet falls back to its
// uuid. placeRef resolves a carried default (the create flows carry a uuid) to
// that value, and placeId resolves the value back to the uuid a created row's
// placement is compared on.
type PlaceRow = { id: string; name: string; path?: string };

export function placeValue(l: PlaceRow): string {
  return l.path || l.id;
}

export function placeRef(rows: PlaceRow[], ref: string | undefined): string {
  if (!ref) return "";
  const hit = rows.find((l) => l.id === ref || l.path === ref) ?? (rows.filter((l) => l.name === ref).length === 1 ? rows.find((l) => l.name === ref) : undefined);
  return hit ? placeValue(hit) : ref;
}

export function placeId(rows: PlaceRow[], value: string): string | undefined {
  if (!value) return undefined;
  return rows.find((l) => l.path === value || l.id === value)?.id ?? value;
}
