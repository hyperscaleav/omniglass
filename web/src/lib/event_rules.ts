import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import type { SeverityLevel } from "./severity";
import type { PreviewEventRule } from "./expressions";
import { rel } from "./format";

// The event rules data layer (#918, #921; ADR-0144): thin typed wrappers over
// /event-rules plus the pure core the Rules page reads a rule through. A rule
// reads one sample key (a property or a metric, whose lane the server derives
// from the registries), opens an alarm at its severity when a sample meets its
// fire criteria for the fire sustain, and clears it on its clear criteria (or,
// absent one, the opposite of the fire criteria) held for the clear sustain.
// Rules read like a registry: any rule:read lists them all, with live counts
// over the components the caller may read; writing one takes an all-scope
// grant. Official rules ship with the platform and are read-only.
//
// The pure half (the words a criterion reads as, the ops a lane allows, the
// draft a form edits and the bodies it sends) takes plain values, so the form's
// rules are unit-tested without a component.

export type EventRule = components["schemas"]["EventRuleBody"];
// A stored criterion is an expression (#957); a body may still send the short
// form {op, value}, which the server lowers (eq "down" is value == "down").
export type RuleCriteria = components["schemas"]["CriteriaBody"];
export type RuleCriteriaInput = components["schemas"]["CriteriaInput"];
export type RuleOp = NonNullable<RuleCriteriaInput["op"]>;
export type ShortForm = { op: RuleOp; value: string | number };
export type RuleLane = EventRule["sample_lane"];
export type RuleState = components["schemas"]["EventRuleStateOutputBody"];
export type RuleOpenAlarm = components["schemas"]["RuleOpenAlarmBody"];
export type RuleStreak = components["schemas"]["RuleStreakBody"];
export type RuleCondition = components["schemas"]["RuleConditionBody"];
export type CreateEventRule = components["schemas"]["CreateEventRuleInputBody"];
export type UpdateEventRule = components["schemas"]["UpdateEventRuleInputBody"];

export const EVENT_RULES_KEY = ["event_rules"] as const;
export const ruleStateKey = (name: string) => ["event_rules", name, "state"] as const;

export async function listEventRules(): Promise<EventRule[]> {
  const { data, error } = await api.GET("/event-rules");
  if (error) throw error;
  return (data?.event_rules ?? []) as EventRule[];
}

export async function getRuleState(name: string): Promise<{ conditions: RuleCondition[]; open: RuleOpenAlarm[]; pending: RuleStreak[]; last_judged_at?: string }> {
  const { data, error } = await api.GET("/event-rules/{name}/state", { params: { path: { name } } });
  if (error) throw error;
  return { conditions: data?.conditions ?? [], open: data?.open ?? [], pending: data?.pending ?? [], last_judged_at: data?.last_judged_at };
}

// judgedWords says when a rule last judged an arriving sample (#929): the
// server stamps it at most once per rule every 5s, so the words carry that
// bound. A rule that has judged nothing says so.
export function judgedWords(at: string | undefined): string {
  return at ? `Last judged a sample ${rel(at)} (to within 5s)` : "Has not judged a sample yet";
}

export async function createEventRule(body: CreateEventRule): Promise<EventRule> {
  const { data, error } = await api.POST("/event-rules", { body });
  if (error) throw error;
  return data as EventRule;
}

