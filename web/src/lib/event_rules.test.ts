import { describe, it, expect, afterEach, vi } from "vitest";
import {
  type EventRule,
  OPS_BY_LANE,
  clearWords,
  createBody,
  defaultSeverity,
  draftFromRule,
  emptyDraft,
  fireWords,
  getRuleState,
  judgedWords,
  listEventRules,
  readDuration,
  readValue,
  sustainWords,
  updateBody,
  updateEventRule,
  lowerShort,
  previewBody,
  shortForm,
  toBuilder,
  toExpression,
  validateDraft,
  withLane,
} from "./event_rules";
import { uuidFor } from "./testids";

const reach: EventRule = {
  id: uuidFor("rule-reachability-lost"), name: "reachability-lost", label: "Reachability lost",
  selector: { component_types: [], components: [] }, sample_key: "endpoint-reachable", sample_lane: "property",
  fire_criteria: { expr: 'value == "down"' }, clear_criteria: { expr: 'value == "up"' },
  fire_sustain: "15s", clear_sustain: "0s", severity: "high",
  fire_event_type: "condition-started", clear_event_type: "condition-ended", enabled: true, official: true, version: 1,
  open_alarms: 0, pending_streaks: 0, conditions_in_force: 0,
};

const hot: EventRule = {
  id: uuidFor("rule-too-hot"), name: "too-hot",
  selector: { component_types: ["display"], components: [] }, sample_key: "temperature-c", sample_lane: "metric",
  fire_criteria: { expr: "value > 40.0" }, fire_sustain: "1m0s", clear_sustain: "30s",
  severity: "average", fire_event_type: "condition-started", enabled: true, official: false, version: 3, open_alarms: 0, pending_streaks: 0, conditions_in_force: 0,
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("reading a rule in words", () => {
  it("reads the official reachability rule the way an operator says it", () => {
    expect(fireWords(reach)).toBe("reads down for 15s");
    expect(clearWords(reach)).toBe("clears on up");
  });

  it("reads a metric comparison with its unit, and an absent clear as the opposite", () => {
    expect(fireWords(hot, "°C")).toBe("reads above 40 °C for 1m");
    expect(clearWords(hot)).toBe("clears on the opposite after 30s");
  });

  it("says a ne criterion as anything but", () => {
    expect(fireWords({ ...reach, fire_criteria: { expr: 'value != "up"' }, fire_sustain: "0s" })).toBe("reads anything but up");
  });

  it("reads an expression with no short form as the expression itself", () => {
    const expr = 'value == "down" && instance != "aux"';
    expect(fireWords({ ...reach, fire_criteria: { expr }, fire_sustain: "0s" })).toBe(`reads ${expr}`);
  });

  it("drops a zero sustain and trims Go's trailing zero units", () => {
    expect(sustainWords("0s")).toBe("");
    expect(sustainWords("")).toBe("");
    expect(sustainWords("5m0s")).toBe("5m");
    expect(sustainWords("1h0m0s")).toBe("1h");
    expect(sustainWords("1m30s")).toBe("1m30s");
  });
});

describe("when a rule last judged a sample", () => {
  it("says a rule that has judged nothing so, and otherwise how long ago, with the bound", () => {
    expect(judgedWords(undefined)).toBe("Has not judged a sample yet");
    const ago = new Date(Date.now() - 12_000).toISOString();
    expect(judgedWords(ago)).toBe("Last judged a sample 12s ago (to within 5s)");
  });
});

describe("the form's rules", () => {
  it("limits a property to eq/ne and offers a metric all six", () => {
    expect(OPS_BY_LANE.property).toEqual(["eq", "ne"]);
    expect(OPS_BY_LANE.metric).toEqual(["eq", "ne", "gt", "ge", "lt", "le"]);
  });

  it("pulls an op the new lane cannot evaluate back to eq", () => {
    const d = { ...emptyDraft("average"), lane: "metric" as const, fireOp: "gt" as const, clearOp: "le" as const };
    const moved = withLane(d, "endpoint-reachable", "property");
    expect(moved.fireOp).toBe("eq");
    expect(moved.clearOp).toBe("eq");
    expect(moved.lane).toBe("property");
    expect(withLane({ ...d, fireOp: "ne" }, "x", "property").fireOp).toBe("ne");
  });

  it("types a value by lane", () => {
    expect(readValue("down", "property")).toEqual({ value: "down", error: null });
    expect(readValue("40.5", "metric")).toEqual({ value: 40.5, error: null });
    expect(readValue("-3", "metric")).toEqual({ value: -3, error: null });
    expect(readValue("warm", "metric").error).toMatch(/not one/);
    expect(readValue(" ", "property").error).toBeTruthy();
  });

  it("keeps property text exact, refusing edge whitespace rather than trimming it", () => {
    // A property compares text exactly, so " down" would never match "down":
    // the form says so instead of silently sending something else.
    expect(readValue(" down", "property").error).toMatch(/spaces/);
    expect(readValue("down ", "property").error).toMatch(/spaces/);
    expect(readValue("hdmi 1", "property")).toEqual({ value: "hdmi 1", error: null });
  });

  it("reads a metric as a strict decimal: no hex, exponent, blank or padding", () => {
    for (const raw of ["0x10", "1e3", "", "  ", " 40", "40 ", "Infinity", "4_0", "."]) {
      expect(readValue(raw, "metric").error, raw).toBeTruthy();
    }
    expect(readValue("0.25", "metric")).toEqual({ value: 0.25, error: null });
  });

  it("reads a Go duration, blank as none", () => {
    expect(readDuration("")).toBeNull();
    expect(readDuration("30s")).toBeNull();
    expect(readDuration("1h30m")).toBeNull();
    expect(readDuration("30")).toMatch(/duration/);
    expect(readDuration("thirty seconds")).toMatch(/duration/);
  });

  it("refuses an incomplete draft field by field", () => {
    const e = validateDraft({ ...emptyDraft(""), name: "Bad Name", fireSustain: "soon", fireEventType: "" }, "create");
    expect(e.name).toMatch(/Lowercase/);
    expect(e.sampleKey).toBeTruthy();
    expect(e.fireSustain).toBeTruthy();
    expect(e.fireEventType).toBeTruthy();
    // Edit never judges the create-only name.
    expect(validateDraft({ ...draftFromRule(hot), name: "" }, "edit")).toEqual({});
  });

  it("asks for a clear value only when the author chose a clear criterion", () => {
    const d = { ...draftFromRule(reach), clearValue: "" };
    expect(validateDraft(d, "edit").clearValue).toBeTruthy();
    expect(validateDraft({ ...d, clearMode: "opposite" }, "edit").clearValue).toBeUndefined();
  });

  it("defaults severity to the worst level that does not take its component out", () => {
    const levels = [
      { name: "info", label: "Info", color: "gray", rank: 10, outage: false },
      { name: "average", label: "Average", color: "orange", rank: 30, outage: false },
      { name: "high", label: "High", color: "red", rank: 40, outage: true },
    ] as Parameters<typeof defaultSeverity>[0];
    expect(defaultSeverity(levels)).toBe("average");
    expect(defaultSeverity([])).toBe("");
  });
});

describe("the short form an expression lowers from", () => {
  it("reads back the comparisons the server lowers a short form to", () => {
    expect(shortForm({ expr: 'value == "down"' })).toEqual({ op: "eq", value: "down" });
    expect(shortForm({ expr: 'value != "say \\"hi\\""' })).toEqual({ op: "ne", value: 'say "hi"' });
    expect(shortForm({ expr: "value >= 80.0" })).toEqual({ op: "ge", value: 80 });
    expect(shortForm({ expr: "value < -0.5" })).toEqual({ op: "lt", value: -0.5 });
    expect(shortForm({ expr: "value == true" })).toEqual({ op: "eq", value: "true" });
  });

  it("has none for an expression the builder cannot say", () => {
    // The lowering never writes an exponent, so one is an author's expression.
    for (const expr of ['value == "down" && instance != "aux"', "value > 40 || value < 0", "false", "value matches 'x'", "upper(value) == \"X\"",
      "value > 1e21", "value > 2.5E-7", "value > 1.0e+3"]) {
      expect(shortForm({ expr })).toBeNull();
    }
  });

  it("treats an empty expression as no expression: the builder owns it and an edit is sent", () => {
    const was = { ...reach, official: false, fire_criteria: { expr: "" }, clear_criteria: undefined };
    const d = draftFromRule(was);
    expect(d.fireExpr).toBeNull();
    expect(updateBody({ ...d, fireValue: "down" }, was)).toEqual({ fire_criteria: { op: "eq", value: "down" } });
  });

  it("keeps an expression the builder cannot say: an edit never overwrites it", () => {
    const expr = 'value == "down" && instance != "aux"';
    const was = { ...reach, official: false, fire_criteria: { expr }, clear_criteria: undefined };
    const d = draftFromRule(was);
    expect(d.fireExpr).toBe(expr);
    expect(validateDraft(d, "edit")).toEqual({});
    expect(updateBody({ ...d, label: "Reach" }, was)).toEqual({ label: "Reach" });
    expect(createBody({ ...d, name: "copy" }).fire_criteria).toEqual({ expr });
  });
});

describe("the bodies a draft sends", () => {
  it("creates with typed criteria, durations, and no clear criterion for the opposite", () => {
    const d = { ...emptyDraft("average"), name: "too-hot", sampleKey: "temperature-c", lane: "metric" as const,
      fireOp: "gt" as const, fireValue: "40", fireSustain: "1m", componentTypes: ["display"] };
    expect(createBody(d)).toEqual({
      name: "too-hot", sample_key: "temperature-c",
      fire_criteria: { op: "gt", value: 40 }, fire_sustain: "1m", clear_sustain: "0s",
      severity: "average", fire_event_type: "condition-started", clear_event_type: "condition-ended", enabled: true, selector: { component_types: ["display"], components: [] },
    });
  });

  it("creates with a text clear criterion when one is chosen", () => {
    const d = { ...emptyDraft("high"), name: "reach", sampleKey: "endpoint-reachable", fireValue: "down",
      clearMode: "custom" as const, clearValue: "up", label: "Reach" };
    const b = createBody(d);
    expect(b.clear_criteria).toEqual({ op: "eq", value: "up" });
    expect(b.label).toBe("Reach");
  });

  it("drops a clear criterion on edit with clear_negates, sending nothing else", () => {
    const was = { ...reach, official: false };
    const d = { ...draftFromRule(was), clearMode: "opposite" as const };
    expect(updateBody(d, was)).toEqual({ clear_negates: true });
  });

  it("takes no severity as a choice (informational), and requires a fire event type", () => {
    expect(Object.keys(validateDraft({ ...draftFromRule(hot), severity: "" }, "edit"))).toEqual([]);
    expect(Object.keys(validateDraft({ ...emptyDraft(), name: "x", sampleKey: "k", fireValue: "1" }, "create"))).not.toContain("severity");
    expect(validateDraft({ ...draftFromRule(hot), fireEventType: "" }, "edit").fireEventType).toBeTruthy();
    expect(createBody({ ...draftFromRule(hot), severity: "" }).severity).toBeUndefined();
  });

  it("sends nothing for an edit that changes nothing", () => {
    expect(updateBody(draftFromRule(hot), hot)).toEqual({});
  });

  it("sends only the fields an edit changed", () => {
    expect(updateBody({ ...draftFromRule(hot), label: "Too hot" }, hot)).toEqual({ label: "Too hot" });
    expect(updateBody({ ...draftFromRule(hot), fireValue: "45", clearSustain: "1m" }, hot)).toEqual({
      fire_criteria: { op: "gt", value: 45 }, clear_sustain: "1m",
    });
    // The selector replaces wholesale, so it rides whole once any entry moves.
    expect(updateBody({ ...draftFromRule(hot), componentTypes: [] }, hot)).toEqual({
      selector: { component_types: [], components: [] },
    });
    // A custom clear rides when it changed, and its value is typed by lane.
    expect(updateBody({ ...draftFromRule(reach), clearValue: "degraded" }, reach)).toEqual({
      clear_criteria: { op: "eq", value: "degraded" },
    });
  });

  it("leaves a stale selector entry alone unless the edit touches the selector", () => {
    // A rule watching a component that was since deleted: a label edit must not
    // restate the selector, or the server would judge the stale id afresh.
    const stale = { ...hot, selector: { component_types: ["gone-type"], components: [uuidFor("comp-gone")] } };
    expect(updateBody({ ...draftFromRule(stale), label: "Renamed" }, stale)).toEqual({ label: "Renamed" });
  });
});

describe("the data layer", () => {
  afterEach(() => vi.restoreAllMocks());

  it("unwraps the list envelope", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ event_rules: [reach] }));
    expect(await listEventRules()).toEqual([reach]);
  });

  it("reads a rule's live state, nulls as empty", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ open: null, pending: null }));
    expect(await getRuleState("reachability-lost")).toEqual({ conditions: [], open: [], pending: [] });
  });

  it("PATCHes by name and throws the problem body on refusal", async () => {
    const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "official rules are read-only" }, 409));
    await expect(updateEventRule("reachability-lost", { enabled: false })).rejects.toMatchObject({ detail: "official rules are read-only" });
    const req = spy.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toMatch(/\/event-rules\/reachability-lost$/);
    expect(await req.json()).toEqual({ enabled: false });
  });
});

