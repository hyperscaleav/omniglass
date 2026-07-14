import { describe, it, expect } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
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
  { kind: "system", id: "kiosk", display_name: "Kiosk", official: false },
  { kind: "component", id: "display", display_name: "Display", official: true },
  { kind: "secret", id: "oauth2-client", display_name: "OAuth2 Client", official: false, fields: [] },
];

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
});
