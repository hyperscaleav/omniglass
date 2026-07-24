import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Types from "./Types";
import { TYPES_KEY, type TypeRow } from "../lib/types";
import { classifierPropertiesKey, type ClassifierProperty } from "../lib/classifier_properties";
import { PROPERTIES_KEY, type PropertyRow } from "../lib/properties";
import { ME_KEY, type Me } from "../lib/auth";

// The Types page is a segmented tab control (Location / Secret) over the shared
// FlatList, one tab per type registry. Each tab rebuilds its own FlatList (keyed
// on the active kind) over the same unified listTypes query, so switching tabs
// swaps the visible rows without a refetch. Secret is read-only (no create); a
// custom location row is writable only when the caller holds type:create, and its
// detail carries the location type's declared-property contract. A system's shape
// is the standard it conforms to, which has its own page. Data is seeded into the
// query cache so no server is needed.
const seed: TypeRow[] = [
  { kind: "location", id: "campus", display_name: "Campus", official: true, icon: "map-pin" },
  { kind: "location", id: "wing", display_name: "Wing", official: false, icon: "map-pin", allowed_parent_types: ["campus", "root"] },
  { kind: "secret", id: "oauth2-client", display_name: "OAuth2 Client", official: false, fields: [] },
];

// The location type contract shown on the wing blade, plus the catalog the editor
// joins each line to for its display name and data type.
const wingContract: ClassifierProperty[] = [{ property_type_name: "floor_area_sqm", property_type_id: "floor_area_sqm-id", default_value: 40, required: false }];
const catalog: PropertyRow[] = [
  { name: "floor_area_sqm", data_type: "int", display_name: "Floor area", official: true },
  { name: "seat_count", data_type: "int", display_name: "Seat count", official: true },
];

const asides = () => document.querySelectorAll("aside[data-blade]");

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...TYPES_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...PROPERTIES_KEY], catalog);
  qc.setQueryData([...classifierPropertiesKey("location-type", "wing")], wingContract);
  qc.setQueryData([...classifierPropertiesKey("location-type", "campus")], []);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Types />
    </QueryClientProvider>
  ));
}

describe("Types page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders one tab per type registry, and none for the promoted standard", () => {
    mount();
    expect(screen.getByRole("tab", { name: "Location" })).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Secret" })).toBeTruthy();
    // system_type was promoted to the standard, which has its own catalog page.
    expect(screen.queryByRole("tab", { name: "System" })).toBeNull();
  });

  it("defaults to the Location tab: a location row shows, a secret-only row does not", () => {
    mount();
    expect(screen.getByText("campus")).toBeTruthy();
    expect(screen.queryByText("oauth2-client")).toBeNull();
  });

  it("switches rows on tab click: Secret shows its row and hides campus", async () => {
    mount();
    fireEvent.click(screen.getByRole("tab", { name: "Secret" }));
    expect(await screen.findByText("oauth2-client")).toBeTruthy();
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

  it("offers no create form on the read-only Secret tab", async () => {
    mount();
    fireEvent.click(screen.getByRole("tab", { name: "Secret" }));
    await screen.findByText("oauth2-client");
    expect(screen.queryByText("New type")).toBeNull();
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
      if (method === "PATCH" && url.includes("/location-types/wing")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(
          JSON.stringify({ id: "wing", display_name: "Wing", official: false, icon: "map-pin", allowed_parent_types: ["campus", "root"] }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      // The post-save invalidation refetches the unified listTypes query; any
      // shape satisfies the parser (each registry reads only its own key).
      return new Response(
        JSON.stringify({ location_types: [], system_types: [], secret_types: [] }),
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

  it("edit blade on a secret kind does not show the allowed-parents editor", async () => {
    mount();
    fireEvent.click(screen.getByRole("tab", { name: "Secret" }));
    fireEvent.click(await screen.findByText("oauth2-client"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // A secret type is read-only: no pencil, and no location-only editor.
    expect(within(blade).queryByLabelText("Edit")).toBeNull();
    expect(within(blade).queryByText("Allowed parents")).toBeNull();
  });

  // The location type is a classifier: its blade carries the declared-property
  // contract every location of the type resolves against, on the shared
  // ContractEditor (the same panel a product and a standard use).
  it("shows the location type's declared-property contract on its blade", async () => {
    mount();
    fireEvent.click(screen.getByText("wing"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).getByText("Declared properties")).toBeTruthy();
    expect(within(blade).getByText("the location type contract")).toBeTruthy();
    expect(within(blade).getByText(/A location of this type inherits every property/)).toBeTruthy();
    expect(within(blade).getByText("floor_area_sqm")).toBeTruthy();
    expect(within(blade).getByText("40")).toBeTruthy(); // the declared default
    // Writable for this caller (owner), so the picker offers what is not declared.
    const picker = within(blade).getByLabelText("Property to declare") as HTMLSelectElement;
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "seat_count"]);
  });

  it("renders an official location type's contract read-only", async () => {
    mount();
    fireEvent.click(screen.getByText("campus"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).getByText("seed-owned, read-only")).toBeTruthy();
    expect(within(blade).getByText("This location type declares no properties.")).toBeTruthy();
    expect(within(blade).queryByLabelText("Property to declare")).toBeNull();
  });

  it("declares a property on a location type, PUTting to the location-types contract route", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        put = req.clone();
        return new Response(JSON.stringify({ property_type_name: "seat_count", property_type_id: "seat_count-id", default_value: 12, required: false }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response(JSON.stringify({ properties: wingContract }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    mount();
    fireEvent.click(screen.getByText("wing"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.change(within(blade).getByLabelText("Property to declare"), { target: { value: "seat_count" } });
    fireEvent.input(within(blade).getByLabelText("Default for the new property"), { target: { value: "12" } });
    fireEvent.click(within(blade).getByLabelText("Declare property"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/location-types/wing/properties/seat_count");
    expect(await put!.json()).toEqual({ required: false, default_value: 12 }); // coerced to the int data_type
  });
});
