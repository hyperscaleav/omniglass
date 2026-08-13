import { describe, expect, it } from "vitest";
import { createIdentity, deriveName, entityLabel, hasDisplayName, labelIsName } from "./entities";
import { createPen } from "./namegen";
import { seedLabelPen } from "../components/LabelPenField";

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

// labelGenerated RETIRED with the chip (#693), and these are its cases,
// converted rather than dropped.
//
// It answered "is there a platform-rendered label here to MARK", which only a
// marker needs. The chip was its one caller, and the surface that replaced it
// asks a different question with a different answer: an edit blade asks "who
// holds the pen on this field", so that a locked field cannot post the
// platform's own words back as an override. The two disagree on exactly the rows
// below where `platform` and `locked` differ, and each is right for its own
// surface: a rule that rendered nothing has no label to mark, but the pen over
// it is still the platform's and the field must still open locked.
//
// The pen fact itself is unchanged and still on the wire; what went is the
// predicate that existed only to badge it.
describe("the pen, on the surfaces that ask about it", () => {
  it.each([
    ["operator label", { name: "n-1", display_name: "Label", display_name_generated: false }, true, false],
    ["platform label", { name: "n-1", display_name: "Label", display_name_generated: true }, false, true],
    // Held the pen, rendered nothing. The list shows the name and no second
    // line; the blade still opens LOCKED, which is the case the retired
    // predicate answered "false" for and the blade answers "true".
    ["no label", { name: "n-1", display_name: "", display_name_generated: true }, false, true],
    ["blank label", { name: "n-1", display_name: "   ", display_name_generated: true }, false, true],
    // A rule that renders exactly the name: one line in the list, and a locked
    // blade field, for the same reason.
    ["label equals name", { name: "n-1", display_name: "n-1", display_name_generated: true }, false, true],
    ["no pen, no label", { name: "n-1" }, false, false],
  ])("reads %s the same way in the list and on the blade", (_case, entity, secondLine, locked) => {
    // What the LIST does with it: show the identifier on a second line, or not.
    expect(hasDisplayName(entity)).toBe(secondLine);
    // What the BLADE does with it: open the label field locked, or editable.
    const pen = createPen();
    seedLabelPen(pen, entity);
    expect(pen.overridden()).toBe(!locked);
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
