import { describe, it, expect } from "vitest";
import { createRoot } from "solid-js";
import {
  ORDINAL_TOKEN,
  bucketPhrase,
  componentMint,
  createPen,
  penIncomplete,
  penState,
  locationMint,
  mintNote,
  mintShape,
  nameBucket,
  systemMint,
} from "./namegen";
import { type ComponentType } from "./component_types";
import { type SystemType } from "./system_types";
import { type LocationType } from "./location_types";
import { componentTypeByName } from "./component_types";
import { systemTypeByName } from "./system_types";
import { uuidFor } from "./testids";

// The shapes below are the two the platform actually mints
// (internal/storage/namegen.go's nameMint.name): "<stem>-<n>", the bare stem
// when this mint suppresses the first ordinal, and the ordinal alone when the
// type is positional.
describe("mintShape", () => {
  it("writes the ordinal as a token, never as a number", () => {
    expect(mintShape({ stem: "display", bareFirst: false })).toBe(`display-${ORDINAL_TOKEN}`);
    // The token is not a digit, so nothing here can be mistaken for the name
    // the row will end up with.
    expect(/\d/.test(mintShape({ stem: "display", bareFirst: false }))).toBe(false);
  });

  it("shows the bare stem for a mint that suppresses the first ordinal", () => {
    expect(mintShape({ stem: "boardroom", bareFirst: true })).toBe("boardroom");
  });

  it("is the ordinal alone for a positional type, whose ordinal IS its name", () => {
    expect(mintShape({ stem: "", bareFirst: false })).toBe(ORDINAL_TOKEN);
    // A stem-less mint ignores suppression (there would be no name left), the
    // same collapse NameRule.normalized makes server-side.
    expect(mintShape({ stem: "", bareFirst: true })).toBe(ORDINAL_TOKEN);
  });
});

describe("mintNote", () => {
  it("says the number is picked at create, for every shape", () => {
    for (const m of [
      { stem: "display", bareFirst: false },
      { stem: "boardroom", bareFirst: true },
      { stem: "", bareFirst: false },
    ]) {
      expect(mintNote(m)).toMatch(/create/);
    }
  });

  it("warns that a suppressed first ordinal reappears on the ones after it", () => {
    // Without this the operator reads "boardroom" and gets "boardroom-2", which
    // is exactly the silent difference the preview exists to prevent.
    expect(mintNote({ stem: "boardroom", bareFirst: true })).toMatch(/boardroom-2/);
  });
});

const componentTypes: ComponentType[] = [
  { id: uuidFor("ct-device"), name: "device", display_name: "Device", official: true, forked: false, stem: "device", default_tags: [] },
  { id: uuidFor("ct-display"), name: "display", display_name: "Display", official: true, forked: false, parent: "device", stem: "display", default_tags: [] },
  // Inherits its stem from display: the case a walk that read only the node
  // itself would answer wrong.
  { id: uuidFor("ct-wall"), name: "video-wall", display_name: "Video Wall", official: true, forked: false, parent: "display", default_tags: [] },
  { id: uuidFor("ct-bare"), name: "unstemmed", display_name: "Unstemmed", official: false, forked: false, default_tags: [] },
];
const ctByName = componentTypeByName(componentTypes);

describe("componentMint", () => {
  const product = { component_type: "video-wall" };

  it("resolves the stem through the product's component_type chain", () => {
    expect(componentMint(product, ctByName)).toEqual({ stem: "display", bareFirst: false });
  });

  it("never suppresses the first ordinal: a component is display-1, then display-2", () => {
    expect(componentMint({ component_type: "display" }, ctByName)!.bareFirst).toBe(false);
  });

  it("is absent with no product chosen yet", () => {
    expect(componentMint(undefined, ctByName)).toBeNull();
  });

  it("is absent when the chain carries no stem, the refusal the gateway makes", () => {
    // ErrComponentTypeNoStem: a bare "1" says nothing about a device, so the
    // platform declines to name it and the operator has to.
    expect(componentMint({ component_type: "unstemmed" }, ctByName)).toBeNull();
  });
});

const systemTypes: SystemType[] = [
  { id: uuidFor("st-room"), name: "room", display_name: "Room", official: true, stem: "room" },
  { id: uuidFor("st-board"), name: "board", display_name: "Boardroom", official: true, parent: "room", stem: "boardroom" },
  { id: uuidFor("st-div"), name: "divisible", display_name: "Divisible Boardroom", official: true, parent: "board" },
  { id: uuidFor("st-bare"), name: "unstemmed", display_name: "Unstemmed", official: false },
];
const stByName = systemTypeByName(systemTypes);

describe("systemMint", () => {
  it("resolves the stem through the system_type chain and suppresses the first ordinal", () => {
    // ADR-0101: the only boardroom in a room is "boardroom", not "boardroom-1".
    expect(systemMint("divisible", stByName)).toEqual({ stem: "boardroom", bareFirst: true });
  });

  it("is absent for an unclassified system, which the gateway refuses to name", () => {
    expect(systemMint("", stByName)).toBeNull();
    expect(systemMint(undefined, stByName)).toBeNull();
  });

  it("is absent when the chain carries no stem", () => {
    expect(systemMint("unstemmed", stByName)).toBeNull();
  });
});

