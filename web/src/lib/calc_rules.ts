import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { entityNameError } from "./names";

// The calc rules data layer (#999, epic #525; ADR-0147, ADR-0148): thin typed
// wrappers over /calc-rules plus the pure core the Calc rules page reads a rule
// through. A calc rule derives one output key on each owner it selects (each
// component, or each system from its role-bound members) by folding its named
// inputs with a named reducer or an expression. Rules read like a registry:
// any rule:read lists them all, with live counts over the owners the caller
// may read; writing one takes an all-scope grant.
//
// The pure half (the words a reduce reads as, the draft a form edits, the
// bodies it sends) takes plain values, so the form's rules are unit-tested
// without a component.

// The Go server sends an empty list as null; the layer reads every list as a
// list, so the page never guards one.
type Lists<T, K extends keyof T> = Omit<T, K> & { [P in K]-?: NonNullable<T[P]> };
export type CalcInput = components["schemas"]["CalcInputBody"];
export type CalcRule = Lists<components["schemas"]["CalcRuleBody"], "inputs">;
export type CalcReduce = components["schemas"]["CalcReduceBody"];
export type CalcSelector = components["schemas"]["CalcSelectorBody"];
export type CalcOwnerState = Lists<components["schemas"]["CalcOwnerStateBody"], "missing" | "skipped" | "stale">;
export type CalcRuleState = { owners: CalcOwnerState[]; pending_depth: number };
export type CalcPreviewInput = components["schemas"]["CalcPreviewInputBody"];
export type CalcPreviewOwner = Lists<components["schemas"]["CalcPreviewOwnerBody"], "missing" | "skipped" | "stale">;
export type CalcPreview = Omit<components["schemas"]["CalcPreviewOutputBody"], "owners"> & { owners: CalcPreviewOwner[] };
export type CreateCalcRule = components["schemas"]["CreateCalcRuleInputBody"];
export type UpdateCalcRule = components["schemas"]["UpdateCalcRuleInputBody"];
export type OwnerKind = CalcRule["owner_kind"];
export type Reducer = NonNullable<CalcReduce["reducer"]>;

export const CALC_RULES_KEY = ["calc_rules"] as const;
export const calcStateKey = (name: string) => ["calc_rules", name, "state"] as const;

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

