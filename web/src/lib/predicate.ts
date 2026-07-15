// The faceted-filter engine, ported from the design prototype's FilterBar as
// pure, framework-agnostic logic so it is unit-testable on its own (the
// FilterBar component in Phase 3 is a thin Kobalte shell over this). The
// invariant the whole console depends on: values WITHIN one chip are OR
// (additive), chips ACROSS keys are AND, and clicking an active facet removes
// it (dropping the chip when its last value goes).

export type OpKey = "contains" | "eq" | "neq" | "starts" | "ends" | "gt" | "gte" | "lt" | "lte" | "exists" | "absent";

// valueless marks a presence operator that carries no value (exists / absent):
// it tests only whether the field has any value, so it is offered only on a
// presence-capable FilterKey (a tag facet), never on a field that is always set.
type OpSpec = { glyph: string; token: string; label: string; types: ("string" | "number")[]; valueless?: boolean };

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
  exists: { glyph: "∃", token: "?", label: "is set", types: ["string", "number"], valueless: true },
  absent: { glyph: "∄", token: "!?", label: "is absent", types: ["string", "number"], valueless: true },
};

// valueless reports whether an operator carries no value (exists / absent).
export const valueless = (op: OpKey): boolean => OP[op].valueless === true;

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
  presence?: boolean; // offers the value-less exists / absent operators (a tag facet)
};

export type Chip = { key: string; op: OpKey; values: string[] };

// FilterKeys is a page's facet set: a static array, or an accessor when the set
// is dynamic (a directory whose tag facets derive from the loaded rows). Resolve
// it inside a reactive scope so the dynamic facets track their source.
export type FilterKeys<T> = FilterKey<T>[] | (() => FilterKey<T>[]);
export const resolveFilterKeys = <T>(fk: FilterKeys<T>): FilterKey<T>[] => (typeof fk === "function" ? fk() : fk);

// opsFor lists the operators a key offers: those valid for its value type, plus
// the value-less presence operators only when the key is presence-capable.
export const opsFor = (type: "string" | "number", presence = false): OpKey[] =>
  (Object.keys(OP) as OpKey[]).filter((k) => OP[k].types.includes(type) && (!OP[k].valueless || presence));

// tagFilterKeys yields one filter facet per tag key, so a directory can be
// filtered by its rows' effective tags exactly like any other field. Each facet
// reads the row's effective value for its key (empty when the tag is absent),
// autocompletes the values in use, and offers the presence operators. Keys
// already covered by a static facet are skipped so a tag can never shadow a
// column filter. Derived from the loaded rows, so a tag is filterable as soon as
// it appears on any row.
export function tagFilterKeys<T extends { tags: Record<string, string> }>(keyNames: string[], exclude: Set<string>): FilterKey<T>[] {
  return keyNames
    .filter((name) => !exclude.has(name))
    .map((name) => ({
      key: name,
      type: "string" as const,
      hint: "tag",
      presence: true,
      get: (row: T) => row.tags[name] ?? "",
      values: (rows: T[]) => [...new Set(rows.map((r) => r.tags[name]).filter(Boolean))].sort(),
    }));
}

// tagKeysOf collects the distinct tag keys present across rows, sorted, for the
// dynamic facet set.
export function tagKeysOf<T extends { tags: Record<string, string> }>(rows: T[]): string[] {
  const set = new Set<string>();
  for (const r of rows) for (const k of Object.keys(r.tags)) set.add(k);
  return [...set].sort();
}

export function defaultOp<T>(spec: FilterKey<T>): OpKey {
  return spec.hint === "exact" ? "eq" : spec.type === "number" ? "eq" : "contains";
}

// matchOp tests one row value against one target under one operator.
export function matchOp(op: OpKey, rowVal: unknown, target: string): boolean {
  const rv = rowVal === null || rowVal === undefined ? "" : rowVal;
  // Presence operators ignore the target and test only whether the field is set.
  if (op === "exists") return String(rv).trim() !== "";
  if (op === "absent") return String(rv).trim() === "";
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
      // A value-less presence chip carries no values; test the operator once.
      if (valueless(c.op)) return matchOp(c.op, v, "");
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
  // "!?" (absent) and "?" (exists) are the value-less presence tokens, matched
  // before "!=" so the leading "!" is not read as not-equal.
  const tokens: [string, OpKey][] = [
    ["!?", "absent"], ["?", "exists"],
    ["!=", "neq"], [">=", "gte"], ["<=", "lte"], ["~", "contains"], ["=", "eq"],
    ["≠", "neq"], ["^", "starts"], ["$", "ends"], [">", "gt"], ["≥", "gte"], ["<", "lt"], ["≤", "lte"],
  ];
  for (const [tok, o] of tokens) {
    if (rest.startsWith(tok) && OP[o].types.includes(spec.type) && (!OP[o].valueless || spec.presence)) {
      op = o;
      value = rest.slice(tok.length);
      break;
    }
  }
  if (valueless(op)) return { key: spec.key, op, values: [] };
  value = value.trim();
  if (!value) return null;
  return { key: spec.key, op, values: [value] };
}
