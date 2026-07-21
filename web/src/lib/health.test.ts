import { describe, it, expect } from "vitest";
import {
  causes,
  chainSentence,
  holdingRoles,
  impactPhrase,
  impairedRoles,
  quorumLabel,
  verdictOf,
  verdictRank,
  worstAlarm,
  worstVerdict,
  type EstateHealth,
  type HealthRole,
} from "./health";

const role = (over: Partial<HealthRole>): HealthRole => ({
  name: "main-display",
  display_name: "Main display",
  impact: "outage",
  impaired: true,
  quorum: 2,
  satisfying: 1,
  required: ["display"],
  degraded: [],
  assigned_to: [],
  alarms: [],
  ...over,
});

describe("verdictOf", () => {
  it("narrows to the three states the console knows", () => {
    expect(verdictOf("healthy")).toBe("healthy");
    expect(verdictOf("degraded")).toBe("degraded");
    expect(verdictOf("outage")).toBe("outage");
  });
  it("is null for anything else, rather than guessing", () => {
    expect(verdictOf(undefined)).toBeNull();
    expect(verdictOf("")).toBeNull();
    expect(verdictOf("brand-new-state")).toBeNull();
  });
});

describe("worstVerdict", () => {
  it("takes the worst, which is how a location rolls up its systems", () => {
    expect(worstVerdict(["healthy", "outage", "degraded"])).toBe("outage");
    expect(worstVerdict(["healthy", "degraded"])).toBe("degraded");
    expect(worstVerdict(["healthy", "healthy"])).toBe("healthy");
  });
  it("ignores states it cannot read, and is null when nothing is readable", () => {
    expect(worstVerdict(["healthy", undefined, "nonsense"])).toBe("healthy");
    expect(worstVerdict([])).toBeNull();
  });
  it("ranks outage above degraded above healthy", () => {
    expect(verdictRank("outage")).toBeGreaterThan(verdictRank("degraded"));
    expect(verdictRank("degraded")).toBeGreaterThan(verdictRank("healthy"));
  });
});

describe("impairedRoles", () => {
  const h = {
    verdict: "outage",
    roles: [
      role({ name: "mic", display_name: "Table mic", impaired: true, impact: "degraded" }),
      role({ name: "display", display_name: "Main display", impaired: true, impact: "outage" }),
      role({ name: "panel", display_name: "Touch panel", impaired: false, impact: "none" }),
    ],
  } as unknown as EstateHealth;

  it("keeps only the impaired ones, worst impact first", () => {
    expect(impairedRoles(h).map((r) => r.name)).toEqual(["display", "mic"]);
  });

  it("names what is holding, which is the other half of the answer", () => {
    expect(holdingRoles(h).map((r) => r.name)).toEqual(["panel"]);
  });

  it("reads an absent health as no roles at all rather than throwing", () => {
    expect(impairedRoles(undefined)).toEqual([]);
    expect(holdingRoles(undefined)).toEqual([]);
  });
});

describe("quorumLabel and impactPhrase", () => {
  it("reads the fill against the quorum in the API's own terms", () => {
    expect(quorumLabel({ satisfying: 1, quorum: 2 })).toBe("1 of 2 satisfying");
  });
  it("says what an impaired role means for its system", () => {
    expect(impactPhrase("outage")).toBe("outage");
    expect(impactPhrase("degraded")).toBe("degraded");
    expect(impactPhrase("none")).toBe("no change");
  });
});

// The join the API does not hand over: which alarm took which required capability
// away. It is the middle link of the chain the panel renders.
describe("causes", () => {
  const r = role({
    degraded: ["display", "hdmi-input"],
    alarms: [
      { id: "a1", severity: "warning", message: "Lamp hours exceeded", component: "disp-1", raised_at: "2026-07-20T09:00:00Z", capabilities: ["display"] },
      { id: "a2", severity: "critical", message: "HDMI board failed", component: "disp-2", raised_at: "2026-07-20T10:00:00Z", capabilities: ["display", "hdmi-input"] },
    ],
  });

  it("pairs each degraded capability with the alarms that took it, worst first", () => {
    const out = causes(r);
    expect(out.map((c) => c.capability)).toEqual(["display", "hdmi-input"]);
    expect(out[0].alarms.map((a) => a.id)).toEqual(["a2", "a1"]); // critical leads
    expect(out[1].alarms.map((a) => a.id)).toEqual(["a2"]);
  });

  it("is empty when no alarm reaches the role, so short-staffed reads differently", () => {
    expect(causes(role({ degraded: [], alarms: [] }))).toEqual([]);
    expect(worstAlarm(role({ alarms: [] }))).toBeNull();
  });

  it("picks the worst, most recent alarm as the one that explains the role", () => {
    expect(worstAlarm(r)?.id).toBe("a2");
  });
});

// The claim the slice makes, in one line. Every link is named: the alarm, the
// component it is on, the capability it took, the role that fell below quorum, and
// what that contributes to the verdict on screen.
describe("chainSentence", () => {
  it("names the alarm, the component, the capability, the role, and the verdict", () => {
    const s = chainSentence(
      role({
        degraded: ["display"],
        alarms: [{ id: "a2", severity: "critical", message: "HDMI board failed", component: "disp-2", raised_at: "2026-07-20T10:00:00Z", capabilities: ["display"] }],
      }),
      "outage",
    );
    expect(s).toBe(
      "A critical alarm on disp-2 degrades display, so Main display satisfies 1 of 2 and contributes outage, which is why this system reads outage.",
    );
  });

  it("refuses to credit a role for a verdict a worse role set", () => {
    const s = chainSentence(
      role({ impact: "degraded", degraded: ["display"], alarms: [{ id: "a1", severity: "warning", message: "Lamp hours", component: "disp-1", raised_at: "2026-07-20T09:00:00Z", capabilities: ["display"] }] }),
      "outage",
    );
    expect(s).toContain("contributes degraded, though this system reads outage on a worse role");
  });

  it("says short-staffed plainly when no alarm reaches the role", () => {
    const s = chainSentence(role({ satisfying: 0, impact: "degraded", alarms: [], degraded: [] }), "degraded");
    expect(s).toContain("No alarm reaches Main display");
    expect(s).toContain("too few components are assigned");
    expect(s).toContain("this system reads degraded");
  });
});