describe("the builder teaches the expression it lowers to", () => {
  it("lowers a comparison exactly as the server does", () => {
    expect(lowerShort("eq", "down", "property", "string")).toBe('value == "down"');
    expect(lowerShort("ne", 'say "hi"', "property", "string")).toBe('value != "say \\"hi\\""');
    expect(lowerShort("gt", 40, "metric")).toBe("value > 40.0");
    expect(lowerShort("le", -2.5, "metric")).toBe("value <= -2.5");
    expect(lowerShort("eq", "true", "property", "bool")).toBe("value == true");
    expect(lowerShort("eq", "on", "property", "bool")).toBe("false");
    expect(lowerShort("ne", "on", "property", "bool")).toBe("true");
    expect(lowerShort("eq", undefined, "property", "string")).toBeNull();
    // Plain decimals, as the server writes them: never an exponent.
    expect(lowerShort("gt", 1e-7, "metric")).toBe("value > 0.0000001");
    expect(lowerShort("lt", 1e21, "metric")).toBe("value < 1000000000000000000000.0");
  });
});

describe("writing criteria as an expression", () => {
  it("switches to an expression seeded with the builder's own, and back when it is still a comparison", () => {
    const d = { ...emptyDraft(), sampleKey: "temperature-c", lane: "metric" as const, fireOp: "gt" as const, fireValue: "40" };
    const e = toExpression(d, "fire");
    expect(e.fireExpr).toBe("value > 40.0");
    expect(toBuilder({ ...e, fireExpr: "value >= 45.0" }, "fire")).toMatchObject({ fireExpr: null, fireOp: "ge", fireValue: "45" });
    // An expression the builder cannot say goes back to the builder's last comparison.
    expect(toBuilder({ ...e, fireExpr: "value > 40 && instance != \"aux\"" }, "fire")).toMatchObject({ fireExpr: null, fireOp: "gt", fireValue: "40" });
  });

  it("sends an expression that changed, and requires one that is not empty", () => {
    const was = { ...hot };
    const d = { ...draftFromRule(was), fireExpr: "value > 40 && instance != \"aux\"" };
    expect(updateBody(d, was)).toEqual({ fire_criteria: { expr: "value > 40 && instance != \"aux\"" } });
    expect(validateDraft({ ...d, fireExpr: "  " }, "edit").fireExpr).toBeTruthy();
    expect(validateDraft({ ...draftFromRule(reach), clearExpr: "" }, "edit").clearExpr).toBeTruthy();
    expect(createBody({ ...d, name: "x" }).fire_criteria).toEqual({ expr: "value > 40 && instance != \"aux\"" });
  });

  it("previews the draft as a candidate rule: key, selector and criteria, as sent", () => {
    const d = { ...draftFromRule(hot), fireExpr: "value > 41" };
    expect(previewBody(d)).toEqual({
      sample_key: "temperature-c", selector: { component_types: ["display"], components: [] },
      fire_criteria: { expr: "value > 41" }, clear_criteria: undefined,
    });
  });
});

