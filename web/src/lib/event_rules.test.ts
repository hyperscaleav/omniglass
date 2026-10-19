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
  listEventRules,
  readDuration,
  readValue,
  scopedOnly,
  sustainWords,
  updateBody,
  updateEventRule,
  validateDraft,
  withLane,
} from "./event_rules";
import { uuidFor } from "./testids";

const reach: EventRule = {
  id: uuidFor("rule-reachability-lost"), name: "reachability-lost", label: "Reachability lost",
  selector: { component_types: [], components: [] }, sample_key: "endpoint-reachable", sample_lane: "property",
  fire_criteria: { op: "eq", value: "down" }, clear_criteria: { op: "eq", value: "up" },
  fire_sustain: "15s", clear_sustain: "0s", severity: "high", enabled: true, official: true, version: 1,
};

const hot: EventRule = {
  id: uuidFor("rule-room-too-hot"), name: "room-too-hot",
  selector: { component_types: ["display"], components: [] }, sample_key: "temperature-c", sample_lane: "metric",
  fire_criteria: { op: "gt", value: 40 }, fire_sustain: "1m0s", clear_sustain: "30s",
  severity: "average", enabled: true, official: false, version: 3,
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
    expect(fireWords({ ...reach, fire_criteria: { op: "ne", value: "up" }, fire_sustain: "0s" })).toBe("reads anything but up");
  });

  it("drops a zero sustain and trims Go's trailing zero units", () => {
    expect(sustainWords("0s")).toBe("");
    expect(sustainWords("")).toBe("");
    expect(sustainWords("5m0s")).toBe("5m");
    expect(sustainWords("1h0m0s")).toBe("1h");
    expect(sustainWords("1m30s")).toBe("1m30s");
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
    expect(readValue("warm", "metric").error).toMatch(/not one/);
    expect(readValue(" ", "property").error).toBeTruthy();
  });

  it("reads a Go duration, blank as none", () => {
    expect(readDuration("")).toBeNull();
    expect(readDuration("30s")).toBeNull();
    expect(readDuration("1h30m")).toBeNull();
    expect(readDuration("30")).toMatch(/duration/);
    expect(readDuration("thirty seconds")).toMatch(/duration/);
  });

  it("refuses an incomplete draft field by field", () => {
    const e = validateDraft({ ...emptyDraft(""), name: "Bad Name", fireSustain: "soon" }, "create");
    expect(e.name).toMatch(/Lowercase/);
    expect(e.sampleKey).toBeTruthy();
    expect(e.fireSustain).toBeTruthy();
    expect(e.severity).toBeTruthy();
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

describe("the bodies a draft sends", () => {
  it("creates with typed criteria, durations, and no clear criterion for the opposite", () => {
    const d = { ...emptyDraft("average"), name: "room-too-hot", sampleKey: "temperature-c", lane: "metric" as const,
      fireOp: "gt" as const, fireValue: "40", fireSustain: "1m", componentTypes: ["display"] };
    expect(createBody(d)).toEqual({
      name: "room-too-hot", sample_key: "temperature-c",
      fire_criteria: { op: "gt", value: 40 }, fire_sustain: "1m", clear_sustain: "0s",
      severity: "average", enabled: true, selector: { component_types: ["display"], components: [] },
    });
  });

  it("creates with a text clear criterion when one is chosen", () => {
    const d = { ...emptyDraft("high"), name: "reach", sampleKey: "endpoint-reachable", fireValue: "down",
      clearMode: "custom" as const, clearValue: "up", label: "Reach" };
    const b = createBody(d);
    expect(b.clear_criteria).toEqual({ op: "eq", value: "up" });
    expect(b.label).toBe("Reach");
  });

  it("drops a clear criterion on edit with clear_negates", () => {
    const d = { ...draftFromRule({ ...reach, official: false }), clearMode: "opposite" as const };
    const b = updateBody(d, reach);
    expect(b.clear_negates).toBe(true);
    expect(b.clear_criteria).toBeUndefined();
    expect(b.fire_sustain).toBe("15s");
    expect(b.clear_sustain).toBe("0s");
  });

  it("sends no clear_negates when the rule never had a clear criterion", () => {
    const b = updateBody(draftFromRule(hot), hot);
    expect(b.clear_negates).toBeUndefined();
    expect(b.fire_criteria).toEqual({ op: "gt", value: 40 });
    expect(b.selector).toEqual({ component_types: ["display"], components: [] });
  });
});

describe("scopedOnly", () => {
  it("is true only when every grant is bounded", () => {
    expect(scopedOnly({ grants: [{ scope_kind: "location" }] })).toBe(true);
    expect(scopedOnly({ grants: [{ scope_kind: "location" }, { scope_kind: "all" }] })).toBe(false);
    expect(scopedOnly({ grants: [] })).toBe(false);
    expect(scopedOnly(null)).toBe(false);
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
    expect(await getRuleState("reachability-lost")).toEqual({ open: [], pending: [] });
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
