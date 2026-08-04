import { describe, expect, it } from "vitest";
import { createIdentity, deriveName, entityLabel, hasDisplayName } from "./entities";

describe("entityLabel", () => {
  it("prefers the display name", () => {
    expect(entityLabel({ name: "hq-boardroom-dsp", display_name: "HQ Boardroom DSP" })).toBe("HQ Boardroom DSP");
  });

  // The API stores "" rather than null for an unset display name, and a value of
  // whitespace would render as a blank row, so both fall back to the name.
  it.each([
    ["absent", undefined],
    ["null", null],
    ["empty", ""],
    ["whitespace", "   "],
  ])("falls back to the name when the display name is %s", (_label, dn) => {
    expect(entityLabel({ name: "hq-boardroom-dsp", display_name: dn })).toBe("hq-boardroom-dsp");
  });

  // Nothing is derived. An acronym-heavy name sentence-cased reads as a typo, so
  // the name is shown exactly as it is stored and stays recognisable as an id.
  it("does not case, humanise, or otherwise rewrite the name", () => {
    expect(entityLabel({ name: "hq-boardroom-nvx-tx" })).toBe("hq-boardroom-nvx-tx");
  });

  it("does not trim the name itself, which is stored normalized", () => {
    expect(entityLabel({ name: "codec-1", display_name: "  Codec One  " })).toBe("Codec One");
  });
});

describe("hasDisplayName", () => {
  it("is true only when the display name says something the name does not", () => {
    expect(hasDisplayName({ name: "codec-1", display_name: "Codec One" })).toBe(true);
    expect(hasDisplayName({ name: "codec-1", display_name: "" })).toBe(false);
    expect(hasDisplayName({ name: "codec-1" })).toBe(false);
  });

  // A display name set to exactly the name is not a second thing to show.
  it("is false when the display name merely repeats the name", () => {
    expect(hasDisplayName({ name: "codec-1", display_name: "codec-1" })).toBe(false);
  });
});

describe("deriveName", () => {
  it("turns what an operator types into the name the API accepts", () => {
    expect(deriveName("HQ Boardroom DSP")).toBe("hq-boardroom-dsp");
    expect(deriveName("Executive Boardroom")).toBe("executive-boardroom");
  });

  // The cases that bite. Each one would produce a value the server rejects with
  // a 422 if it were passed through unchanged.
  it.each([
    ["leading and trailing space", "  Conf Room 301  ", "conf-room-301"],
    ["punctuation runs", "A/V Rack #2", "a-v-rack-2"],
    ["repeated separators", "Room   ---   B", "room-b"],
    ["trailing punctuation", "Boardroom!", "boardroom"],
    ["leading punctuation", "#2 Rack", "2-rack"],
    ["a leading digit, which the pattern allows", "2nd Floor", "2nd-floor"],
    ["diacritics folded, not dropped", "Café Lounge", "cafe-lounge"],
    ["already a name", "hq-boardroom-dsp", "hq-boardroom-dsp"],
    ["nothing usable", "---", ""],
    ["empty", "", ""],
    ["only punctuation", "!!!", ""],
  ])("handles %s", (_label, input, want) => {
    expect(deriveName(input)).toBe(want);
  });

  // The API caps the name at 100 characters, and a naive slice can cut mid-word
  // and leave a trailing separator, which the pattern forbids.
  it("respects the length ceiling without leaving a trailing separator", () => {
    const name = deriveName("a".repeat(98) + " bc");
    expect(name.length).toBeLessThanOrEqual(100);
    expect(name.endsWith("-")).toBe(false);
  });

  // The contract, asserted directly against the pattern the API enforces.
  it("only ever produces the empty string or a name the API pattern accepts", () => {
    const pattern = /^[a-z0-9][a-z0-9-]*$/;
    for (const s of ["HQ Boardroom DSP", "  x  ", "A/V #2", "Café", "2nd", "---", "", "ROOM 1!"]) {
      const name = deriveName(s);
      expect(name === "" || pattern.test(name)).toBe(true);
    }
  });
});

describe("createIdentity", () => {
  it("derives the name from the display name as it is typed", () => {
    const id = createIdentity();
    id.setDisplay("HQ Boardroom");
    expect(id.name()).toBe("hq-boardroom");
    id.setDisplay("HQ Boardroom DSP");
    expect(id.name()).toBe("hq-boardroom-dsp");
    expect(id.nameDerived()).toBe(true);
  });

  // The rule that makes the pattern usable rather than infuriating: once the
  // operator has taken the name, more typing in the display name must not take it
  // back. This is the assertion the whole primitive exists for.
  it("stops following once the operator edits the name by hand", () => {
    const id = createIdentity();
    id.setDisplay("HQ Boardroom");
    expect(id.name()).toBe("hq-boardroom");

    id.setName("boardroom-a");
    expect(id.nameDerived()).toBe(false);

    id.setDisplay("HQ Boardroom DSP");
    expect(id.name()).toBe("boardroom-a");
    expect(id.display()).toBe("HQ Boardroom DSP");
  });

  // An existing entity's name is already the operator's. Relabelling must never
  // rename: the API takes a rename explicitly and it is a deliberate act.
  it("never derives over an existing name", () => {
    const id = createIdentity({ display: "HQ Boardroom", name: "boardroom-a" });
    expect(id.nameDerived()).toBe(false);
    id.setDisplay("Something Else Entirely");
    expect(id.name()).toBe("boardroom-a");
  });

  it("leaves the name empty when the display name derives to nothing", () => {
    const id = createIdentity();
    id.setDisplay("---");
    expect(id.name()).toBe("");
  });
});
