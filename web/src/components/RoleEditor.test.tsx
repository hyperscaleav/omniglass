import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import RoleEditor from "./RoleEditor";
import { standardRolesKey, type DeclaredRole } from "../lib/system_roles";
import { CAPABILITIES_KEY, type Capability } from "../lib/capabilities";
import { ME_KEY, type Me } from "../lib/auth";
import { BladeEditContext, createEditSlot } from "../lib/blades";
import { uuidFor } from "../lib/testids";

// The editor curates the roles a standard declares: the slots every conforming
// system needs filled, each with the capabilities a component must provide and how
// many components the slot wants. Data is seeded into the query cache so no server
// is needed; the PUT / DELETE fetches are faked where a test drives them. The
// mount hosts the editor inside a blade edit slot, as the standard blade does:
// the controls are edit-state affordances (#621), so the default mount enters
// edit mode; `editing: false` witnesses the read state.
const declared: DeclaredRole[] = [
  { name: "table-mic", display_name: "Table microphone", quorum: 2, capabilities: ["microphone"], impact: "degraded" },
  { name: "main-display", display_name: "Main display", quorum: 1, capabilities: ["display", "hdmi-in"], impact: "outage" },
];

const catalog: Capability[] = [
  { id: uuidFor("microphone"), name: "microphone", display_name: "Microphone", official: true },
  { id: uuidFor("speaker"), name: "speaker", display_name: "Speaker", official: true },
  { id: uuidFor("display"), name: "display", display_name: "Display", official: true },
  { id: uuidFor("hdmi-in"), name: "hdmi-in", display_name: "HDMI input", official: true },
];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };
// A reader of everything: it can see the declarations but never write one.
const reader: Me = { principal: { id: "r", kind: "human" }, permissions: ["*:read"], grants: [] };

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(opts: { me?: Me; official?: boolean; rows?: DeclaredRole[]; editing?: boolean } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...standardRolesKey("meeting-room")], opts.rows ?? declared);
  qc.setQueryData([...CAPABILITIES_KEY], catalog);
  qc.setQueryData([...ME_KEY], opts.me ?? owner);
  const edit = createEditSlot();
  if (opts.editing ?? true) edit.begin();
  const result = render(() => (
    <QueryClientProvider client={qc}>
      <BladeEditContext.Provider value={edit}>
        <RoleEditor id="meeting-room" official={opts.official ?? false} />
      </BladeEditContext.Provider>
    </QueryClientProvider>
  ));
  return { ...result, edit };
}

const roleRow = (name: HTMLElement) => name.closest("div.flex-col") as HTMLElement;

