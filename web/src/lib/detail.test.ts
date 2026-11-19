import { describe, expect, it } from "vitest";
import { componentCrumbs, contentsOf, landingFor, memberModel, slotsOf, systemCrumbs, systemsAtLocation } from "./detail";
import { type FleetView } from "./fleet";
import { uuidFor } from "./testids";

// The detail view's model (#872): systems are the unit Omniglass monitors,
// locations are folders and metadata, components are the pieces of systems. One
// view per system, its location as context; a location holding one system lands on
// it; the path names each location once.

const loc = (h: string, label: string, parent = "") => ({
  id: uuidFor(h), name: h, label, location_type: "room", location_type_id: uuidFor("t-room"), parent: parent ? uuidFor(parent) : undefined, verdict: "healthy",
});
const sys = (h: string, label: string, at: string) => ({ id: uuidFor(h), name: h, label, location: at ? uuidFor(at) : undefined, verdict: "healthy", dots: [] });

const view = {
  locations: [
    loc("campus", "East Campus"), loc("hall", "Innovation Hall", "campus"), loc("aud", "Auditorium", "hall"),
    loc("lab", "Media Lab", "hall"), loc("rack", "Rack Room", "hall"),
  ],
  systems: [
    sys("s-aud", "Auditorium", "aud"),
    sys("s-lab-1", "Media Lab", "lab"), sys("s-lab-2", "Media Lab Teaching 2", "lab"),
    sys("s-loose", "Signage", ""),
  ],
} as unknown as FleetView;

describe("where a location lands", () => {
  it("lands a location holding exactly one system on that system", () => {
    expect(landingFor(view, uuidFor("aud"))).toEqual({ kind: "system", id: uuidFor("s-aud") });
  });

  it("keeps a place holding two systems, or none, as a location", () => {
    expect(landingFor(view, uuidFor("lab"))).toEqual({ kind: "location" });
    expect(landingFor(view, uuidFor("hall"))).toEqual({ kind: "location" });
  });

  it("lists the systems at a location by label", () => {
    expect(systemsAtLocation(view, uuidFor("lab")).map((s) => s.label)).toEqual(["Media Lab", "Media Lab Teaching 2"]);
  });
});

describe("the path names each location once", () => {
  it("ends a sole system's path at its location's parent, since the view is titled by the location", () => {
    expect(systemCrumbs(view, uuidFor("s-aud")).map((c) => c.label)).toEqual(["East Campus", "Innovation Hall"]);
  });

  it("keeps a shared location in a system's path, since the location has a view of its own", () => {
    const crumbs = systemCrumbs(view, uuidFor("s-lab-2"));
    expect(crumbs.map((c) => c.label)).toEqual(["East Campus", "Innovation Hall", "Media Lab"]);
    expect(crumbs[2]).toMatchObject({ kind: "location", id: uuidFor("lab") });
  });

  it("gives an unplaced system no path", () => {
    expect(systemCrumbs(view, uuidFor("s-loose"))).toEqual([]);
  });

  it("names a component's one-system location once, as the system", () => {
    const crumbs = componentCrumbs(view, uuidFor("aud"), uuidFor("s-aud"));
    expect(crumbs.map((c) => c.label)).toEqual(["East Campus", "Innovation Hall", "Auditorium"]);
    expect(crumbs[2]).toMatchObject({ kind: "system", id: uuidFor("s-aud") });
  });

  // A system reads as its location, so appending it after a different location
  // read "Hall / Rack Room / Auditorium", as if the auditorium were inside
  // the rack room. The path is where the component sits; the systems it
  // serves are listed on its view.
  it("walks a component placed apart from its system to its own location, and stops there", () => {
    const crumbs = componentCrumbs(view, uuidFor("rack"), uuidFor("s-aud"));
    expect(crumbs.map((c) => `${c.kind}:${c.label}`)).toEqual([
      "location:East Campus", "location:Innovation Hall", "location:Rack Room",
    ]);
  });
});

