import { describe, it, expect } from "vitest";
import { CHANNELS, TRANSITIONS, draftErrors, draftFrom, emptyDraft, matchSummary, toggled, whenError, type RuleMatch, type Subscription } from "./subscriptions";

describe("draftErrors", () => {
  it("passes a named draft with an edge and a channel", () => {
    expect(draftErrors({ ...emptyDraft(), name: "hq-outages" })).toEqual({});
  });
  it("asks for a name, and judges it by the entity name rule", () => {
    expect(draftErrors(emptyDraft()).name).toMatch(/Name it/);
    expect(draftErrors({ ...emptyDraft(), name: "HQ Outages" }).name).toMatch(/Lowercase/);
  });
  it("refuses a draft with no edge or no channel", () => {
    const e = draftErrors({ ...emptyDraft(), name: "x", on: [], channels: [] });
    expect(e.on).toBeTruthy();
    expect(e.channels).toBeTruthy();
  });
  it("leaves the when to the server: an empty one always holds", () => {
    expect(draftErrors({ ...emptyDraft(), name: "x", when: "" })).toEqual({});
  });
});

describe("toggled", () => {
  it("adds and removes, keeping the canonical order", () => {
    expect(toggled(["cleared"], "raised", TRANSITIONS)).toEqual(["raised", "cleared"]);
    expect(toggled(["raised", "cleared"], "raised", TRANSITIONS)).toEqual(["cleared"]);
    expect(toggled([], "email", CHANNELS)).toEqual(["email"]);
  });
});

describe("draftFrom", () => {
  it("seeds the drawer from a rule, dropping words it does not know", () => {
    const s = {
      id: "1", name: "hq", label: "HQ", on: ["raised", "bogus"], when: "severity_rank >= 40", channels: ["inbox"],
      enabled: false, created_at: "", updated_at: "",
    } as Subscription;
    expect(draftFrom(s)).toEqual({ name: "hq", label: "HQ", on: ["raised"], when: "severity_rank >= 40", channels: ["inbox"], enabled: false });
  });
});

describe("whenError", () => {
  it("reads a when refusal placed on body.when, with its position", () => {
    const e = { status: 422, detail: "when: bad", errors: [{ location: "body.when", message: "unknown name sevrity", value: { line: 1, column: 3 } }] };
    expect(whenError(e)).toBe("Line 1, column 3: unknown name sevrity");
  });
  it("is null for any other refusal", () => {
    expect(whenError({ status: 409, detail: "taken" })).toBeNull();
    expect(whenError(null)).toBeNull();
  });
});

describe("matchSummary", () => {
  it("counts what would have told you, what would not, and what could not be judged", () => {
    const m = (matched: boolean, error?: string) => ({ matched, error }) as RuleMatch;
    expect(matchSummary([m(true), m(false), m(false, "boom"), m(true)])).toEqual({ matched: 2, missed: 1, errored: 1 });
  });
});
