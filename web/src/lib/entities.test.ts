import { describe, expect, it } from "vitest";
import { createIdentity, deriveName, entityLabel, hasDisplayName, labelGenerated, labelIsName } from "./entities";

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

// labelIsName is the question a surface asks to decide the FACE it renders in:
// the data face for an identifier, the prose face for words somebody (or some
// rule) chose. It is deliberately blind to the pen, because a rendered label is
// prose whoever owns it.
describe("labelIsName", () => {
  it("is true exactly when the label an operator reads is the identifier", () => {
    expect(labelIsName({ name: "codec-1" })).toBe(true);
    expect(labelIsName({ name: "codec-1", display_name: "" })).toBe(true);
    expect(labelIsName({ name: "codec-1", display_name: "  " })).toBe(true);
    expect(labelIsName({ name: "codec-1", display_name: "codec-1" })).toBe(true);
    expect(labelIsName({ name: "codec-1", display_name: "Codec One" })).toBe(false);
  });

  it("does not consult the pen: a rendered label is prose whoever owns it", () => {
    expect(labelIsName({ name: "display-1", display_name: "Display 1", display_name_generated: true })).toBe(false);
    expect(labelIsName({ name: "display-1", display_name: "", display_name_generated: true })).toBe(true);
  });
});

// hasDisplayName answers "did a HUMAN choose this label", which decides whether a
// surface shows the name on a second line of its own.
//
// It used to answer that by comparing two strings, and that was the same question
// only while every label was operator-typed. #682 made a label something a rule
// can render, and a rendered label differs from the name just as an operator's
// does, so the string comparison would have grown a second identifier line under
// every row in the estate. The pen (display_name_generated) is the fact that was
// missing, and #683 put it on the wire.
//
// The string comparison survives as the second half of a conjunction rather than
// being replaced: a row can hold the pen and still read as its own name (a rule
// that renders nothing stores NULL and KEEPS the pen, ADR-0098), and an entity
// with no pen at all (every catalog registry row: a product, a vendor, a role)
// is unchanged by this slice.
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

  // The redefinition. Same label, same name, opposite answer, and the pen is the
  // only thing that differs.
  it("is false for a label the platform rendered, however unlike the name it is", () => {
    expect(hasDisplayName({ name: "display-1", display_name: "Display 1", display_name_generated: true })).toBe(false);
  });

  it("is true for a label an operator typed, which is what the pen being false means", () => {
    expect(hasDisplayName({ name: "display-1", display_name: "Display 1", display_name_generated: false })).toBe(true);
  });

  // A registry row carries no pen at all, so undefined must read as "the operator
  // owns it": every catalog page would otherwise lose its identifier line.
  it("treats an absent pen as operator-owned", () => {
    expect(hasDisplayName({ name: "shure-mxa920", display_name: "Shure MXA920" })).toBe(true);
  });

  // A rule with nothing to say about a row stores NULL and keeps the pen
  // (ADR-0098), so the row reads as its own name and has no second line to show.
  it("is false when the platform holds the pen but rendered nothing", () => {
    expect(hasDisplayName({ name: "codec-1", display_name: "", display_name_generated: true })).toBe(false);
  });
});

// labelGenerated is the other half of the pen: not "is there a second string"
// but "whose words are these". It is what makes a platform-owned label visually
// distinguishable from one the operator chose, which is the thing a bare
// hasDisplayName can no longer say on its own.
describe("labelGenerated", () => {
  it("is true only when the platform holds the pen AND has something to show for it", () => {
    expect(labelGenerated({ name: "display-1", display_name: "Display 1", display_name_generated: true })).toBe(true);
    expect(labelGenerated({ name: "display-1", display_name: "Display 1", display_name_generated: false })).toBe(false);
    expect(labelGenerated({ name: "display-1" })).toBe(false);
  });

  // Holding the pen over an empty render is not a generated label: what the row
  // shows IS its name, and marking it "generated" would claim credit for the
  // fallback.
  it("is false when the pen rendered nothing, so the name is what shows", () => {
    expect(labelGenerated({ name: "codec-1", display_name: "", display_name_generated: true })).toBe(false);
    expect(labelGenerated({ name: "codec-1", display_name: "   ", display_name_generated: true })).toBe(false);
    expect(labelGenerated({ name: "codec-1", display_name: "codec-1", display_name_generated: true })).toBe(false);
  });
});

// The two predicates partition the labels that differ from their name: exactly
// one of them is true for any row showing a label, and neither is true for a row
// showing only its name. A surface can therefore branch on them without a third
// state to invent.
describe("hasDisplayName and labelGenerated", () => {
  it.each([
    ["operator label", { name: "n-1", display_name: "Label", display_name_generated: false }, true, false],
    ["platform label", { name: "n-1", display_name: "Label", display_name_generated: true }, false, true],
    ["no label", { name: "n-1", display_name: "", display_name_generated: true }, false, false],
    ["no pen, no label", { name: "n-1" }, false, false],
  ])("never both for %s", (_case, entity, operator, platform) => {
    expect(hasDisplayName(entity)).toBe(operator);
    expect(labelGenerated(entity)).toBe(platform);
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