describe("the slots a component fills", () => {
  const declared = [
    { name: "dsp", label: "DSP", assigned_to: ["dsp-1"] },
    { name: "amp", label: "Amplifier", assigned_to: ["amp-1", "dsp-1"] },
    { name: "cam", label: "", assigned_to: [] },
  ];
  it("names every slot the component staffs, by label", () => {
    expect(slotsOf("dsp-1", declared)).toEqual(["DSP", "Amplifier"]);
  });
  it("says nothing for a member filling no slot", () => {
    expect(slotsOf("panel-1", declared)).toEqual([]);
  });
});

describe("a system's components as rows", () => {
  const body = {
    cards: [
      { componentId: "c-bar", name: "videobar-1", down: false, shared: ["Overflow Room"], slots: [{ label: "Conferencing Bar" }], noSlot: false },
      { componentId: "c-pow", name: "device-1", down: false, shared: [], slots: [], noSlot: true },
    ],
    groups: [
      { name: "mic", label: "Room Microphone", quorum: 2, satisfying: 1, short: 1, spare: 1, impact: "degraded", members: ["mic-1"], memberCards: [
        { componentId: "c-mic", name: "mic-1", down: true, shared: [], slots: [{ label: "Room Microphone", position: "Left" }], noSlot: false },
        { componentId: "c-bar2", name: "bar-2", down: false, shared: [], slots: [{ label: "Room Microphone" }, { label: "Conferencing Bar" }], noSlot: false },
      ] },
      { name: "disp", label: "Main Display", quorum: 1, satisfying: 0, short: 1, spare: 0, impact: "outage", members: [], memberCards: [] },
    ],
  };
  const ctx = {
    verdict: (id: string) => (id === "c-mic" ? "outage" as const : "healthy" as const),
    alarm: (id: string) => (id === "c-mic" ? "No route to host" : undefined),
    product: (id: string) => ({ "c-bar": "Lyra Bar 80" } as Record<string, string>)[id] ?? "",
    label: (id: string, name: string) => (id === "c-bar" ? "Video Bar" : name),
    icon: () => "box",
  };

  it("names each ungrouped member's slot, product and the systems it also serves", () => {
    const m = memberModel(body, ctx);
    expect(m.rows.map((r) => [r.label, r.slot, r.product, r.also.join(",")])).toEqual([
      ["Video Bar", "Conferencing Bar", "Lyra Bar 80", "Overflow Room"],
      ["device-1", "", "", ""],
    ]);
    expect(m.rows[1].noSlot).toBe(true);
  });

  // A staffed seat whose occupant is down is a failure the alarms explain,
  // not an empty slot: the live console drew "empty slot" under a slot whose
  // one device had dropped off the network.
  it("draws an empty slot only for a seat nobody staffs, never for a down occupant", () => {
    const m = memberModel({ cards: [], groups: [
      { name: "dsp", label: "DSP", quorum: 1, satisfying: 0, short: 1, spare: 0, impact: "degraded", members: ["dsp-1"], memberCards: [
        { componentId: "c-dsp", name: "dsp-1", down: true, shared: [], slots: [{ label: "DSP" }], noSlot: false },
      ] },
    ] }, ctx);
    expect(m.groups[0].empty).toBe(0);
    expect(m.groups[0].tone).toBe("degraded");
  });

  it("groups a slot only where it says something a column cannot, with its arithmetic and its gap", () => {
    const m = memberModel(body, ctx);
    expect(m.groups.map((g) => [g.label, g.arithmetic, g.tone, g.empty])).toEqual([
      ["Room Microphone", "1 of 2, 1 spare", "degraded", 1],
      ["Main Display", "0 of 1", "incomplete", 1],
    ]);
    expect(m.groups[0].members[0]).toMatchObject({ label: "mic-1", slot: "Left", health: "outage", alarm: "No route to host" });
    // A grouped member keeps the other slots it fills: its one home is the
    // group, so the column names what the group's header does not.
    expect(m.groups[0].members[1].slot).toBe("Conferencing Bar");
  });
});

describe("what a location holds", () => {
  const types = [{ name: "room", label: "Room" }, { name: "floor", label: "Floor" }];
  it("counts the locations directly beneath it by their type's label, as the outline does", () => {
    expect(contentsOf(view, uuidFor("hall"), types)).toBe("3 rooms");
  });
  it("says Empty for a location holding nothing beneath it", () => {
    expect(contentsOf(view, uuidFor("aud"), types)).toBe("Empty");
  });
});
