import type { CommandTypeRow } from "./command_types";
import type { PropertyRow } from "./properties";
import type { MetricRow } from "./metric_types";
import type { Driver } from "./drivers";
import type { Endpoint } from "./endpoints";
import { readCommandValue } from "./commands";

// The command form's controls, generated from schemas (#2466). A command type
// declares what it takes in two places: the type it TARGETS (a property type's
// validation fragment, or a metric type's data type and unit) describes its
// value, and its own params_schema describes its params. Both are JSON Schema
// fragments, so one PURE function (schema in, control description out) decides
// the control, and the form only renders what it is handed: an enum is a select,
// a number carries its range and unit and refuses what is outside it, a boolean
// is a switch, and free text is for a free string and nothing narrower.
//
// Validation lives here in TypeScript, never in a native min/max attribute
// (validation-guard.test.ts): the same refusal reads the same on every surface.

type Base = {
  // Where the value goes: "value" is the intended value, "params.<name>" a param.
  field: string;
  label: string;
  description?: string;
  required: boolean;
};

export type ControlSpec = Base &
  (
    // `numeric`: the options are numbers, sent as numbers.
    | { kind: "select"; options: string[]; numeric?: boolean }
    | { kind: "number"; integer: boolean; min?: number; max?: number; unit?: string }
    // The two values a switch sends: true/false, or the strings "true"/"false"
    // a string-typed property enumerates.
    | { kind: "switch"; on: unknown; off: unknown }
    // `json`: a property's value reads as a JSON scalar when it parses as one.
    | { kind: "text"; pattern?: string; json?: boolean }
  );

type Schema = Record<string, unknown>;
const isObject = (v: unknown): v is Schema => typeof v === "object" && v !== null && !Array.isArray(v);
const paramName = (field: string) => field.replace(/^params\./, "");
const num = (v: unknown): number | undefined => (typeof v === "number" && Number.isFinite(v) ? v : undefined);

// A unit that means a fixed range: a metric type declares no range of its own,
// so the one a unit implies is the only one there is to enforce.
const UNIT_RANGES: Record<string, [number, number]> = { ratio: [0, 1], percent: [0, 100] };

export function controlFromSchema(
  field: string,
  schema: unknown,
  ctx: { label?: string; description?: string; required?: boolean; unit?: string; json?: boolean },
): ControlSpec {
  const s = isObject(schema) ? schema : {};
  const base: Base = {
    field,
    label: [ctx.label, s.title, paramName(field)].find((x): x is string => typeof x === "string" && x !== "")!,
    description: ctx.description ?? (typeof s.description === "string" ? s.description : undefined),
    required: ctx.required ?? true,
  };
  const en = Array.isArray(s.enum) ? s.enum : undefined;
  if (en && en.length > 0) {
    const strings = en.map(String);
    if (en.every((v) => typeof v === "string") && strings.length === 2 && strings.includes("true") && strings.includes("false")) {
      return { ...base, kind: "switch", on: "true", off: "false" };
    }
    return { ...base, kind: "select", options: strings, numeric: en.every((v) => typeof v === "number") || undefined };
  }
  if (s.type === "boolean") return { ...base, kind: "switch", on: true, off: false };
  if (s.type === "integer" || s.type === "number") {
    let min = num(s.minimum);
    let max = num(s.maximum);
    const implied = ctx.unit ? UNIT_RANGES[ctx.unit] : undefined;
    if (implied && min === undefined && max === undefined) [min, max] = implied;
    return { ...base, kind: "number", integer: s.type === "integer", min, max, unit: ctx.unit };
  }
  return { ...base, kind: "text", pattern: typeof s.pattern === "string" ? s.pattern : undefined, json: ctx.json };
}

// commandControls is every control a command type's issue form shows: its value
// (from the type it targets) then its params (from its params_schema). A target
// type the caller could not read leaves the value as free text, the old form.
export function commandControls(
  ct: CommandTypeRow,
  types: { properties?: readonly PropertyRow[]; metrics?: readonly MetricRow[] },
): ControlSpec[] {
  const out: ControlSpec[] = [];
  if (ct.target_metric_type) {
    const m = types.metrics?.find((x) => x.name === ct.target_metric_type);
    // A metric lane stores numbers whatever the type could be read.
    const schema = { type: m?.data_type === "int" ? "integer" : "number" };
    out.push(controlFromSchema("value", schema, { label: "Value", unit: m?.unit, description: m?.description }));
  } else if (ct.target_property_type) {
    const p = types.properties?.find((x) => x.name === ct.target_property_type);
    const v = isObject(p?.validation) ? p.validation : {};
    const schema = p?.data_type === "bool" ? { type: "boolean" } : v;
    out.push(controlFromSchema("value", schema, { label: "Value", json: true, description: p?.description }));
  }
  const ps = isObject(ct.params_schema) ? ct.params_schema : undefined;
  if (ps && isObject(ps.properties)) {
    const required = Array.isArray(ps.required) ? ps.required.map(String) : [];
    for (const [name, sub] of Object.entries(ps.properties)) {
      out.push(controlFromSchema(`params.${name}`, sub, { label: name, required: required.includes(name) }));
    }
  }
  return out;
}

