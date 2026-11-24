// The field-level diff the audit drawer renders (#473): a pure projection of
// the old/new row images an audit event carries into rows the UI can style.
// Kept out of the component so the classification (added, removed, changed,
// same) is unit-testable without a DOM.

export type DiffState = "added" | "removed" | "changed" | "same";

export type DiffRow = {
  key: string;
  // Rendered values; undefined when the field is absent on that side (an
  // added field has no before, a removed one no after).
  before?: string;
  after?: string;
  state: DiffState;
};

// show renders a field value for the diff cell: strings bare, everything else
// as JSON indented two spaces, so a nested object (an endpoint's params, a
// driver's spec) reads as the structure it is rather than one long line, and
// never pretends to be a scalar. The cell keeps the newlines (#1088).
const show = (v: unknown): string => (typeof v === "string" ? v : JSON.stringify(v, null, 2));

const isObj = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

// diffFields projects the two images into per-field rows, keys sorted. A
// missing image (create has no old, delete no new) leaves every field on the
// present side. A non-object image (a scalar or array the write recorded)
// collapses to one synthetic "value" row rather than being dropped.
export function diffFields(oldV: unknown, newV: unknown): DiffRow[] {
  const oldAbsent = oldV === undefined || oldV === null;
  const newAbsent = newV === undefined || newV === null;
  if (oldAbsent && newAbsent) return [];
  if ((!oldAbsent && !isObj(oldV)) || (!newAbsent && !isObj(newV))) {
    const before = oldAbsent ? undefined : show(oldV);
    const after = newAbsent ? undefined : show(newV);
    const state: DiffState = oldAbsent ? "added" : newAbsent ? "removed" : before === after ? "same" : "changed";
    return [{ key: "value", before, after, state }];
  }
  const o = (oldV ?? {}) as Record<string, unknown>;
  const n = (newV ?? {}) as Record<string, unknown>;
  const keys = [...new Set([...Object.keys(o), ...Object.keys(n)])].sort();
  return keys.map((k) => {
    const inOld = !oldAbsent && k in o;
    const inNew = !newAbsent && k in n;
    const before = inOld ? show(o[k]) : undefined;
    const after = inNew ? show(n[k]) : undefined;
    const state: DiffState = !inOld ? "added" : !inNew ? "removed" : before === after ? "same" : "changed";
    return { key: k, before, after, state };
  });
}

const ACRONYMS = new Set(["id", "uuid", "url", "uri", "ip", "api", "tls", "dns", "mcp", "http", "https", "ssh", "snmp"]);

// humanizeField reads a recorded field name as words: the Go struct names the
// older writers record ("LastOrphanedAt", "PlatformComponentID") and the
// snake_case column names the newer ones do ("owner_kind"). Words split at case
// changes and underscores; an acronym keeps its capitals ("Component ID"). The
// first word takes a capital, the rest stay lower, so the label wraps at spaces
// instead of mid-word in a narrow column. A name with no letters is returned
// as it came.
export function humanizeField(key: string): string {
  const words = key.match(/[A-Z]+(?![a-z])|[A-Z]?[a-z]+|\d+/g);
  if (!words || !/[A-Za-z]/.test(key)) return key;
  return words
    .map((w, i) => {
      if (ACRONYMS.has(w.toLowerCase())) return w.toUpperCase();
      const lower = w.toLowerCase();
      return i === 0 ? lower[0].toUpperCase() + lower.slice(1) : lower;
    })
    .join(" ");
}

// isTimestamp is true for an RFC 3339 instant, the shape every created_at,
// updated_at and *_at column serializes to; the change table shows one as a date
// in the console's own format, not the raw string.
export function isTimestamp(v: string): boolean {
  return /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/.test(v);
}
