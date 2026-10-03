import { describe, it, expect } from "vitest";
import {
  activeRoles,
  impactPhrase,
  inactiveRoles,
  quorumLabel,
  verdictOf,
  verdictRank,
  worstVerdict,
  type FleetHealth,
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

describe("activeRoles and inactiveRoles", () => {
  it("reads an absent health as no roles at all rather than throwing", () => {
    expect(activeRoles(undefined)).toEqual([]);
    expect(inactiveRoles(undefined)).toEqual([]);
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
  } as unknown as FleetHealth;

  it("counts only the roles whose alternate answered the choice as in play", () => {
    expect(activeRoles(withInactiveChoice).map((r) => r.name)).toEqual(["video-bar"]);
  });

  it("keeps the losing alternate's roles apart rather than dropping them", () => {
    expect(inactiveRoles(withInactiveChoice).map((r) => r.name)).toEqual(["codec", "camera"]);
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
