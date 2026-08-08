import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import RolesPanel from "./RolesPanel";
import { systemRolesKey, type EffectiveRole } from "../lib/system_roles";
import { COMPONENTS_KEY, type Component as Comp } from "../lib/components";
import { SYSTEMS_KEY, type System } from "../lib/systems";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";
import { systemHealthKey, type EstateHealth } from "../lib/health";

// The panel resolves a system's roles: what its standard declares plus what the
// system declares of its own, each with the typed-slot guard (#626) it enforces
// at assignment, its quorum, and who fills it. Rows are seeded into the query
// cache so no server is needed; the assign / unassign writes are faked where a
// test drives one.
const roles: EffectiveRole[] = [
  // Inherited and short a component: the standard wants two, one is in place.
  {
    name: "table-mic",
    display_name: "Table microphone",
    quorum: 2,
    impact: "degraded",
    accepted_types: ["video-bar"],
    pinned_products: [],
    position_labels: [],
    from_standard: true,
    assigned_to: ["mic-1"],
    assigned: 1,
    understaffed: 1,
  },
  // Inherited and staffed: no marker, nothing wanted.
  {
    name: "main-display",
    display_name: "Main display",
    quorum: 1,
    impact: "outage",
    accepted_types: ["display"],
    pinned_products: [],
    position_labels: [],
    from_standard: true,
    assigned_to: ["disp-1"],
    assigned: 1,
    understaffed: 0,
  },
  // Declared on this system, not by its standard.
  {
    name: "spare-panel",
    display_name: "Spare panel",
    quorum: 1,
    impact: "none",
    accepted_types: ["touch-panel"],
    pinned_products: [],
    position_labels: [],
    from_standard: false,
    assigned_to: [],
    assigned: 0,
    understaffed: 1,
  },
];

const system: System = { id: uuidFor("s-1"), name: "boardroom", display_name: "Boardroom", member_count: 3 };
const components: Comp[] = [
  { id: uuidFor("c-1"), name: "mic-1", display_name: "Ceiling Mic 1", system: "boardroom", system_count: 1 },
  { id: uuidFor("c-2"), name: "panel-1", display_name: "Touch Panel 1", system: "boardroom", system_count: 1 },
  { id: uuidFor("c-3"), name: "disp-1", display_name: "Display 1", system: "boardroom", system_count: 1 },
];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };

// The health read's own arithmetic for the same three roles above, the
// occupancy-aware counterpart of the roles read's understaffed: satisfying,
// short, and spare replace assigned/understaffed once health has loaded (#626
// Task 9). table-mic is short one occupant beyond its assignment count would
// suggest, which is the case understaffed alone cannot express.
const health: EstateHealth = {
  owner_kind: "system",
  owner: "boardroom",
  verdict: "degraded",
  transitions: [],
  systems: [],
  roles: [
    {
      name: "table-mic", display_name: "Table microphone", impact: "degraded",
      quorum: 2, satisfying: 1, short: 1, spare: 0, impaired: true, active: true,
      assigned_to: ["mic-1"], down: [], alarms: [],
    },
    {
      name: "main-display", display_name: "Main display", impact: "outage",
      quorum: 1, satisfying: 1, short: 0, spare: 0, impaired: false, active: true,
      assigned_to: ["disp-1"], down: [], alarms: [],
    },
    {
      name: "spare-panel", display_name: "Spare panel", impact: "none",
      quorum: 1, satisfying: 0, short: 1, spare: 0, impaired: true, active: true,
      assigned_to: [], down: [], alarms: [],
    },
  ],
};

function json(body: unknown, status = 200, type = "application/json") {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": type } });
}

function mount(opts: { rows?: EffectiveRole[]; canUpdate?: boolean; health?: EstateHealth | null } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...systemRolesKey("boardroom")], opts.rows ?? roles);
  qc.setQueryData([...COMPONENTS_KEY], components);
  qc.setQueryData([...SYSTEMS_KEY], [system]);
  qc.setQueryData([...ME_KEY], owner);
  if (opts.health !== null) qc.setQueryData([...systemHealthKey("boardroom")], opts.health ?? health);
  return render(() => (
    <QueryClientProvider client={qc}>
      <RolesPanel system="boardroom" canUpdate={opts.canUpdate ?? true} />
    </QueryClientProvider>
  ));
}

// A role's row is the block its display name sits in.
const roleRow = (label: HTMLElement) => label.closest("div.flex-col") as HTMLElement;

