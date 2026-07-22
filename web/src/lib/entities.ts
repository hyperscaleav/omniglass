import { createSignal } from "solid-js";

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

// deriveKey turns what an operator typed into the key the API will accept.
//
// The API enforces `^[a-z0-9][a-z0-9-]*$` with a 100 character ceiling, so this
// produces exactly that or the empty string. It never produces something the
// server would reject: the point is that an operator types "HQ Boardroom DSP"
// and never has to think about the character class.
//
// Diacritics are folded rather than dropped, so "Café" becomes "cafe" and not
// "caf". Anything else outside the class becomes a separator, runs collapse, and
// leading and trailing separators go, because the pattern demands the first
// character be alphanumeric.
//
// It is deliberately NOT reversible and not a general slugifier. Two display
// names can derive the same key; the server's uniqueness check is what settles
// that, and the key stays editable so an operator can resolve it themselves.
export function deriveKey(display: string): string {
  return display
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "") // fold diacritics onto their base letter
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 100)
    .replace(/-+$/g, ""); // the slice can leave a trailing separator behind
}

// createIdentity owns the coupling between the two identity fields on a create
// form: the operator types a display name, the key derives from it live, and the
// moment they edit the key by hand it becomes theirs and stops following.
//
// That last rule is the whole reason this is a primitive rather than three
// copies of a signal pair. A form that keeps overwriting a hand-edited key is
// worse than one that never derived it, and it is the part that is easy to get
// wrong in each page separately.
//
// Passing an existing name marks the key as already the operator's, so an edit
// form can never rewrite a live key from its display name. Renaming is a
// deliberate act (the API takes it explicitly), never a side effect of relabelling.
export function createIdentity(initial?: { display?: string; name?: string }) {
  const [display, setDisplayRaw] = createSignal(initial?.display ?? "");
  const [name, setNameRaw] = createSignal(initial?.name ?? "");
  const [keyOwned, setKeyOwned] = createSignal(Boolean(initial?.name));

  return {
    display,
    name,
    // True while the key is still following the display name, which is what the
    // form uses to decide whether to say so beneath the field.
    keyDerived: () => !keyOwned(),
    setDisplay: (v: string) => {
      setDisplayRaw(v);
      if (!keyOwned()) setNameRaw(deriveKey(v));
    },
    setName: (v: string) => {
      setKeyOwned(true);
      setNameRaw(v);
    },
  };
}
