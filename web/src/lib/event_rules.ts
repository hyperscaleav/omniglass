import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import type { SeverityLevel } from "./severity";
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
export type RuleCriteria = components["schemas"]["CriteriaBody"];
export type RuleOp = RuleCriteria["op"];
export type RuleLane = EventRule["sample_lane"];
export type RuleState = components["schemas"]["EventRuleStateOutputBody"];
export type RuleOpenAlarm = components["schemas"]["RuleOpenAlarmBody"];
export type RuleStreak = components["schemas"]["RuleStreakBody"];
export type CreateEventRule = components["schemas"]["CreateEventRuleInputBody"];
export type UpdateEventRule = components["schemas"]["UpdateEventRuleInputBody"];

export const EVENT_RULES_KEY = ["event_rules"] as const;
export const ruleStateKey = (name: string) => ["event_rules", name, "state"] as const;

export async function listEventRules(): Promise<EventRule[]> {
  const { data, error } = await api.GET("/event-rules");
  if (error) throw error;
  return (data?.event_rules ?? []) as EventRule[];
}

export async function getRuleState(name: string): Promise<{ open: RuleOpenAlarm[]; pending: RuleStreak[]; last_judged_at?: string }> {
  const { data, error } = await api.GET("/event-rules/{name}/state", { params: { path: { name } } });
  if (error) throw error;
  return { open: data?.open ?? [], pending: data?.pending ?? [], last_judged_at: data?.last_judged_at };
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

// criteriaPhrase is the value half of a criterion: "down", "anything but up",
// "above 40". A metric's unit rides after its number when the registry names one.
export function criteriaPhrase(c: RuleCriteria, lane: RuleLane, unit?: string): string {
  const v = valueText(c.value);
  if (lane === "property") return c.op === "ne" ? `anything but ${v}` : v;
  const n = unit ? `${v} ${unit}` : v;
  switch (c.op) {
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
  clearMode: ClearMode;
  clearOp: RuleOp;
  clearValue: string;
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
    fireOp: "eq", fireValue: "", clearMode: "opposite", clearOp: "eq", clearValue: "",
    fireSustain: "", clearSustain: "", severity, fireEventType: GENERIC_FIRE_EVENT_TYPE,
    clearEventType: GENERIC_CLEAR_EVENT_TYPE, message: "", enabled: true,
    componentTypes: [], components: [],
  };
}

export function draftFromRule(r: EventRule): RuleDraft {
  return {
    name: r.name,
    label: r.label ?? "",
    description: r.description ?? "",
    sampleKey: r.sample_key,
    lane: r.sample_lane,
    fireOp: r.fire_criteria.op,
    fireValue: valueText(r.fire_criteria.value),
    clearMode: r.clear_criteria ? "custom" : "opposite",
    clearOp: r.clear_criteria?.op ?? "eq",
    clearValue: r.clear_criteria ? valueText(r.clear_criteria.value) : "",
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

export type DraftErrors = Partial<Record<"name" | "sampleKey" | "fireValue" | "clearValue" | "fireSustain" | "clearSustain" | "severity", string>>;

// validateDraft is the form's one judgement, shared by create and edit (the
// name is create-only, so edit skips it). A severity is required, except on
// an edit of a rule that has none: an informational rule stays informational
// (#948; choosing "None" on any rule is #951).
export function validateDraft(d: RuleDraft, mode: "create" | "edit", was?: EventRule): DraftErrors {
  const e: DraftErrors = {};
  if (mode === "create") {
    if (!d.name.trim()) e.name = "A name for the rule.";
    else if (!NAME.test(d.name.trim())) e.name = "Lowercase letters, digits and single hyphens, e.g. too-hot.";
  }
  if (!d.sampleKey) e.sampleKey = "The property or metric the rule reads.";
  else {
    const f = readValue(d.fireValue, d.lane);
    if (f.error) e.fireValue = f.error;
    if (d.clearMode === "custom") {
      const c = readValue(d.clearValue, d.lane);
      if (c.error) e.clearValue = c.error;
    }
  }
  const fs = readDuration(d.fireSustain);
  if (fs) e.fireSustain = fs;
  const cs = readDuration(d.clearSustain);
  if (cs) e.clearSustain = cs;
  const informational = mode === "edit" && !!was && !was.severity;
  if (!d.severity && !informational) e.severity = "The level the alarm is raised at.";
  return e;
}

const sustainOut = (s: string) => (s.trim() === "" || s.trim() === "0" ? "0s" : s.trim());

function criteria(op: RuleOp, raw: string, lane: RuleLane): RuleCriteria {
  return { op, value: readValue(raw, lane).value };
}

// createBody: the POST a valid draft sends. Optional text is left off when blank;
// a clear criterion rides only when the author chose one (absent clears on the
// opposite).
export function createBody(d: RuleDraft): CreateEventRule {
  const body: CreateEventRule = {
    name: d.name.trim(),
    sample_key: d.sampleKey,
    fire_criteria: criteria(d.fireOp, d.fireValue, d.lane),
    fire_sustain: sustainOut(d.fireSustain),
    clear_sustain: sustainOut(d.clearSustain),
    severity: d.severity,
    fire_event_type: d.fireEventType,
    enabled: d.enabled,
    selector: { component_types: d.componentTypes, components: d.components },
  };
  if (d.clearEventType) body.clear_event_type = d.clearEventType;
  if (d.label.trim()) body.label = d.label.trim();
  if (d.description.trim()) body.description = d.description.trim();
  if (d.message.trim()) body.message = d.message.trim();
  if (d.clearMode === "custom") body.clear_criteria = criteria(d.clearOp, d.clearValue, d.lane);
  return body;
}

const sameCriteria = (a: RuleCriteria | undefined, b: RuleCriteria | undefined) =>
  a?.op === b?.op && JSON.stringify(a?.value) === JSON.stringify(b?.value);
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
  const fire = criteria(d.fireOp, d.fireValue, d.lane);
  if (!sameCriteria(fire, was.fire_criteria)) body.fire_criteria = fire;
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
    const clear = criteria(d.clearOp, d.clearValue, d.lane);
    if (!sameCriteria(clear, was.clear_criteria ?? undefined)) body.clear_criteria = clear;
  } else if (was.clear_criteria) body.clear_negates = true;
  return body;
}
