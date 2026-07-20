import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Types from "./Types";
import { TYPES_KEY, type TypeRow } from "../lib/types";
import { FIELD_DEFINITIONS_KEY, type FieldDefinition } from "../lib/fields";
import { KEYS_KEY, type KeyRow } from "../lib/keys";
import { ME_KEY, type Me } from "../lib/auth";

// The Types page is a segmented tab control (Location / System / Component /
// Secret) over the shared FlatList, one tab per type registry. Each tab rebuilds
// its own FlatList (keyed on the active kind) over the same unified listTypes
// query, so switching tabs swaps the visible rows without a refetch. Secret is
// read-only (no create); a custom location/system/component row is writable
// only when the caller holds type:create. Data is seeded into the query cache
// so no server is needed.
const seed: TypeRow[] = [
  { kind: "location", id: "campus", display_name: "Campus", official: true, icon: "map-pin" },
  { kind: "location", id: "wing", display_name: "Wing", official: false, icon: "map-pin", allowed_parent_types: ["campus", "root"] },
  { kind: "system", id: "kiosk", display_name: "Kiosk", official: false },
  { kind: "component", id: "display", display_name: "Display", official: true },
  { kind: "secret", id: "oauth2-client", display_name: "OAuth2 Client", official: false, fields: [] },
];

const asides = () => document.querySelectorAll("aside[data-blade]");

// The field-definition editor on the component blade reads the field-definition
// catalog and the key catalog. Seed a required string field and an int field with a
// default on the "display" component type.
const fieldDefs: FieldDefinition[] = [
  { id: "fd-serial", component_type: "display", name: "serial_number", key: "serial_number", display_name: "Serial number", data_type: "string", required: true },
  { id: "fd-diag", component_type: "display", name: "diagonal_inches", key: "diagonal_inches", display_name: "Diagonal inches", data_type: "int", default_value: 55, required: false },
];
const keyCatalog: KeyRow[] = [
  { name: "serial_number", data_type: "string", display_name: "Serial number", official: true },
  { name: "diagonal_inches", data_type: "int", display_name: "Diagonal inches", official: false },
  { name: "asset_tag", data_type: "string", display_name: "Asset tag", official: false },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...TYPES_KEY], seed);
  qc.setQueryData([...FIELD_DEFINITIONS_KEY], fieldDefs);
  qc.setQueryData([...KEYS_KEY], keyCatalog);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Types />
    </QueryClientProvider>
  ));
}

// Open the "display" component type's blade (the field-definition editor lives there).
async function openDisplayBlade(): Promise<HTMLElement> {
  fireEvent.click(screen.getByRole("tab", { name: "Component" }));
  fireEvent.click(await screen.findByText("display"));
  return waitFor(() => {
    const el = asides()[0];
    if (!el) throw new Error("no blade yet");
    return el as HTMLElement;
  });
}

