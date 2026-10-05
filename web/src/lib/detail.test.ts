import { describe, expect, it } from "vitest";
import { componentCrumbs, landingFor, memberModel, rolesOf, systemCrumbs, systemsAtPlace } from "./detail";
import { type FleetView } from "./fleet";
import { uuidFor } from "./testids";

// The detail view's model (#872): systems are the unit Omniglass monitors,
// places are folders and metadata, components are the pieces of systems. One
// view per system, its place as context; a place holding one system lands on
// it; the path names each place once.

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
    sys("s-lab-1", "Media Lab", "lab"), sys("s-lab-2", "Media Lab Classroom 2", "lab"),
    sys("s-loose", "Signage", ""),
  ],
} as unknown as FleetView;

describe("where a place lands", () => {
  it("lands a place holding exactly one system on that system", () => {
    expect(landingFor(view, uuidFor("aud"))).toEqual({ kind: "system", id: uuidFor("s-aud") });
  });

  it("keeps a place holding two systems, or none, as a place", () => {
    expect(landingFor(view, uuidFor("lab"))).toEqual({ kind: "place" });
    expect(landingFor(view, uuidFor("hall"))).toEqual({ kind: "place" });
  });

  it("lists the systems at a place by label", () => {
    expect(systemsAtPlace(view, uuidFor("lab")).map((s) => s.label)).toEqual(["Media Lab", "Media Lab Classroom 2"]);
  });
});

describe("the path names each place once", () => {
  it("ends a sole system's path at its place's parent, since the view is titled by the place", () => {
    expect(systemCrumbs(view, uuidFor("s-aud")).map((c) => c.label)).toEqual(["East Campus", "Innovation Hall"]);
  });

  it("keeps a shared place in a system's path, since the place has a view of its own", () => {
    const crumbs = systemCrumbs(view, uuidFor("s-lab-2"));
    expect(crumbs.map((c) => c.label)).toEqual(["East Campus", "Innovation Hall", "Media Lab"]);
    expect(crumbs[2]).toMatchObject({ kind: "location", id: uuidFor("lab") });
  });

  it("gives an unplaced system no path", () => {
    expect(systemCrumbs(view, uuidFor("s-loose"))).toEqual([]);
  });

  it("names a component's one-system place once, as the system", () => {
    const crumbs = componentCrumbs(view, uuidFor("aud"), uuidFor("s-aud"));
    expect(crumbs.map((c) => c.label)).toEqual(["East Campus", "Innovation Hall", "Auditorium"]);
    expect(crumbs[2]).toMatchObject({ kind: "system", id: uuidFor("s-aud") });
  });

  it("walks a component placed apart from its system to its own place, then names the system", () => {
    const crumbs = componentCrumbs(view, uuidFor("rack"), uuidFor("s-aud"));
    expect(crumbs.map((c) => `${c.kind}:${c.label}`)).toEqual([
      "location:East Campus", "location:Innovation Hall", "location:Rack Room", "system:Auditorium",
    ]);
  });
});

describe("the roles a component fills", () => {
  const declared = [
    { name: "dsp", label: "DSP", assigned_to: ["dsp-1"] },
    { name: "amp", label: "Amplifier", assigned_to: ["amp-1", "dsp-1"] },
    { name: "cam", label: "", assigned_to: [] },
  ];
  it("names every role the component staffs, by label", () => {
    expect(rolesOf("dsp-1", declared)).toEqual(["DSP", "Amplifier"]);
  });
  it("says nothing for a member filling no role", () => {
    expect(rolesOf("panel-1", declared)).toEqual([]);
  });
});

describe("a system's components as rows", () => {
  const body = {
    cards: [
      { componentId: "c-bar", name: "videobar-1", down: false, shared: ["Overflow Room"], roles: [{ label: "Conferencing Bar" }], noRole: false },
      { componentId: "c-pow", name: "device-1", down: false, shared: [], roles: [], noRole: true },
    ],
    groups: [
      { name: "mic", label: "Room Microphone", quorum: 2, satisfying: 1, short: 1, spare: 1, impact: "degraded", members: ["mic-1"], memberCards: [
        { componentId: "c-mic", name: "mic-1", down: true, shared: [], roles: [{ label: "Room Microphone", position: "Left" }], noRole: false },
        { componentId: "c-bar2", name: "bar-2", down: false, shared: [], roles: [{ label: "Room Microphone" }, { label: "Conferencing Bar" }], noRole: false },
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

  it("names each ungrouped member's role, product and the systems it also serves", () => {
    const m = memberModel(body, ctx);
    expect(m.rows.map((r) => [r.label, r.role, r.product, r.also.join(",")])).toEqual([
      ["Video Bar", "Conferencing Bar", "Lyra Bar 80", "Overflow Room"],
      ["device-1", "", "", ""],
    ]);
    expect(m.rows[1].noRole).toBe(true);
  });

  it("groups a role only where it says something a column cannot, with its arithmetic and its gap", () => {
    const m = memberModel(body, ctx);
    expect(m.groups.map((g) => [g.label, g.arithmetic, g.tone, g.empty])).toEqual([
      ["Room Microphone", "1 of 2, 1 spare", "degraded", 1],
      ["Main Display", "0 of 1", "incomplete", 1],
    ]);
    expect(m.groups[0].members[0]).toMatchObject({ label: "mic-1", role: "Left", health: "outage", alarm: "No route to host" });
    // A grouped member keeps the other roles it fills: its one home is the
    // group, so the column names what the group's header does not.
    expect(m.groups[0].members[1].role).toBe("Conferencing Bar");
  });
});
