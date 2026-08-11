import { type ComponentType } from "./component_types";
import { type LocationType } from "./location_types";
import { type SystemType } from "./system_types";
import { resolveInherited } from "./typechain";

// What the console can honestly say about a generated name BEFORE the row
// exists, which is the shape and not the value.
//
// The platform mints a name as "<stem>-<n>" (internal/storage/namegen.go), and
// the two halves are knowable at very different times. The STEM comes from the
// classification an operator has already chosen on the form: a component's
// product resolves to a component_type chain, a system's system_type chain, a
// location's location_type name rule. The ORDINAL is the lowest free number
// among the LIVE siblings in the placement bucket, allocated inside the create's
// own transaction under an advisory lock, so it does not exist until the row
// does and no read can predict it: another create in the same bucket can take it
// between any preview and the commit.
//
// So this module renders the stem and writes the ordinal as a TOKEN. The
// operator sees "display-n" and a sentence saying where the number comes from,
// never a "display-4" that quietly becomes display-5. That is the whole ruling
// (ADR-0104): show what is knowable, mark what is not, and do not mint
// speculatively to fill the gap.
//
// What this module deliberately does NOT do is render the LABEL. A label comes
// from a Go text/template over a closed map that carries the row's own Name and
// Ordinal (ADR-0098), so it is unknowable for exactly the same reason and a
// second implementation of that engine in TypeScript is the defect slice 3 of
// this epic swept 42 copies of.

// EstateKind is the three entities whose names the platform can own. The
// registry pages (products, vendors, types, users) are not here: their names
// have no generator and stay globally unique, so a create form there derives the
// name from the display name instead (lib/entities.ts's createIdentity).
export type EstateKind = "component" | "system" | "location";

// NameMint is the SHAPE of a generated name, the client's reading of the
// server's `nameMint`: the resolved stem, plus whether the first of that stem in
// a bucket carries an ordinal at all (ADR-0101).
export interface NameMint {
  stem: string;
  bareFirst: boolean;
}

// The stand-in for the number the platform allocates at create. A letter rather
// than a digit or a "1", so nothing rendered from a mint can be read as the name
// the row will actually get.
export const ORDINAL_TOKEN = "n";

// mintShape is what the name will look like, with the unknowable half written as
// ORDINAL_TOKEN. It mirrors nameMint.name's three cases exactly, so the console
// cannot describe a shape the gateway does not mint.
export function mintShape(m: NameMint): string {
  if (m.stem === "") return ORDINAL_TOKEN;
  if (m.bareFirst) return m.stem;
  return `${m.stem}-${ORDINAL_TOKEN}`;
}

// mintNote is the sentence that has to travel with the shape. For a suppressing
// mint it is load-bearing rather than decorative: mintShape renders "boardroom",
// the second one in the room is "boardroom-2", and an operator who was shown the
// first without being told about the second was shown a value that silently
// turned out different.
export function mintNote(m: NameMint): string {
  if (m.stem === "") {
    return `The name is a number: the lowest one free here, picked when you create this.`;
  }
  if (m.bareFirst) {
    return `The first one here carries no number; the next take ${m.stem}-2, ${m.stem}-3 and so on. Which you get is picked when you create this.`;
  }
  return `${ORDINAL_TOKEN} is the lowest number free here, picked when you create this.`;
}

// componentMint resolves the mint a component would be named from: the stem of
// the component_type its product is classified under, inherited up the chain.
//
// Null is "the platform will not name this", and it covers the gateway's own
// refusal as well as the empty form: a chain with no stem anywhere is
// ErrComponentTypeNoStem, because a device called "1" says nothing about what it
// is. A component never suppresses its first ordinal (a rack's first display is
// "display-1"), which is why bareFirst is a constant here and a resolved fact on
// the other two.
export function componentMint(
  product: { component_type?: string } | undefined,
  typesByName: Map<string, ComponentType>,
): NameMint | null {
  const stem = resolveInherited(product?.component_type, typesByName, (t) => t.stem);
  return stem ? { stem, bareFirst: false } : null;
}

// systemMint is componentMint's system-tier twin: the system_type chain's stem,
// suppressing the first ordinal (ADR-0101, a room with one boardroom in it does
// not call it "boardroom-1"). Null for an unclassified system, which is
// ErrSystemTypeRequiredForName: system_type_id is nullable and the stem lives on
// it, so there is no source for a name.
export function systemMint(
  typeName: string | undefined,
  typesByName: Map<string, SystemType>,
): NameMint | null {
  const stem = resolveInherited(typeName, typesByName, (t) => t.stem);
  return stem ? { stem, bareFirst: true } : null;
}

// locationMint takes the location_type's name rule, which IS a mint (ADR-0102):
// there is no chain to walk, because location_type is flat, and there is no
// reshaping between the stored declaration and the shape allocation tests
// against. Null is the opt-out, the state every shipped type but floor is in.
//
// The one collapse mirrors NameRule.normalized: a stem-less rule ignores
// suppression, since suppressing the ordinal of a name that IS its ordinal
// leaves nothing.
export function locationMint(type: LocationType | undefined): NameMint | null {
  const rule = type?.name_rule;
  if (!rule) return null;
  const stem = rule.stem ?? "";
  return { stem, bareFirst: stem === "" ? false : Boolean(rule.bare_first) };
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
export function bucketPhrase(kind: EstateKind, bucket: NameBucket, path: string[]): string {
  switch (bucket.under) {
    case "parent":
      return path.length ? `under ${path.join(" / ")}` : "under the chosen parent";
    case "location":
      return path.length ? `at ${path.join(" / ")}` : "at the chosen location";
    default:
      return kind === "location" ? "at the estate root" : `among the unplaced ${kind}s`;
  }
}