describe("criteria that do not compile, and expressions the builder cannot say", () => {
  const broken = { ...reach, official: false, sample_key: "door-state", fire_criteria: { expr: 'value == "open"' }, clear_criteria: undefined,
    criteria_error: { field: "fire_criteria" as const, line: 1, column: 7, message: "invalid operation: == (mismatched types bool and string)" } };

  it("opens a side whose criteria do not compile as an expression, with the stored source, though the builder could parse it", () => {
    const d = draftFromRule(broken);
    expect(d.fireExpr).toBe('value == "open"');
  });

  it("always sends a side that had an error, even unchanged, so a save repairs or re-judges it", () => {
    const d = draftFromRule(broken);
    expect(updateBody({ ...d, fireExpr: "value" }, broken).fire_criteria).toEqual({ expr: "value" });
    expect(updateBody(d, broken).fire_criteria).toEqual({ expr: 'value == "open"' });
    const viaBuilder = toBuilder(d, "fire");
    expect(updateBody({ ...viaBuilder, fireValue: "open" }, broken).fire_criteria).toEqual({ op: "eq", value: "open" });
  });

  it("keeps an expression the builder cannot say across a switch to the builder and back", () => {
    const rich = 'value > 40 && instance != "aux"';
    const d = { ...draftFromRule(hot), fireExpr: rich };
    const b = toBuilder(d, "fire");
    expect(b.fireExpr).toBeNull();
    expect(toExpression(b, "fire").fireExpr).toBe(rich);
    // One the builder can say is the builder's: switching back lowers the comparison.
    const simple = toBuilder({ ...d, fireExpr: "value >= 45.0" }, "fire");
    expect(toExpression({ ...simple, fireValue: "50" }, "fire").fireExpr).toBe("value >= 50.0");
  });
});
