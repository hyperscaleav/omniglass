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
  label: "Main display",
  impact: "outage",
  impaired: true,
  quorum: 2,
  satisfying: 1,
  short: 1,
  spare: 0,
  down: [],
  assigned_to: [],
  alarms: [],
  active: true,
  ...over,
});

describe("verdictOf", () => {
  it("narrows to the three states the console knows", () => {
    expect(verdictOf("healthy")).toBe("healthy");
    expect(verdictOf("incomplete")).toBe("incomplete");
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
    expect(worstVerdict(["incomplete", "degraded"])).toBe("degraded");
    expect(worstVerdict(["healthy", "incomplete"])).toBe("incomplete");
    expect(worstVerdict(["healthy", "healthy"])).toBe("healthy");
  });
  it("ignores states it cannot read, and is null when nothing is readable", () => {
    expect(worstVerdict(["healthy", undefined, "nonsense"])).toBe("healthy");
    expect(worstVerdict([])).toBeNull();
  });
  it("ranks outage above degraded above healthy", () => {
    expect(verdictRank("outage")).toBeGreaterThan(verdictRank("degraded"));
    // incomplete sits between healthy and degraded: worth surfacing above a
    // clean system, worth burying under anything actually broken.
    expect(verdictRank("degraded")).toBeGreaterThan(verdictRank("incomplete"));
    expect(verdictRank("incomplete")).toBeGreaterThan(verdictRank("healthy"));
  });
});

describe("impairedRoles", () => {
  const h = {
    verdict: "outage",
    roles: [
      role({ name: "mic", label: "Table mic", impaired: true, impact: "degraded" }),
      role({ name: "display", label: "Main display", impaired: true, impact: "outage" }),
      role({ name: "panel", label: "Touch panel", impaired: false, impact: "none" }),
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

  // A role belonging to a choice's LOSING alternate can still read impaired
  // on its own terms (active: false); its impaired/short/spare did not move
  // the verdict, because a different alternate answered the choice. Showing
  // it as an ordinary impairment is the exact contradiction active exists to
  // prevent (see healthRoleBody's own doc string): the seeded huddle-room
  // shape is precisely this (a satisfied all-in-one alternate beside an
  // unbuilt component-built one).
  const withInactiveChoice = {
    verdict: "healthy",
    roles: [
      role({ name: "video-bar", label: "Video bar", impaired: false, impact: "outage", active: true, choice: "conferencing", alternate: "all-in-one" }),
      role({ name: "codec", label: "Codec", impaired: true, impact: "outage", active: false, choice: "conferencing", alternate: "component-built" }),
      role({ name: "camera", label: "Camera", impaired: true, impact: "outage", active: false, choice: "conferencing", alternate: "component-built" }),
    ],
  } as unknown as EstateHealth;

  it("excludes an impaired role whose alternate lost the choice: it did not move the verdict", () => {
    expect(impairedRoles(withInactiveChoice)).toEqual([]);
  });

  it("does not count an inactive role as holding either: it is not in play, not fine", () => {
    expect(holdingRoles(withInactiveChoice).map((r) => r.name)).toEqual(["video-bar"]);
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

// The join the API does not hand over: which alarm took which down component
// down. It is the middle link of the chain the panel renders.
describe("causes", () => {
  const r = role({
    down: ["disp-1", "disp-2"],
    alarms: [
      { id: "a1", severity: "warning", message: "Lamp hours exceeded", component: "disp-1", raised_at: "2026-07-20T09:00:00Z" },
      { id: "a2", severity: "critical", message: "HDMI board failed", component: "disp-2", raised_at: "2026-07-20T10:00:00Z" },
    ],
  });

  it("pairs each down component with the alarms on it, worst first", () => {
    const out = causes(r);
    expect(out.map((c) => c.component)).toEqual(["disp-1", "disp-2"]);
    expect(out[0].alarms.map((a) => a.id)).toEqual(["a1"]);
    expect(out[1].alarms.map((a) => a.id)).toEqual(["a2"]);
  });

  it("is empty when no component is down, so short-staffed reads differently", () => {
    expect(causes(role({ down: [], alarms: [] }))).toEqual([]);
    expect(worstAlarm(role({ alarms: [] }))).toBeNull();
  });

  it("picks the worst, most recent alarm as the one that explains the role", () => {
    expect(worstAlarm(r)?.id).toBe("a2");
  });
});

// The claim the slice makes, in one line. Every link is named: the alarm, the
// component it is on, the component it took down, the role that fell below
// quorum, and what that contributes to the verdict on screen.
describe("chainSentence", () => {
  it("names the alarm, the component, the role, and the verdict", () => {
    const s = chainSentence(
      role({
        down: ["disp-2"],
        alarms: [{ id: "a2", severity: "critical", message: "HDMI board failed", component: "disp-2", raised_at: "2026-07-20T10:00:00Z" }],
      }),
      "outage",
    );
    expect(s).toBe(
      "A critical alarm on disp-2 takes it out of the role, so Main display satisfies 1 of 2 and contributes outage, which is why this system reads outage.",
    );
  });

  it("refuses to credit a role for a verdict a worse role set", () => {
    const s = chainSentence(
      role({ impact: "degraded", down: ["disp-1"], alarms: [{ id: "a1", severity: "warning", message: "Lamp hours", component: "disp-1", raised_at: "2026-07-20T09:00:00Z" }] }),
      "outage",
    );
    expect(s).toContain("contributes degraded, though this system reads outage on a worse role");
  });

  it("says short-staffed plainly when no component is down", () => {
    const s = chainSentence(role({ satisfying: 0, impact: "degraded", alarms: [], down: [] }), "degraded");
    expect(s).toContain("No component assigned to Main display is down");
    expect(s).toContain("too few are assigned");
    expect(s).toContain("this system reads degraded");
  });
});
