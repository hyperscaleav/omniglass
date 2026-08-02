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
// as compact JSON, so nested objects stay readable without pretending to be
// scalars.
const show = (v: unknown): string => (typeof v === "string" ? v : JSON.stringify(v));

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
