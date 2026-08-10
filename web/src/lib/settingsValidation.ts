import { settingsSchema } from "../api/settings.schema.gen";

export type Constraint = {
  type?: string;
  enum?: readonly string[];
  pattern?: string;
  minLength?: number;
  maxLength?: number;
  minimum?: number;
  maximum?: number;
};

// constraintFor looks up a generated field constraint by namespace and key.
export function constraintFor(ns: string, key: string): Constraint | undefined {
  return (settingsSchema as Record<string, Record<string, Constraint>>)[ns]?.[key];
}

// isList reports whether a setting is list-valued, from the generated schema
// rather than from the shape of whatever value happens to be resolved: a form
// that guessed from the value would render the wrong control the moment a list
// arrived empty.
export function isList(c: Constraint | undefined): boolean {
  return c?.type === "array";
}

// A list is edited as one line of comma-separated entries and written as a JSON
// array. The two functions below are that translation, and they are a pair on
// purpose: reading a list the console cannot write back is how a control that
// always 422s gets shipped.

// listText renders a stored list as the line an operator edits.
export function listText(value: unknown): string {
  return Array.isArray(value) ? value.map((v) => String(v)).join(", ") : String(value ?? "");
}

// textList parses that line back into the array the API takes. Entries are
// trimmed, and an all-blank line is an empty list rather than a list holding one
// blank, which is the difference between "no acronyms" and a dictionary entry
// that matches nothing.
export function textList(raw: string): string[] {
  if (raw.trim() === "") return [];
  return raw.split(",").map((s) => s.trim());
}

// validateField returns an inline error message for a draft value, or null when it
// satisfies the generated constraint. An unknown field (no constraint) is itself an
// error: the form only edits declared settings.
export function validateField(c: Constraint | undefined, raw: string): string | null {
  if (!c) return "unknown setting";
  if (isList(c)) {
    // A stray or doubled comma is the one mistake this control invites, and it
    // would otherwise be stored as a blank entry: harmless-looking, and matching
    // nothing forever.
    return textList(raw).some((s) => s === "") ? "list has an empty entry" : null;
  }
  if (c.enum && !c.enum.includes(raw)) return `must be one of: ${c.enum.join(", ")}`;
  if ((c.type === "integer" || c.type === "number") && raw !== "" && Number.isNaN(Number(raw))) {
    return "must be a number";
  }
  if (raw !== "" && !Number.isNaN(Number(raw))) {
    if (c.minimum !== undefined && Number(raw) < c.minimum) return `must be at least ${c.minimum}`;
    if (c.maximum !== undefined && Number(raw) > c.maximum) return `must be at most ${c.maximum}`;
  }
  if (c.pattern && !new RegExp(c.pattern).test(raw)) return `must match ${c.pattern}`;
  if (c.minLength !== undefined && raw.length < c.minLength) return `must be at least ${c.minLength} characters`;
  if (c.maxLength !== undefined && raw.length > c.maxLength) return `must be at most ${c.maxLength} characters`;
  return null;
}