export async function getCalcState(name: string): Promise<CalcRuleState> {
  const { data, error } = await api.GET("/calc-rules/{name}/state", { params: { path: { name } } });
  if (error) throw error;
  return { owners: (data?.owners ?? []).map((o) => lists(o) as CalcOwnerState), pending_depth: data?.pending_depth ?? 0 };
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

// ---- the reducers ---------------------------------------------------------

// The named reducers and the input value types each folds (the calc site's
// declaration, docs/src/generated/expressions.json); the server refuses any
// other pairing with a 422, so the form offers only these.
export type ValueType = "number" | "text" | "bool";
export const REDUCERS: { name: Reducer; accepts: ValueType[]; words: string }[] = [
  { name: "average", accepts: ["number"], words: "the average of" },
  { name: "min", accepts: ["number"], words: "the lowest" },
  { name: "max", accepts: ["number"], words: "the highest" },
  { name: "sum", accepts: ["number"], words: "the total of" },
  { name: "count", accepts: ["number", "text", "bool"], words: "how many values of" },
  { name: "majority", accepts: ["text", "bool"], words: "the most common" },
  { name: "any", accepts: ["bool"], words: "whether any of" },
  { name: "all", accepts: ["bool"], words: "whether all of" },
];

// reducersFor are the reducers that fold an input of this value type.
export function reducersFor(vt: ValueType | undefined): Reducer[] {
  return vt ? REDUCERS.filter((r) => r.accepts.includes(vt)).map((r) => r.name) : [];
}

// valueTypeOf is the value type a key reads as: a metric is a number, a bool
// property a bool, any other property text.
export function valueTypeOf(lane: "metric" | "property", dataType?: string): ValueType {
  if (lane === "metric") return "number";
  return dataType === "bool" ? "bool" : "text";
}

// reduceWords reads a rule's reduce: "average of room-temperature" for a
// named reducer, the expression itself otherwise.
export function reduceWords(r: Pick<CalcRule, "reduce" | "inputs">): string {
  if (r.reduce.reducer) {
    const def = REDUCERS.find((x) => x.name === r.reduce.reducer);
    return `${def?.words ?? r.reduce.reducer} ${r.inputs[0]?.key ?? ""}`.trim();
  }
  return r.reduce.expr ?? "";
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

// ---- the form draft -------------------------------------------------------

export type CalcDraft = {
  name: string;
  label: string;
  description: string;
  ownerKind: OwnerKind;
  componentTypes: string[];
  components: string[];
  systemTypes: string[];
  systems: string[];
  inputs: { as: string; key: string }[];
  mode: "reducer" | "expr";
  reducer: Reducer | "";
  expr: string;
  outputKey: string;
  enabled: boolean;
};

export function emptyCalcDraft(): CalcDraft {
  return {
    name: "", label: "", description: "", ownerKind: "component", componentTypes: [], components: [], systemTypes: [], systems: [],
    inputs: [{ as: "", key: "" }], mode: "reducer", reducer: "", expr: "", outputKey: "", enabled: true,
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
    inputs: (r.inputs ?? []).map((i) => ({ as: i.as, key: i.key })),
    mode: r.reduce.reducer ? "reducer" : "expr",
    reducer: r.reduce.reducer ?? "",
    expr: r.reduce.expr ?? "",
    outputKey: r.output_key,
    enabled: r.enabled,
  };
}

// An input's name: an identifier the reduce can say (the server also refuses
// keywords and builtin names, which it explains in its 422).
const IDENT = /^[A-Za-z_][A-Za-z0-9_]*$/;

export type CalcDraftErrors = Partial<Record<"name" | "inputs" | "reduce" | "outputKey", string>> & { input?: Record<number, string> };

// validateCalcDraft is the form's one judgement, on create and edit alike.
// typeOf answers an input key's value type (undefined when unknown), so a
// named reducer is checked against what it folds.
export function validateCalcDraft(d: CalcDraft, typeOf: (key: string) => ValueType | undefined, editing?: string): CalcDraftErrors {
  const e: CalcDraftErrors = {};
  const name = d.name.trim();
  if (!name) e.name = "A name for the rule.";
  else if (name !== editing && entityNameError(name)) e.name = entityNameError(name)!;
  const input: Record<number, string> = {};
  const seenAs = new Set<string>();
  const seenKey = new Set<string>();
  d.inputs.forEach((x, i) => {
    if (!x.key) input[i] = "The key this input reads.";
    else if (!IDENT.test(x.as)) input[i] = "A name the reduce uses: a letter or underscore, then letters, digits or underscores.";
    else if (seenAs.has(x.as)) input[i] = `${x.as} names two inputs.`;
    else if (seenKey.has(x.key)) input[i] = `${x.key} is read twice; read it once.`;
    seenAs.add(x.as);
    seenKey.add(x.key);
  });
  if (Object.keys(input).length) e.input = input;
  if (d.inputs.length === 0) e.inputs = "At least one input.";
  if (d.mode === "reducer") {
    if (!d.reducer) e.reduce = "A reducer.";
    else if (d.inputs.length !== 1) e.reduce = "A reducer folds one input; use an expression to combine several.";
    else {
      const vt = typeOf(d.inputs[0].key);
      if (vt && !reducersFor(vt).includes(d.reducer)) e.reduce = `${d.reducer} does not fold ${vt} values.`;
    }
  } else if (!d.expr.trim()) e.reduce = "An expression over the inputs.";
  if (!d.outputKey) e.outputKey = "The property or metric the rule writes.";
  return e;
}

export function hasErrors(e: CalcDraftErrors): boolean {
  return Object.keys(e).some((k) => k !== "input") || Object.keys(e.input ?? {}).length > 0;
}

function selectorOut(d: CalcDraft): CalcSelector {
  return d.ownerKind === "system"
    ? { system_types: d.systemTypes, systems: d.systems }
    : { component_types: d.componentTypes, components: d.components };
}

function reduceOut(d: CalcDraft): CalcReduce {
  return d.mode === "reducer" ? { reducer: d.reducer as Reducer } : { expr: d.expr };
}

// createCalcBody: the POST a valid draft sends.
export function createCalcBody(d: CalcDraft): CreateCalcRule {
  const body: CreateCalcRule = {
    name: d.name.trim(),
    owner_kind: d.ownerKind,
    selector: selectorOut(d),
    inputs: d.inputs.map((i) => ({ as: i.as, key: i.key })),
    reduce: reduceOut(d),
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
  const inputs = d.inputs.map((i) => ({ as: i.as, key: i.key }));
  if (!same(inputs, was.inputs.map((i) => ({ as: i.as, key: i.key })))) body.inputs = inputs;
  const reduce = reduceOut(d);
  const wasReduce = was.reduce.reducer ? { reducer: was.reduce.reducer } : { expr: was.reduce.expr };
  if (!same(reduce, wasReduce)) body.reduce = reduce;
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
    inputs: d.inputs.map((i) => ({ as: i.as, key: i.key })),
    reduce: reduceOut(d),
    output_key: d.outputKey,
  };
  if (editing) body.name = editing;
  return body;
}

// hiddenWords says how many members or instances the caller may not read:
// counted, never named.
export function hiddenWords(n: number, what: string): string {
  return n === 1 ? `1 ${what} you cannot read` : `${n} ${what}s you cannot read`;
}
