// The faceted-filter engine, ported from the design prototype's FilterBar as
// pure, framework-agnostic logic so it is unit-testable on its own (the
// FilterBar component in Phase 3 is a thin Kobalte shell over this). The
// invariant the whole console depends on: values WITHIN one chip are OR
// (additive), chips ACROSS keys are AND, and clicking an active facet removes
// it (dropping the chip when its last value goes).

export type OpKey = "contains" | "eq" | "neq" | "starts" | "ends" | "gt" | "gte" | "lt" | "lte";

type OpSpec = { glyph: string; token: string; label: string; types: ("string" | "number")[] };

export const OP: Record<OpKey, OpSpec> = {
  contains: { glyph: "~", token: "~", label: "contains", types: ["string"] },
  eq: { glyph: "=", token: "=", label: "equals", types: ["string", "number"] },
  neq: { glyph: "≠", token: "!=", label: "not equal", types: ["string", "number"] },
  starts: { glyph: "^", token: "^", label: "starts with", types: ["string"] },
  ends: { glyph: "$", token: "$", label: "ends with", types: ["string"] },
  gt: { glyph: ">", token: ">", label: "greater than", types: ["number"] },
  gte: { glyph: "≥", token: ">=", label: "at least", types: ["number"] },
  lt: { glyph: "<", token: "<", label: "less than", types: ["number"] },
  lte: { glyph: "≤", token: "<=", label: "at most", types: ["number"] },
};

export const chipGlyph = (op: OpKey): string => OP[op].glyph;

// A FilterKey describes one filterable field: its value getter, type, and
// optional facet/autocomplete sources.
export type FilterKey<T> = {
  key: string;
  type: "string" | "number";
  hint?: string; // "exact" | "substring" | a label
  get: (row: T) => unknown;
  values?: (rows: T[]) => string[]; // autocomplete + facet catalog
  valueLabel?: (v: string) => string;
};

export type Chip = { key: string; op: OpKey; values: string[] };

export const opsFor = (type: "string" | "number"): OpKey[] =>
  (Object.keys(OP) as OpKey[]).filter((k) => OP[k].types.includes(type));

export function defaultOp<T>(spec: FilterKey<T>): OpKey {
  return spec.hint === "exact" ? "eq" : spec.type === "number" ? "eq" : "contains";
}

// matchOp tests one row value against one target under one operator.
export function matchOp(op: OpKey, rowVal: unknown, target: string): boolean {
  const rv = rowVal === null || rowVal === undefined ? "" : rowVal;
  // Compare numerically only when the field value is actually a number; comparing
  // numeric-looking strings (a "type"/"name" facet) would false-match (e.g. "01"
  // vs "1"). Number fields still get numeric equality.
  if (op === "eq") {
    if (typeof rowVal === "number") return rowVal === Number(target);
    return String(rv).toLowerCase() === target.toLowerCase();
  }
  if (op === "neq") return String(rv).toLowerCase() !== target.toLowerCase();
  const s = String(rv).toLowerCase();
  const t = target.toLowerCase();
  if (op === "contains") return s.includes(t);
  if (op === "starts") return s.startsWith(t);
  if (op === "ends") return s.endsWith(t);
  const a = Number(rv);
  const b = Number(target);
  if (op === "gt") return a > b;
  if (op === "gte") return a >= b;
  if (op === "lt") return a < b;
  if (op === "lte") return a <= b;
  return true;
}

// buildPredicate composes the chips into a row test: within a chip the values
// are OR (some), across chips they are AND (every). A chip whose key is unknown
// is ignored (true), never excluding rows.
export function buildPredicate<T>(keys: FilterKey<T>[], chips: Chip[]): (row: T) => boolean {
  const byKey = Object.fromEntries(keys.map((k) => [k.key, k]));
  return (row: T) =>
    chips.every((c) => {
      const spec = byKey[c.key];
      if (!spec) return true;
      const v = spec.get(row);
      return c.values.some((val) => matchOp(c.op, v, val));
    });
}

// facetActive reports whether (key, val) is currently a filter facet.
export function facetActive(chips: Chip[], key: string, val: string): boolean {
  const c = chips.find((x) => x.key === key);
  return !!c && c.values.includes(val);
}

// toggleFacet adds or removes a facet value, keeping ONE chip per key (so the
// within-key values are OR). Removing the last value drops the chip. This is the
// summary-card / badge / legend click behavior; the result is a new chip array.
export function toggleFacet(chips: Chip[], key: string, val: string): Chip[] {
  const i = chips.findIndex((c) => c.key === key);
  if (i < 0) return [...chips, { key, op: "eq", values: [val] }];
  const c = chips[i];
  const has = c.values.includes(val);
  const values = has ? c.values.filter((v) => v !== val) : [...c.values, val];
  if (!values.length) return chips.filter((_, j) => j !== i);
  return chips.map((x, j) => (j === i ? { ...x, values } : x));
}

// tokenToChip parses a typed "key:<glyph?>value" token into a chip, falling back
// to fallbackKey when no key prefix is present.
export function tokenToChip<T>(raw: string, keys: FilterKey<T>[], fallbackKey: string): Chip | null {
  const colon = raw.indexOf(":");
  let key: string;
  let rest: string;
  if (colon < 0) {
    key = fallbackKey;
    rest = raw.trim();
  } else {
    key = raw.slice(0, colon).trim();
    rest = raw.slice(colon + 1);
  }
  const spec = keys.find((k) => k.key === key) ?? keys.find((k) => k.key === fallbackKey);
  if (!spec) return null;
  let op = defaultOp(spec);
  let value = rest;
  const tokens: [string, OpKey][] = [
    ["!=", "neq"], [">=", "gte"], ["<=", "lte"], ["~", "contains"], ["=", "eq"],
    ["≠", "neq"], ["^", "starts"], ["$", "ends"], [">", "gt"], ["≥", "gte"], ["<", "lt"], ["≤", "lte"],
  ];
  for (const [tok, o] of tokens) {
    if (rest.startsWith(tok) && OP[o].types.includes(spec.type)) {
      op = o;
      value = rest.slice(tok.length);
      break;
    }
  }
  value = value.trim();
  if (!value) return null;
  return { key: spec.key, op, values: [value] };
}