// The positional and stemmed rows are types an OPERATOR would declare, not
// shipped ones: no seeded location type carries a name rule (ADR-0103, which
// made `floor` nominal because a designation is B2 or 12A, never an ordinal).
// A parking deck is the honest positional place, and the campus is the shipped
// shape, which is the opt-out.
const locationTypes: LocationType[] = [
  { id: uuidFor("lt-campus"), name: "campus", display_name: "Campus", icon: "landmark", official: true, forked: false, allowed_parent_types: ["root"] },
  { id: uuidFor("lt-deck"), name: "deck", display_name: "Deck", icon: "layers", official: false, forked: false, allowed_parent_types: ["building"], name_rule: { stem: "", bare_first: false } },
  { id: uuidFor("lt-room"), name: "room", display_name: "Room", icon: "door-open", official: false, forked: false, allowed_parent_types: [], name_rule: { stem: "room", bare_first: true } },
];

describe("locationMint", () => {
  it("takes the type's name rule verbatim: the rule IS the mint", () => {
    expect(locationMint(locationTypes[2])).toEqual({ stem: "room", bareFirst: true });
  });

  it("is positional for a rule with an empty stem", () => {
    expect(locationMint(locationTypes[1])).toEqual({ stem: "", bareFirst: false });
  });

  it("collapses suppression on a stem-less rule, as NameRule.normalized does", () => {
    const odd: LocationType = { ...locationTypes[1], name_rule: { stem: "", bare_first: true } };
    expect(locationMint(odd)).toEqual({ stem: "", bareFirst: false });
  });

  it("is absent for a type with no rule, which is the opt-out", () => {
    expect(locationMint(locationTypes[0])).toBeNull();
    expect(locationMint(undefined)).toBeNull();
  });
});

// The bucket is the placement scope the scoped-unique index groups on, and its
// precedence is the server's (internal/storage/namegen.go's placedNameScope):
// a parent wins over a location, and neither is the unplaced bucket.
describe("nameBucket", () => {
  it("prefers a parent over a location", () => {
    expect(nameBucket("p-1", "l-1")).toEqual({ under: "parent", id: "p-1" });
  });

  it("falls back to the location when there is no parent", () => {
    expect(nameBucket("", "l-1")).toEqual({ under: "location", id: "l-1" });
  });

  it("is the parentless bucket when neither is set", () => {
    expect(nameBucket("", "")).toEqual({ under: "none", id: "" });
    // A location has no located-at column at all, so the two-bucket shape falls
    // out of calling it with no location.
    expect(nameBucket("")).toEqual({ under: "none", id: "" });
  });
});

describe("bucketPhrase", () => {
  it("names the placement path, so the scope is visible without being part of the name", () => {
    expect(bucketPhrase("component", { under: "location", id: "l" }, ["HQ", "West", "1", "Boardroom"]))
      .toBe("at HQ / West / 1 / Boardroom");
    expect(bucketPhrase("component", { under: "parent", id: "p" }, ["Video Bar 1"]))
      .toBe("under Video Bar 1");
  });

  it("says which parentless bucket it is, per kind", () => {
    expect(bucketPhrase("component", { under: "none", id: "" }, [])).toBe("among the unplaced components");
    expect(bucketPhrase("system", { under: "none", id: "" }, [])).toBe("among the unplaced systems");
    expect(bucketPhrase("location", { under: "none", id: "" }, [])).toBe("at the estate root");
  });

  it("falls back to the bucket kind when the path cannot be resolved", () => {
    // A placement whose row has not loaded is still a real bucket; saying so
    // beats printing an empty path that reads as "no scope at all".
    expect(bucketPhrase("component", { under: "location", id: "l" }, [])).toBe("at the chosen location");
    expect(bucketPhrase("component", { under: "parent", id: "p" }, [])).toBe("under the chosen parent");
  });
});

// The pen (#699): one identity field's ownership, and the invariant that keeps a
// locked field and what gets posted from disagreeing.
describe("the pen", () => {
  // createPen calls createSignal, so it needs an owner: createRoot is the
  // smallest one, and disposing it is what keeps a test from leaking a
  // reactive scope into the next.
  const withPen = (fn: (p: ReturnType<typeof createPen>) => void) =>
    createRoot((dispose) => {
      fn(createPen());
      dispose();
    });

  it("starts locked and holding nothing, so a create body omits the field", () => {
    withPen((p) => {
      expect(p.overridden()).toBe(false);
      expect(p.value()).toBe("");
    });
  });

  it("clears the value when the pen goes back to the platform", () => {
    // The whole wire contract, in one place: re-locking cannot leave a value
    // behind for the create body to pick up, so "locked" and "posts nothing"
    // are the same state rather than two that have to be kept in step.
    withPen((p) => {
      p.setOverridden(true);
      p.setValue("front-mic");
      p.setOverridden(false);
      expect(p.value()).toBe("");
    });
  });

  it("reads a typed value as overridden even when the flag says otherwise", () => {
    // A name typed while nothing could generate one survives the operator then
    // choosing a classification that can: the value is the stronger signal.
    withPen((p) => {
      p.setValue("one-off");
      expect(penState(true, p)).toBe("overridden");
    });
  });

  it("is generated when available and untouched, unavailable when not", () => {
    withPen((p) => {
      expect(penState(true, p)).toBe("generated");
      expect(penState(false, p)).toBe("unavailable");
    });
  });

  it("blocks the submit only where nothing generates and nothing is typed", () => {
    withPen((p) => {
      // Locked over a value the platform will supply: complete.
      expect(penIncomplete(true, p)).toBe(false);
      // Nothing will generate and the operator has typed nothing: incomplete.
      expect(penIncomplete(false, p)).toBe(true);
      p.setValue("one-off");
      expect(penIncomplete(false, p)).toBe(false);
    });
  });
});
