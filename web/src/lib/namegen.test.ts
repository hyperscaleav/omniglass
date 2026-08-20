import { describe, it, expect } from "vitest";
import { createRoot } from "solid-js";
import {
  bucketPhrase,
  createPen,
  ordinalNote,
  penIncomplete,
  penState,
  nameBucket,
} from "./namegen";

// What is left of this module after #702 folded the name into the server's one
// answer: the sentence beside a drafted name, the placement bucket it is unique
// in, and the pen.
//
// The mints are GONE, and their tests with them (componentMint, systemMint,
// locationMint, mintShape, ORDINAL_TOKEN). They resolved a stem in the browser
// and wrote a token where the ordinal went, which is the duplication #695 was
// filed about; the gateway answers the whole name now, so there is no second
// implementation left here to test. The behaviours they covered are asserted
// against the real generator instead, in internal/storage/label_draft_test.go.
describe("ordinalNote", () => {
  it("names the number the form is holding", () => {
    expect(ordinalNote(3)).toMatch(/\b3\b/);
  });

  it("says the number is provisional and what happens if it moves", () => {
    // The one thing the value on screen cannot say for itself. It replaced the
    // old "picked at create" sentence, which was true of a token and is not
    // true of a number the form is about to post as a precondition.
    expect(ordinalNote(1)).toMatch(/refused/);
    // And it promises no reservation, because asking took none: the number is
    // true now, not held.
    expect(ordinalNote(1)).not.toMatch(/reserved|held for you/);
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
    expect(bucketPhrase("location", { under: "none", id: "" }, [])).toBe("at the fleet root");
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

  it("is generated when the platform has a value and the field is untouched", () => {
    // available now means "the server's draft carries a name" (#702), which is
    // why the not-yet-answered case and the never-will-be case are one state
    // for the FIELD: both are editable, and neither is a lock over an empty box.
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
