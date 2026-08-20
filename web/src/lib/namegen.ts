import { createSignal } from "solid-js";

// What a create form knows about the name the platform is about to write, which
// since #702 is the name itself and not a shape.
//
// The platform mints a name from a resolved stem and the lowest ordinal free in
// the placement bucket (internal/storage/namegen.go). Both halves used to be
// answered here: the console walked the type chain for the stem and wrote the
// token "n" where the number went, because ADR-0104 ruled the ordinal
// unknowable before the row existed. The distinction that ruling missed is that
// READING the lowest free ordinal is not allocating one, so the server now
// answers the whole name (lib/labeldraft.ts), and this module no longer knows
// what a mint looks like or how to resolve a stem.
//
// What is left here is what the answer is worth: the note that says the number
// is provisional, the placement bucket it is unique in, and the pen that decides
// whether the operator or the platform owns each field.
//
// The number being provisional is not hidden any more, it is HANDLED: the form
// posts it back as the create's expected_name and a create that would land a
// different name is refused rather than renumbered.

// FleetKind is the three entities whose names the platform can own. The
// registry pages (products, vendors, types, users) are not here: their names
// have no generator and stay globally unique, so a create form there derives the
// name from the label instead (lib/entities.ts's createIdentity).
export type FleetKind = "component" | "system" | "location";

// ordinalNote is the sentence that travels with a drafted name. It says the one
// thing the value itself cannot: that it is true right now rather than reserved.
//
// It replaced a per-mint sentence (#702). The old one had to explain the token
// and, for a suppressing mint, warn that the "boardroom" on screen becomes
// "boardroom-2" for the second one in the room; both were consequences of
// showing a shape. The form now shows the name the create will use, and the
// case the warning existed for is the one the precondition refuses.
export function ordinalNote(ordinal: number): string {
  return `${ordinal} is the lowest number free here right now. If another create takes it first, this one is refused rather than renamed.`;
}

// NameBucket is the placement scope the name has to be unique in: the client's
// reading of the server's nameScope, which is the same grouping the scoped-name
// unique indexes enforce (db/migrations/20260808090000_names_scope_to_placement.sql).
export interface NameBucket {
  under: "parent" | "location" | "none";
  id: string;
}

// nameBucket resolves the bucket from what the placement fields hold, in the
// server's precedence: a parent wins over a location, and neither is the
// parentless bucket. Calling it with no location is the two-bucket shape a
// location has, which falls out rather than being a second function, because a
// location has no located-at column to be bucketed by in the first place.
export function nameBucket(parentID: string, locationID = ""): NameBucket {
  if (parentID) return { under: "parent", id: parentID };
  if (locationID) return { under: "location", id: locationID };
  return { under: "none", id: "" };
}

// bucketPhrase renders the bucket as the placement CONTEXT beside the name
// field: what the name has to be unique in, shown and not editable.
//
// It is deliberately not a prefix on the name. Names became scoped to placement,
// so a name no longer contains its ancestry (a boardroom is `boardroom`, not
// `hq-west-2-boardroom`), and rendering the path into the field would put back
// the redundancy the scoping removed. The path is context; the name is the
// address within it.
export function bucketPhrase(kind: FleetKind, bucket: NameBucket, path: string[]): string {
  switch (bucket.under) {
    case "parent":
      return path.length ? `under ${path.join(" / ")}` : "under the chosen parent";
    case "location":
      return path.length ? `at ${path.join(" / ")}` : "at the chosen location";
    default:
      return kind === "location" ? "at the fleet root" : `among the unplaced ${kind}s`;
  }
}

// A PEN is one identity field's ownership: the value, and whether the operator
// has claimed it (#699). The two travel together because they are one fact told
// two ways, and separating them is how a form ends up locked while posting a
// value, or unlocked while posting none.
//
// The invariant is enforced in one place, setOverridden: handing the pen BACK
// clears the value. So "locked" and "posts nothing" are the same state rather
// than two states a caller has to keep in step, and a page's create body stays
// the `value || undefined` it already was.
export interface Pen {
  value: () => string;
  setValue: (v: string) => void;
  overridden: () => boolean;
  setOverridden: (v: boolean) => void;
}

export function createPen(): Pen {
  const [value, setValue] = createSignal("");
  const [overridden, setOverridden] = createSignal(false);
  return {
    value,
    setValue,
    overridden,
    setOverridden: (v: boolean) => {
      setOverridden(v);
      if (!v) setValue("");
    },
  };
}

// penState is which of the three a field is in, and it is derived rather than
// stored so the three cannot disagree.
//
// available is "the platform HAS a value for this field", which for the name is
// now the server's drafted name rather than a client-side reading of whether it
// could resolve a stem (#702). That collapses two states the form used to keep
// apart, "no value yet" and "no value ever", into one for the FIELD, and the
// difference reappears where it belongs, in the hint: a permanent refusal names
// the missing fact, a form still waiting for its answer says so. Both are
// editable, and neither shows a lock over an empty box, which is the state this
// affordance exists to prevent.
export type PenState = "generated" | "overridden" | "unavailable";

export function penState(available: boolean, p: Pen): PenState {
  if (p.overridden() || p.value().trim() !== "") return available ? "overridden" : "unavailable";
  return available ? "generated" : "unavailable";
}

// penIncomplete is the submit gate: a field the operator owns and has left
// empty. A LOCKED field is never incomplete, whatever it is showing, because
// the platform is the one filling it in.
export function penIncomplete(available: boolean, p: Pen): boolean {
  return !available && p.value().trim() === "";
}
