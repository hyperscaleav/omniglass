import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Properties from "./Properties";
import { PROPERTIES_KEY, type PropertyRow } from "../lib/properties";
import { ME_KEY, type Me } from "../lib/auth";

// The Properties page is a single FlatList over the /property-types catalog: the
// categorical lane of the signal registry (its numeric twin is the Metrics page).
// Official (seed-owned) properties are read-only; a custom property is writable
// only when the caller holds property_type:create / property_type:update, the
// resource the API stamps. Data is seeded into the query cache so no server is
// needed.
const seed: PropertyRow[] = [
  { name: "serial-number", data_type: "string", display_name: "Serial number", official: true },
  { name: "interface-reachable", data_type: "string", display_name: "Interface Reachable", official: true },
  { name: "rack-unit", data_type: "string", display_name: "Rack unit", official: false },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };
// A role-scoped principal holding exactly what the API stamps (property_type),
// no wildcard. This is the caller the "property" gate string broke for (#592):
// the server honored property_type:create while the console, checking
// "property", hid the affordances.
const cataloger: Me = {
  principal: { id: "u-cat", kind: "human" },
  human: { username: "cataloger" },
  permissions: ["property_type:create", "property_type:update", "property_type:delete"],
  grants: [],
};

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...PROPERTIES_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Properties />
    </QueryClientProvider>
  ));
}

describe("Properties page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists the seeded properties", () => {
    mount();
    expect(screen.getByText("serial-number")).toBeTruthy();
    expect(screen.getByText("interface-reachable")).toBeTruthy();
    expect(screen.getByText("rack-unit")).toBeTruthy();
  });

  it("renders the display name and the name in one identity cell, not two columns", () => {
    mount();
    // A dotted keyspace name is validated differently from a kebab one, not a
    // different concept, so the header is the one word every list uses; what changes
    // is that the display name now sits above the name in the same cell.
    const cell = screen.getByText("serial-number").closest("td");
    expect(cell?.textContent).toContain("Serial number");
    expect(screen.getByRole("columnheader", { name: "Name" })).toBeTruthy();
    expect(screen.queryByRole("columnheader", { name: "Label" })).toBeNull();
  });

  it("shows New property for a caller holding property:create", () => {
    mount(admin);
    expect(screen.getByText("New property")).toBeTruthy();
  });

  it("hides New property from a read-only viewer", () => {
    mount(viewer);
    expect(screen.queryByText("New property")).toBeNull();
  });

  // The authz-string regressions (#592): the API stamps property_type, so a
  // role-scoped principal holding exactly that must see the same affordances a
  // wildcard admin does. These failed while the page gated on "property".
  it("shows New property under the exact resource the API stamps (property_type)", () => {
    mount(cataloger);
    expect(screen.getByText("New property")).toBeTruthy();
  });

  it("lets a role-scoped property_type principal edit a custom row", async () => {
    mount(cataloger);
    fireEvent.click(screen.getByText("Rack unit"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).getByLabelText("Edit")).toBeTruthy();
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByRole("button", { name: /delete/i })).toBeTruthy();
  });
});
