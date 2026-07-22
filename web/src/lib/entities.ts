// How an estate entity is labelled, in one place.
//
// Every entity carries two identities: a **key** (`name`, the kebab identifier
// the API and CLI address it by) and an optional **display name**. The label is
// the display name when there is one and the key when there is not.
//
// Nothing is derived from the key. Sentence-casing `hq-boardroom-dsp` gives
// "Hq boardroom dsp", and this domain is acronyms (DSP, HDMI, NVX, PTZ, UC,
// AVoIP), so any mechanical casing mangles them and makes an ABSENT display name
// look like a typo rather than an absence. The key shown as-is is honest, and it
// keeps the gap visible.
//
// This rule used to be written out six times: `nodeLabel` in lib/nodes.ts, the
// `display:` mapper on each of the Components, Systems, and Locations pages, and
// three inline copies in the Variables owner picker. Six copies of one rule is
// how the Components list ended up showing a key where its neighbours showed a
// label.

// Labelled is the shape of anything the console labels: the key, plus the
// optional operator-facing name. It is deliberately structural rather than a
// union of entity types, so a generated body satisfies it without a cast.
export interface Labelled {
  name: string;
  display_name?: string | null;
}

// entityLabel is what an operator reads. A display name of "" or whitespace is
// the same as absent: the API stores the empty string, and a label of " " would
// render as a blank row.
export function entityLabel(e: Labelled): string {
  return e.display_name?.trim() || e.name;
}

// hasDisplayName reports whether the label and the key are different things,
// which is what decides whether a surface shows the key on its own line. When
// they are the same, showing it twice is noise.
export function hasDisplayName(e: Labelled): boolean {
  return entityLabel(e) !== e.name;
}
