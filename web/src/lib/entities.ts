import { createSignal } from "solid-js";

// How an fleet entity is labelled, in one place.
//
// The identity triad is `id` (a uuid, immutable), `name` (the renameable kebab
// identifier an operator types and the API and CLI address the row by), and
// `label` (an optional friendly string a human reads). Only the last two
// are operator-facing, and the label is the label when there is one and
// the name when there is not.
//
// Nothing is derived from the name. Sentence-casing `hq-boardroom-dsp` gives
// "Hq boardroom dsp", and this domain is acronyms (DSP, HDMI, NVX, PTZ, UC,
// AVoIP), so any mechanical casing mangles them and makes an ABSENT label
// look like a typo rather than an absence. The name shown as-is is honest, and it
// keeps the gap visible.
//
// This rule used to be written out six times: `nodeLabel` in lib/nodes.ts, the
// `display:` mapper on each of the Components, Systems, and Locations pages, and
// three inline copies in the Variables owner picker. Six copies of one rule is
// how the Components list ended up showing a name where its neighbours showed a
// label.

// The two operator-facing words of the triad, and the only place either one is
// written as a field label. A field says which fact it is bound to and takes its
// label from here (see BladeField's `bind` prop), so the pairing cannot be got
// wrong: there is no prop by which a caller labels one of these two at all.
//
// This map replaced a source guard that paired a label to its binding with a
// regex over four call forms and an eight-line lookahead window. The rule is the
// same rule; it is enforced by the type instead. What survives of that guard is a
// backstop asserting these strings appear as a field label only in this file.
export const IDENTITY_LABELS = {
  name: "Name",
  label: "Label",
} as const;

export type IdentityBinding = keyof typeof IDENTITY_LABELS;

// Labelled is the shape of anything the console labels: the name, plus the
// optional label and the pen that says who owns it. It is deliberately
// structural rather than a union of entity types, so a generated body satisfies
// it without a cast, and so a row that is not an fleet entity at all (a
// property, whose identifier column is named something else) can be adapted to
// it in one expression rather than growing a seventh copy of the rule.
//
// label_generated is optional because only component, system and location
// carry it. Every catalog registry row (a product, a vendor, a role) has a label
// no rule renders, so its absence must read as "the operator owns it".
export interface Labelled {
  name: string;
  label?: string | null;
  label_generated?: boolean;
}

// entityLabel is what an operator reads. A label of "" or whitespace is
// the same as absent: the API stores the empty string, and a label of " " would
// render as a blank row.
export function entityLabel(e: Labelled): string {
  return e.label?.trim() || e.name;
}

// byLabel is how a list of labelled rows is ordered: alphabetically by the label
// an operator reads, with UNLABELLED rows last and the name breaking ties.
//
// It exists because the console re-sorts every registry list client-side, so
// this rule is written twice and the two halves have to agree. The SQL half is
// `order by nullif(label, '') nulls last, name` (#613, ADR-0118). Both are
// spelled explicitly for the same reason: an unset label is the empty string,
// `"".localeCompare(anything)` is negative, and a plain sort by label therefore
// puts every unlabelled row at the TOP of the picker rather than the bottom.
//
// Whitespace counts as unset, the same way entityLabel treats it, so a label of
// " " does not sort ahead of every real one.
//
// This is NOT the rendered label: it deliberately does not fall back to the name
// for a row that has no label, because that would interleave labelled and
// unlabelled rows rather than grouping the unlabelled ones at the end. Ordering
// by the rendered label is a separate open decision.
export function byLabel(a: Labelled, b: Labelled): number {
  const al = a.label?.trim() ?? "";
  const bl = b.label?.trim() ?? "";
  if (al !== bl) {
    if (al === "") return 1;
    if (bl === "") return -1;
    return al.localeCompare(bl);
  }
  return a.name.localeCompare(b.name);
}

// labelIsName reports that the label an operator reads IS the identifier, so a
// surface renders it in the data face and has no second line to show. Whoever
// holds the pen: a rule that had nothing to say about a row keeps the pen and
// stores no label (ADR-0098), and what shows is still the name.
//
// This is the whole of the string comparison, and the two predicates below are
// its refinement by the pen rather than replacements for it.
export function labelIsName(e: Labelled): boolean {
  return entityLabel(e) === e.name;
}

