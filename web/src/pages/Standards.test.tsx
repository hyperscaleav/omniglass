import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Standards from "./Standards";
import { STANDARDS_KEY, type Standard } from "../lib/standards";
import { classifierPropertiesKey, type ClassifierProperty } from "../lib/classifier_properties";
import { PROPERTIES_KEY, type PropertyRow } from "../lib/properties";
import { ME_KEY, type Me } from "../lib/auth";

// Standards is the catalog of blueprints a system conforms to, on the flat
// FlatList surface beside Products. An official (seed-owned) row is read-only (no
// pencil, no Delete, and a read-only contract); a custom row carries Edit, Delete,
// and a writable declared-property contract on its detail blade. Data is seeded
// into the query cache so no server is needed.
const seed: Standard[] = [
  { id: "u-meeting-room", name: "meeting-room", display_name: "Meeting room", official: true },
  { id: "u-huddle-space", name: "huddle-space", display_name: "Huddle space", official: false, parent_standard: "meeting-room", parent_standard_id: "u-meeting-room" },
];

const contract: ClassifierProperty[] = [{ property_name: "seat_count", property_id: "seat_count-id", default_value: 8, required: true }];
const catalog: PropertyRow[] = [
  { name: "seat_count", data_type: "int", display_name: "Seat count", official: true },
  { name: "has_camera", data_type: "bool", display_name: "Has camera", official: true },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...STANDARDS_KEY], seed);
  qc.setQueryData([...PROPERTIES_KEY], catalog);
  qc.setQueryData([...classifierPropertiesKey("standard", "huddle-space")], contract);
  qc.setQueryData([...classifierPropertiesKey("standard", "meeting-room")], []);
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

  it("an official row is read-only: no pencil, no delete, and a read-only contract", async () => {
    mount();
    const blade = await openBlade("Meeting room");
    expect(within(blade).queryByLabelText("Edit")).not.toBeInTheDocument();
    expect(within(blade).queryByRole("button", { name: /delete/i })).not.toBeInTheDocument();
    expect(within(blade).getByText("seed-owned, read-only")).toBeInTheDocument();
    expect(within(blade).queryByLabelText("Property to declare")).not.toBeInTheDocument();
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
    expect(within(blade).getByText("the standard contract")).toBeInTheDocument();
    expect(within(blade).getByText("seat_count")).toBeInTheDocument();
    expect(within(blade).getByText("8")).toBeInTheDocument(); // the declared default
    expect(within(blade).getByText("required")).toBeInTheDocument();
    // Writable: the picker offers what the standard does not already declare.
    const picker = within(blade).getByLabelText("Property to declare") as HTMLSelectElement;
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "has_camera"]);
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
});
