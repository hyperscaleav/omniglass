import { describe, expect, it } from "vitest";
import { alarmRows, componentCards, sinceOf, systemZoomVM } from "./system_zoom";
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
      impaired: true, active: true, assigned_to: ["videobar-1", "mic-1"], down: ["mic-1"],
      alarms: [
        { id: "al-1", component: uuidFor("sz-c-mic"), severity: "critical", message: "No route to host", raised_at: "2026-08-15T14:20:00Z" },
        { id: "al-2", component: uuidFor("sz-c-mic"), severity: "warning", message: "Fan speed high", raised_at: "2026-08-15T13:00:00Z" },
      ],
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
      impaired: true, active: false, assigned_to: [], down: [],
      alarms: [{ id: "al-ghost", component: uuidFor("sz-c-ghost"), severity: "critical", message: "From the build not in use", raised_at: "2026-08-10T00:00:00Z" }],
      choice: "conferencing", alternate: "component-system",
    },
  ],
  systems: [],
  transitions: [
    { ts: "2026-08-01T09:00:00Z", verdict: "healthy" },
    { ts: "2026-08-15T14:20:00Z", verdict: "degraded" },
  ],
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
    // The id rides beside the name so the page can open the leaf: names repeat
    // across rooms, ids do not.
    expect(mic.occupants.map((o) => o.componentId)).toEqual([uuidFor("sz-c-bar"), uuidFor("sz-c-mic")]);
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

  it("carries only the build in use per choice: the alternate a room did not choose is the editor's business", () => {
    const conf = vm().choices.find((c) => c.name === "conferencing")!;
    expect(conf.activeAlternate).toBe("all-in-one");
    expect(conf.alternates.map((a) => a.name)).toEqual(["all-in-one"]);
    expect(conf.alternates[0].roles.map((r) => r.name)).toEqual(["conf-bar"]);
    // The losing build's role is nowhere on the zoom.
    const all = [...vm().unconditional, ...vm().choices.flatMap((c) => c.alternates.flatMap((a) => a.roles))];
    expect(all.find((r) => r.name === "conf-codec")).toBeUndefined();
  });

  it("puts a member filling no role in the no-role strip, no error", () => {
    expect(vm().noRole.map((m) => m.name)).toEqual(["device-1"]);
    expect(vm().noRole[0].sharedWith).toEqual([]);
  });
});

describe("alarmRows", () => {
  const now = Date.parse("2026-08-15T17:00:00Z");
  it("flattens the active roles' alarms worst-first, joined to the role and the component's id", () => {
    const rows = alarmRows(health, view, uuidFor("sz-sys"));
    expect(rows.map((r) => r.severity)).toEqual(["critical", "warning"]);
    expect(rows[0]).toMatchObject({
      message: "No route to host",
      component: "mic-1",
      componentId: uuidFor("sz-c-mic"),
      roleLabel: "Room Microphone",
    });
  });

  it("never carries an alarm from a build not in use: an inactive role's alarms did not move the verdict", () => {
    const rows = alarmRows(health, view, uuidFor("sz-sys"));
    expect(rows.find((r) => r.message === "From the build not in use")).toBeUndefined();
  });

  it("renders one row per alarm even when one down component staffs two active roles (the wire repeats per role)", () => {
    const doubled = {
      ...health,
      roles: [
        ...(health.roles ?? []),
        {
          name: "spare-mic", label: "Spare Microphone", impact: "none", quorum: 1, satisfying: 0, short: 1, spare: 0,
          impaired: true, active: true, assigned_to: ["mic-1"], down: ["mic-1"],
          alarms: [{ id: "al-1", component: uuidFor("sz-c-mic"), severity: "critical", message: "No route to host", raised_at: "2026-08-15T14:20:00Z" }],
        },
      ],
    } as never;
    const rows = alarmRows(doubled, view, uuidFor("sz-sys"));
    expect(rows.filter((r) => r.id === "al-1")).toHaveLength(1);
  });

  it("is empty for a healthy report", () => {
    expect(alarmRows({ ...health, roles: [] } as never, view, uuidFor("sz-sys"))).toEqual([]);
  });
  void now;
});

describe("sinceOf", () => {
  it("names the last recorded edge and its age", () => {
    const now = Date.parse("2026-08-15T17:00:00Z");
    const s = sinceOf(health, now)!;
    expect(s.ts).toBe("2026-08-15T14:20:00Z");
    expect(s.ms).toBe(now - Date.parse("2026-08-15T14:20:00Z"));
  });

  it("is null when nothing was ever recorded", () => {
    expect(sinceOf({ ...health, transitions: [] } as never, Date.now())).toBeNull();
  });
});

// The components-first body (#790): a system shows the things in it, one card
// per component with a role BADGE; role-level grouping only where it earns it.
describe("componentCards", () => {
  const body = () => componentCards(systemZoomVM(health, declared, view, uuidFor("sz-sys")));

  it("gives every component one card with role badges; a grouped component's one home is its group", () => {
    const { cards, groups } = body();
    // The bar staffs the grouped mic role AND answers the choice: it renders
    // inside the group, wearing both badges, and never twice.
    expect(cards.find((c) => c.name === "videobar-1")).toBeUndefined();
    const bar = groups.find((g) => g.label === "Room Microphone")!.memberCards.find((c) => c.name === "videobar-1")!;
    expect(bar.roles.map((r) => r.label).sort()).toEqual(["Conferencing Bar", "Room Microphone"]);
    expect(bar.roles.find((r) => r.label === "Room Microphone")!.position).toBe("Left");
    expect(bar.componentId).toBe(uuidFor("sz-c-bar"));
    const power = cards.find((c) => c.name === "device-1")!;
    expect(power.roles).toEqual([]);
    expect(power.noRole).toBe(true);
  });

  it("groups only where grouping says something: a quorum>1 or short role, never the 1:1 case", () => {
    const groups = body().groups;
    // room-mic: quorum 2, short 1 -> grouped with its arithmetic.
    const mic = groups.find((g) => g.label === "Room Microphone")!;
    expect(mic.quorum).toBe(2);
    expect(mic.short).toBe(1);
    expect(mic.members).toEqual(["videobar-1", "mic-1"]);
    // conf-bar: quorum 1, whole -> its occupant renders as a plain card, no group.
    expect(groups.find((g) => g.label === "Conferencing Bar")).toBeUndefined();
  });

  it("an unstaffed role is an empty group wearing the role badge, never a card", () => {
    const display = body().groups.find((g) => g.label === "Main Display")!;
    expect(display.members).toEqual([]);
    expect(display.short).toBe(1);
    expect(body().cards.some((c) => c.roles.some((r) => r.label === "Main Display"))).toBe(false);
  });

  it("never surfaces a role from the build not in use", () => {
    const all = [...body().cards.flatMap((c) => c.roles.map((r) => r.label)), ...body().groups.map((g) => g.label)];
    expect(all).not.toContain("conf-codec");
  });
});