describe("RolesPanel", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows a role's typed-slot guard, its staffing, and who fills it", () => {
    const { getByText } = mount();
    const row = roleRow(getByText("Table microphone"));
    expect(within(row).getByText("table-mic")).toBeTruthy(); // the address, beside the label
    expect(within(row).getByText("video-bar")).toBeTruthy();
    // The health read's arithmetic (1 of 2 satisfying), not the roles read's
    // assignment count: see the divergence test below for why they can differ.
    expect(within(row).getByText("1 of 2 satisfying")).toBeTruthy();
    expect(within(row).getByText("mic-1")).toBeTruthy();
  });

  it("marks a short role from the health read, and leaves a satisfied one unmarked", () => {
    const { getByText } = mount();
    expect(within(roleRow(getByText("Table microphone"))).getByText("short 1")).toBeTruthy();
    const staffed = roleRow(getByText("Main display"));
    expect(within(staffed).queryByText(/short/)).toBeNull();
    expect(within(staffed).getByText("1 of 1 satisfying")).toBeTruthy();
  });

  it("falls back to the roles read's assignment arithmetic while health has not loaded yet", () => {
    const { getByText } = mount({ health: null });
    // No health seeded: the lens must not render undefined, so it falls back
    // to the health-blind figures the roles read already carries.
    expect(within(roleRow(getByText("Table microphone"))).getByText("understaffed")).toBeTruthy();
    expect(within(roleRow(getByText("Table microphone"))).getByText("2 wanted, 1 assigned")).toBeTruthy();
    const staffed = roleRow(getByText("Main display"));
    expect(within(staffed).queryByText("understaffed")).toBeNull();
    expect(within(staffed).getByText("1 wanted, 1 assigned")).toBeTruthy();
  });

  it("reads short from health, not assignment count: a down occupant still counts as assigned", () => {
    // main-display's sole assignee carries a critical alarm: the roles read
    // would still call it fully staffed (understaffed 0), but health's
    // satisfying/short is occupancy-aware and must win on screen.
    const degradedMainDisplay: EstateHealth = {
      ...health,
      roles: health.roles!.map((r) =>
        r.name === "main-display"
          ? { ...r, satisfying: 0, short: 1, impaired: true, down: ["disp-1"], alarms: [{ id: "a-1", severity: "critical", message: "HDMI failed", component: "disp-1", raised_at: new Date().toISOString() }] }
          : r,
      ),
    };
    const { getByText } = mount({ health: degradedMainDisplay });
    const row = roleRow(getByText("Main display"));
    expect(within(row).getByText("short 1")).toBeTruthy();
    expect(within(row).getByText("0 of 1 satisfying")).toBeTruthy();
    // The occupant is marked down, not silently dropped from "filled by".
    const downBadge = within(row).getByTitle("An active alarm has taken this component down");
    expect(downBadge.textContent).toContain("disp-1");
    expect(downBadge.textContent).toContain("down");
  });

  it("groups a role declared on the system apart from the ones its standard declares", () => {
    const { getByRole, getByText } = mount();
    const adhoc = getByRole("group", { name: /ad hoc/i });
    expect(within(adhoc).getByText("Spare panel")).toBeTruthy();
    expect(within(adhoc).queryByText("Table microphone")).toBeNull(); // inherited rows stay above
    expect(getByText("declared on this system, not by its standard")).toBeTruthy();
  });

  it("says a role nobody fills is unfilled rather than showing an empty list", () => {
    const { getByText } = mount();
    expect(within(roleRow(getByText("Spare panel"))).getByText("nobody yet")).toBeTruthy();
  });

  it("assigns the picked component to the role", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") { put = req.clone(); return new Response(null, { status: 204 }); }
      return json({ system: "boardroom", roles });
    });

    const { getByText, getByLabelText } = mount();
    // The component already filling table-mic itself is not offered again;
    // disp-1 (main-display's occupant) still appears, just disabled (see the
    // dedicated test below), so it stays out of this equality's true values.
    const picker = getByLabelText("Component to fill table-mic") as HTMLSelectElement;
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "disp-1", "panel-1"]);

    fireEvent.change(picker, { target: { value: "panel-1" } });
    fireEvent.click(getByLabelText("Assign to table-mic"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/systems/boardroom/roles/table-mic/assignments/panel-1");
    expect(getByText("Table microphone")).toBeTruthy(); // the panel stays put
  });

  // #626: a component fills at most one role per system. Once a picker lets
  // an operator pick a component already staffing a DIFFERENT role here, the
  // server's 422 refusal is one the operator could not have anticipated,
  // which the panel's own stated principle (the refusal teaches) forbids.
  // Excluding it silently would be worse: showing it disabled, with the
  // reason, is what the panel already does for the typed-slot guard.
  it("disables a component already staffing a different role here, naming which one", () => {
    const { getByLabelText } = mount();
    const picker = getByLabelText("Component to fill table-mic") as HTMLSelectElement;
    const disp1 = Array.from(picker.options).find((o) => o.value === "disp-1")!;
    expect(disp1.disabled).toBe(true);
    expect(disp1.textContent).toContain("already fills");
    expect(disp1.textContent).toContain("Main display");
    // panel-1 fills nothing anywhere in this system, so it stays selectable.
    const panel1 = Array.from(picker.options).find((o) => o.value === "panel-1")!;
    expect(panel1.disabled).toBe(false);
  });

  // The refusal is the lesson: a component may fill a role only if its product's
  // type falls within an accepted type, and the server's 422 names both parties.
  // The panel must show that message, not a generic failure, or the operator
  // learns nothing about why the assignment did not take.
  it("surfaces the server's refusal, naming both parties", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        return json(
          {
            title: "Unprocessable Entity",
            status: 422,
            detail: 'component "panel-1" is a touch-panel; role "table-mic" wants a video-bar',
          },
          422,
          "application/problem+json",
        );
      }
      return json({ system: "boardroom", roles });
    });

    const { getByText, getByLabelText, queryByText } = mount();
    fireEvent.change(getByLabelText("Component to fill table-mic"), { target: { value: "panel-1" } });
    fireEvent.click(getByLabelText("Assign to table-mic"));

    // The refusal belongs to the role that refused, and reads as the server sent it.
    const row = roleRow(getByText("Table microphone"));
    const alert = await waitFor(() => within(row).getByRole("alert"));
    expect(alert.textContent).toBe('component "panel-1" is a touch-panel; role "table-mic" wants a video-bar');
    expect(queryByText("The operation failed.")).toBeNull(); // never swallowed into a generic line
    expect(within(roleRow(getByText("Main display"))).queryByRole("alert")).toBeNull();
  });

  it("unassigns a component from the role", async () => {
    let del: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "DELETE") { del = req.clone(); return new Response(null, { status: 204 }); }
      return json({ system: "boardroom", roles });
    });

    const { getByLabelText } = mount();
    fireEvent.click(getByLabelText("Unassign mic-1 from table-mic"));

    await waitFor(() => expect(del).toBeTruthy());
    expect(del!.url).toContain("/systems/boardroom/roles/table-mic/assignments/mic-1");
  });

  // Drag-to-reorder, gated on TestAssignedToIsPositionOrdered (Go, green):
  // reuses moveItem's shape (listmodel.ts) via swapPath, but the wire call is
  // the server's actual primitive, a single pairwise swap.
  it("drag-reorders two occupants via the position-swap route", async () => {
    const twoOccupants: EffectiveRole[] = [{ ...roles[0], assigned_to: ["mic-1", "panel-1"] }, roles[1], roles[2]];
    const swaps: Request[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.includes(":swapPositions")) {
        swaps.push(req.clone());
        return new Response(null, { status: 204 });
      }
      return json({ system: "boardroom", roles: twoOccupants });
    });

    const { getByText } = mount({ rows: twoOccupants });
    const from = getByText("mic-1").closest("span.badge") as HTMLElement;
    const to = getByText("panel-1").closest("span.badge") as HTMLElement;

    fireEvent.dragStart(from);
    fireEvent.dragOver(to);
    fireEvent.drop(to);

    await waitFor(() => expect(swaps.length).toBe(1));
    expect(swaps[0].url).toContain("/systems/boardroom/roles/table-mic:swapPositions");
    expect(await swaps[0].json()).toEqual({ position: 1, with: 2 });
  });

  it("does not wire dragging when there is only one occupant to reorder", () => {
    const { getByText } = mount(); // default fixture: table-mic has one occupant
    const badge = getByText("mic-1").closest("span.badge") as HTMLElement;
    expect(badge.draggable).toBe(false);
  });

  it("shows no assign or unassign control when the caller cannot update the system", () => {
    const { getByText, queryByLabelText } = mount({ canUpdate: false });
    expect(getByText("Table microphone")).toBeTruthy(); // the read still renders
    expect(queryByLabelText("Component to fill table-mic")).toBeNull();
    expect(queryByLabelText("Unassign mic-1 from table-mic")).toBeNull();
  });

  it("explains what a role is when the system has none", () => {
    const { getByText } = mount({ rows: [] });
    expect(getByText(/a slot this system needs filled/i)).toBeTruthy();
  });
});