export async function updateEventRule(name: string, body: UpdateEventRule): Promise<EventRule> {
  const { data, error } = await api.PATCH("/event-rules/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return data as EventRule;
}

export async function deleteEventRule(name: string): Promise<void> {
  const { error } = await api.DELETE("/event-rules/{name}", { params: { path: { name } } });
  if (error) throw error;
}

// ---- the lane and its comparisons ------------------------------------------

// A property compares text, so only equality means anything; a metric compares
// numbers with all six. The server refuses the rest with a 422; the form never
// offers them.
export const OPS_BY_LANE: Record<RuleLane, RuleOp[]> = {
  property: ["eq", "ne"],
  metric: ["eq", "ne", "gt", "ge", "lt", "le"],
};

// The words each op reads as in a select, lane-aware: "is" for text, a
// comparison for numbers.
export function opLabel(op: RuleOp, lane: RuleLane): string {
  if (lane === "property") return op === "eq" ? "is" : "is not";
  return { eq: "equals", ne: "does not equal", gt: "above", ge: "at or above", lt: "below", le: "at or below" }[op];
}

// ---- reading a rule in words -----------------------------------------------

// sustainWords renders a Go duration the way an operator says it: "5m0s" is
// 5m and "1h0m0s" is 1h, and a zero sustain is no sustain at all (""), since
// "for 0s" says nothing.
export function sustainWords(d: string | undefined): string {
  const s = (d ?? "").trim();
  if (s === "" || /^0+(\.0+)?(ns|us|µs|ms|s|m|h)?$/.test(s)) return "";
  return s.replace(/(\d)m0s$/, "$1m").replace(/(\d)h0m$/, "$1h");
}

function valueText(v: unknown): string {
  return typeof v === "string" ? v : JSON.stringify(v);
}

// shortForm reads a stored expression back as the builder's {op, value}
// when it is one the server's lowering writes: value, a comparison, and a
// JSON string, a number, or true or false. Anything else (an expression an
// author wrote) has no short form: the console shows it as written and never
// overwrites it (editing it arrives with the expression field, #960).
const SYMBOL_OP: Record<string, RuleOp> = { "==": "eq", "!=": "ne", ">": "gt", ">=": "ge", "<": "lt", "<=": "le" };
const LOWERED = /^value (==|!=|>=|<=|>|<) (.+)$/;

export function shortForm(c: RuleCriteria | null | undefined): ShortForm | null {
  const m = c ? LOWERED.exec(c.expr) : null;
  if (!m) return null;
  const lit = m[2];
  if (lit === "true" || lit === "false") return { op: SYMBOL_OP[m[1]], value: lit };
  // A plain decimal only: the lowering writes numbers as Postgres does, with
  // no exponent, so a literal with one is an author's expression.
  if (!/^("(?:[^"\\]|\\.)*"|-?\d+(\.\d+)?)$/.test(lit)) return null;
  try {
    const v: unknown = JSON.parse(lit);
    return typeof v === "string" || typeof v === "number" ? { op: SYMBOL_OP[m[1]], value: v } : null;
  } catch {
    return null;
  }
}

// criteriaErrorWords says where a rule's criteria failed to compile: "fire
// criteria, line 1, column 7", the position only when it has one.
export function criteriaErrorWords(e: NonNullable<EventRule["criteria_error"]>): string {
  const where = e.field === "clear_criteria" ? "clear criteria" : "fire criteria";
  return e.line ? `${where}, line ${e.line}, column ${e.column}` : where;
}

// criteriaPhrase is the value half of a criterion: "down", "anything but up",
// "above 40". A metric's unit rides after its number when the registry names
// one; an expression with no short form reads as itself.
export function criteriaPhrase(c: RuleCriteria, lane: RuleLane, unit?: string): string {
  const sf = shortForm(c);
  if (!sf) return c.expr;
  const v = valueText(sf.value);
  if (lane === "property") return sf.op === "ne" ? `anything but ${v}` : v;
  const n = unit ? `${v} ${unit}` : v;
  switch (sf.op) {
    case "gt": return `above ${n}`;
    case "ge": return `at or above ${n}`;
    case "lt": return `below ${n}`;
    case "le": return `at or below ${n}`;
    case "ne": return `anything but ${n}`;
    default: return n;
  }
}

// fireWords: "reads down for 15s", or "reads above 40" when it fires at once.
export function fireWords(r: Pick<EventRule, "fire_criteria" | "fire_sustain" | "sample_lane">, unit?: string): string {
  const s = sustainWords(r.fire_sustain);
  return `reads ${criteriaPhrase(r.fire_criteria, r.sample_lane, unit)}${s ? ` for ${s}` : ""}`;
}

// clearWords: "clears on up", "clears on the opposite", either with "after 30s"
// when the clear holds a sustain of its own.
export function clearWords(r: Pick<EventRule, "clear_criteria" | "clear_sustain" | "sample_lane">, unit?: string): string {
  const s = sustainWords(r.clear_sustain);
  const what = r.clear_criteria ? criteriaPhrase(r.clear_criteria, r.sample_lane, unit) : "the opposite";
  return `clears on ${what}${s ? ` after ${s}` : ""}`;
}

// ---- the form draft --------------------------------------------------------

export type ClearMode = "opposite" | "custom";

export type RuleDraft = {
  name: string;
  label: string;
  description: string;
  sampleKey: string;
  lane: RuleLane;
  fireOp: RuleOp;
  fireValue: string;
  // fireExpr is a stored fire expression the builder cannot say (no short
  // form), kept as written; null when the builder's op and value are it.
  fireExpr: string | null;
  clearMode: ClearMode;
  clearOp: RuleOp;
  clearValue: string;
  clearExpr: string | null;
  fireSustain: string;
  clearSustain: string;
  severity: string;
  fireEventType: string;
  clearEventType: string;
  message: string;
  enabled: boolean;
  componentTypes: string[];
  components: string[];
};

// defaultSeverity is the most severe level that does NOT take its component
// out: a new rule degrades until its author decides it is an outage. Judged on
// the registry's own rank and outage flag, never on a level's name.
export function defaultSeverity(levels: SeverityLevel[] | undefined): string {
  const sorted = [...(levels ?? [])].sort((a, b) => a.rank - b.rank);
  const degrading = sorted.filter((l) => !l.outage);
  return (degrading[degrading.length - 1] ?? sorted[0])?.name ?? "";
}

// The official generic event types a rule writes when it needs no semantic
// type of its own (#948): a new rule starts on them until the form offers the
// registry (#951).
export const GENERIC_FIRE_EVENT_TYPE = "condition-started";
export const GENERIC_CLEAR_EVENT_TYPE = "condition-ended";

export function emptyDraft(severity = ""): RuleDraft {
  return {
    name: "", label: "", description: "", sampleKey: "", lane: "property",
    fireOp: "eq", fireValue: "", fireExpr: null, clearMode: "opposite", clearOp: "eq", clearValue: "", clearExpr: null,
    fireSustain: "", clearSustain: "", severity, fireEventType: GENERIC_FIRE_EVENT_TYPE,
    clearEventType: GENERIC_CLEAR_EVENT_TYPE, message: "", enabled: true,
    componentTypes: [], components: [],
  };
}

export function draftFromRule(r: EventRule): RuleDraft {
  const fire = shortForm(r.fire_criteria);
  const clear = r.clear_criteria ? shortForm(r.clear_criteria) : null;
  return {
    name: r.name,
    label: r.label ?? "",
    description: r.description ?? "",
    sampleKey: r.sample_key,
    lane: r.sample_lane,
    fireOp: fire?.op ?? "eq",
    fireValue: fire ? valueText(fire.value) : "",
    // An empty expression is no expression: the builder owns it.
    fireExpr: fire ? null : r.fire_criteria.expr || null,
    clearMode: r.clear_criteria ? "custom" : "opposite",
    clearOp: clear?.op ?? "eq",
    clearValue: clear ? valueText(clear.value) : "",
    clearExpr: r.clear_criteria && !clear ? r.clear_criteria.expr || null : null,
    fireSustain: sustainWords(r.fire_sustain),
    clearSustain: sustainWords(r.clear_sustain),
    severity: r.severity ?? "",
    fireEventType: r.fire_event_type,
    clearEventType: r.clear_event_type ?? "",
    message: r.message ?? "",
    enabled: r.enabled,
    componentTypes: [...(r.selector?.component_types ?? [])],
    components: [...(r.selector?.components ?? [])],
  };
}

// withLane moves a draft to a new sample key's lane, pulling any op the new
// lane cannot evaluate back to eq (a metric's "above" means nothing to text).
export function withLane(d: RuleDraft, sampleKey: string, lane: RuleLane): RuleDraft {
  const ok = OPS_BY_LANE[lane];
  return {
    ...d,
    sampleKey,
    lane,
    fireOp: ok.includes(d.fireOp) ? d.fireOp : "eq",
    clearOp: ok.includes(d.clearOp) ? d.clearOp : "eq",
  };
}

// A Go duration, as time.ParseDuration reads one: one or more number-unit
// pairs (30s, 5m, 1h30m, 1.5s). Blank is 0s.
const GO_DURATION = /^(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+$/;

export function readDuration(raw: string): string | null {
  const s = raw.trim();
  if (s === "" || s === "0") return null;
  return GO_DURATION.test(s) ? null : "A duration like 30s, 5m or 1h30m; blank holds for no time.";
}

// readValue types a criterion's value by lane: text for a property, a number
// for a metric. The error is the inline verdict; the value is what is sent.
// Nothing is trimmed: a property compares text exactly, so " down" would never
// match "down", and the form refuses edge spaces with a reason rather than
// sending something other than what was typed. A metric is a plain decimal
// (40, -2.5): Number() would also take hex, exponents and blanks, which read
// as something else to a person.
const DECIMAL = /^-?\d+(\.\d+)?$/;

export function readValue(raw: string, lane: RuleLane): { value?: string | number; error: string | null } {
  if (raw.trim() === "") return { error: lane === "metric" ? "A number to compare against." : "The value to compare against." };
  if (raw !== raw.trim()) return { error: "No leading or trailing spaces: the value is compared exactly as written." };
  if (lane === "property") return { value: raw, error: null };
  if (!DECIMAL.test(raw)) return { error: "A metric compares numbers; this is not one. Write a plain decimal, like 40 or -2.5." };
  return { value: Number(raw), error: null };
}

const NAME = /^[a-z0-9]+(-[a-z0-9]+)*$/;

export type DraftErrors = Partial<Record<"name" | "sampleKey" | "fireValue" | "clearValue" | "fireExpr" | "clearExpr" | "fireSustain" | "clearSustain" | "fireEventType", string>>;

// validateDraft is the form's one judgement, shared by create and edit (the
// name is create-only, so edit skips it). No severity is a choice, not a gap:
// "None (informational)" makes a rule that records its events and raises no
// alarm (#951). The fire event type is required; the clear one is optional.
export function validateDraft(d: RuleDraft, mode: "create" | "edit"): DraftErrors {
  const e: DraftErrors = {};
  if (mode === "create") {
    if (!d.name.trim()) e.name = "A name for the rule.";
    else if (!NAME.test(d.name.trim())) e.name = "Lowercase letters, digits and single hyphens, e.g. too-hot.";
  }
  if (!d.sampleKey) e.sampleKey = "The property or metric the rule reads.";
  else {
    const f = readValue(d.fireValue, d.lane);
    if (d.fireExpr !== null) {
      if (!d.fireExpr.trim()) e.fireExpr = "An expression that is true when the condition starts.";
    } else if (f.error) e.fireValue = f.error;
    if (d.clearMode === "custom" && d.clearExpr !== null) {
      if (!d.clearExpr.trim()) e.clearExpr = "An expression that is true when the condition ends.";
    } else if (d.clearMode === "custom") {
      const c = readValue(d.clearValue, d.lane);
      if (c.error) e.clearValue = c.error;
    }
  }
  const fs = readDuration(d.fireSustain);
  if (fs) e.fireSustain = fs;
  const cs = readDuration(d.clearSustain);
  if (cs) e.clearSustain = cs;
  if (!d.fireEventType) e.fireEventType = "The event the rule writes when its condition starts.";
  return e;
}

const sustainOut = (s: string) => (s.trim() === "" || s.trim() === "0" ? "0s" : s.trim());

function criteria(op: RuleOp, raw: string, lane: RuleLane): RuleCriteriaInput {
  return { op, value: readValue(raw, lane).value };
}

const fireOut = (d: RuleDraft): RuleCriteriaInput => (d.fireExpr !== null ? { expr: d.fireExpr } : criteria(d.fireOp, d.fireValue, d.lane));
const clearOut = (d: RuleDraft): RuleCriteriaInput => (d.clearExpr !== null ? { expr: d.clearExpr } : criteria(d.clearOp, d.clearValue, d.lane));

// createBody: the POST a valid draft sends. Optional text is left off when blank;
// a clear criterion rides only when the author chose one (absent clears on the
// opposite).
export function createBody(d: RuleDraft): CreateEventRule {
  const body: CreateEventRule = {
    name: d.name.trim(),
    sample_key: d.sampleKey,
    fire_criteria: fireOut(d),
    fire_sustain: sustainOut(d.fireSustain),
    clear_sustain: sustainOut(d.clearSustain),
    fire_event_type: d.fireEventType,
    enabled: d.enabled,
    selector: { component_types: d.componentTypes, components: d.components },
  };
  // No severity is an informational rule: it records its events, raises no alarm.
  if (d.severity) body.severity = d.severity;
  if (d.clearEventType) body.clear_event_type = d.clearEventType;
  if (d.label.trim()) body.label = d.label.trim();
  if (d.description.trim()) body.description = d.description.trim();
  if (d.message.trim()) body.message = d.message.trim();
  if (d.clearMode === "custom") body.clear_criteria = clearOut(d);
  return body;
}

// sameCriteria compares the builder's short form with a stored expression by
// reading the expression back (shortForm), not by re-lowering it here.
const sameCriteria = (a: RuleCriteriaInput, b: RuleCriteria | null | undefined) => {
  const sf = shortForm(b);
  return !!sf && sf.op === a.op && JSON.stringify(sf.value) === JSON.stringify(a.value);
};
const sameList = (a: string[], b: string[] | null | undefined) => a.length === (b ?? []).length && a.every((x, i) => x === b![i]);
const sameSustain = (draft: string, stored: string | undefined) => sustainWords(sustainOut(draft)) === sustainWords(stored);

// updateBody: the PATCH an edit sends, carrying only what the edit changed
// (absent is unchanged on the server). Restating an unchanged field is not
// harmless: the server judges what it is sent, so a rule whose selected
// component was since deleted could not take a label edit if the selector rode
// along. The selector replaces wholesale, so it rides whole once any entry
// moves; choosing "the opposite" over a rule that had a clear criterion says so
// with clear_negates, since an absent clear_criteria changes nothing.
export function updateBody(d: RuleDraft, was: EventRule): UpdateEventRule {
  const body: UpdateEventRule = {};
  if (d.label.trim() !== (was.label ?? "")) body.label = d.label.trim();
  if (d.description.trim() !== (was.description ?? "")) body.description = d.description.trim();
  if (d.message.trim() !== (was.message ?? "")) body.message = d.message.trim();
  if (d.sampleKey !== was.sample_key) body.sample_key = d.sampleKey;
  // An expression rides when it differs from the stored one; the builder's
  // comparison when it differs from what the stored expression reads back as.
  if (d.fireExpr !== null) {
    if (d.fireExpr !== was.fire_criteria.expr) body.fire_criteria = { expr: d.fireExpr };
  } else {
    const fire = criteria(d.fireOp, d.fireValue, d.lane);
    if (!sameCriteria(fire, was.fire_criteria)) body.fire_criteria = fire;
  }
  if (!sameSustain(d.fireSustain, was.fire_sustain)) body.fire_sustain = sustainOut(d.fireSustain);
  if (!sameSustain(d.clearSustain, was.clear_sustain)) body.clear_sustain = sustainOut(d.clearSustain);
  if (d.severity !== (was.severity ?? "")) body.severity = d.severity;
  if (d.fireEventType !== was.fire_event_type) body.fire_event_type = d.fireEventType;
  if (d.clearEventType !== (was.clear_event_type ?? "")) body.clear_event_type = d.clearEventType;
  if (d.enabled !== was.enabled) body.enabled = d.enabled;
  if (!sameList(d.componentTypes, was.selector?.component_types) || !sameList(d.components, was.selector?.components)) {
    body.selector = { component_types: d.componentTypes, components: d.components };
  }
  if (d.clearMode === "custom") {
    if (d.clearExpr !== null) {
      if (d.clearExpr !== was.clear_criteria?.expr) body.clear_criteria = { expr: d.clearExpr };
    } else {
      const clear = criteria(d.clearOp, d.clearValue, d.lane);
      if (!sameCriteria(clear, was.clear_criteria)) body.clear_criteria = clear;
    }
  } else if (was.clear_criteria) body.clear_negates = true;
  return body;
}

// ---- criteria as expressions (#960) ------------------------------------------

const OP_SYMBOL: Record<RuleOp, string> = { eq: "==", ne: "!=", gt: ">", ge: ">=", lt: "<", le: "<=" };

// lowerShort is the expression the server stores a comparison as (rules.Lower
// in internal/rules): text JSON-quoted, a number made a float literal, a bool
// property's threshold that is neither true nor false the constant it always
// meant. The builder shows it beneath the comparison, so the short form
// teaches the language. Null when the comparison has no value yet.
export function lowerShort(op: RuleOp, value: string | number | undefined, lane: RuleLane, dataType?: string): string | null {
  if (value === undefined || value === "") return null;
  const sym = OP_SYMBOL[op];
  if (lane === "metric") {
    if (typeof value !== "number" || !Number.isFinite(value)) return null;
    const n = String(value);
    return `value ${sym} ${/[.e]/.test(n) ? n : `${n}.0`}`;
  }
  const text = String(value);
  if (dataType === "bool") {
    if (text === "true" || text === "false") return `value ${sym} ${text}`;
    return op === "eq" ? "false" : "true";
  }
  return `value ${sym} ${JSON.stringify(text)}`;
}

type Side = "fire" | "clear";

// toExpression switches one criterion to an expression, seeded with what its
// comparison lowers to (or empty when it has none yet).
export function toExpression(d: RuleDraft, side: Side, dataType?: string): RuleDraft {
  const op = side === "fire" ? d.fireOp : d.clearOp;
  const raw = side === "fire" ? d.fireValue : d.clearValue;
  const expr = lowerShort(op, readValue(raw, d.lane).value, d.lane, dataType) ?? "";
  return side === "fire" ? { ...d, fireExpr: expr } : { ...d, clearExpr: expr };
}

// toBuilder switches one criterion back to the comparison builder: the
// expression's own comparison when it is one, else the builder's last.
export function toBuilder(d: RuleDraft, side: Side): RuleDraft {
  const expr = side === "fire" ? d.fireExpr : d.clearExpr;
  const sf = expr === null ? null : shortForm({ expr });
  if (side === "fire") return { ...d, fireExpr: null, ...(sf ? { fireOp: sf.op, fireValue: valueText(sf.value) } : {}) };
  return { ...d, clearExpr: null, ...(sf ? { clearOp: sf.op, clearValue: valueText(sf.value) } : {}) };
}

// previewBody is the draft as a candidate rule for POST /event-rules:preview:
// what it reads, what it watches and its criteria, as a save would send them.
export function previewBody(d: RuleDraft): PreviewEventRule {
  return {
    sample_key: d.sampleKey,
    selector: { component_types: d.componentTypes, components: d.components },
    fire_criteria: fireOut(d),
    clear_criteria: d.clearMode === "custom" ? clearOut(d) : undefined,
  };
}
