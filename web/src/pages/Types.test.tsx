import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Types from "./Types";
import { TYPES_KEY, type TypeRow } from "../lib/types";
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

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...TYPES_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Types />
    </QueryClientProvider>
  ));
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
});
