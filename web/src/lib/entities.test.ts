import { describe, expect, it } from "vitest";
import { entityLabel, hasDisplayName } from "./entities";

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
