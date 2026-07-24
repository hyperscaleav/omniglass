import { describe, it, expect } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Roles from "./Roles";
import { ROLES_KEY, type Role } from "../lib/principals";

// The Roles page is a config over the shared FlatList (the same list shell and blade
// detail as Users and Groups): a directory row per role, ordered by tier, and a
// read-only blade whose permission grid shows the role's NET capabilities, held
// alongside missing, against the universe of permissions the API enforces. Data is
// seeded into the query cache so no server is needed.
// platform:<action> is the install-wide half of a cascade write (the least-specific
// tier), registered in the universe alongside the resource permissions, so the blade
// must render it like any other capability.
const UNIVERSE = ["audit:read:admin", "component:create", "component:delete", "component:read", "platform:update", "system:read"];
const seed: Role[] = [
  { id: "owner", name: "owner", official: true, display_name: "Owner", description: "Full control, break-glass.", permissions: [">"], inherits: [], effective_permissions: [">"], permission_universe: UNIVERSE, held: UNIVERSE },
  { id: "admin", name: "admin", official: true, display_name: "Administrator", description: "Manage the estate.", permissions: ["audit:read:admin"], inherits: ["operator"], effective_permissions: ["*:read", "principal:*", "audit:read:admin"], permission_universe: UNIVERSE, held: UNIVERSE },
  { id: "operator", name: "operator", official: true, display_name: "Operator", description: "Day-to-day ops.", permissions: ["component:create"], inherits: ["viewer"], effective_permissions: ["*:read", "component:create"], permission_universe: UNIVERSE, held: ["component:create", "component:read", "system:read"] },
  { id: "viewer", name: "viewer", official: true, display_name: "Viewer", description: "Read only.", permissions: ["*:read"], inherits: [], effective_permissions: ["*:read"], permission_universe: UNIVERSE, held: ["component:read", "system:read"] },
];

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ROLES_KEY], seed);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Roles />
    </QueryClientProvider>
  ));
}

describe("Roles page", () => {
  it("renders a directory row per role, ordered by tier (viewer before owner)", () => {
    mount();
    expect(screen.getByText("Owner")).toBeTruthy();
    expect(screen.getByText("Viewer")).toBeTruthy();
    expect(screen.getByText("Administrator")).toBeTruthy();
    const body = document.body.textContent ?? "";
    expect(body.indexOf("Viewer")).toBeLessThan(body.indexOf("Owner"));
  });

  it("opens a read-only blade defaulting to the held permissions, one per line", async () => {
    mount();
    fireEvent.click(screen.getByText("Viewer"));
    expect(await screen.findByText("Read only.")).toBeTruthy();
    // Default (Held) mode lists the permissions the role holds, each as a full string.
    expect(screen.getByText("component:read")).toBeTruthy();
    expect(screen.getByText("system:read")).toBeTruthy();
    // A permission the role does NOT hold is not shown in Held mode.
    expect(screen.queryByText("audit:read:admin")).toBeNull();
  });

  it("reveals only the missing permissions when the Missing filter is chosen", async () => {
    mount();
    fireEvent.click(screen.getByText("Viewer"));
    await screen.findByText("Read only.");
    fireEvent.click(screen.getByRole("tab", { name: /missing/i }));
    // audit:read:admin is in the universe but not held by viewer, so it shows here.
    expect(await screen.findByText("audit:read:admin")).toBeTruthy();
    expect(screen.getByText("component:create")).toBeTruthy();
    // ...and a held permission is NOT listed under Missing (the filter is not leaky).
    expect(screen.queryByText("component:read")).toBeNull();
  });

  it("shows held and missing together in the All filter, visually distinguished", async () => {
    mount();
    fireEvent.click(screen.getByText("Viewer"));
    await screen.findByText("Read only.");
    fireEvent.click(screen.getByRole("tab", { name: /^all/i }));
    // Held rows carry the ✓ glyph; missing rows carry the · glyph and are struck.
    // The glyph is the only differentiator in All mode, so assert it, not just presence.
    const heldRow = (await screen.findByText("component:read")).closest("div");
    expect(heldRow?.textContent).toContain("✓");
    const missingRow = screen.getByText("audit:read:admin").closest("div");
    expect(missingRow?.textContent).toContain("·");
    expect(screen.getByText("audit:read:admin").className).toContain("line-through");
  });

  it("shows the owner holding the whole universe with nothing missing", async () => {
    mount();
    fireEvent.click(screen.getByText("Owner"));
    await screen.findByText("Full control, break-glass.");
    fireEvent.click(screen.getByRole("tab", { name: /missing/i }));
    // Owner holds every permission, so Missing is empty.
    expect(await screen.findByText(/holds every permission/i)).toBeTruthy();
  });

  it("lights the install-wide platform capability on a role that holds it", async () => {
    mount();
    fireEvent.click(screen.getByText("Administrator"));
    await screen.findByText("Manage the estate.");
    // Held mode, rendered as the raw capability string like every other one: the
    // tier permission is not special-cased or relabelled in the grid.
    expect(await screen.findByText("platform:update")).toBeTruthy();
  });

  it("shows the install-wide platform capability as missing on a role that lacks it", async () => {
    mount();
    fireEvent.click(screen.getByText("Viewer"));
    await screen.findByText("Read only.");
    expect(screen.queryByText("platform:update")).toBeNull();
    fireEvent.click(screen.getByRole("tab", { name: /missing/i }));
    expect(await screen.findByText("platform:update")).toBeTruthy();
  });

  it("shows what a role inherits in its row", () => {
    mount();
    // viewer appears as its own id badge AND in operator's Inherits cell.
    expect(screen.getAllByText("viewer").length).toBeGreaterThanOrEqual(2);
  });

  it("wraps the catalog in the shared ListShell filter bar", () => {
    mount();
    expect(screen.getByRole("combobox")).toBeTruthy();
    expect(screen.getByPlaceholderText("filter roles by name or permission")).toBeTruthy();
  });
});
