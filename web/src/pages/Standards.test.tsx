import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Standards from "./Standards";
import { STANDARDS_KEY, type Standard } from "../lib/standards";
import { classifierPropertiesKey, type ClassifierProperty } from "../lib/classifier_properties";
import { classifierMetricsKey } from "../lib/classifier_metrics";
import { standardRolesKey, type DeclaredRole } from "../lib/system_roles";
import { PROPERTIES_KEY, type PropertyRow } from "../lib/properties";
import { METRICS_KEY } from "../lib/metric_types";
import { COMPONENT_TYPES_KEY, type ComponentType } from "../lib/component_types";
import { PRODUCTS_KEY, type Product } from "../lib/products";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// Standards is the catalog of blueprints a system conforms to, on the flat
// FlatList surface beside Products. An official row is read-only (no
// pencil, no Delete, and a read-only contract); a custom row carries Edit, Delete,
// and a writable declared-property contract on its detail blade. Data is seeded
// into the query cache so no server is needed.
const seed: Standard[] = [
  { id: uuidFor("std-meeting-room"), name: "meeting-room", display_name: "Meeting room", official: true },
  { id: uuidFor("std-huddle-space"), name: "huddle-space", display_name: "Huddle space", official: false, parent_standard: "meeting-room", parent_standard_id: uuidFor("std-meeting-room") },
];

const contract: ClassifierProperty[] = [{ property_type_name: "seat_count", property_type_id: "seat_count-id", default_value: 8, required: true }];
const catalog: PropertyRow[] = [
  { name: "seat_count", data_type: "int", display_name: "Seat count", official: true },
  { name: "has_camera", data_type: "bool", display_name: "Has camera", official: true },
];

// The roles the custom standard declares (RoleEditor keys the query on the
// standard's uuid, which is what the page passes it), plus the component_type
// and product registries its typed-slot pickers read (#626).
const roles: DeclaredRole[] = [{ name: "table-mic", display_name: "Table microphone", quorum: 2, accepted_types: ["video-bar"], pinned_products: [], impact: "degraded" }];
const componentTypes: ComponentType[] = [{ id: uuidFor("ct-video-bar"), name: "video-bar", display_name: "Video Bar", official: true, default_tags: [] }];
const products: Product[] = [{ id: uuidFor("prod-cisco-room-bar"), name: "cisco-room-bar", display_name: "Cisco Room Bar", kind: "device", component_type: "video-bar", component_type_id: uuidFor("ct-video-bar"), official: true }];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...STANDARDS_KEY], seed);
  qc.setQueryData([...PROPERTIES_KEY], catalog);
  qc.setQueryData([...classifierPropertiesKey("standard", "huddle-space")], contract);
  qc.setQueryData([...classifierPropertiesKey("standard", "meeting-room")], []);
  // The metric lane mounts beside the property one; empty contracts and an
  // empty catalog keep it inert without a server.
  qc.setQueryData([...METRICS_KEY], []);
  qc.setQueryData([...classifierMetricsKey("standard", "huddle-space")], []);
  qc.setQueryData([...classifierMetricsKey("standard", "meeting-room")], []);
  // The role panel mounts beside the contract lanes, keyed on the uuid.
  qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
  qc.setQueryData([...PRODUCTS_KEY], products);
  qc.setQueryData([...standardRolesKey(uuidFor("std-huddle-space"))], roles);
  qc.setQueryData([...standardRolesKey(uuidFor("std-meeting-room"))], []);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Standards />
    </QueryClientProvider>
  ));
}

const openBlade = async (rowText: string) => {
  fireEvent.click(screen.getByText(rowText));
  return await waitFor(() => {
    const el = asides()[0];
    if (!el) throw new Error("no blade yet");
    return el as HTMLElement;
  });
};