describe("Types page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders one tab per type registry", () => {
    mount();
    expect(screen.getByRole("tab", { name: "Location" })).toBeTruthy();
    expect(screen.getByRole("tab", { name: "System" })).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Component" })).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Secret" })).toBeTruthy();
  });

  it("defaults to the Location tab: a location row shows, a component-only row does not", () => {
    mount();
    expect(screen.getByText("campus")).toBeTruthy();
    expect(screen.queryByText("display")).toBeNull();
  });

  it("switches rows on tab click: Component shows display and hides campus", async () => {
    mount();
    fireEvent.click(screen.getByRole("tab", { name: "Component" }));
    expect(await screen.findByText("display")).toBeTruthy();
    expect(screen.queryByText("campus")).toBeNull();
  });

  it("offers no New type control on the read-only Secret tab", async () => {
    mount();
    fireEvent.click(screen.getByRole("tab", { name: "Secret" }));
    expect(await screen.findByText("oauth2-client")).toBeTruthy();
    expect(screen.queryByText("New type")).toBeNull();
  });

  it("shows New type on a writable tab for a caller holding type:create", () => {
    mount(admin);
    expect(screen.getByText("New type")).toBeTruthy();
  });

  it("hides New type on a writable tab for a caller without type:create", () => {
    mount(viewer);
    expect(screen.queryByText("New type")).toBeNull();
  });

  it("shows the allowed-parents editor on the location create form, with a Root option", async () => {
    mount();
    fireEvent.click(screen.getByText("New type"));
    expect(await screen.findByText("Allowed parents")).toBeTruthy();
    expect(screen.getByText("Root (no parent)")).toBeTruthy();
  });

  it("does not show the allowed-parents editor on a non-location create form", async () => {
    mount();
    fireEvent.click(screen.getByRole("tab", { name: "System" }));
    fireEvent.click(await screen.findByText("New type"));
    expect(screen.queryByText("Allowed parents")).toBeNull();
  });

  // Regression for the nested-<label> bug: the picker used to live inside the
  // shared Field component, whose root is a for-less <label>. A click anywhere
  // in that label (the heading, the hint) forwards to the first labelable
  // descendant, which was the Root checkbox. This must fail against that markup
  // (see Root's checked state flip after the heading/hint click) and pass once
  // the heading and hint render as plain, non-label text outside any wrapping
  // <label>.
  it("clicking the Allowed parents heading or hint does not check Root", async () => {
    mount();
    fireEvent.click(screen.getByText("New type"));
    await screen.findByText("Allowed parents");
    const root = screen.getByLabelText("Root (no parent)") as HTMLInputElement;
    expect(root.checked).toBe(false);
    fireEvent.click(screen.getByText("Allowed parents"));
    expect(root.checked).toBe(false);
    fireEvent.click(screen.getByText(/Leave every box unchecked/));
    expect(root.checked).toBe(false);
  });

  it("edit blade on a location type pre-checks the existing allowed_parent_types and saving sends that set", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const method = req.method;
      const url = req.url;
      if (method === "PATCH" && url.includes("/types/location/wing")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(
          JSON.stringify({ id: "wing", display_name: "Wing", official: false, icon: "map-pin", allowed_parent_types: ["campus", "root"] }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      // The post-save invalidation refetches the unified listTypes query; any
      // shape satisfies the parser (each registry reads only its own key).
      return new Response(
        JSON.stringify({ location_types: [], system_types: [], component_types: [], secret_types: [] }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    });

    mount();
    fireEvent.click(screen.getByText("wing"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));

    // Round-trip: the existing set (campus, root) arrives pre-checked.
    const root = within(blade).getByLabelText("Root (no parent)") as HTMLInputElement;
    const campus = within(blade).getByLabelText(/Campus/) as HTMLInputElement;
    expect(root.checked).toBe(true);
    expect(campus.checked).toBe(true);

    fireEvent.click(within(blade).getByText("Save"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).toHaveProperty("allowed_parent_types");
    expect(sent).toMatchObject({ allowed_parent_types: expect.arrayContaining(["campus", "root"]) });
    expect((sent as { allowed_parent_types: string[] }).allowed_parent_types).toHaveLength(2);
  });

  it("read-only blade shows allowed parents by display name, with the root sentinel labeled", async () => {
    mount();
    fireEvent.click(screen.getByText("wing"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // Read-only (no Edit): wing's set is [campus, root]. The chip resolves the
    // type id to its display name (Campus, not the raw "campus"), and the sentinel
    // renders as "Root", so the two read consistently.
    expect(within(blade).getByText("Campus")).toBeTruthy();
    expect(within(blade).getByText("Root")).toBeTruthy();
    expect(within(blade).queryByText("campus")).toBeNull();
  });

  it("edit blade on a non-location kind does not show the allowed-parents editor", async () => {
    mount();
    fireEvent.click(screen.getByRole("tab", { name: "System" }));
    fireEvent.click(await screen.findByText("kiosk"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).queryByText("Allowed parents")).toBeNull();
  });

  // The component blade carries the field-definition editor: each declared field
  // shows its key and data_type, and (holding field:create) a per-row edit and
  // delete plus a key-picker add row. This covers the gap where fields had no
  // edit or delete.
  it("the component blade lists a declared field with its key and per-row edit/delete", async () => {
    mount();
    const blade = await openDisplayBlade();
    // The field reads clearly: its label, its mono key, and its data_type badge.
    expect(within(blade).getByText("Serial number")).toBeTruthy();
    expect(within(blade).getByText("serial_number")).toBeTruthy();
    // Per-row edit and delete controls exist (the gap this slice closes).
    expect(within(blade).getByLabelText("Edit serial_number")).toBeTruthy();
    expect(within(blade).getByLabelText("Delete serial_number")).toBeTruthy();
  });

  it("offers a key-picker add-a-field control on the component blade for field:create", async () => {
    mount(admin);
    const blade = await openDisplayBlade();
    expect(within(blade).getByText("Add a field")).toBeTruthy();
    // The KeyPicker renders a searchable combobox input.
    expect(within(blade).getByRole("combobox")).toBeTruthy();
  });

  it("hides the add control and per-row edit from a caller without field:create", async () => {
    mount(viewer);
    const blade = await openDisplayBlade();
    // The declared field still reads, but no add row and no per-row edit/delete.
    expect(within(blade).getByText("serial_number")).toBeTruthy();
    expect(within(blade).queryByText("Add a field")).toBeNull();
    expect(within(blade).queryByLabelText("Edit serial_number")).toBeNull();
  });

  it("editing a field row reveals its default input and a Save", async () => {
    mount();
    const blade = await openDisplayBlade();
    fireEvent.click(within(blade).getByLabelText("Edit diagonal_inches"));
    // The inline editor shows a Save control (the default input is type-aware).
    expect(await within(blade).findByText("Save")).toBeTruthy();
  });
});