export const rangeWords = (c: { min?: number; max?: number; unit?: string }): string | undefined => {
  const u = c.unit ? ` ${c.unit}` : "";
  if (c.min !== undefined && c.max !== undefined) return `${c.min} to ${c.max}${u}`;
  if (c.min !== undefined) return `${c.min} or more${u}`;
  if (c.max !== undefined) return `${c.max} or less${u}`;
  return undefined;
};

export type ControlRead = { value?: unknown; error?: string; omitted?: boolean };

// readControl judges one typed draft against its control.
export function readControl(c: ControlSpec, typed: string): ControlRead {
  const t = typed.trim();
  switch (c.kind) {
    case "switch":
      return { value: typed === "on" ? c.on : c.off };
    case "select": {
      if (t === "") return c.required ? { error: `Choose ${c.label.toLowerCase()}.` } : { omitted: true };
      if (!c.options.includes(t)) return { error: `${t} is not one of ${c.options.join(", ")}.` };
      return { value: c.numeric ? Number(t) : t };
    }
    case "number": {
      if (t === "") return c.required ? { error: "Enter the number to set." } : { omitted: true };
      const n = Number(t);
      if (!Number.isFinite(n)) return { error: "Enter a number." };
      if (c.integer && !Number.isInteger(n)) return { error: "Enter a whole number." };
      const range = rangeWords(c);
      if ((c.min !== undefined && n < c.min) || (c.max !== undefined && n > c.max)) return { error: `${t} is outside ${range}.` };
      return { value: n };
    }
    case "text": {
      if (t === "") return c.required ? { error: "Enter the value to set." } : { omitted: true };
      if (c.pattern && !new RegExp(c.pattern).test(t)) return { error: `The value does not match ${c.pattern}.` };
      if (c.json) return readCommandValue(typed, "property");
      return { value: t };
    }
  }
}

export type Draft = Record<string, string>;
export const initialDraft = (controls: readonly ControlSpec[]): Draft =>
  Object.fromEntries(controls.map((c) => [c.field, c.kind === "switch" ? "off" : ""]));

export type DraftRead = { value?: unknown; params?: Record<string, unknown>; error?: string; errors: Record<string, string> };

// readDraft reads every control; `errors` is by field so each shows beside its
// own control, `error` the first for the submit gate.
export function readDraft(controls: readonly ControlSpec[], draft: Draft): DraftRead {
  const errors: Record<string, string> = {};
  const out: DraftRead = { errors };
  for (const c of controls) {
    const r = readControl(c, draft[c.field] ?? "");
    if (r.error) {
      errors[c.field] = r.error;
      continue;
    }
    if (r.omitted) continue;
    if (c.field === "value") out.value = r.value;
    else (out.params ??= {})[paramName(c.field)] = r.value;
  }
  out.error = Object.values(errors)[0];
  if (!out.error) delete out.error;
  return out;
}

// commandGap says why no command can reach a component: the drivers its
// endpoints use that bind none, and the drivers that bind some (the fix), the
// product's own first.
export type DriverOffer = { driver: string; commands: string[] };
export function commandGap(
  componentId: string,
  endpoints: readonly Endpoint[],
  drivers: readonly Driver[],
  productDriver: string | undefined,
): { attachedWithout: string[]; offering: DriverOffer[]; productDriver?: DriverOffer } {
  const bindsOf = (d: Driver) => [...new Set((d.spec?.commands ?? []).map((b) => b.command_type))].sort();
  const byName = new Map(drivers.map((d) => [d.name, d]));
  const attached = [...new Set(endpoints.filter((e) => e.component_id === componentId && e.driver).map((e) => e.driver!))];
  const attachedWithout = attached.filter((n) => {
    const d = byName.get(n);
    return d && bindsOf(d).length === 0;
  });
  const offering = drivers
    .map((d) => ({ driver: d.name, commands: bindsOf(d) }))
    .filter((o) => o.commands.length > 0)
    .sort((a, b) => (a.driver === productDriver ? -1 : b.driver === productDriver ? 1 : a.driver.localeCompare(b.driver)));
  return { attachedWithout, offering, productDriver: offering.find((o) => o.driver === productDriver) };
}
