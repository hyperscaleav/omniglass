import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { entityNameError } from "./names";
import { CALC_ALIAS_MAX, CALC_RESERVED_NAMES } from "./calc_names.gen";

// The calc rules data layer (#999, #1018, epic #525; ADR-0147, ADR-0148): thin
// typed wrappers over /calc-rules plus the pure core the Calc rules page reads
// a rule through. A calc rule derives one output key on each owner it selects
// (each component, or each system from its role-bound members) by an
// expression over its inputs, each bound by its alias as the list of its
// current values. Rules read like a registry:
// any rule:read lists them all, with live counts over the owners the caller
// may read; writing one takes an all-scope grant.
//
// The pure half (the alias rules, the draft a form edits, the bodies it
// sends, the start-from templates) takes plain values, so the form's rules are unit-tested
// without a component.

// The Go server sends an empty list as null; the layer reads every list as a
// list, so the page never guards one.
type Lists<T, K extends keyof T> = Omit<T, K> & { [P in K]-?: NonNullable<T[P]> };
export type CalcInput = components["schemas"]["CalcInputBody"];
export type CalcInputRead = components["schemas"]["CalcInputReadBody"];
export type CalcRule = Lists<components["schemas"]["CalcRuleBody"], "inputs">;
export type CalcReduce = components["schemas"]["CalcReduceBody"];
export type CalcSelector = components["schemas"]["CalcSelectorBody"];
export type CalcOwnerState = Lists<components["schemas"]["CalcOwnerStateBody"], "missing" | "skipped" | "stale">;
export type CalcRuleState = { owners: CalcOwnerState[]; total: number; pending_depth: number };
export type CalcPreviewInput = components["schemas"]["CalcPreviewInputBody"];
export type CalcPreviewOwner = Lists<components["schemas"]["CalcPreviewOwnerBody"], "missing" | "skipped" | "stale">;
export type CalcPreview = Omit<components["schemas"]["CalcPreviewOutputBody"], "owners"> & { owners: CalcPreviewOwner[] };
export type CreateCalcRule = components["schemas"]["CreateCalcRuleInputBody"];
export type UpdateCalcRule = components["schemas"]["UpdateCalcRuleInputBody"];
export type OwnerKind = CalcRule["owner_kind"];

export const CALC_RULES_KEY = ["calc_rules"] as const;
export const calcStateKey = (name: string, limit?: number) => ["calc_rules", name, "state", limit ?? 0] as const;

// calcRuleHref is where a calc rule opens: the Calc rules page, its blade open.
// The ref is the rule's name or its uuid; the page resolves either.
export const calcRuleHref = (ref: string) => `/calc-rules?rule=${encodeURIComponent(ref)}`;

export async function listCalcRules(): Promise<CalcRule[]> {
  const { data, error } = await api.GET("/calc-rules");
  if (error) throw error;
  return (data?.calc_rules ?? []).map(normalizeRule);
}

function normalizeRule(r: components["schemas"]["CalcRuleBody"]): CalcRule {
  return { ...r, inputs: r.inputs ?? [] };
}

function lists<T extends { missing?: string[] | null; skipped?: unknown[] | null; stale?: unknown[] | null }>(o: T) {
  return { ...o, missing: o.missing ?? [], skipped: o.skipped ?? [], stale: o.stale ?? [] };
}

// getCalcState reads a rule's state on the owners the caller may read, at
// most limit of them in the server's order (failing, pending, missing,
// valued), with how many there are in all.
export async function getCalcState(name: string, limit?: number): Promise<CalcRuleState> {
  const { data, error } = await api.GET("/calc-rules/{name}/state", { params: { path: { name }, query: limit ? { limit } : {} } });
  if (error) throw error;
  return {
    owners: (data?.owners ?? []).map((o) => lists(o) as CalcOwnerState),
    total: data?.total ?? 0,
    pending_depth: data?.pending_depth ?? 0,
  };
}

export async function createCalcRule(body: CreateCalcRule): Promise<CalcRule> {
  const { data, error } = await api.POST("/calc-rules", { body });
  if (error) throw error;
  return normalizeRule(data as components["schemas"]["CalcRuleBody"]);
}