describe("RoleEditor on a standard", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists each declared role with its quorum and the capabilities it requires", () => {
    const { getByText } = mount();
    expect(getByText("Declared roles")).toBeTruthy();
    expect(getByText("the standard's roles")).toBeTruthy();
    const row = roleRow(getByText("table-mic"));
    expect(within(row).getByText("Table microphone")).toBeTruthy();
    expect(within(row).getByText("2 wanted")).toBeTruthy();
    expect(within(row).getByText("microphone")).toBeTruthy();
  });

  it("declares a role, PUTting its name, label, quorum, and required capabilities", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        put = req.clone();
        return json({ name: "camera", display_name: "Room camera", quorum: 1, capabilities: ["speaker"] });
      }
      return json({ roles: declared });
    });

    const { getByLabelText } = mount();
    // The rest of the form appears only once the role is named: the name is the
    // address, and it is invented here, not picked from a catalog.
    fireEvent.input(getByLabelText("Role name"), { target: { value: "camera" } });
    fireEvent.input(getByLabelText("Name for the new role"), { target: { value: "Room camera" } });
    fireEvent.input(getByLabelText("Quorum for the new role"), { target: { value: "3" } });
    fireEvent.change(getByLabelText("Capability to require"), { target: { value: "speaker" } });
    fireEvent.click(getByLabelText("Declare role"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/standards/meeting-room/roles/camera");
    expect(await put!.json()).toEqual({ quorum: 3, display_name: "Room camera", capabilities: ["speaker"] });
  });

  it("edits a role in place, replacing the required set wholesale", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        put = req.clone();
        return json({ name: "main-display", display_name: "Main display", quorum: 2, capabilities: ["display"] });
      }
      return json({ roles: declared });
    });

    const { getByLabelText } = mount();
    fireEvent.click(getByLabelText("Edit main-display"));
    const quorum = getByLabelText("Quorum for main-display") as HTMLInputElement;
    expect(quorum.value).toBe("1"); // seeded from the declaration
    fireEvent.input(quorum, { target: { value: "2" } });
    // Drop one of the two requirements: the write carries the whole set, not a delta.
    fireEvent.click(getByLabelText("Stop requiring hdmi-in"));
    fireEvent.click(getByLabelText("Save main-display"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/standards/meeting-room/roles/main-display");
    expect(await put!.json()).toEqual({ quorum: 2, display_name: "Main display", capabilities: ["display"] });
  });

  it("refuses a quorum that is not a whole number of components, before writing", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") { put = req.clone(); return json({}); }
      return json({ roles: declared });
    });

    const { getByLabelText, getByRole } = mount();
    fireEvent.click(getByLabelText("Edit table-mic"));
    fireEvent.input(getByLabelText("Quorum for table-mic"), { target: { value: "1.5" } });
    fireEvent.click(getByLabelText("Save table-mic"));

    expect(getByRole("alert").textContent).toContain("whole number");
    expect(put).toBeUndefined();
  });

  it("withdraws a role after confirmation, calling DELETE", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    let del: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "DELETE") { del = req.clone(); return new Response(null, { status: 204 }); }
      return json({ roles: declared });
    });

    const { getByLabelText } = mount();
    fireEvent.click(getByLabelText("Withdraw table-mic"));

    await waitFor(() => expect(del).toBeTruthy());
    // The confirm says what withdrawing costs: conforming systems lose the role.
    expect(confirmSpy.mock.calls[0][0]).toContain("conforming system");
    expect(del!.url).toContain("/standards/meeting-room/roles/table-mic");
  });

  it("renders an official standard's roles read-only", () => {
    const { getByText, queryByLabelText } = mount({ official: true });
    expect(getByText("table-mic")).toBeTruthy(); // the list still renders
    expect(getByText("official roles, read-only")).toBeTruthy();
    expect(queryByLabelText("Role name")).toBeNull();
    expect(queryByLabelText("Edit table-mic")).toBeNull();
    expect(queryByLabelText("Withdraw table-mic")).toBeNull();
  });

  it("hides the write controls from a principal without standard:update", () => {
    const { getByText, queryByLabelText } = mount({ me: reader });
    expect(getByText("table-mic")).toBeTruthy();
    expect(queryByLabelText("Role name")).toBeNull();
    expect(queryByLabelText("Edit table-mic")).toBeNull();
    expect(queryByLabelText("Withdraw table-mic")).toBeNull();
  });

  it("shows the empty state when a standard declares no roles", () => {
    const { getByText } = mount({ rows: [] });
    expect(getByText("This standard declares no roles.")).toBeTruthy();
  });

  // The blade model (#621): outside blade edit mode the panel is facts alone,
  // whatever the caller may do; the pencil is the one route to the controls.
  it("renders the roles as facts alone while the blade is in read mode", () => {
    const { getByText, queryByLabelText } = mount({ editing: false });
    expect(getByText("table-mic")).toBeTruthy();
    expect(getByText("2 wanted")).toBeTruthy();
    expect(queryByLabelText("Role name")).toBeNull();
    expect(queryByLabelText("Edit table-mic")).toBeNull();
    expect(queryByLabelText("Withdraw table-mic")).toBeNull();
  });
});


// Symmetric with ContractEditor: leaving blade edit mode discards the open
// role drafts, so nothing resurrects on the next pencil press (#621's review).
describe("RoleEditor drafts die with the edit session", () => {
  afterEach(() => vi.restoreAllMocks());

  it("closes an open role edit when edit mode ends and stays closed on re-entry", async () => {
    const { edit, getByLabelText, queryByLabelText } = mount();
    fireEvent.click(getByLabelText("Edit main-display"));
    expect(queryByLabelText("Name for main-display")).toBeTruthy();
    edit.cancel();
    await Promise.resolve();
    expect(queryByLabelText("Name for main-display")).toBeNull();
    edit.begin();
    await Promise.resolve();
    expect(queryByLabelText("Name for main-display")).toBeNull();
  });
});
