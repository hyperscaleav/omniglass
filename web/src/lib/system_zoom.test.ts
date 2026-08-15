import { describe, expect, it } from "vitest";
import { systemZoomVM } from "./system_zoom";
import type { FleetView } from "./fleet";
import type { EffectiveRole, FleetHealth } from "./system_zoom";
import { uuidFor } from "./testids";

// The system zoom's view model (#636): the typed slots a system needs filled,
// merged from the two reads that each carry half the story. The health read
// has the figures and the choice grouping (satisfying, short, active, down);
// the roles read has the declaration (accepted types, pinned products,
// positions). Merged by role name, grouped by choice, and never recomputed:
// every figure on a card is the server's own.

const view: FleetView = {
  locations: [
    {
      id: uuidFor("sz-room"),
      name: "boardroom-a",
      label: "Boardroom A",
      location_type: "room",
      location_type_id: uuidFor("szt-room"),
      parent: "",
      verdict: "degraded",
    },
  ],
  systems: [
    {
      id: uuidFor("sz-sys"),
      name: "boardroom",
      label: "Boardroom",
      location: uuidFor("sz-room"),
      verdict: "degraded",
      dots: [
        { component: uuidFor("sz-c-bar"), name: "videobar-1", verdict: "healthy", primary: true, shared: true },
        { component: uuidFor("sz-c-mic"), name: "mic-1", verdict: "outage", primary: true, shared: false },
        { component: uuidFor("sz-c-power"), name: "device-1", verdict: "healthy", primary: true, shared: false },
      ],
    },
    {
      id: uuidFor("sz-other"),
      name: "overflow",
      label: "Overflow Room",
      location: null,
      verdict: "healthy",
      dots: [{ component: uuidFor("sz-c-bar"), name: "videobar-1", verdict: "healthy", primary: false, shared: true }],
    },
  ],
} as unknown as FleetView;

const health: FleetHealth = {
  verdict: "degraded",
  roles: [
    // Unconditional, occupant down: the failure gap.
    {
      name: "room-mic", label: "Room Microphone", impact: "degraded", quorum: 2, satisfying: 1, short: 1, spare: 0,
      impaired: true, active: true, assigned_to: ["videobar-1", "mic-1"], down: ["mic-1"], alarms: [],
    },
    // Unconditional, nobody staffed: the commissioning gap.
    {
      name: "main-display", label: "Main Display", impact: "outage", quorum: 1, satisfying: 0, short: 1, spare: 0,
      impaired: true, active: true, assigned_to: [], down: [], alarms: [],
    },
    // A choice answered by all-in-one: the bar staffs it.
    {
      name: "conf-bar", label: "Conferencing Bar", impact: "outage", quorum: 1, satisfying: 1, short: 0, spare: 0,
      impaired: false, active: true, assigned_to: ["videobar-1"], down: [], alarms: [],
      choice: "conferencing", alternate: "all-in-one",
    },
    // The losing alternate's roles: legal build, not outstanding work.
    {
      name: "conf-codec", label: "conf-codec", impact: "outage", quorum: 1, satisfying: 0, short: 1, spare: 0,
      impaired: true, active: false, assigned_to: [], down: [], alarms: [],
      choice: "conferencing", alternate: "component-system",
    },
  ],
  systems: [],
  transitions: [],
} as unknown as FleetHealth;

const declared: EffectiveRole[] = [
  {
    name: "room-mic", label: "Room Microphone", quorum: 2, assigned: 2, impact: "degraded", from_standard: true,
    accepted_types: ["microphone", "video-bar"], pinned_products: [], assigned_to: ["videobar-1", "mic-1"],
    positions: [1, 3], position_labels: ["Left", "", "Right"],
  },
  {
    name: "main-display", label: "Main Display", quorum: 1, assigned: 0, impact: "outage", from_standard: true,
    accepted_types: ["display"], pinned_products: ["samsung-qm55"], assigned_to: [], positions: [], position_labels: [],
  },
  {
    name: "conf-bar", label: "Conferencing Bar", quorum: 1, assigned: 1, impact: "outage", from_standard: true,
    accepted_types: ["video-bar"], pinned_products: [], assigned_to: ["videobar-1"], alternate: "conferencing/all-in-one",
    positions: [1], position_labels: [],
  },
  {
    name: "conf-codec", label: "conf-codec", quorum: 1, assigned: 0, impact: "outage", from_standard: true,
    accepted_types: ["codec"], pinned_products: [], assigned_to: [], alternate: "conferencing/component-system",
    positions: [], position_labels: [],
  },
] as unknown as EffectiveRole[];

const vm = () => systemZoomVM(health, declared, view, uuidFor("sz-sys"));

describe("systemZoomVM", () => {
  it("merges figures with declarations by role name, verbatim", () => {
    const mic = vm().unconditional.find((s) => s.name === "room-mic")!;
    expect(mic.satisfying).toBe(1);
    expect(mic.quorum).toBe(2);
    expect(mic.acceptedTypes).toEqual(["microphone", "video-bar"]);
    const display = vm().unconditional.find((s) => s.name === "main-display")!;
    expect(display.pinnedProducts).toEqual(["samsung-qm55"]);
  });

  it("tells a commissioning gap from a failure gap", () => {
    const mic = vm().unconditional.find((s) => s.name === "room-mic")!;
    const display = vm().unconditional.find((s) => s.name === "main-display")!;
    expect(mic.gap).toBe("occupant-down");
    expect(display.gap).toBe("unstaffed");
  });

  it("marks each occupant's own state and position label", () => {
    const mic = vm().unconditional.find((s) => s.name === "room-mic")!;
    expect(mic.occupants.map((o) => o.name)).toEqual(["videobar-1", "mic-1"]);
    expect(mic.occupants[1].down).toBe(true);
    expect(mic.occupants[0].down).toBe(false);
    // positions [1, 3] against labels ["Left", "", "Right"]: 1-based, gaps legal.
    expect(mic.occupants[0].positionLabel).toBe("Left");
    expect(mic.occupants[1].positionLabel).toBe("Right");
  });

  it("names the other system a shared occupant serves", () => {
    const bar = vm().choices[0].alternates.find((a) => a.name === "all-in-one")!.roles[0];
    expect(bar.occupants[0].sharedWith).toEqual(["Overflow Room"]);
    const mic = vm().unconditional.find((s) => s.name === "room-mic")!;
    expect(mic.occupants[1].sharedWith).toEqual([]);
  });

  it("groups by choice, marks the active alternate, and keeps the loser's figures out of the verdict story", () => {
    const conf = vm().choices.find((c) => c.name === "conferencing")!;
    expect(conf.activeAlternate).toBe("all-in-one");
    // The answering build leads whatever order the wire sent.
    expect(conf.alternates[0].name).toBe("all-in-one");
    const loser = conf.alternates.find((a) => a.name === "component-system")!;
    expect(loser.active).toBe(false);
    // The loser's roles ride along as the legal build they are, active false.
    expect(loser.roles[0].active).toBe(false);
  });

  it("puts a member filling no role in the no-role strip, no error", () => {
    expect(vm().noRole.map((m) => m.name)).toEqual(["device-1"]);
    expect(vm().noRole[0].sharedWith).toEqual([]);
  });
});
