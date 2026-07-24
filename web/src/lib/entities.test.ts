import { describe, expect, it } from "vitest";
import { createIdentity, deriveKey, entityLabel, hasDisplayName } from "./entities";

describe("entityLabel", () => {
  it("prefers the display name", () => {
    expect(entityLabel({ name: "hq-boardroom-dsp", display_name: "HQ Boardroom DSP" })).toBe("HQ Boardroom DSP");
  });

  // The API stores "" rather than null for an unset display name, and a value of
  // whitespace would render as a blank row, so both fall back to the key.
  it.each([
    ["absent", undefined],
    ["null", null],
    ["empty", ""],
    ["whitespace", "   "],
  ])("falls back to the key when the display name is %s", (_label, dn) => {
    expect(entityLabel({ name: "hq-boardroom-dsp", display_name: dn })).toBe("hq-boardroom-dsp");
  });

  // Nothing is derived. An acronym-heavy key sentence-cased reads as a typo, so
  // the key is shown exactly as it is stored and stays recognisable as an id.
  it("does not case, humanise, or otherwise rewrite the key", () => {
    expect(entityLabel({ name: "hq-boardroom-nvx-tx" })).toBe("hq-boardroom-nvx-tx");
  });

  it("does not trim the key itself, which is stored normalized", () => {
    expect(entityLabel({ name: "codec-1", display_name: "  Codec One  " })).toBe("Codec One");
  });
});

describe("hasDisplayName", () => {
  it("is true only when the label says something the key does not", () => {
    expect(hasDisplayName({ name: "codec-1", display_name: "Codec One" })).toBe(true);
    expect(hasDisplayName({ name: "codec-1", display_name: "" })).toBe(false);
    expect(hasDisplayName({ name: "codec-1" })).toBe(false);
  });

  // A display name set to exactly the key is not a second thing to show.
  it("is false when the display name merely repeats the key", () => {
    expect(hasDisplayName({ name: "codec-1", display_name: "codec-1" })).toBe(false);
  });
});

describe("deriveKey", () => {
  it("turns what an operator types into the key the API accepts", () => {
    expect(deriveKey("HQ Boardroom DSP")).toBe("hq-boardroom-dsp");
    expect(deriveKey("Executive Boardroom")).toBe("executive-boardroom");
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
    ["already a key", "hq-boardroom-dsp", "hq-boardroom-dsp"],
    ["nothing usable", "---", ""],
    ["empty", "", ""],
    ["only punctuation", "!!!", ""],
  ])("handles %s", (_label, input, want) => {
    expect(deriveKey(input)).toBe(want);
  });

  // The API caps the key at 100 characters, and a naive slice can cut mid-word
  // and leave a trailing separator, which the pattern forbids.
  it("respects the length ceiling without leaving a trailing separator", () => {
    const key = deriveKey("a".repeat(98) + " bc");
    expect(key.length).toBeLessThanOrEqual(100);
    expect(key.endsWith("-")).toBe(false);
  });

  // The contract, asserted directly against the pattern the API enforces.
  it("only ever produces the empty string or a key the API pattern accepts", () => {
    const pattern = /^[a-z0-9][a-z0-9-]*$/;
    for (const s of ["HQ Boardroom DSP", "  x  ", "A/V #2", "Café", "2nd", "---", "", "ROOM 1!"]) {
      const key = deriveKey(s);
      expect(key === "" || pattern.test(key)).toBe(true);
    }
  });
});

describe("createIdentity", () => {
  it("derives the key from the display name as it is typed", () => {
    const id = createIdentity();
    id.setDisplay("HQ Boardroom");
    expect(id.name()).toBe("hq-boardroom");
    id.setDisplay("HQ Boardroom DSP");
    expect(id.name()).toBe("hq-boardroom-dsp");
    expect(id.keyDerived()).toBe(true);
  });

  // The rule that makes the pattern usable rather than infuriating: once the
  // operator has taken the key, more typing in the display name must not take it
  // back. This is the assertion the whole primitive exists for.
  it("stops following once the operator edits the key by hand", () => {
    const id = createIdentity();
    id.setDisplay("HQ Boardroom");
    expect(id.name()).toBe("hq-boardroom");

    id.setName("boardroom-a");
    expect(id.keyDerived()).toBe(false);

    id.setDisplay("HQ Boardroom DSP");
    expect(id.name()).toBe("boardroom-a");
    expect(id.display()).toBe("HQ Boardroom DSP");
  });

  // An existing entity's key is already the operator's. Relabelling must never
  // rename: the API takes a rename explicitly and it is a deliberate act.
  it("never derives over an existing key", () => {
    const id = createIdentity({ display: "HQ Boardroom", name: "boardroom-a" });
    expect(id.keyDerived()).toBe(false);
    id.setDisplay("Something Else Entirely");
    expect(id.name()).toBe("boardroom-a");
  });

  it("leaves the key empty when the display name derives to nothing", () => {
    const id = createIdentity();
    id.setDisplay("---");
    expect(id.name()).toBe("");
  });
});