// hasLabel reports whether a HUMAN chose this label, which is what decides
// whether a surface shows the name on a second line of its own.
//
// It used to be that string comparison alone, and it was the same question only
// while a label was only ever operator-typed. A label rule (#682, ADR-0098) renders a
// label that differs from the name exactly as an operator's does, so the string
// comparison alone would grow a second identifier line under every row in the
// fleet: 15,000 rows each saying their name twice, which is the noise this
// predicate exists to prevent.
//
// The pen (label_generated) is the fact that answers it, and #683 put it
// on the wire. The string comparison stays as the second half of the conjunction
// rather than being replaced, because a row can hold the pen and still read as
// its own name: a rule with nothing to say about a row stores NULL and KEEPS the
// pen (ADR-0098), so "the platform owns this field" does not imply "there is a
// label here".
export function hasLabel(e: Labelled): boolean {
  return !e.label_generated && !labelIsName(e);
}

// labelGenerated retired here in #693, with the chip that was its only caller.
//
// It was "is there a platform-rendered label here to MARK", which is a question
// only a marker asks: it returned false for a row whose rule rendered nothing,
// because there was nothing on screen to attribute. The pen's state now shows on
// the edit blade instead (components/LabelPenField.tsx), and a FIELD asks a
// different question, "who holds the pen", whose answer for that same row is
// "the platform" and whose field must open locked. Keeping one predicate for two
// questions with different answers is how a surface ends up marking one thing
// and doing another, so the predicate went and the raw pen
// (label_generated) is read where it is asked about.
//
// hasLabel above still reads the pen, so the fact has not left this
// module, only the badge's spelling of it.

// deriveName turns what an operator typed into the name the API will accept.
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
// names can derive the same name; the server's uniqueness check is what settles
// that, and the name stays editable so an operator can resolve it themselves.
export function deriveName(display: string): string {
  return display
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "") // fold diacritics onto their base letter
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 100)
    .replace(/-+$/g, ""); // the slice can leave a trailing separator behind
}

// createIdentity owns the coupling between the two operator-facing identity
// fields on a create form: the operator types a label, the name derives
// from it live, and the moment they edit the name by hand it becomes theirs and
// stops following.
//
// That last rule is the whole reason this is a primitive rather than three
// copies of a signal pair. A form that keeps overwriting a hand-edited name is
// worse than one that never derived it, and it is the part that is easy to get
// wrong in each page separately.
//
// Passing an existing name marks it as already the operator's, so an edit form
// can never rewrite a live name from its label. Renaming is a deliberate
// act (the API takes it explicitly), never a side effect of relabelling.
//
// It is for the REGISTRY and identity pages only: products, vendors, standards,
// drivers, groups, nodes, the three type registries, and users. Their names have
// no generator and stay globally unique, so deriving one from what the operator
// typed is exactly the right affordance there.
//
// The three FLEET entities (component, system, location) left it in #688, and
// the reason is not stylistic. A blank name on those is the REQUEST to have the
// platform mint one from the entity's classification, so a form that filled the
// field in from a typed label claimed the platform's pen on the operator's
// behalf the moment they typed a word. Their create forms use
// components/CreateIdentity.tsx, which holds the two fields independent and
// shows what the blank one will be filled with.
export function createIdentity(initial?: { display?: string; name?: string }) {
  const [display, setDisplayRaw] = createSignal(initial?.display ?? "");
  const [name, setNameRaw] = createSignal(initial?.name ?? "");
  const [nameOwned, setNameOwned] = createSignal(Boolean(initial?.name));

  return {
    display,
    name,
    // True while the name is still following the label, which is what the
    // form uses to decide whether to say so beneath the field.
    nameDerived: () => !nameOwned(),
    setDisplay: (v: string) => {
      setDisplayRaw(v);
      if (!nameOwned()) setNameRaw(deriveName(v));
    },
    setName: (v: string) => {
      setNameOwned(true);
      setNameRaw(v);
    },
  };
}