export async function updateCalcRule(name: string, body: UpdateCalcRule): Promise<CalcRule> {
  const { data, error } = await api.PATCH("/calc-rules/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return normalizeRule(data as components["schemas"]["CalcRuleBody"]);
}

// renameCalcRule moves a rule's name: its own custom method, as for event
// rules (#977); nothing inside the platform holds the name.
export async function renameCalcRule(name: string, to: string): Promise<CalcRule> {
  const { data, error } = await api.POST("/calc-rules/{name}:rename", { params: { path: { name } }, body: { name: to } });
  if (error) throw error;
  return normalizeRule(data as components["schemas"]["CalcRuleBody"]);
}

export async function deleteCalcRule(name: string): Promise<void> {
  const { error } = await api.DELETE("/calc-rules/{name}", { params: { path: { name } } });
  if (error) throw error;
}

export async function previewCalcRule(body: CalcPreviewInput): Promise<CalcPreview> {
  const { data, error } = await api.POST("/calc-rules:preview", { body });
  if (error) throw error;
  return { ...(data as components["schemas"]["CalcPreviewOutputBody"]), owners: (data?.owners ?? []).map((o) => lists(o) as CalcPreviewOwner) };
}

// ---- the value types ------------------------------------------------------

export type ValueType = "number" | "text" | "bool";

// valueTypeOf is the value type a key reads as: a metric is a number, a bool
// property a bool, any other property text.
export function valueTypeOf(lane: "metric" | "property", dataType?: string): ValueType {
  if (lane === "metric") return "number";
  return dataType === "bool" ? "bool" : "text";
}

// liveWords reads a rule's live counts for a title: "3 computed, 1 pending".
export function liveWords(r: Pick<CalcRule, "owners_computed" | "owners_pending" | "owners_unsupported" | "owners_failed">): string {
  const parts = [
    r.owners_computed ? `${r.owners_computed} computed` : "",
    r.owners_pending ? `${r.owners_pending} pending` : "",
    r.owners_unsupported ? `${r.owners_unsupported} without inputs` : "",
    r.owners_failed ? `${r.owners_failed} failing` : "",
  ].filter(Boolean);
  return parts.length ? parts.join(", ") : "nothing computed yet";
}

// ---- aliases -----------------------------------------------------------------

// An input's alias (#1018) is the name the reduce's expression binds it by,
// held to the server's rules, generated from them (calc_names.gen.ts, make
// gen) so the form refuses exactly what a save would: an identifier of at
// most CALC_ALIAS_MAX, not a reserved name, unique in the rule.
const IDENT = /^[A-Za-z_][A-Za-z0-9_]*$/;
const RESERVED = new Set(CALC_RESERVED_NAMES);

// aliasError says what is wrong with an alias, given the other inputs'
// aliases; undefined when it is fine.
export function aliasError(alias: string, others: string[]): string | undefined {
  if (!alias) return "An alias, the name the expression uses for this input.";
  if (!IDENT.test(alias)) return "An alias is a letter or underscore, then letters, digits or underscores.";
  if (alias.length > CALC_ALIAS_MAX) return `An alias is at most ${CALC_ALIAS_MAX} characters.`;
  if (RESERVED.has(alias)) return `${alias} is reserved: a keyword, a function's name, or one the expression binds itself.`;
  if (others.includes(alias)) return `${alias} names two inputs.`;
  return undefined;
}

// suggestAlias is the alias a key's name suggests: its name in snake_case
// (camel humps split, every run of other characters one underscore), made
// valid: a leading digit prefixed with _, a reserved or taken name suffixed
// _2, _3, and so on, never past CALC_ALIAS_MAX.
export function suggestAlias(key: string, taken: string[]): string {
  let base = key
    .replace(/([a-z0-9])([A-Z])/g, "$1_$2")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
  if (!base) base = "input";
  if (/^[0-9]/.test(base)) base = "_" + base;
  base = base.slice(0, CALC_ALIAS_MAX);
  if (!aliasError(base, taken)) return base;
  for (let n = 2; ; n++) {
    const suffix = `_${n}`;
    const candidate = base.slice(0, CALC_ALIAS_MAX - suffix.length) + suffix;
    if (!aliasError(candidate, taken)) return candidate;
  }
}

// ---- the form draft -------------------------------------------------------

// A draft input: its alias and key, and whether the author has edited the
// alias (until then, choosing a key fills it from the key's name).
export type DraftInput = { alias: string; key: string; aliasEdited: boolean };

export type CalcDraft = {
  name: string;
  label: string;
  description: string;
  ownerKind: OwnerKind;
  componentTypes: string[];
  components: string[];
  systemTypes: string[];
  systems: string[];
  inputs: DraftInput[];
  expr: string;
  outputKey: string;
  enabled: boolean;
};

export function emptyCalcDraft(): CalcDraft {
  return {
    name: "", label: "", description: "", ownerKind: "component", componentTypes: [], components: [], systemTypes: [], systems: [],
    inputs: [{ alias: "", key: "", aliasEdited: false }], expr: "", outputKey: "", enabled: true,
  };
}

export function draftFromCalcRule(r: CalcRule): CalcDraft {
  return {
    name: r.name,
    label: r.label ?? "",
    description: r.description ?? "",
    ownerKind: r.owner_kind,
    componentTypes: [...(r.selector?.component_types ?? [])],
    components: [...(r.selector?.components ?? [])],
    systemTypes: [...(r.selector?.system_types ?? [])],
    systems: [...(r.selector?.systems ?? [])],
    inputs: (r.inputs ?? []).map((i) => ({ alias: i.alias, key: i.key, aliasEdited: true })),
    expr: r.reduce.expr ?? "",
    outputKey: r.output_key,
    enabled: r.enabled,
  };
}

// withInputKey is the inputs with one input's key chosen: its alias filled
// from the key's name (suggestAlias, clear of the other inputs' aliases)
// unless the author has edited it.
export function withInputKey(inputs: DraftInput[], i: number, key: string): DraftInput[] {
  return inputs.map((x, j) => {
    if (j !== i) return x;
    if (x.aliasEdited) return { ...x, key };
    const taken = inputs.filter((_, k) => k !== i).map((y) => y.alias);
    return { ...x, key, alias: key ? suggestAlias(key, taken) : "" };
  });
}

export type CalcDraftErrors = Partial<Record<"name" | "inputs" | "reduce" | "outputKey", string>> & { input?: Record<number, string> };

// validateCalcDraft is the form's one judgement, on create and edit alike.
export function validateCalcDraft(d: CalcDraft, editing?: string): CalcDraftErrors {
  const e: CalcDraftErrors = {};
  const name = d.name.trim();
  if (!name) e.name = "A name for the rule.";
  else if (name !== editing && entityNameError(name)) e.name = entityNameError(name)!;
  const input: Record<number, string> = {};
  const seenKey = new Set<string>();
  d.inputs.forEach((x, i) => {
    const earlier = d.inputs.slice(0, i).map((y) => y.alias);
    if (!x.key) input[i] = "The key this input reads.";
    else if (seenKey.has(x.key)) input[i] = `${x.key} is read twice; read it once and use its alias.`;
    else {
      const a = aliasError(x.alias, earlier);
      if (a) input[i] = a;
    }
    seenKey.add(x.key);
  });
  if (Object.keys(input).length) e.input = input;
  if (d.inputs.length === 0) e.inputs = "At least one input.";
  if (!d.expr.trim()) e.reduce = "An expression over the inputs' aliases, e.g. max(" + (d.inputs[0]?.alias || "x") + ").";
  if (!d.outputKey) e.outputKey = "The property or metric the rule writes.";
  return e;
}

export function hasErrors(e: CalcDraftErrors): boolean {
  return Object.keys(e).some((k) => k !== "input") || Object.keys(e.input ?? {}).length > 0;
}

// boundInputs are the inputs an expression check can bind now: each with a
// key and an alias the server would accept, the first of a repeated alias.
export function boundInputs(inputs: DraftInput[]): CalcInput[] {
  const out: CalcInput[] = [];
  for (const x of inputs) {
    if (x.key && !aliasError(x.alias, out.map((y) => y.alias)) && !out.some((y) => y.key === x.key)) out.push({ alias: x.alias, key: x.key });
  }
  return out;
}

function selectorOut(d: CalcDraft): CalcSelector {
  return d.ownerKind === "system"
    ? { system_types: d.systemTypes, systems: d.systems }
    : { component_types: d.componentTypes, components: d.components };
}

const inputsOut = (d: CalcDraft): CalcInput[] => d.inputs.map((i) => ({ alias: i.alias, key: i.key }));

// createCalcBody: the POST a valid draft sends.
export function createCalcBody(d: CalcDraft): CreateCalcRule {
  const body: CreateCalcRule = {
    name: d.name.trim(),
    owner_kind: d.ownerKind,
    selector: selectorOut(d),
    inputs: inputsOut(d),
    reduce: { expr: d.expr },
    output_key: d.outputKey,
    enabled: d.enabled,
  };
  if (d.label.trim()) body.label = d.label.trim();
  if (d.description.trim()) body.description = d.description.trim();
  return body;
}

const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

// updateCalcBody: the PATCH an edit sends, carrying only what it changed (the
// owner kind is fixed at create, and the name moves by :rename).
export function updateCalcBody(d: CalcDraft, was: CalcRule): UpdateCalcRule {
  const body: UpdateCalcRule = {};
  if (d.label.trim() !== (was.label ?? "")) body.label = d.label.trim();
  if (d.description.trim() !== (was.description ?? "")) body.description = d.description.trim();
  const sel = selectorOut(d);
  const wasSel = was.owner_kind === "system"
    ? { system_types: was.selector?.system_types ?? [], systems: was.selector?.systems ?? [] }
    : { component_types: was.selector?.component_types ?? [], components: was.selector?.components ?? [] };
  if (!same(sel, wasSel)) body.selector = sel;
  const inputs = inputsOut(d);
  if (!same(inputs, was.inputs.map((i) => ({ alias: i.alias, key: i.key })))) body.inputs = inputs;
  if (d.expr !== (was.reduce.expr ?? "")) body.reduce = { expr: d.expr };
  if (d.outputKey !== was.output_key) body.output_key = d.outputKey;
  if (d.enabled !== was.enabled) body.enabled = d.enabled;
  return body;
}

// previewCalcBody: the candidate a preview computes. An edit names the saved
// rule, so its own stored version is not a second writer of its output.
export function previewCalcBody(d: CalcDraft, editing?: string): CalcPreviewInput {
  const body: CalcPreviewInput = {
    owner_kind: d.ownerKind,
    selector: selectorOut(d),
    inputs: inputsOut(d),
    reduce: { expr: d.expr },
    output_key: d.outputKey,
  };
  if (editing) body.name = editing;
  return body;
}

// ---- start-from templates ---------------------------------------------------

// A template inserts a list function with an empty slot, the cursor in it,
// so the author's next keystroke (or a completion) names the input to fold.
// label draws the slot as ▮.
export type CalcTemplate = { label: string; text: string; cursor: number; title: string };

function template(text: string, cursor: number, title: string): CalcTemplate {
  return { label: text.slice(0, cursor) + "▮" + text.slice(cursor), text, cursor, title };
}

// calcTemplates are the templates for a rule of an owner kind: the list
// functions over one input's values, and on a system a count over its
// members' records.
export function calcTemplates(ownerKind: OwnerKind): CalcTemplate[] {
  const out = [
    template("max()", 4, "The largest of an input's values"),
    template("min()", 4, "The smallest of an input's values"),
    template("avg()", 4, "The mean of an input's values"),
    template("sum()", 4, "The total of an input's values"),
    template("len()", 4, "How many values an input has"),
    template("any()", 4, "Whether any of an input's bools is true"),
    template("all()", 4, "Whether every one of an input's bools is true"),
    template("majority()", 9, "The most common of an input's values; a tie goes to the one that sorts first"),
  ];
  if (ownerKind === "system") {
    out.push(template("count(members, )", 15, "How many members pass a test of their own values, e.g. .has.t && .value.t > 30"));
  }
  return out;
}

// hiddenWords says how many members or instances the caller may not read:
// counted, never named.
export function hiddenWords(n: number, what: string): string {
  return n === 1 ? `1 ${what} you cannot read` : `${n} ${what}s you cannot read`;
}
