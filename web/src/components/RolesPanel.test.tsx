import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import RolesPanel from "./RolesPanel";
import { systemRolesKey, type EffectiveRole } from "../lib/system_roles";
import { COMPONENTS_KEY, type Component as Comp } from "../lib/components";
import { SYSTEMS_KEY, type System } from "../lib/systems";
import { ME_KEY, type Me } from "../lib/auth";

// The panel resolves a system's roles: what its standard declares plus what the
// system declares of its own, each with the capabilities it requires, its quorum,
// and who fills it. Rows are seeded into the query cache so no server is needed;
// the assign / unassign writes are faked where a test drives one.
const roles: EffectiveRole[] = [
  // Inherited and short a component: the standard wants two, one is in place.
  {
    name: "table-mic",
    display_name: "Table microphone",
    quorum: 2,
    impact: "degraded",
    capabilities: ["microphone", "speaker"],
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
    capabilities: ["display"],
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
    capabilities: ["touch-panel"],
    from_standard: false,
    assigned_to: [],
    assigned: 0,
    understaffed: 1,
  },
];

const system: System = { id: "s-1", name: "boardroom", display_name: "Boardroom", member_count: 3 };
const components: Comp[] = [
  { id: "c-1", name: "mic-1", display_name: "Ceiling Mic 1", system: "boardroom", system_count: 1 },
  { id: "c-2", name: "panel-1", display_name: "Touch Panel 1", system: "boardroom", system_count: 1 },
  { id: "c-3", name: "disp-1", display_name: "Display 1", system: "boardroom", system_count: 1 },
];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };

function json(body: unknown, status = 200, type = "application/json") {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": type } });
}

function mount(opts: { rows?: EffectiveRole[]; canUpdate?: boolean } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...systemRolesKey("boardroom")], opts.rows ?? roles);
  qc.setQueryData([...COMPONENTS_KEY], components);
  qc.setQueryData([...SYSTEMS_KEY], [system]);
  qc.setQueryData([...ME_KEY], owner);
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

  it("shows a role's required capabilities, its staffing, and who fills it", () => {
    const { getByText } = mount();
    const row = roleRow(getByText("Table microphone"));
    expect(within(row).getByText("table-mic")).toBeTruthy(); // the address, beside the label
    expect(within(row).getByText("microphone")).toBeTruthy();
    expect(within(row).getByText("speaker")).toBeTruthy();
    expect(within(row).getByText("2 wanted, 1 assigned")).toBeTruthy();
    expect(within(row).getByText("mic-1")).toBeTruthy();
  });

  it("marks an understaffed role, and leaves a staffed one unmarked", () => {
    const { getByText } = mount();
    expect(within(roleRow(getByText("Table microphone"))).getByText("understaffed")).toBeTruthy();
    const staffed = roleRow(getByText("Main display"));
    expect(within(staffed).queryByText("understaffed")).toBeNull();
    expect(within(staffed).getByText("1 wanted, 1 assigned")).toBeTruthy();
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
    // The components already filling the role are not offered again.
    const picker = getByLabelText("Component to fill table-mic") as HTMLSelectElement;
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "disp-1", "panel-1"]);

    fireEvent.change(picker, { target: { value: "panel-1" } });
    fireEvent.click(getByLabelText("Assign to table-mic"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/systems/boardroom/roles/table-mic/assignments/panel-1");
    expect(getByText("Table microphone")).toBeTruthy(); // the panel stays put
  });

  // The refusal is the lesson: a component may fill a role only if it provides
  // every capability the role requires, and the server's 422 names the gap. The
  // panel must show that message, not a generic failure, or the operator learns
  // nothing about why the assignment did not take.
  it("surfaces the server's refusal, naming the capabilities the component is missing", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        return json(
          {
            title: "Unprocessable Entity",
            status: 422,
            detail: 'component "panel-1" cannot fill role "table-mic": missing microphone, speaker',
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
    expect(alert.textContent).toBe('component "panel-1" cannot fill role "table-mic": missing microphone, speaker');
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