describe("Standards page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists the catalog with each row's origin and variant parent", async () => {
    mount();
    expect(await screen.findByText("Meeting room")).toBeInTheDocument();
    expect(screen.getByText("Huddle space")).toBeInTheDocument();
    expect(screen.getByText("Variant of")).toBeInTheDocument();
  });

  it("an official row is read-only: greyed pencil and delete, and a read-only contract", async () => {
    mount();
    const blade = await openBlade("Meeting room");
    const editBtn = within(blade).getByLabelText("Edit") as HTMLButtonElement;
    const deleteBtn = within(blade).getByRole("button", { name: /delete/i }) as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(deleteBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Official: ships with Omniglass and updates with it.");
    // Both contract lanes render read-only: neither panel offers a declare picker.
    expect(within(blade).getAllByText("official, read-only")).toHaveLength(2);
    expect(within(blade).queryByLabelText("Property to declare")).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Metric to declare")).not.toBeInTheDocument();
  });

  it("a custom row carries edit, delete, and the variant-parent picker", async () => {
    mount();
    const blade = await openBlade("Huddle space");
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByRole("button", { name: /delete/i })).toBeInTheDocument();
    // The picker offers the other standards, never the row itself (no self-variant).
    const picker = within(blade).getByLabelText("Variant of") as HTMLSelectElement;
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "meeting-room"]);
    expect(picker.value).toBe("meeting-room"); // seeded from the row
  });

  it("carries the declared-property contract on a custom standard's detail", async () => {
    mount();
    const blade = await openBlade("Huddle space");
    expect(within(blade).getByText("Declared properties")).toBeInTheDocument();
    // Both catalog lanes mount: the property contract and its metric sibling.
    expect(within(blade).getByText("Declared metrics")).toBeInTheDocument();
    expect(within(blade).getAllByText("the standard contract")).toHaveLength(2);
    expect(within(blade).getByText("seat_count")).toBeInTheDocument();
    expect(within(blade).getByText("8")).toBeInTheDocument(); // the declared default
    expect(within(blade).getByText("required")).toBeInTheDocument();
    // Writable once the pencil flips edit mode (#621): the picker offers what
    // the standard does not already declare.
    fireEvent.click(within(blade).getByLabelText("Edit"));
    const picker = within(blade).getByLabelText("Property to declare") as HTMLSelectElement;
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "has_camera"]);
  });

  // The blade model (#621): a blade opens read-only, and EVERY mutating control,
  // the contract panels' declare/edit/withdraw and the role panel's declare row
  // included, appears only after the pencil flips the blade into edit mode.
  it("keeps the contract and role controls out of read mode until the pencil flips edit", async () => {
    mount();
    const blade = await openBlade("Huddle space");
    // Read mode: the declared line and role render as fact rows...
    expect(within(blade).getByText("seat_count")).toBeInTheDocument();
    expect(within(blade).getByText("8")).toBeInTheDocument();
    expect(within(blade).getByText("table-mic")).toBeInTheDocument();
    expect(within(blade).getByText("2 wanted")).toBeInTheDocument();
    // ...and nothing shaped like a mutating control renders, even for an admin.
    expect(within(blade).queryByLabelText("Property to declare")).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Metric to declare")).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Edit seat_count")).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Withdraw seat_count")).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Role name")).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Edit table-mic")).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Withdraw table-mic")).not.toBeInTheDocument();
    // The pencil enters edit mode; the existing controls return as they were.
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByLabelText("Property to declare")).toBeInTheDocument();
    expect(within(blade).getByLabelText("Withdraw seat_count")).toBeInTheDocument();
    expect(within(blade).getByLabelText("Role name")).toBeInTheDocument();
    expect(within(blade).getByLabelText("Withdraw table-mic")).toBeInTheDocument();
  });

  // The permissionless arm of the same rule: a viewer's blade is locked, so
  // the pencil renders greyed (naming the missing permission) and there is
  // still no route into the controls.
  it("gives a viewer a greyed pencil and no contract or role controls on a custom row", async () => {
    mount(viewer);
    const blade = await openBlade("Huddle space");
    expect(within(blade).getByText("seat_count")).toBeInTheDocument();
    const editBtn = within(blade).getByLabelText("Edit") as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Requires standard:update");
    expect(within(blade).queryByLabelText("Property to declare")).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Role name")).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Withdraw seat_count")).not.toBeInTheDocument();
  });

  it("patches display name and variant parent on save", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH") {
        sent = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ ...seed[1], display_name: "Huddle" }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ standards: seed }), { status: 200, headers: { "Content-Type": "application/json" } });
    });

    mount();
    const blade = await openBlade("Huddle space");
    fireEvent.click(within(blade).getByLabelText("Edit"));
    fireEvent.input(within(blade).getByDisplayValue("Huddle space"), { target: { value: "Huddle" } });
    fireEvent.change(within(blade).getByLabelText("Variant of"), { target: { value: "" } });
    fireEvent.click(within(blade).getByText("Save"));

    await waitFor(() => expect(sent).toBeTruthy());
    // Clearing the picker drops the parent from the patch (a standalone standard).
    expect(sent).toEqual({ display_name: "Huddle" });
  });

  it("hides New standard from a caller without standard:create", () => {
    mount(viewer);
    expect(screen.queryByText(/New standard/i)).toBeNull();
  });

  it("offers the create form to a caller who may create", async () => {
    mount();
    fireEvent.click(screen.getByRole("button", { name: /new standard/i }));
    expect(await screen.findByText("Create standard")).toBeInTheDocument();
    expect(screen.getByPlaceholderText("meeting-room")).toBeInTheDocument();
  });

  // One Name column carries both identities (components/IdentityCell), so the
  // catalog reads the same way as every other list rather than spending a second
  // column on the fact.
  it("carries both identities in one Name column, and no retired second column", async () => {
    mount();
    await screen.findByText("Huddle space");
    const heads = Array.from(document.querySelectorAll("thead th")).map((th) => th.textContent?.trim());
    expect(heads).toEqual(["Name", "Variant of", "Origin", ""]);
    const cell = screen.getByText("Huddle space").closest("td")!;
    expect(within(cell).getByText("huddle-space")).toBeInTheDocument();
  });

  // The handle follows the display name until the operator claims it, so a
  // standard gets a valid kebab address without anyone thinking about the
  // character class (lib/entities).
  it("derives the handle from the display name until the operator edits it", async () => {
    mount();
    fireEvent.click(screen.getByRole("button", { name: /new standard/i }));
    const display = (await screen.findByPlaceholderText("Meeting room")) as HTMLInputElement;
    const handle = screen.getByPlaceholderText("meeting-room") as HTMLInputElement;

    fireEvent.input(display, { target: { value: "Huddle Space" } });
    expect(handle.value).toBe("huddle-space");

    fireEvent.input(handle, { target: { value: "huddle" } });
    fireEvent.input(display, { target: { value: "Huddle Space Two" } });
    expect(handle.value).toBe("huddle");
  });
});

// The catalog addresses rows by the name (ADR-0062): the first column
// shows it, and the substring filter matches it. With uuid-shaped fixture ids
// these fail when the page feeds the uuid anywhere an operator reads or types.
describe("Standards addressing honesty (#469)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the handle in the Name column and finds the row by it in the filter", async () => {
    mount();
    expect(await screen.findByText("Huddle space")).toBeInTheDocument();
    const row = screen.getByText("Huddle space").closest("tr")!;
    expect(within(row).getByText("huddle-space")).toBeInTheDocument();
    const input = screen.getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "huddle-space" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByText("Huddle space")).toBeInTheDocument();
    expect(screen.queryByText("Meeting room")).toBeNull();
  });
});
