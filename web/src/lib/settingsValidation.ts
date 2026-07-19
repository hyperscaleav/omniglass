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

// validateField returns an inline error message for a draft value, or null when it
// satisfies the generated constraint. An unknown field (no constraint) is itself an
// error: the form only edits declared settings.
export function validateField(c: Constraint | undefined, raw: string): string | null {
  if (!c) return "unknown setting";
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
